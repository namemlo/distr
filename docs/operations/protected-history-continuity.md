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

The export uses one read-only repeatable-read transaction. Schema 138 through
165 contain the stable customer/target/external-execution audit projection;
schema 166 and later include the expanded v2 release, plan, task, execution,
observation, and state lineage. Never compare exact artifacts from different
schema versions.

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
tutorial records, supply its separately retained canonical allowlist:

```shell
distr protected-history compare \
  --baseline protected-history-before.json \
  --current protected-history-after.json \
  --require-exact \
  --approved-retirement-allowlist approved-retirement-allowlist.json
```

The allowlist must use schema
`distr.protected-history-retirement-allowlist/v1`, purpose
`sample_domain_retirement`, the exact baseline artifact ID and scope, the
preview checksum, an approved request UUID/checksum/state, and a canonical list
of exact kind/UUID/baseline-hash entries. Every allowance must correspond to one
actually missing record. It cannot permit an addition, modification, pattern,
cross-scope deletion, or unused future deletion.

Retain the baseline, current artifact, comparison JSON, allowlist, approval, and
all SHA-256 sidecars together. This repository contract does not claim that any
particular live client history has been inspected.
