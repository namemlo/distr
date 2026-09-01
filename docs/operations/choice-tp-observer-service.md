# Choice TP independent observer service

This runbook installs and operates the adopter-side Choice TP DEV observer. It is read-only against the target,
submits only independently measured observation evidence to Distr, and has no database, Jenkins, executor, Docker
socket, or deployment capability.

## Security boundary

Provision a dedicated operating-system or container identity and four observer-only files:

1. an SSH private key restricted on the Choice TP host to the documented read-only command allowlist;
2. a `known_hosts` file containing the exact Choice TP host key verified through an approved independent channel;
3. an opaque Distr observer token from a registration scoped to the exact Deployment Unit and measurement set; and
4. a separately generated Ed25519 evidence-signing private key.

Do not copy these from a Jenkins credential, executor secret, deployment workspace, or operator account. Record the
SHA-256 fingerprints of the Jenkins/executor secrets in `executorCredentialFingerprints`; the service hashes its
three observer credentials at startup and refuses any match. It also requires distinct observer and executor
credential-set IDs in both service configuration and every intent.

Register one observer for the exact Choice TP DEV Deployment Unit with the minimum measurements used here:

```text
artifactDigest
configChecksum
schemaVersion
capabilityChecksum
platform
topologyChecksum
health
```

The observer token is used only as `Authorization: Observer <token>` against
`POST /api/observer/v1/observations`. Store only the returned observer ID and a local SHA-256 token fingerprint in
configuration. Never put the token, SSH key, signing key, or authorization header in an intent, log, evidence file,
environment variable, image layer, or repository.

## Prepare configuration

Copy `examples/choice-tp-observer/service.example.json` and replace all synthetic UUIDs and credential
fingerprints. The `componentInstanceIds` must be the exact Customer and Transaction instances inside the scoped
Deployment Unit. Keep the committed target profile unchanged unless a separately reviewed profile/checksum change
is intended.

Calculate the normalized token fingerprint without printing the token:

```shell
node -e "const fs=require('fs'),c=require('crypto');const t=fs.readFileSync(process.argv[1],'utf8').trim();process.stdout.write('sha256:'+c.createHash('sha256').update(t).digest('hex')+'\n')" \
  /etc/choice-tp-observer/secrets/distr-observer-token
```

Calculate file-byte fingerprints for the observer and executor key files with `sha256sum`. Record only the
Jenkins/executor fingerprints in `executorCredentialFingerprints`; do not mount those credentials into the service.

Generate the evidence key separately:

```shell
openssl genpkey -algorithm Ed25519 \
  -out /etc/choice-tp-observer/secrets/evidence-ed25519.pem
chmod 0400 /etc/choice-tp-observer/secrets/evidence-ed25519.pem
```

The retained legacy file must be the reviewed
`choice-tp-c0-t0-baseline-runtime-evidence.json` with exact file checksum:

```text
sha256:cbebf0295b9eda637afc207f03a28a3c67a99c2d701c5ca99697176ff5343429
```

The config separately pins Customer C0 and Transaction T0 artifact/configuration digests. Startup verifies the file
bytes, component pins, `LEGACY_LIVENESS_ONLY` classification, and `BASELINE_OR_ROLLBACK_ONLY` use. This artifact is
historical baseline/rollback evidence only. It is never treated or submitted as current C1/T1 standard readiness.

After editing, recalculate the canonical service checksum from the repository root:

```shell
node --input-type=module -e "import fs from 'node:fs';import {sha256} from './examples/choice-tp-observer/observer.mjs';const p=process.argv[1],v=JSON.parse(fs.readFileSync(p));delete v.canonicalChecksum;console.log(sha256(v))" \
  /etc/choice-tp-observer/service.json
```

Write that digest into `canonicalChecksum`, then run `--check`. Validation reads local files only; it does not open
SSH, call Distr, query a database, or mutate a runtime.

## Intent handoff

Issue a new short-lived immutable intent for each observation. Start from
`examples/choice-tp-observer/intent.c1-t1.example.json`, replace all synthetic IDs, times, source sequences, and
expected values, and recalculate its canonical checksum. Both component sequences must advance monotonically.

The service is deliberately bound to current checkpoint C1/T1. Customer C1 and Transaction T1 artifact digests
must differ from the pinned C0/T0 digests. The independently measured runtime metadata must exactly match the
intent's schema, capability, and topology values; `/alive` is preferred and `/healthz` is the fixed fallback.

Place intents into the inbox atomically so the poller never sees a partial file:

```shell
install -o choice-tp-observer -g choice-tp-observer -m 0600 \
  /secure/new-intent.json /var/lib/choice-tp-observer/intents/.new-intent.json.tmp
mv /var/lib/choice-tp-observer/intents/.new-intent.json.tmp \
  /var/lib/choice-tp-observer/intents/new-intent.json
```

Do not change an intent after handoff. A changed observation must use a new intent and higher component sequences.

## Container installation

Build with `NODE_IMAGE` set to an approved Node base image including its immutable digest, publish the resulting
observer image through the approved artifact pipeline, then resolve its immutable repository digest. Production
Compose requires `CHOICE_TP_OBSERVER_IMAGE` to contain that `@sha256:` digest:

```shell
export CHOICE_TP_OBSERVER_IMAGE='registry.example/choice-tp-observer@sha256:<verified-digest>'
docker compose -f examples/choice-tp-observer/compose.yaml config
docker compose -f examples/choice-tp-observer/compose.yaml up -d
```

Create host `config`, `secrets`, `intents`, `evidence`, and `state` directories first. The container runs as
UID/GID `10001`, with a read-only root filesystem, all capabilities dropped, `no-new-privileges`, no Docker socket,
read-only config/secret/intent mounts, and writable evidence/state mounts only. No Jenkins or executor volume is
allowed.

Verify:

```shell
docker compose -f examples/choice-tp-observer/compose.yaml ps
docker compose -f examples/choice-tp-observer/compose.yaml logs --since 10m choice-tp-observer
```

Each poll emits only intent IDs, status, evidence checksums, HTTP status, and bounded error classes. It never emits
credentials, SSH output, HTTP bodies, config contents, or runtime-probe bodies.

After Distr accepts an observation, the existing Fleet and execution detail views show its exact artifact digest,
config checksum, platform, schema version, capability checksum, and health. Fleet also shows the evidence checksum
when a single current accepted observation supplies it. A Fleet conflict intentionally withholds a singular runtime
identity; reconcile the competing observations rather than treating one aggregate value as authoritative.

## systemd installation

Install Node.js 26 and OpenSSH client from the approved host baseline. Create an unprivileged account and private
directories:

```shell
useradd --system --home-dir /var/lib/choice-tp-observer --shell /usr/sbin/nologin choice-tp-observer
install -d -o root -g choice-tp-observer -m 0750 /etc/choice-tp-observer
install -d -o choice-tp-observer -g choice-tp-observer -m 0700 \
  /var/lib/choice-tp-observer/intents \
  /var/lib/choice-tp-observer/evidence \
  /var/lib/choice-tp-observer/state
install -d -o root -g root -m 0755 /opt/choice-tp-observer
```

Install `observer.mjs`, `service.mjs`, and `README.md` under `/opt/choice-tp-observer`; install the prepared profile,
config, known-hosts pin, legacy C0/T0 artifact, and observer-only secrets under `/etc/choice-tp-observer`. The
observer account needs read access to those files, while private keys and token remain mode `0400` or `0440`.

Copy the unit files, validate, and enable only the timer:

```shell
install -m 0644 examples/choice-tp-observer/systemd/choice-tp-observer.service /etc/systemd/system/
install -m 0644 examples/choice-tp-observer/systemd/choice-tp-observer.timer /etc/systemd/system/
systemctl daemon-reload
systemd-analyze verify /etc/systemd/system/choice-tp-observer.service
systemctl enable --now choice-tp-observer.timer
systemctl list-timers choice-tp-observer.timer
```

The oneshot runs once per minute and cannot overlap through the durable service lock. The unit has a strict
read-only filesystem except for evidence and state, no capabilities, no home/device access, and network access only
for SSH to the fixed target and HTTPS to Distr.

## Restart and recovery

For every intent, the service writes a mode-`0600` signed evidence file with exclusive create before the first
submission. State is updated through a same-directory temporary file and rename. If the process stops after one
component is accepted, the next run verifies the retained Ed25519 signature, config/profile/intent scope, and
evidence checksum, then resubmits the exact two requests. Distr's exact material replay is idempotent; the observer
does not remeasure or change `capturedAt` under the reused sequences.

Transient transport, HTTP `408`, `429`, and `5xx` failures use bounded exponential retry. Authentication, scope,
validation, oversized response, and other non-transient errors are not retried within the poll. After
`maxTotalAttemptsPerIntent`, the state becomes `EXHAUSTED`; retain the evidence and state. Do not delete evidence,
decrement/reuse a source sequence, or edit the signed artifact. Diagnose the bounded failure, then issue a fresh
intent with higher sequences if a new measurement is required.

## Rollback

Rollback affects only this observer service. It does not authorize a Choice TP deployment, container restart,
database operation, Jenkins action, or Distr history edit.

1. Stop the Compose service or disable and stop the systemd timer.
2. Copy the current config, image digest/unit version, state file, and evidence directory to immutable operator
   storage. Do not remove the original evidence.
3. Restore the previous digest-pinned observer image or previous `/opt/choice-tp-observer` files together with its
   matching checksummed config and state snapshot.
4. Run `--check` before enabling the previous version.
5. Re-enable the service and confirm the next poll either skips completed intents or replays an existing exact
   signed artifact.

Never roll back by deleting the state file while leaving uncertain submissions, reusing an accepted source
sequence with new bytes, replacing the C0/T0 evidence pin, or substituting Jenkins/executor credentials. If the
matching prior state snapshot is unavailable, leave the observer stopped and reconcile retained Distr observations
and evidence checksums before issuing a new higher-sequence intent.
