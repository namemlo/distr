# Community End-to-End Demo

This demo is provider-neutral and credential-free. It has two layers:

- a live local Hub API smoke journey that can start isolated local dependencies and then clean them up;
- an API-only live release-to-task journey that publishes a bundle through the Hub, creates a task, leases it to a demo agent, executes a safe HTTP check, records events/logs, and verifies operator timelines;
- a deterministic advanced release-to-task fixture verifier used as a fast CI subset.

## One-Command Live Demo

Run from the repository root:

```shell
node examples/community-e2e/live-demo.mjs --start-local --cleanup
```

The live demo starts isolated Docker Compose dependencies, starts the Hub, waits for `/ready`, runs
`hack/e2e-smoke-test.mjs` against the API, runs an API-only release-to-task journey through public Hub
and agent endpoints, and then runs the deterministic advanced release-to-task verifier. Cleanup only removes the
`distr-community-e2e-demo` Compose project resources.

If a Hub is already running:

```shell
DISTR_DEMO_DISPOSABLE_HUB=true DISTR_HOST=http://localhost:8080 node examples/community-e2e/live-demo.mjs --require-running-hub
```

The running-Hub mode creates a per-run random demo user and organization, soft-deletes that demo organization, and verifies it is no longer accessible. Use it only with disposable infrastructure unless `DISTR_DEMO_ALLOW_SHARED_HUB=true` is set deliberately. `--start-local` ignores ambient `DISTR_HOST` and `DATABASE_URL`; use `DISTR_DEMO_HOST` or `DISTR_DEMO_DATABASE_URL` for demo-specific overrides.

## Deterministic Advanced-Flow Verifier

Run the fast supplemental verifier with:

```shell
node examples/community-e2e/run-demo.mjs
```

The verifier checks:

- release bundle publication shape;
- deployment process and plan references;
- agent lease and safe built-in action steps;
- redacted event and log output;
- cleanup ordering;
- stable canonical flow digest.

For machine-readable output:

```shell
node examples/community-e2e/run-demo.mjs --json
```

## Optional Docker Wrapper

If Docker is available and you only need the fast verifier:

```shell
docker compose -f examples/community-e2e/compose.yaml run --rm demo
```

The wrapper requires no cloud, GitHub, GitLab, AWS, registry, or commercial credentials. The dependency services use
ports `15432`, `11025`, `18025`, `19000`, and `19001` so they do not share the root `distr-dev` volumes.

## CI Coverage

`.github/workflows/community-release-hardening.yaml` runs the fast verifier and includes a full release-gates job
that starts the Hub with `DISTR_DEMO_DISPOSABLE_HUB=true` and invokes `live-demo.mjs --require-running-hub` after build, test, `go vet`, golangci-lint, Prettier, vulnerability, Node+Go license, and secret-safety gates.
