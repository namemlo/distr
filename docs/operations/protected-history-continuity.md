# Protected-History Continuity

Use this procedure to prove exact client-scoped history continuity across a
backup/restore or release gate. Keep adopter names and UUIDs in the adopter-owned
evidence repository.

## Export and fingerprint

Run the operator image against the fenced source database. Supply the vendor
organization plus every protected customer organization and deployment target:

```shell
distr protected-history export \
  --organization-id <organization-uuid> \
  --customer-organization-id <customer-uuid> \
  --deployment-target-id <target-uuid> \
  --output protected-history-before.json
sha256sum protected-history-before.json >protected-history-before.json.sha256
distr protected-history fingerprint --artifact protected-history-before.json
```

The export uses one read-only repeatable-read transaction. Schemas 138 through
165 use the complete schema-138 table set with whole-row payloads across release,
deployment, task, execution, log, observation, and external-execution
timestamp-audit families. The later registered projections are deliberate:

- schema 166 provides the expanded control-plane projection;
- schema 167 adds `ExecutionRuntimeEvidence`;
- schema 168 adds `DeploymentPlanResolvedRequirement`;
- schema 169 adds `BaselineAdoptionComponent`; and
- schema 170 adds contained prior `ProtectedHistoryArtifact` rows and their
  correlated `ControlPlaneAuditEvent` rows.

An export from schema 171 or any other unregistered future schema is refused so
new protected tables cannot be silently omitted. Never compare exact artifacts
from different schema versions.

## Retain a Hub-created artifact

For a release or recovery handoff, prefer a Hub-created retained artifact in
addition to any CLI sidecar. Enable the existing `operator_control_plane_v2`
flag and configure the dedicated object store:

```shell
PROTECTED_HISTORY_OBJECT_STORE_ENABLED=true
PROTECTED_HISTORY_S3_REGION=<region>
PROTECTED_HISTORY_S3_BUCKET=<bucket>
# Optional for S3-compatible providers:
PROTECTED_HISTORY_S3_ENDPOINT=https://object-store.example.com
PROTECTED_HISTORY_S3_ACCESS_KEY_ID=<access-key>
PROTECTED_HISTORY_S3_SECRET_ACCESS_KEY=<secret-key>
PROTECTED_HISTORY_S3_USE_PATH_STYLE=false
```

Omit the endpoint and static credentials to use the standard AWS endpoint and
credential chain. These settings are independent from `REGISTRY_S3_*` and
`TARGET_CONFIG_S3_*`; the Hub never falls back to either configuration.

Create the retained artifact with only exact scope, a distinct current
organization-member reviewer, and an idempotency key:

```http
POST /api/v1/protected-history-artifacts
Content-Type: application/json

{
  "customerOrganizationIds": ["<customer-uuid>"],
  "deploymentTargetIds": ["<target-uuid>"],
  "reviewerUserAccountId": "<reviewer-user-uuid>",
  "idempotencyKey": "release-2026-09-01-before-upgrade"
}
```

The Hub derives the organization and issuer from authentication, exports the
current scope, writes a checksum-addressed create-only S3 object, reads it back,
and atomically retains metadata plus `protected_history.retained` audit
evidence. It never accepts caller artifact bytes, references, or checksums.
Repeating identical material returns the original retained row after readback;
changing scope, issuer, reviewer, key binding, or stored bytes conflicts.

Use `GET /api/v1/protected-history-artifacts/{id}` for retained metadata and
`GET /api/v1/protected-history-artifacts/{id}/verification` for exact object
readback. Both are read-only. Keep the returned retention checksum, audit event
ID/sequence, audit-binding checksum, and object identity in the release
handoff. Migration 170 cannot be rolled back after any retained row exists.

## Exact comparison

Export the same exact scope after restore or deployment, then run:

```shell
distr protected-history compare \
  --baseline protected-history-before.json \
  --current protected-history-after.json \
  --require-exact
```

`UNCHANGED` is the only normal continuity result. Additions, deletions,
modifications, schema changes, and scope changes are violations.

## Separately approved sample retirement

Do not weaken the normal exact comparison for cleanup. If the existing
sample-domain retirement workflow intentionally removed reviewed sample or
tutorial records, supply all three separately retained canonical authorization
artifacts:

```shell
distr protected-history compare \
  --baseline protected-history-before.json \
  --current protected-history-after.json \
  --require-exact \
  --approved-retirement-allowlist approved-retirement-allowlist.json \
  --retirement-approval retirement-approval.json \
  --sample-membership sample-membership.json
```

The CLI rejects a partial set: all three inputs are required and may be used only
with `--require-exact`. Approved retirement also requires schema 162 or later so
the authorization records exist in the protected baseline.

The artifacts have separate responsibilities:

- `distr.protected-history-retirement-allowlist/v1` uses purpose
  `sample_domain_retirement`, lists canonical exact kind/UUID/baseline-hash
  entries, and binds the other two artifact IDs.
- `distr.protected-history-retirement-approval/v1` binds the same baseline,
  scope, and preview checksum to the exact baseline `ApprovalRequest`, one or
  more approving `ApprovalDecision` records, and the applied or verified
  `SampleRetirementJob`.
- `distr.protected-history-sample-membership/v1` binds every allowlist entry to
  exact baseline `SampleRetirementOwnershipEvidence` and applied
  `SampleRetirementItem` records. The protected record itself must be directly
  bound to the exact application, deployment target, or environment subject.

Every allowance must correspond to one actually missing record, and every item
must have matching authorization and membership proof. The three artifacts
cannot permit an addition, modification, pattern, cross-scope deletion,
indirect ownership, or unused future deletion.

Retain the baseline, current artifact, comparison JSON, all three authorization
artifacts, and all SHA-256 sidecars together. For a guarded restore, also retain
`release-restore-snapshot.json`, `restore-plan.env`, the self-checksummed
`restore-switch-journal.env`, and `restore-applied.env` when present. This
repository contract does not claim that any particular live client history has
been inspected.
