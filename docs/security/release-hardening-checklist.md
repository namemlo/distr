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
DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery bash hack/test-server-compose-timestamp-expand.sh
go test -p=1 ./internal/externalexecutiontimestamp ./internal/migrations ./internal/db ./internal/hubexecutor ./internal/mapping ./cmd/hub/cmd -count=1 -timeout 30m
go test -p=1 ./...
go vet ./...
pnpm run lint
git diff --check
```

Run environment-specific scans where available:

```shell
GOVULNCHECK_MODULE=golang.org/x/vuln/cmd/govulncheck
go install "${GOVULNCHECK_MODULE}$(printf '\100')v1.6.0"
node hack/pr050-govulncheck.mjs
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

### Reviewed Go vulnerability exception

The Go scanner remains mandatory and fail closed. `hack/pr050-govulncheck.mjs` accepts only the exact reviewed
`github.com/docker/docker` version `v28.5.2+incompatible` initialization traces for `GO-2026-4883`, `GO-2026-4887`,
`GO-2026-5617`, `GO-2026-5668`, and `GO-2026-5746`. The machine-readable policy is
`docs/security/govulncheck-reviewed-findings.json`; EMLO Platform reviewed it on 2026-07-17, the EMLO Platform Owner
is accountable for review, and it expires at 2026-08-17T00:00:00Z. Runtime and static validation seal the complete
canonical policy with SHA-256, so any package, file, function, frame order, affected/fixed metadata, dependency
family, ID, or link change requires an explicit reviewed code update.

Any new, missing, duplicate, or changed reachable finding, trace, module/version, scanner protocol, advisory
metadata, malformed output, or expired policy fails. A separate `go list -deps` defense rejects affected Moby
daemon package families from the shipped Hub and agents. Module/package-only reports that the scanner classifies as
not called are reported as informational and are never called accepted. Successful output says
`accepted reviewed risk`; it does not claim zero vulnerabilities. Generic allowlists, suppressions, scanner
downgrades, and runtime overrides remain prohibited.

Submitted Go VulnDB feedback is recorded at
[GO-2026-4883](https://github.com/golang/vulndb/issues/4922#issuecomment-4976353536),
[GO-2026-4887](https://github.com/golang/vulndb/issues/4921#issuecomment-4976353689),
[GO-2026-5617](https://github.com/golang/vulndb/issues/5993),
[GO-2026-5668](https://github.com/golang/vulndb/issues/5994), and
[GO-2026-5746](https://github.com/golang/vulndb/issues/5995).

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
- Provenance and authorized deletion-tombstone rows are append-only. Corrections use a new complete superseding
  manifest; source deletion without the fixed transaction-local retention operation is rejected. Resolution after
  authorized retention is provenance-only and never mutates the null tombstone or recreates a live shadow. The
  tombstone must predate the promoting manifest's applied time, using one trigger-captured statement timestamp for
  its complete cell set; later deletion cannot erase a populated live shadow and be accepted as retained provenance.
- The retention GUC and tombstone triggers are application-integrity controls under trusted database-owner
  credentials, not a PostgreSQL privilege boundary. Least-privilege runtime/migrator roles and direct ledger-insert
  denial remain explicit deferred hardening.
- Vulnerability, license, and secret scanners remain mandatory. Generic suppression and allowlists remain
  prohibited; only the exact reviewed, expiring, fail-closed Go policy above may accept findings.
- Timestamp evidence is control-plane metadata and never authorizes access to an adopter workload database.

### Dirty-recovery evidence safety

- `timestamp-expand-recover-dirty` is the only supported migration-138 dirty-marker repair path. Direct
  `schema_migrations` edits, raw/manual `migrate force`, and any unaudited Force call are prohibited.
- Keep the active evidence directory deployment-user-owned with mode `0700`. Recovery plans, results, numbered
  interrupted-result archives, and every checksum sidecar must be regular non-symlink files owned by that same user
  with mode `0600`.
- Treat `timestamp-dirty-recovery-plan.json`, `timestamp-dirty-recovery-result.json`,
  `timestamp-dirty-recovery-result.interrupted-NNN.partial`, and all corresponding `.sha256` sidecars as immutable
  audit evidence. A missing sidecar may be created only through the wrapper's validated create-new repair path;
  orphan, mismatched, unsafe, or noncontiguous artifacts fail closed.
- Operator identity is a stable non-secret 1-128-character identifier. Reason is one quoted, trimmed, printable,
  single-line, non-secret 1-256-character argument. Neither value may contain credentials, connection strings, URLs,
  tokens, payloads, customer data, or local/remote paths.
- Every retry uses the same manifest mode and exact approved content/checksum, evidence directory, identity, reason,
  active fence, and target image/evidence binding. A different source pathname is acceptable only for the same
  approved bytes and staged checksum. A retained result is reusable only after checksum, content, and live-marker
  validation.
- Recovery only repairs the catalog-proven marker. It never authorizes DDL, external-execution data changes, Hub
  startup, compatibility persistence, fence clearing, or access to any workload database.
- Never fabricate a timestamp fence or capture bundle after an ordinary zero-history release fails. No-manifest
  recovery is permitted only when those complete records predate migration; otherwise restore verified evidence or
  escalate.

## Secret Handling Rules

- Use secret references in fixtures, not secret values.
- Do not log full authorization headers, registry passwords, signing keys, or rendered secret files.
- Do not store secret values in release bundles, process snapshots, plans, leases, events, compatibility metadata,
  demo output, or documentation examples.
- Use `[REDACTED]`, `<placeholder>`, `local`, or `secret-ref:<name>` when examples need to show a sensitive field.

## Accepted PR-050 Result

PR-050 is acceptable when validation passes and scanner findings are fixed, classified by raw govulncheck as not
called, or match the exact unexpired reviewed Go policy above.
