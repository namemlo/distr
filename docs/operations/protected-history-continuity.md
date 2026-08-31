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
- schema 168 adds `DeploymentPlanResolvedRequirement`; and
- schema 169 adds `BaselineAdoptionComponent`.

An export from schema 170 or any other unregistered future schema is refused so
new protected tables cannot be silently omitted. Never compare exact artifacts
from different schema versions.

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
