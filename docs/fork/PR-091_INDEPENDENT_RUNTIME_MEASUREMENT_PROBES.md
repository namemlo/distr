# PR-091: Independent Runtime Measurement Probes

## Purpose

Close the independent-observer gap where schema, capability, and topology
values were copied from plan intent instead of measured from the runtime.

## Generic user story

As a release operator, I need the independent observer to obtain every
promotion-critical runtime fact from a separate bounded source so that a plan
echo cannot be mistaken for deployment verification.

## Behavior

- Choice TP observer profiles use schema v2 and bind one runtime probe per
  component.
- `http-json/v1` reads an exact JSON record from a profile-pinned loopback
  metadata path.
- `command-json/v1` runs only a profile-pinned executable under
  `/usr/local/libexec` with fixed arguments and `/usr/bin/timeout`.
- Probe output is limited to 4 KiB and exactly three fields:
  `schemaVersion`, `capabilityChecksum`, and `topologyChecksum`.
- The observer canonicalizes and checksums the accepted record, stores no raw
  probe output, compares all three values to the immutable intent, and submits
  the measured values.
- Mismatches produce truthful `UNHEALTHY`/`FAILED` evidence. Missing or invalid
  measurements fail closed before any observation submission.

## Security and scope

Commands, paths, hosts, adapters, and arguments cannot come from the plan
intent. SSH, HTTP, command, and output limits are explicit. Errors redact raw
stdout, stderr, response bodies, credentials, and config content.

Schema is release/build metadata obtained only through an approved non-database
health/metadata endpoint or configured safe probe. This change does not access
a client database, install a live probe, modify Choice TP, contact Jenkins, or
use any live system.

## Compatibility

There is no Hub API, database, migration, UI, or agent protocol change. Profile
v1 is intentionally rejected because it has no independent runtime measurement
binding. Operators must issue a v2 profile and recalculate the intent checksum.

## Verification

```shell
node --test examples/choice-tp-observer/observer.test.mjs
```

The focused suite uses in-memory substitutes only. It verifies independent
measured-value submission, mismatch classification, evidence checksum binding,
safe HTTP and command adapters, timeout/output bounds, unsafe executable
rejection, extra-field rejection, and credential redaction.
