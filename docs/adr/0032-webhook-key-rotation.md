# ADR-0032: Webhook Key Rotation

## Status

Accepted

## Context

PR-028 introduced HMAC signing for outbound webhook calls. PR-029 through PR-031 hardened network execution, runtime limits, and replay behavior. The remaining operational gap was signing-key lifecycle: deployments need a way to rotate webhook secrets without breaking in-flight retries, older replay records, or deterministic verification.

The existing action contract used one resolved `signingSecret`, so a rotation immediately replaced the only key visible to the agent.

## Decision

Webhook inputs now support `signingSecrets`, an ordered list of resolved signing keys. The final entry is the active key and its one-based index is the signing key version. The agent signs outbound requests with the active key, sends `X-Distr-Key-Version`, and records non-secret audit outputs for `signingKeyVersion` and `keyRotationApplied`.

The legacy `signingSecret` input remains valid and maps to version `1`. Supplying both `signingSecret` and `signingSecrets` is rejected as ambiguous. Empty and duplicate rotation entries are rejected.

Signature verification uses constant-time comparison. For rotated multi-key configurations, a key version is required and only that exact version may match. Legacy single-secret verification may omit the version and still match version `1`. This preserves compatibility for old single-secret callers while preventing a rotated configuration from silently accepting an ambiguous or mismatched key.

Task lease resolution and StepRun redaction understand `signingSecrets`, so secret references are resolved only for the agent lease and raw key material remains out of stored events, logs, and outputs.

## Consequences

Webhook signing supports safe key rotation without breaking single-secret configurations.

Replay remains side-effect free and can reconstruct stored key-version audit outputs when present, while old PR-031 success events without those outputs still replay.

The action remains version `v1`; consumers that do not use `signingSecrets` keep their existing behavior aside from receiving non-secret key-version metadata on new executions.
