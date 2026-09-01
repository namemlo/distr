# EMLO Fork Adopter-Term Release Profile

This profile is only for the public `namemlo/distr` custom fork. It records the
intentional adopter vocabulary already present in the Choice TP pilot and its
durable observer example, Jenkins/ECR publication integration, and
remittance-oriented fork evidence.
It is not an upstream or Distr Community release result.

The default command remains the strict community scan and accepts no profile
baseline:

```shell
node hack/control-plane-adopter-term-scan.mjs --base "$SCAN_BASE"
```

The named fork must opt in explicitly:

```shell
node hack/control-plane-adopter-term-scan.mjs --base "$SCAN_BASE" --profile emlo-fork
```

`SCAN_BASE` must be the reviewed fork-delta base at or before the baseline
source commit, not merely the previous incremental commit. For this initial
inventory, use `fork/main` resolving to
`50c0bec4b2ad4e8bb206e749dab39edb0e5ce469` so every reviewed identity is
present and consumed.

The profile reads
`docs/fork/EMLO_FORK_ADOPTER_TERM_BASELINE.json`. Every allowed finding binds
an exact repository-relative file, line number, rule category, and SHA-256 of
the complete source line or path. The schema has no glob, regular-expression,
directory, label-only, or blanket-ignore field. A new occurrence, changed
line, moved line, new category, or new adopter-bearing path therefore remains
a rejected finding until a reviewer updates that exact identity.

The current baseline is bound to the corrected combined source commit
`a6497c77b78f19f597cc61fa7c00b72876863dcf` and the reviewed delta from
`fork/main` at `50c0bec4b2ad4e8bb206e749dab39edb0e5ce469`. Source text is stored only as a
SHA-256 value so the policy does not reproduce private-path fixtures or other
sensitive-looking test values.

The findings use bytewise repository-path, line, category, and source-hash
ordering. The scanner rejects noncanonical serialization, duplicate rows, and
any stale, unused, or forged identity. The baseline source commit must include
the requested scan base and remain an ancestor of the scanned checkout.

Before updating the baseline:

1. Run the strict scan and retain its complete finding report.
2. Remove terms that do not belong in the named custom fork.
3. Review every remaining new exact identity and its file ownership.
4. Regenerate the complete canonical inventory and independently verify the
   count, order, source commit, and absence of unused identities.
5. Run the strict-failure, exact-profile-pass, injected-finding, stale-row,
   forged-row, and canonical-serialization tests in
   `hack/control-plane-adopter-term-scan.test.mjs`.

This profile changes only adopter-term classification. It does not suppress or
modify vulnerability, secret, license, dependency, provenance, migration,
signature, or runtime security gates. Upstream contribution branches and
community releases must use the default strict command.
