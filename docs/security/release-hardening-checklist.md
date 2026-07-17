# Release Hardening Checklist

Use this checklist before tagging or proposing upstream contribution slices.

## Boundary Review

| Boundary               | Expected control                                                                                      | Evidence surface                                                                |
| ---------------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Organization isolation | Cross-organization resources are rejected.                                                            | `internal/db/*_test.go`, `internal/handlers/*_test.go`                          |
| RBAC                   | Mutations require scoped permissions or admin compatibility.                                          | `internal/middleware/permissions_test.go`, `internal/types/permissions_test.go` |
| Agent authentication   | Agent clients send bearer tokens and agent endpoints verify token scope.                              | `internal/agentclient/*_test.go`, agent handler tests                           |
| Leases and replay      | Task leases are explicit, expiring, and heartbeat-controlled.                                         | `internal/db/*lease*_test.go`, `internal/agentclient/task_leases_test.go`       |
| Secret redaction       | Secret values are redacted from events, logs, errors, metadata, and demo output.                      | `internal/stepredaction`, action adapter tests                                  |
| File-system safety     | File-render and OCI job actions reject traversal and symlink escapes.                                 | `cmd/agent/docker/*_action_test.go`                                             |
| Webhooks               | Signed requests, replay protection, and network hardening remain covered.                             | `internal/actionregistry`, webhook policy tests                                 |
| Config as Code         | Unknown fields, wrong reference shapes, drive-relative paths, and plaintext secrets are rejected.     | `internal/configascode/validation_test.go`                                      |
| Compatibility metadata | Legacy projections omit secrets and do not rewrite source rows.                                       | `internal/deploymentcompat`, `internal/db/deployment_compatibility_test.go`     |
| Timestamp provenance   | Historical wall clocks are converted only with explicit per-cell evidence; provenance is append-only. | `internal/externalexecutiontimestamp`, timestamp migration/repository tests     |

## Required Scans

Run and record:

```shell
node hack/pr050-validate-release-hardening.mjs
node hack/pr054a-validate-timestamp-expand.mjs
bash hack/validate-migrations.sh
bash hack/test-server-compose-timestamp-expand.sh
go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m
go test -p=1 ./...
go vet ./...
pnpm run lint
git diff --check
```

Run environment-specific scans where available:

```shell
govulncheck ./...
pnpm audit --prod
node hack/pr050-license-scan.mjs # installed Node packages and Go modules
trivy fs --scanners vuln,secret,license .
```

If a scanner reports a finding, record:

- affected package, file, or image;
- severity and exploitability;
- whether the finding is reachable in Community edition;
- fix version or accepted-risk rationale;
- owner and follow-up PR.

## Timestamp Evidence Safety

- Manifest and provenance reports are Distr control-plane timestamp evidence. They may contain execution/event
  identifiers, source table and column names, exact legacy `rawValue`, decisions, `sourceZone`, evaluated offsets,
  `convertedValue`, counts, checksums, `evidenceReference`, approving identity, author/reviewer identity, and release
  identity.
- Free-text author, reviewer, approving-identity, and opaque evidence-reference values must be reviewed as
  non-sensitive before sealing. They must never contain DSNs, credentials, payloads, messages, tokens, passwords,
  customer data, or private absolute paths.
- Draft, reviewed, approved, backup, restore, and fence artifacts use restrictive permissions and are retained
  outside the source repository.
- `UNRESOLVED` is not converted into UTC by deployment approval.
- Provenance rows are append-only; corrections use a new complete superseding manifest.
- Vulnerability, license, and secret scanners remain mandatory and are not suppressed or allowlisted for this
  release.
- Timestamp evidence is control-plane metadata and never authorizes access to an adopter workload database.

## Secret Handling Rules

- Use secret references in fixtures, not secret values.
- Do not log full authorization headers, registry passwords, signing keys, or rendered secret files.
- Do not store secret values in release bundles, process snapshots, plans, leases, events, compatibility metadata,
  demo output, or documentation examples.
- Use `[REDACTED]`, `<placeholder>`, `local`, or `secret-ref:<name>` when examples need to show a sensitive field.

## Accepted PR-050 Result

PR-050 is acceptable when validation passes and any remaining scanner findings are documented as either fixed,
not reachable, or accepted with a short rationale.
