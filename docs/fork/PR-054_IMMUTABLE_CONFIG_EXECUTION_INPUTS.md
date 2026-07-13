# PR-054 - Immutable Config Execution Inputs

## Generic User Story

As a deployment operator, I want an external executor to receive exact immutable references for both the service
configuration and compose definition, so that it cannot silently deploy mutable configuration after preflight.

## Scope

- Continue accepting object-versioned immutable configuration references.
- Accept an S3 content-addressed URI only when its `_immutable/sha256/{digest}/...` path matches the declared
  SHA-256 checksum and it has no query, fragment, credentials, or non-normalized path.
- Freeze the compose reference and checksum when an external execution is prepared.
- Return the compose and service-config inputs together in external-execution expected state.
- Keep observed target-component state limited to runtime service state; compose input identity is execution
  evidence rather than a component observation.

## Required Impact Report

### Database/schema impact

Migration 137 adds `expected_compose_reference` and `expected_compose_checksum` to `ExternalExecution`. Empty defaults
preserve already-created execution rows; every newly prepared callback execution resolves and stores both values
from its frozen release contract.

### Public API impact

`GET /api/v1/external-executions/{externalExecutionId}` adds `composeReference` and `composeChecksum` under
`expectedState`. Existing expected-state and callback fields are unchanged.

### Frontend/UI impact

None. These fields are deployment inputs consumed by an external executor. Existing task and timeline views retain
their current structure.

### Agent/protocol impact

None. Docker and Kubernetes agent payloads are unchanged.

### Feature-flag impact

No new flag. This extends the existing release-contract and callback-mode external-execution surfaces.

### Security impact

Positive. Omitting an object version is fail-closed unless the URI embeds the declared checksum in the supported
content-addressed path. The executor no longer needs to fetch a mutable compose key. The URI carries no credentials;
authorization remains in the executor's credential store.

### Backward-compatibility impact

Existing versioned object contracts remain valid and retain their `versionId` query parameter. Existing execution
rows deserialize with empty compose fields. New callback executions require both immutable service-config and
compose objects, so an old release contract without the compose object is rejected before dispatch.

## Expected-State Example

```json
{
  "configReference": "s3://config-bucket/service.json?versionId=v42",
  "configChecksum": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "composeReference": "s3://config-bucket/_immutable/sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc/environment/docker-compose.yaml",
  "composeChecksum": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
}
```

## Validation

- Release-contract tests cover matching and mismatched content-addressed URIs.
- External-execution repository tests cover independent versioned config and content-addressed compose references.
- Mapping tests cover the additive public API fields.
- Migration 137 up/down checks and the full PostgreSQL migration chain run before deployment.
- Full Go tests, static analysis, community builds, diff checks, and credential scans run before deployment.
