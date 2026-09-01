# ADR-0084: Native Schema Evidence Gating

## Status

Accepted.

## Context

Target Deployment Plan v2 already freezes Component Release pins, target
placement, observed baselines, and structured migration contracts. It did not
require an independently produced schema report or an executable migration
decision before publication, admission, or task creation. A contract could
therefore describe a migration path without proving that the exact target was
currently at its expected source state or that prior and target application
versions were compatible with every schema state used during the change.

Schema evidence is target-specific and expires. It cannot be owned by the
target-neutral Product Release, inferred from application SemVer, or replaced
by executor intent. Reading a client workload database from the Hub would also
cross the deployment trust boundary and couple planning to a live system.

## Decision

Schema evidence is supplied as two immutable `adapter_input` objects in the
selected Target Config Snapshot:

- `distr.schema-report/v1` with media type
  `application/vnd.distr.schema-report.v1+json`; and
- `distr.migration-evidence/v1` with media type
  `application/vnd.distr.migration-evidence.v1+json`.

The Hub reads each object through the existing immutable S3 binding with a
256-KiB document limit. It verifies the exact reference, version ID when
present, media type, size, and outer SHA-256 checksum, then uses strict JSON
decoding that rejects unknown fields and trailing values.

Each document also carries an internal lowercase `sha256:` checksum. The
checksum covers Distr's normalized, whitespace-free JSON form with the
document's `checksum` field omitted. Strings are trimmed, timestamps are UTC,
and mixed-version facts are sorted by application version, schema version,
schema checksum, then compatibility. Migration bindings retain dependency
order. Migration evidence binds the exact schema-report checksum.

Evidence scope contains the organization, deployment scope, deployment unit,
environment assignment, environment, deployment target, and Target Config
Snapshot IDs. Component identity contains the exact Component Release ID,
release checksum, component key, and application version. Every component
instance with a database boundary, and every component with a structured
migration contract, requires exactly one matching report/evidence pair.

The migration decision is one of:

- `COMPATIBLE_NO_MIGRATION_REQUIRED`: no structured migration contract applies
  and no migration binding may be present; or
- `MIGRATION_BOUND`: every applicable migration contract is bound in exact
  dependency order, including contract checksum and each source/result schema
  version and checksum.

Both decisions require an exact mixed-version matrix. The matrix is the
Cartesian product of the prior and target application versions with the
current schema state and every intermediate/result schema state. Every fact
must be present exactly once and marked compatible; missing, negative,
duplicate, or extra facts fail closed.

The schema report and migration evidence must be current at the planning
instant, the migration-evidence validity interval must be contained by the
report interval, and `expectedCurrent` must match both the report and the
authoritative plan baseline. The complete validated evidence bundle and object
bindings are frozen in the canonical Target Deployment Plan bytes.

Distr revalidates the frozen plan checksum and evidence:

- immediately before published-plan persistence;
- before authorization or admission-evidence preparation; and
- before task lookup, resource advisory locks, preflight persistence, task
  creation, or other execution mutation.

Draft validation reports stable `schema_evidence_*` issues. A stale, expired,
incomplete, or checksum-invalid frozen plan returns a conflict at admission or
execution instead of mutating evidence, locks, preflight state, or tasks.

No database migration is introduced. Parsed evidence is retained only inside
the existing canonical plan payload. This change does not query a live target,
client database, observer runtime, or executor, and it does not run a
persistence migration, runtime mutation, or cleanup operation.

## Consequences

- Operators can audit the exact report, migration decision, object bindings,
  contract chain, and compatibility matrix used to authorize a plan.
- Expiry remains enforceable after publication because admission and execution
  re-evaluate evidence against their current database decision time.
- Target Config Snapshot publication remains metadata-only; the bounded object
  body is read only while validating a target plan.
- Canonical plan payloads may grow, but the existing 4-MiB target-plan limit
  remains authoritative.
- Existing v2 plans with neither schema-evidence requirements nor structured
  migration contracts remain readable and executable. Historical plans with
  structured migration contracts but no frozen schema evidence fail closed for
  new admission or execution.

## Alternatives Considered

Reading the target database during Hub planning was rejected because it would
cross the live client boundary and make planning nondeterministic. Trusting
only Component Release migration contracts was rejected because contracts do
not prove current target state or mixed-version compatibility. Persisting new
schema-evidence tables was rejected because immutable canonical plan bytes
already provide the required retention and checksum boundary. Revalidating
only at publication was rejected because freshness and plan integrity can
change before admission or task creation.

## Validation

Focused tests cover strict decoding, internal and outer checksums, both
decision modes, exact target/component/baseline matching, expiry, ordered
contract binding, complete mixed-version coverage, bounded object reads,
canonical ordering, API projection, frozen-plan revalidation, and mutation
ordering. Repository Go tests, migration lint, formatting, and diff checks are
run without contacting a live system or client database.
