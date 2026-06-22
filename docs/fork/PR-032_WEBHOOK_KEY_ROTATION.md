# PR-032 - Webhook key rotation

PR-032 extends Docker-agent `distr.webhook` signing from a single secret to an ordered rotation list while preserving legacy `signingSecret` inputs.

## Scope

Included:

- webhook input support for `signingSecrets`, an ordered list of resolved signing secrets
- active signing with the latest key in the list
- `X-Distr-Key-Version` request metadata
- built-in `signingKeyVersion` and `keyRotationApplied` outputs for audit and replay reconstruction
- constant-time multi-key signature verification helper
- fail-closed validation for empty, duplicate, or ambiguous signing key configuration
- lease-time resolution and redaction support for rotated signing secret references

Not included:

- UI changes
- database schema changes
- remote inbound webhook receiver changes
- action version bump

## Behavior

Legacy inputs with `signingSecret` continue to sign exactly with that resolved secret and report key version `1`.

Inputs with `signingSecrets` use the last entry as the active key. For example, `["old", "new"]` signs outbound requests with `new` and sends `X-Distr-Key-Version: 2`. For multi-key configurations, verification requires a key-version header and only that exact key version may match, so missing or mismatched version metadata fails closed. Missing-version fallback is retained only for legacy single-secret verification.

Succeeded webhook events now include non-secret audit outputs for `signingKeyVersion` and `keyRotationApplied`. Replay reconstruction understands those outputs but remains backward compatible with PR-031 events that only stored `statusCode` and `attempts`.

## Verification

Focused tests cover:

- active rotated key signing and `X-Distr-Key-Version`
- audit outputs and secret redaction for rotated keys
- empty and duplicate signing key rejection
- exact version signature verification and legacy single-secret missing-version compatibility
- registry schema acceptance for `signingSecrets`
- lease-time rotated secret reference resolution
