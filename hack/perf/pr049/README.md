# PR-049 Compatibility Performance Fixtures

These benchmarks are for bottleneck discovery, not universal throughput promises.

## Smoke Profile

Runs in ordinary developer environments with reduced fixture sizes:

```sh
go test -run '^$' -bench 'PR049' -benchmem ./internal/deploymentcompat
```

## Full Profile

Runs the roadmap-scale fixture labels:

- 1,000 deployment targets
- 100 concurrent online agents
- 100-component release bundle
- 500-step aggregate wave
- large step logs
- many scoped-variable candidates

```sh
PR049_FULL_BENCH=1 go test -run '^$' -bench 'PR049' -benchmem ./internal/deploymentcompat
```

Record the Go version, database version if DB-backed benchmarks are added, machine class, elapsed time,
allocation count, and fixture scale with every published result. Do not add fragile wall-clock gates to
ordinary CI; use the smoke profile to keep benchmark construction from regressing.

## Current Baseline

The first PR-049 benchmark slice measures deterministic legacy projection and canonicalization. DB query-count
regression coverage lives in repository tests for timeline and backfill behavior. Future DB-backed benchmarks
should use the same six scenario labels and keep full-scale execution opt-in.
