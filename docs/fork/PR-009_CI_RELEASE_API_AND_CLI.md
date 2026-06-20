# PR-009 CI Release API and CLI

## Scope

PR-009 adds CI-friendly Release Bundle creation and a public CLI for the existing Release Bundle workflow from the community fork roadmap.

Included:

- Optional idempotency support on `POST /api/v1/release-bundles`.
- Organization-scoped idempotency keys stored as hashes, not raw key values.
- Transactional duplicate prevention for retried or concurrent create requests.
- Stable `409 Conflict` response when a reused idempotency key carries a different canonical request.
- Generic non-secret CI source metadata on Release Bundles.
- Strict OCI digest validation for existing OCI component digest fields.
- `distr release create`, `distr release validate`, and `distr release publish` commands that call the public API.
- Neutral CI examples for Jenkins, GitHub Actions, GitLab CI, and plain curl.
- Backend, repository, handler, live PostgreSQL, CLI, and documentation tests.

Excluded:

- Lifecycle eligibility, promotion checks, or explanation APIs.
- Deployment process schemas, process snapshots, deployment plans, approvals, task execution, retention, notifications, or agent behavior.
- Registry credentials, provider-specific registry integrations, or Jenkins-specific server behavior.
- Automatic publication during create.
- API token administration or new RBAC systems.
- Angular Release UI changes unless a shared public type correction is unavoidable.

Those features remain PR-010 or later roadmap work.

## Generic User Story

As a CI system, I can create a complete draft Release Bundle through the public API using immutable artifact digests and a retry-safe idempotency key, validate the draft, and explicitly publish it only after server-side validation passes.

## Feature Flag

All PR-009 API behavior remains behind the existing `release_bundles` experimental feature flag.

When the flag is disabled, Release Bundle create, validate, and publish endpoints remain unavailable as they were before PR-009. The CLI surfaces those server responses without bypassing feature gating.

## API Contract

### Create Release Bundle

```http
POST /api/v1/release-bundles
Idempotency-Key: optional-client-generated-key
```

The request body remains compatible with the existing PR-006 create request. PR-009 adds optional non-secret `sourceMetadata` fields:

```json
{
  "sourceMetadata": {
    "repository": "https://example.invalid/org/project",
    "branch": "main",
    "tag": "v1.2.3",
    "ciProvider": "generic-ci",
    "ciRunId": "12345",
    "ciRunUrl": "https://ci.example.invalid/builds/12345"
  }
}
```

`sourceRevision` remains the source commit or revision field.

Create responses return the Release Bundle representation. When an idempotent retry returns an existing bundle, the response shape remains the same as a first create.

If the same organization reuses the same idempotency key with a different canonical request, the API returns:

```http
409 Conflict
Content-Type: application/json
```

```json
{
  "code": "idempotency_key_reused_with_different_request",
  "message": "idempotency key was already used with a different release bundle request"
}
```

Malformed UUIDs, invalid payloads, invalid references, duplicate release numbers, invalid digest values, and missing organization-scoped resources continue to return the existing appropriate 4xx responses.

### Validate Release Bundle

```http
POST /api/v1/release-bundles/{releaseBundleId}/validate
```

Validation continues to use the PR-007 behavior. PR-009 does not add promotion, deployment planning, or lifecycle eligibility checks.

### Publish Release Bundle

```http
POST /api/v1/release-bundles/{releaseBundleId}/publish
```

Publication continues to use the PR-007 behavior. Create does not publish automatically.

## Idempotency Rules

- Idempotency keys are scoped by organization.
- Raw keys are trimmed for validation and hashing, then discarded.
- Empty keys are ignored as absent.
- Key hashes are stored with a canonical request checksum and created Release Bundle ID.
- The canonical checksum is derived from the server-normalized create request, including release-defining source metadata and component digests.
- Replaying the same key and same canonical request returns the original bundle.
- Replaying the same key and different canonical request returns the structured conflict response.
- Concurrent same-key creates are serialized inside one database transaction path.
- Different organizations can reuse the same idempotency key independently.

## Digest Rules

OCI image and OCI artifact components must use immutable digests in this form:

```text
sha256:<64 hex characters>
```

Mutable tags, missing digest values, non-`sha256` algorithms, and malformed hex values are rejected.

## CLI Contract

The existing `distr` command gains:

```bash
distr release create --file release.json --idempotency-key "$KEY"
distr release create --file - --output json
distr release validate RELEASE_BUNDLE_ID
distr release publish RELEASE_BUNDLE_ID
```

Global configuration:

- `--server`, or `DISTR_SERVER_URL`;
- `--token`, or `DISTR_API_TOKEN`;
- `--output json|text`.

Flags take precedence over environment variables.

Exit codes:

- `0`: success;
- `2`: local usage, request construction, or input error;
- `3`: server validation result is invalid;
- `4`: authentication or authorization failure;
- `5`: API, network, or server failure.

The CLI never prints tokens, authorization headers, or raw request secrets.

## Database

PR-009 adds a reversible migration for:

- Release Bundle source metadata columns;
- an organization-scoped idempotency table with key hash, canonical request checksum, created Release Bundle ID, and creation timestamp;
- a unique constraint on organization and key hash.

The rollback removes only PR-009-owned schema.

## Compatibility

Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged.

Existing Release Bundle clients that omit `Idempotency-Key` keep the previous create semantics.

PR-009 does not alter PR-007 publication state transitions or bypass validation.
