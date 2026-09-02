# ADR-0085: Deployment Plan Source-History Validation

## Status

Accepted.

## Context

Target Deployment Plan v2 builds an accumulated component changelog between
an authoritative baseline and a selected Component Release. The original
query bounded candidate releases by publication time and Product Release
membership. Those facts prove that a release was published for the same
application, but they do not prove that its source commit belongs to the
baseline-to-candidate source path. A release from a sibling branch could
therefore appear in the changelog, and a repository or source-revision mismatch
would not block plan publication.

Published Component Release v2 contracts already freeze a source repository,
source commit, component-scoped change summary, and ordered change commits.
Product Releases retain an immutable snapshot of each selected component
contract. These existing facts can support a bounded, database-local
consistency gate without contacting a source host, CI system, runtime target,
or workload database.

The `changes.commits` list is component scoped. It may omit the candidate
source commit when the final repository commits did not touch that component,
and historical adoption releases may have an empty list. It is therefore not
a representation of every Git parent edge.

## Decision

Plan resolution carries the selected Component Release contract's source
repository, source commit, and ordered component change commits as in-memory
facts. They are excluded from canonical plan JSON because the public release
pin and persisted plan schemas remain unchanged.

For a non-bootstrap baseline, draft validation loads the published baseline,
candidate, and bounded accumulated Component Releases in the same
organization, application, and component scope. Before a preview checksum can
be published, it applies these rules:

- baseline and candidate source repository and commit facts must exist;
- source commits must be lowercase 40-character Git object identifiers;
- baseline, candidate, and every selected path release must use the exact same
  trimmed repository identity;
- the `ReleaseBundle.source_revision` projection must match the immutable
  Component Release contract commit;
- a changed source commit requires a non-empty, valid, duplicate-free declared
  component commit path;
- the baseline commit may be absent from the component-scoped delta or may be
  its first boundary, but it may not appear later in the path;
- when the candidate source commit appears in the component path, it must be
  the final entry and must map to the selected candidate release;
- more than one published component release mapping to the same declared path
  commit is ambiguous and blocks publication; and
- published releases outside the declared component path are excluded from
  accumulated notes, while releases on the path are ordered by the declared
  commit order rather than publication time.

When baseline and candidate source commits are equal, validation accepts the
transition without a source-notes delta. Bootstrap plans have no prior source
boundary and retain the existing bounded changelog behavior.

Missing or incomplete proof produces the stable blocking validation issue
`source_history_unverified`. Repository disagreement, reordered or duplicate
commits, conflicting source projections, and ambiguous release mappings
produce `source_history_divergent`. These issues are returned by draft
validation through the existing `issues[]` response. Publication recomputes
validation and rejects any issue, so no immutable plan is inserted for a
blocked preview.

This gate proves consistency between immutable Distr release facts and the
declared component change path. It does not independently prove Git parent
edges. Cryptographic ancestry requires a separately authenticated source-host
or CI attestation binding at least the repository, baseline commit, candidate
commit, ancestry result, and attestation identity. Until such an attestation is
part of the release evidence contract, operators must not describe this check
as independent Git ancestry verification.

## Consequences

- Accumulated changelogs no longer include publication-time sibling releases.
- Missing or conflicting source facts stop publication with a stable,
  operator-readable blocker instead of producing a misleading plan.
- Valid linear and skipped-release changelogs remain supported, including
  component-scoped paths whose last relevant commit precedes the repository
  head.
- Existing database tables, migrations, routes, canonical plan bytes, and
  published plans are unchanged.
- No source repository, CI system, runtime target, or client database is read
  during plan validation.
- Independent cryptographic ancestry remains future work and requires an
  authenticated attestation rather than inference from release-note metadata.

## Alternatives Considered

Continuing to sort solely by publication time was rejected because publication
order is not source lineage. Requiring the candidate repository head to be the
last component change commit was rejected because component-scoped changelogs
legitimately omit commits that do not touch the component. Querying a live Git
host during draft validation was rejected because it makes planning dependent
on mutable network availability and credentials. Treating the declared commit
list as cryptographic ancestry proof was rejected because commit identifiers
alone do not encode parent edges.

## Validation

Focused tests cover equal-commit transitions, linear skipped releases,
component-scoped paths that precede the candidate head, sibling exclusion,
missing paths, repository mismatch, reordered candidates, duplicate commits,
query scope, and existing change-set bounds. Repository package tests and diff
checks run without contacting a live system or workload database.
