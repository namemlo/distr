# Choice TP DEV independent observer

This example is an adopter-side, read-only observer for the Choice TP DEV `customer-api` and
`transaction-api` pilot. It is deliberately outside Distr core. It verifies the exact running OCI RepoDigest,
`linux/amd64` platform, fixed Compose/config files, container state, and HTTP health before submitting the
documented independent-observation payload to Distr. Schema, capability, and topology facts are measured through
a separately configured read-only runtime probe; they are never copied from the plan intent.

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

Distr stores only the observer-token fingerprint. The callback is exactly:

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
- the local Choice TP gateway and host header;
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

## Run

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
using its canonical `evidenceChecksum` and a `urn:sha256:` evidence reference. Raw SSH output, HTTP bodies,
authorization headers, tokens, keys, environment variables, and config contents are discarded.

Distr's current observation API has no signature field. The signed envelope remains the independently retained
artifact; the documented API receives only `api.ObservationRequest` fields and the canonical evidence link.

## Tests

```shell
node --test examples/choice-tp-observer/observer.test.mjs
```

The tests use an in-memory SSH adapter and HTTP callback. They do not contact Choice TP, Distr, Jenkins, Docker, a
database, or any other live system.
