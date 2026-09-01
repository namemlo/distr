# Choice TP DEV independent observer

This example is an adopter-side, read-only observer for the Choice TP DEV `customer-api` and
`transaction-api` pilot. It is deliberately outside Distr core. It verifies the exact running OCI RepoDigest,
`linux/amd64` platform, fixed Compose/config files, container state, and HTTP health before submitting the
documented independent-observation payload to Distr. Schema, capability, and topology facts are measured through
a separately configured read-only runtime probe; they are never copied from the plan intent.

The directory contains both the original one-shot command and a durable polling service. The service consumes
immutable intent files from a read-only inbox, persists signed evidence before submission, and replays those exact
bytes after restart. It never remeasures an intent once its signed evidence exists.

The observer never deploys, pulls, starts, stops, restarts, removes, copies, or executes inside a container. It
does not query a client database and does not contact or change Suria, MC, Jenkins, or another service.

## Trust boundaries

Run the observer under a credential set that is separate from Jenkins and the deployment executor:

- a dedicated SSH private key restricted on the Choice TP host to the read-only command surface below;
- a dedicated `known_hosts` file with the pinned Choice TP host key;
- a dedicated Distr observer token registered through `POST /api/v1/observer-registrations`;
- a dedicated Ed25519 evidence-signing private key.

The immutable intent carries both `observerCredentialSetId` and `executorCredentialSetId`. The observer refuses
to run when they are equal. Secret values and private keys are read from operator-supplied files and are never
stored in this repository, evidence, stdout, SSH output, or the Distr request body.

Distr stores only the observer-token fingerprint. The durable service additionally pins that fingerprint in its
checksummed configuration and rejects an observer SSH key, token, or evidence key whose fingerprint matches any
configured Jenkins/executor credential fingerprint. The callback is exactly:

```http
Authorization: Observer <opaque observer token>
POST /api/observer/v1/observations
```

No Jenkins callback or private browser endpoint is used.

## Fixed target profile

[`choice-tp-dev.profile.json`](choice-tp-dev.profile.json) pins:

- `emlo-admin@217.15.166.6:22`;
- the two exact container names;
- the two exact service Compose and config paths;
- the local Choice TP gateway and exact Envoy host header `api-gateway.dev.spi.emlotech.com`;
- `/alive` as the preferred probe and `/healthz` only as its fallback;
- `https://distr.emlotech.com` as the only submission host.

Its canonical checksum prevents host, path, service, gateway, or API rebinding. The only remote executables the
observer can construct are:

```text
/usr/bin/docker inspect
/usr/bin/docker image inspect
/usr/bin/sha256sum
/usr/bin/curl (GET with body discarded)
/usr/bin/curl (bounded local JSON metadata GET)
/usr/bin/timeout (only around a profile-pinned probe under /usr/local/libexec)
```

The code has no command path for Docker Compose, Docker mutation, arbitrary shell, file writes, database clients,
or remote command input from the intent.

## Immutable observation intent

The release/control-plane operator creates one short-lived intent from the frozen target plan. Do not reuse a
source sequence and do not commit a live intent. The intent binds:

- organization, observer, Deployment Unit, and Component Instance UUIDs;
- the fixed target-profile checksum;
- a short `notBefore`/`expiresAt` window;
- exact per-component source sequences;
- expected OCI digest, Compose/config checksums, platform, schema declaration, capability checksum, and topology
  checksum;
- distinct observer and executor credential-set IDs;
- a canonical SHA-256 checksum over every field except `canonicalChecksum`.

[`intent.example.json`](intent.example.json) is non-live documentation with synthetic Distr UUIDs and sequences.
Replace all plan-specific values and validity times, then recalculate the checksum before use. The observer rejects
unknown fields, expired intents, checksum changes, component additions, and target rebinding.

The observer does not query a workload database. Each component has one profile-pinned `runtimeProbe`. The committed
Choice TP DEV profile uses `http-json/v1` against a loopback-only application metadata route through the fixed local
gateway. A deployment may instead select `command-json/v1`, but only for an exact executable below
`/usr/local/libexec`, with fixed profile arguments and `/usr/bin/timeout`. Neither adapter accepts a command, path,
endpoint, or argument from the plan intent.

The probe must return one bounded JSON object and no other fields:

```json
{
  "schemaVersion": "1.1.0",
  "capabilityChecksum": "sha256:<component-release-checksum>",
  "topologyChecksum": "sha256:<frozen-placement-topology-checksum>"
}
```

`schemaVersion` is release/build metadata, not a database migration query. The two checksums are runtime-published
immutable facts generated from the sealed Component Release and frozen placement topology. The service endpoint or
approved safe probe obtains them from non-database build/runtime metadata only. Direct or indirect `psql`, SQL,
connection-string, ORM migration-table, or client workload database access is outside this observer contract.

The observer rejects extra keys, malformed digests, output over 4 KiB, redirects, non-loopback HTTP rebinding,
timeouts, and unsupported command paths. It computes a canonical SHA-256 checksum of the accepted measurement,
retains only that checksum plus the adapter identity in signed evidence, and discards the raw response. Probe output,
stderr, authorization material, config contents, and command output are never included in errors or submissions.

## Durable service

[`service.mjs`](service.mjs) validates all configuration, credentials, profile, pinned host key, and the retained
legacy baseline before it starts polling. [`service.example.json`](service.example.json) binds the exact
organization, observer registration, Deployment Unit, Customer/Transaction Component Instances, observer and
executor credential-set IDs, credential fingerprints, and all state paths. Replace the synthetic UUIDs and
credential fingerprints, then recalculate `canonicalChecksum` over every field except `canonicalChecksum`.

The service has two sealed runtime modes selected only by the checksummed service config and read-only profile mount:

- `service.example.json` plus `choice-tp-dev.profile.json` observes C1/T1 through bounded HTTP metadata and emits
  standard-readiness, promotion-eligible evidence.
- `service.c0-t0.example.json` plus `choice-tp-dev-c0-t0.profile.json` observes the exact C0/T0 pins through the
  restricted runtime-state helper and Swagger liveness only. Its evidence remains baseline/rollback-only.

Use the matching `intent.c1-t1.example.json` or `intent.c0-t0.example.json`. An intent cannot select or override the
adapter, helper, path, checkpoint, or health classification.

The `legacyBaseline` section pins the tracked
[`choice-tp-c0-t0-baseline-runtime-evidence.json`](choice-tp-c0-t0-baseline-runtime-evidence.json) artifact
byte-for-byte as `sha256:791955e37fd9911e472aa03512197a4e013784049e7651eaa772bad74e5a3815`, plus the exact C0/T0 OCI and
configuration digests. Startup requires the mounted artifact to match those pins and to remain classified
`LEGACY_LIVENESS_ONLY` / `BASELINE_OR_ROLLBACK_ONLY`. It is never relabeled as standard readiness and is never
submitted as C1/T1 evidence.

Current measurements use the independently probed artifact, configuration, schema, capability, platform, topology,
and `/alive`-then-`/healthz` result. Their observation request is explicitly classified
`STANDARD_READINESS` / `STANDARD_PROMOTION_ELIGIBLE` with a checksum of the fixed logical health policy. The
evidence reference is `evidence://sha256/<evidence checksum>`.

C0/T0 measurements use the exact sealed helper and legacy Swagger paths. Even when healthy, they retain
`LEGACY_LIVENESS_ONLY` / `BASELINE_OR_ROLLBACK_ONLY` and cannot authorize promotion.

The polling and retry bounds are explicit:

- at most `maxIntentsPerPoll` intent files are considered per poll;
- only transient transport, HTTP `408`, `429`, and `5xx` submission failures are retried;
- exponential delay stops at `maxAttemptsPerPoll` and `maxDelayMs`;
- an intent stops permanently at `maxTotalAttemptsPerIntent`;
- exact replay is safe because Distr treats identical observer/component sequence material as idempotent;
- a durable lock prevents overlapping service instances and is reclaimed only after its configured stale period.

Validate a prepared installation without SSH or Distr calls:

```shell
node examples/choice-tp-observer/service.mjs \
  --config /etc/choice-tp-observer/service.json \
  --check
```

Run one scheduled poll or the continuous container process:

```shell
node examples/choice-tp-observer/service.mjs --config /etc/choice-tp-observer/service.json --once
node examples/choice-tp-observer/service.mjs --config /etc/choice-tp-observer/service.json
node examples/choice-tp-observer/service.mjs --config /etc/choice-tp-observer/service.json --health
node examples/choice-tp-observer/service.mjs --config /etc/choice-tp-observer/service.json --ready
```

Run [`preflight.mjs`](preflight.mjs) before Compose. Use [`compose.yaml`](compose.yaml) with an image reference
containing an immutable `@sha256:` digest, or install the
provided systemd oneshot/timer units. Neither packaging surface accepts credentials through environment variables,
and neither mounts a Jenkins workspace, Docker socket, executor key, or executor token. See
[`docs/operations/choice-tp-observer-service.md`](../../docs/operations/choice-tp-observer-service.md) for install,
operation, recovery, and rollback.

## One-shot run

Generate an Ed25519 evidence key in an approved secret directory, not in the repository:

```shell
openssl genpkey -algorithm Ed25519 -out choice-tp-observer-evidence.pem
```

Then run from the repository root. The evidence path must not already exist; this prevents accidental history
replacement.

```shell
node examples/choice-tp-observer/observer.mjs \
  --profile examples/choice-tp-observer/choice-tp-dev.profile.json \
  --intent /secure/choice-tp-observation-intent.json \
  --ssh-key-file /secure/choice-tp-observer-ssh-key \
  --known-hosts-file /secure/choice-tp-observer-known-hosts \
  --observer-token-file /secure/choice-tp-distr-observer-token \
  --evidence-private-key-file /secure/choice-tp-observer-evidence.pem \
  --output /secure/evidence/choice-tp-observation.json
```

The process exits non-zero if SSH host verification fails, the intent/profile changes, a command returns malformed
or oversized output, a required runtime measurement is incomplete, a container is not running, both health endpoints
fail, or Distr does not return HTTP `202` for both components.

Digest, config, schema, capability, platform, topology, and health mismatches are submitted truthfully as
`UNHEALTHY`/`FAILED` evidence using the measured values. Transport, authentication, malformed-output, and
incomplete-measurement failures are not submitted.

## Evidence

The output is a bounded canonical record containing only fixed identities, checksums, comparison booleans, HTTP
status codes, canonical runtime-probe evidence checksums, and timestamps. It is written with mode `0600`, signed
with Ed25519, and linked to each Distr request
using its canonical `evidenceChecksum` and an `evidence://sha256/` evidence reference. Raw SSH output, HTTP bodies,
authorization headers, tokens, keys, environment variables, and config contents are discarded.

Distr's current observation API has no signature field. The signed envelope remains the independently retained
artifact; the documented API receives only `api.ObservationRequest` fields and the canonical evidence link.

Accepted observation values are visible through the existing Fleet and execution detail surfaces as native
artifact digest, config checksum, platform, schema version, capability checksum, and health fields. Fleet shows a
single evidence checksum only when one current accepted observation supplies it and withholds a singular identity
when observations conflict.

## Tests

```shell
node --test \
  examples/choice-tp-observer/observer.test.mjs \
  examples/choice-tp-observer/service.test.mjs \
  examples/choice-tp-observer/packaging.test.mjs

python -m unittest examples/choice-tp-observer/restricted-ssh/test_restricted_ssh.py
```

The tests use temporary files plus in-memory SSH and HTTP adapters. They cover restart replay after partial
submission, terminal-inbox filtering, retry exhaustion, host/credential/scope pins, sealed C0/T0 versus C1/T1
classification, health/readiness, terminal-only state migration, packaging, restricted SSH, and the original
observer parsing/signing rules. They do not contact
Choice TP, Distr, Jenkins, Docker, a database, or any other live system.
