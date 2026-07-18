# PR-061 - Release Provenance Verification and Safe v1-to-v2 Backfill

## Generic User Story

As a release operator, I want component publication to accept only signed build provenance that matches the
artifact and a frozen trust policy, and I want a restartable way to derive unambiguous v2 release records from
legacy releases without changing historical evidence.

## Scope

- Verify signed in-toto/Sigstore provenance before publishing a Component Release Contract v2.
- Match the attestation subject to the exact artifact digest; bind the signed dependency repository/commit and
  invocation ID/builder to the Component Release; and evaluate the configured predicate, canonical source, build
  type, and external parameters.
- Use only caller-supplied, frozen trust roots and policy inputs. Verification is deterministic and offline.
- Persist an immutable, bounded verification receipt and evidence digest instead of the raw envelope or
  unbounded verifier diagnostics.
- Expose the same verification-result seam for target-plan preflight without adding a PR-063 planner dependency.
- Add an organization-scoped, dry-run-by-default, checkpointed v1-to-v2 release backfill.
- Preserve existing release CLI flags, add an optional local schema assertion, and make v2 schema, canonical
  checksum, and artifact digests visible to automation.

## Out of Scope

- Fetching trust roots, transparency-log state, attestations, or artifacts over the network.
- Treating an evidence reference, SBOM, signature reference, or successful historical deployment as verified
  provenance.
- Inventing missing source, build, artifact, or provenance facts during backfill.
- Rewriting or deleting a v1 release ID, contract, canonical payload, checksum, publication record, or deployment
  history.
- Publishing a Product Release, resolving capabilities, creating target plans, or mutating a target.
- Adding adopter-specific repositories, builders, registries, policies, credentials, or deployment behavior.

## Standard Publication Workflow

1. CI builds the required platforms once and records immutable `sha256` artifact digests.
2. CI emits a signed in-toto provenance envelope whose subject is the artifact being published.
3. An authorized control-plane caller supplies the envelope bytes together with a frozen provenance policy and
   trusted-root document. Supplying an evidence reference alone is insufficient.
4. Distr parses the size-bounded envelope and trusted-root document without resolving network references.
5. Sigstore verification establishes the signature chain and required signed-time evidence against the supplied
   roots.
6. Distr verifies the exact artifact subject digest, signed dependency repository and `gitCommit`, signed
   invocation ID and builder, allowed predicate type, canonical source prefix, allowed build type, and expected
   external parameters. The signed values must exactly equal Component Release `source` and `build`.
7. A successful result is reduced to bounded facts: evidence digest, policy checksum, trust-root identity,
   subject/artifact digest, predicate type, exact builder and build invocation ID, exact source repository and
   commit, build type, external-parameters checksum, signer identity, and verification time.
8. The release publication transaction stores the immutable receipt and publishes the release. Any missing,
   ambiguous, malformed, expired, untrusted, or mismatched input fails closed.

Publication never deploys the release. A later target-plan preflight can re-check the stored bounded facts against
its frozen inputs through the release-bundle preflight seam; this PR does not create or couple to the future
target-plan implementation.

## Trust and Failure Rules

Verification rejects:

- a self-signed signer or a signer outside the supplied trust roots;
- a malformed, tampered, unsupported, or oversized envelope or trust document;
- an expired certificate/root or evidence without the required signed-time proof;
- a subject digest that differs from the exact Component Release artifact digest;
- an absent or mismatched signed source repository, 40-character commit, invocation ID, or builder;
- a predicate type, builder, canonical source, or build type outside the frozen allowlists;
- external parameters that differ from the canonical expected value;
- a policy with no usable trust root or signer identity; and
- any verifier outcome that cannot be reduced to one deterministic accepted result.

The verifier does not use ambient machine trust, a default public-good instance, live TUF metadata, or a network
fallback. Operators update trust by reviewing and distributing a new policy/root version; changing trust does not
silently reinterpret an already-published receipt.

Raw envelopes and verifier error text are transient. The database stores only the bounded accepted facts needed
for audit and preflight. Rejections return stable, redacted reason codes and bounded messages.

## Safe v1-to-v2 Backfill

Prepare a reviewed, bounded artifact evidence document first. The source release ID, artifact key, digest,
observed media type, reference, and immutable evidence digest are exact. Invalid or unsupported document entries
are rejected. A missing or duplicate observation for a source reports `awaitingEvidence`, writes no lineage, and
leaves the deterministic resume cursor before that source:

```json
{
  "schema": "distr.release-backfill-artifact-evidence/v1",
  "reference": "review://backfill/choice-tp-dev/2026-07-18",
  "evidence": [
    {
      "sourceReleaseBundleId": "22222222-2222-2222-2222-222222222222",
      "artifactKey": "service",
      "artifactDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "mediaType": "application/vnd.oci.image.index.v1+json",
      "reference": "review://manifest/22222222-2222-2222-2222-222222222222",
      "evidenceDigest": "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
    }
  ]
}
```

Run a preview first:

```bash
distr backfill-release-contract-v2 \
  --organization-id 00000000-0000-0000-0000-000000000000 \
  --artifact-evidence-file reviewed-artifacts.json \
  --batch-size 100
```

The command is a dry-run unless `--apply` is present. It reports scanned, eligible, `wouldDerive`, persisted
`derived`, already-present, `awaitingEvidence`, blocked, and failed counts plus the last and next stable cursors. A
dry-run always reports `derived=0` and does not write lineage or derived releases.

Apply one reviewed batch with an operator-selected checkpoint. Each invocation mutates at most the bounded
`--batch-size` candidates:

```bash
distr backfill-release-contract-v2 \
  --organization-id 00000000-0000-0000-0000-000000000000 \
  --checkpoint-id 11111111-1111-1111-1111-111111111111 \
  --artifact-evidence-file reviewed-artifacts.json \
  --batch-size 100 \
  --apply
```

Resume after the reported `nextCursor`:

```bash
distr backfill-release-contract-v2 \
  --organization-id 00000000-0000-0000-0000-000000000000 \
  --checkpoint-id 11111111-1111-1111-1111-111111111111 \
  --cursor-created-at 2026-07-18T08:30:00.123456789Z \
  --cursor-release-bundle-id 22222222-2222-2222-2222-222222222222 \
  --artifact-evidence-file reviewed-artifacts.json \
  --batch-size 100 \
  --apply
```

Both cursor fields are required together. Reusing the checkpoint and cursor is idempotent: the source-to-derived
lineage uniqueness prevents a second v2 release. Concurrent attempts either observe the existing lineage or fail
without changing the source. `--checkpoint-id` is required with `--apply`; it is optional for a write-free
dry-run.

The first apply stores the reviewed evidence document reference and SHA-256 of its exact file bytes in an immutable
checkpoint. Every resume with that checkpoint compares both values, so a swapped or edited document is rejected.
Each lineage row stores the exact selected artifact key, digest, media type, evidence reference, and evidence
digest. If the next source has no single reviewed row, apply reports `awaitingEvidence`, does not persist lineage
for that source, and returns the last safe `nextCursor`. Review it into a new immutable document, choose a new
checkpoint ID, and resume from that cursor.

A row is blocked when its v2 identity cannot be derived without guessing after exactly one reviewed observation
has been selected, including ambiguous component membership, mutable or missing artifact identity, a reviewed
key/digest/media type incompatible with the source component, insufficient source/build identity, or incompatible
contract facts. Blocked rows retain a bounded reason code for review. Missing or duplicate reviewed observations
are not blocked-row semantics: they remain `awaitingEvidence` with no lineage and deterministic resume. The
backfill never fabricates provenance or an exact artifact media type. A derived record must satisfy the normal v2
validation and provenance gate before it can be used as a verified published component release.

## Compatibility and Rollback

- V1 route behavior and CLI flags remain unchanged.
- Exact schema dispatch continues to distinguish `distr.release/v1` metadata from the
  `distr.component-release/v2` contract discriminator.
- Backfill creates additive lineage; it never changes a v1 UUID, JSON byte, canonical byte, checksum, status, or
  historical reference.
- Disabling `operator_control_plane_v2` leaves untouched v1 reads and execution behavior available. It does not
  delete v2 rows or perform a lossy reverse conversion.
- Existing v2 records without a trusted verification receipt fail the new publication/preflight gate; they are not
  silently treated as verified.

## Required Impact Report

### Database/schema impact

PR-061 uses the additive evidence-verification and v1-to-v2 lineage/checkpoint relations allocated with the
Component Release v2 schema foundation. They are organization-scoped, append-only/idempotent, and contain bounded
facts only. Verification receipts persist the exact source repository/commit and builder/build ID; checkpoint and
lineage rows bind the reviewed document and selected evidence row. The final PR-061 diff does not allocate a new
migration number or rewrite release rows.

### Public API impact

No new public route family is introduced. Existing release-bundle create, validate, publish, list, and get routes
retain v1 compatibility. Component publication now fails closed when required signed provenance cannot be
verified. Existing responses expose the release kind/schema and immutable checksums/digests needed by the CLI.

### CLI impact

`distr release` retains its server, token, file, idempotency-key, output, and positional-ID behavior. Create adds
optional `--schema v1|v2`, which locally asserts the request discriminator without rewriting it. Text output for v2
includes the bundle ID, status, schema, canonical checksum, and artifact/platform digests; `--output json`
continues to emit the API response. Publish accepts an optional `--provenance-file` containing the complete
`{"provenance": ...}` request; omitting it preserves the v1 request and fails closed for v2. The new top-level
`backfill-release-contract-v2` command is local operator/admin tooling and is dry-run by default.

### Frontend/UI impact

None in this slice. Existing release details continue to show v1 content and v2 evidence references. A future
operator UI may consume the bounded verification status; it must not infer trust from a reference alone.

### Agent/protocol impact

None. No task lease, executor protocol, external callback, or deployment payload changes.

### Security impact

Verification is offline, explicit, size-bounded, fail-closed, and bound to an exact artifact digest and frozen
policy. Only redacted bounded facts are persisted. Trust roots and policy are never discovered implicitly from the
network or supplied by an untrusted release payload.

## Validation

Focused tests cover trusted provenance plus self-signed/untrusted, wrong subject, exact repository/commit and
invocation/builder binding, predicate, source policy, build type, external parameters, expired roots,
malformed/oversized envelopes, and tampered artifacts. Backfill tests cover truthful dry-run counts,
checkpoint/cursor parsing, exact evidence-document checksum binding, a non-live one-invocation batch/next-cursor
harness, reviewed media-type evidence, non-persisted awaiting-evidence rows, ambiguous-row blockers, source
checksum validation, and unchanged
v1 IDs/bytes/checksums. Migration checks cover unique source lineage, exact reviewed evidence linkage, append-only
receipts/checkpoints, and refusal-safe down migration. CLI tests cover v1 flag compatibility, provenance request
transport, evidence-file hashing, and v2 schema/checksum/digest output. Live PostgreSQL concurrency and
tenant-isolation verification is an integration gate rather than a focused unit-test claim.
