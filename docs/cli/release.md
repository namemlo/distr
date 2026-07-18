# Distr Release CLI

The `distr release` commands create, validate, and publish Release Bundles through the public API. They preserve
the Release Contract v1 workflow and accept the discriminated Component Release Contract v2 request shape.

## Configuration

```bash
export DISTR_SERVER_URL="https://distr.example.invalid"
export DISTR_API_TOKEN="distr-api-token"
```

Flags override environment variables:

```bash
distr release create \
  --server "https://distr.example.invalid" \
  --token "$DISTR_API_TOKEN" \
  --file release-bundle.json \
  --idempotency-key "$CI_RUN_ID"
```

The CLI accepts tokens as either a raw Distr personal access token or a complete authorization value beginning with `AccessToken ` or `Bearer `. Tokens and authorization headers are not printed by the CLI.

## Commands

Create a draft Release Bundle:

```bash
distr release create --file release-bundle.json --idempotency-key "$CI_RUN_ID"
```

The request file carries its exact contract discriminator. Use `distr.release-contract/v1` for the existing
embedded v1 contract or `distr.component-release/v2` for a target-neutral component release. The CLI does not
rewrite or infer the schema. CI may add a local assertion that fails before the request when the file contains a
different discriminator:

```bash
distr release create \
  --file component-release.json \
  --schema v2 \
  --idempotency-key "$CI_RUN_ID"
```

`--schema` accepts `v1` or `v2` and is optional. Omitting it preserves the existing create behavior and flags.

Read the request from standard input:

```bash
cat release-bundle.json | distr release create --file -
```

Validate a Release Bundle:

```bash
distr release validate "00000000-0000-0000-0000-000000000000"
```

Publish a Release Bundle after validation succeeds:

```bash
distr release publish "00000000-0000-0000-0000-000000000000"
```

Component v2 publication is fail-closed. The invoking control-plane service must supply a frozen provenance policy,
trusted roots, and the referenced signed evidence through the configured publication integration. The CLI does not
fetch trust roots or evidence, use ambient machine trust, or let release JSON override server trust policy.

Pass the complete publish request from a reviewed local file:

```bash
distr release publish \
  "00000000-0000-0000-0000-000000000000" \
  --provenance-file component-publication.json
```

The file shape is `{"provenance":{"policy":{...},"evidence":[...]}}`. Each evidence item names the exact artifact
key and platform, a provenance reference already declared by the Component Release, the selected frozen trust-root
ID, and an embedded Sigstore bundle. Omitting `--provenance-file` preserves the v1 publish request; a component v2
publish without this input fails closed.

Use JSON output for automation:

```bash
distr release create --file release-bundle.json --output json
```

Text output for a v2 create or publish includes:

- Release Bundle ID and status.
- Exact release-contract schema.
- Canonical checksum.
- Each artifact manifest/index digest and platform digest.

V1 text output remains compatible. `--output json` emits the API response for both schemas.

## Signed Provenance

A provenance evidence reference is not proof by itself. Before a component release can be published, Distr verifies
the signed in-toto/Sigstore envelope offline against caller-supplied frozen roots and policy. Verification binds the
statement to the exact artifact digest, the Component Release source repository and 40-character commit, and the
Component Release build invocation ID and builder. It also checks the allowed predicate type, canonical source
prefix, build type, and external parameters. Missing signed dependency, commit, invocation, or builder facts fail
closed.

Malformed, tampered, oversized, expired, self-signed, untrusted, or mismatched evidence is rejected. Distr stores a
bounded verification receipt containing the verified source repository/commit and builder/invocation ID as well as
the evidence digest, not the raw envelope or unbounded verifier errors. A future target-plan preflight compares
those persisted values to the exact release source/build identity through the same verification-result seam;
`distr release publish` never deploys a target.

## Safe v1-to-v2 Backfill

The hub binary includes a separate organization-scoped operator command. It is a dry-run unless `--apply` is
provided. Prepare a bounded reviewed evidence file for legacy artifact media types, including the immutable
observation digest; backfill never infers an OCI manifest/index or chart media type from the legacy component type:

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

```bash
distr backfill-release-contract-v2 \
  --organization-id 00000000-0000-0000-0000-000000000000 \
  --artifact-evidence-file reviewed-artifacts.json \
  --batch-size 100
```

Review `wouldDerive`, eligible, and blocked counts. A dry-run always reports `derived=0`. Then apply with a stable
checkpoint ID and the same reviewed evidence. Apply processes at most one bounded `--batch-size` batch per
invocation:

```bash
distr backfill-release-contract-v2 \
  --organization-id 00000000-0000-0000-0000-000000000000 \
  --checkpoint-id 11111111-1111-1111-1111-111111111111 \
  --artifact-evidence-file reviewed-artifacts.json \
  --batch-size 100 \
  --apply
```

Resume from the reported `nextCursor` by supplying both cursor fields:

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

The command never changes a v1 ID, contract byte, canonical payload, checksum, status, or historical reference. It
records additive source-to-derived lineage and blocks any row that would require guessing missing or ambiguous v2
facts. Repeating an applied checkpoint/cursor is idempotent. Backfill does not invent provenance; normal v2
verification still applies before a derived record can be treated as a verified published component release.
`--checkpoint-id` is required with `--apply` and optional during the write-free dry-run.

On the first apply, Distr stores the evidence document reference and SHA-256 of the exact file bytes against the
checkpoint. Every resume with that checkpoint must supply the byte-identical file; even an unnoticed row or
whitespace change is rejected as a conflict. Each derived or intrinsically blocked lineage row also stores the
selected artifact key, digest, media type, evidence reference, and evidence digest. A missing or duplicate reviewed
row reports `awaitingEvidence`, does not create blocking lineage, and leaves `nextCursor` at the last safe row. Add
the required review to a new immutable document, select a new checkpoint ID, and resume from that cursor.

## Exit Codes

- `0`: success.
- `2`: local usage, request construction, input, or 4xx request error.
- `3`: validation completed and the server reported the Release Bundle invalid.
- `4`: authentication or authorization failure.
- `5`: network, API, response parsing, or server failure.

## Idempotency

`distr release create` can send an `Idempotency-Key` header:

```bash
distr release create --file release-bundle.json --idempotency-key "$CI_RUN_ID"
```

The server scopes idempotency keys to the authenticated organization. Reusing the same key with the same canonical request returns the original Release Bundle. Reusing the same key with a different canonical request returns a structured `409 Conflict`.

Backfill idempotency is separate from HTTP create idempotency. Its checkpoint, stable cursor, and unique
source-to-derived lineage allow interrupted batches to resume without creating another v2 release.
