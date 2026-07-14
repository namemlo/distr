# Release Hardening Checklist

Use this checklist before tagging or proposing upstream contribution slices.

## Boundary Review

| Boundary               | Expected control                                                                                  | Evidence surface                                                                |
| ---------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Organization isolation | Cross-organization resources are rejected.                                                        | `internal/db/*_test.go`, `internal/handlers/*_test.go`                          |
| RBAC                   | Mutations require scoped permissions or admin compatibility.                                      | `internal/middleware/permissions_test.go`, `internal/types/permissions_test.go` |
| Agent authentication   | Agent clients send bearer tokens and agent endpoints verify token scope.                          | `internal/agentclient/*_test.go`, agent handler tests                           |
| Leases and replay      | Task leases are explicit, expiring, and heartbeat-controlled.                                     | `internal/db/*lease*_test.go`, `internal/agentclient/task_leases_test.go`       |
| Secret redaction       | Secret values are redacted from events, logs, errors, metadata, and demo output.                  | `internal/stepredaction`, action adapter tests                                  |
| File-system safety     | File-render and OCI job actions reject traversal and symlink escapes.                             | `cmd/agent/docker/*_action_test.go`                                             |
| Webhooks               | Signed requests, replay protection, and network hardening remain covered.                         | `internal/actionregistry`, webhook policy tests                                 |
| Config as Code         | Unknown fields, wrong reference shapes, drive-relative paths, and plaintext secrets are rejected. | `internal/configascode/validation_test.go`                                      |
| Compatibility metadata | Legacy projections omit secrets and do not rewrite source rows.                                   | `internal/deploymentcompat`, `internal/db/deployment_compatibility_test.go`     |

## Required Scans

Run and record:

```shell
node hack/pr050-validate-release-hardening.mjs
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

## Secret Handling Rules

- Use secret references in fixtures, not secret values.
- Do not log full authorization headers, registry passwords, signing keys, or rendered secret files.
- Do not store secret values in release bundles, process snapshots, plans, leases, events, compatibility metadata,
  demo output, or documentation examples.
- Use `[REDACTED]`, `<placeholder>`, `local`, or `secret-ref:<name>` when examples need to show a sensitive field.

## Accepted PR-050 Result

PR-050 is acceptable when validation passes and any remaining scanner findings are documented as either fixed,
not reachable, or accepted with a short rationale.
