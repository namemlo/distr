# Config as Code

Config as Code is experimental and guarded by `DISTR_EXPERIMENTAL_FEATURE_FLAGS=config_as_code`.

PR-048 validates declarative documents and records which system is authoritative for a resource. It does not connect to Git, import documents, apply changes, export resources, poll repositories, process webhooks, or resolve secrets from Git.

## Document Envelope

```yaml
apiVersion: distr.sh/v1alpha1
kind: Channel
metadata:
  name: stable
  path: channels/stable.yaml
spec:
  description: Stable channel
```

Supported kinds:

- `DeploymentProcess`
- `Channel`
- `Lifecycle`
- `VariableSetDefinition`
- `StepTemplateReference`
- `Runbook`

Validation rejects unsupported versions or kinds, unknown fields, duplicate YAML keys, absolute paths, path traversal, oversized or deeply nested documents, YAML aliases/anchors, and plaintext secret-like values.

## Authority

Each resource has one authority:

- `DATABASE_MANAGED`: normal UI/API mutations are allowed.
- `GIT_MANAGED`: reads are allowed, but normal mutations return `409 Conflict`.

Resources without authority records behave as `DATABASE_MANAGED`.

Authority state transitions are explicit:

```text
DATABASE_MANAGED -> GIT_MANAGED
GIT_MANAGED -> DATABASE_MANAGED
```

Changing authority records stores repository path, source revision, document checksum, actor, timestamp, and a non-secret audit event. Document contents and secret values are not stored in authority audit records.

## Schema Version Policy

`distr.sh/v1alpha1` is the only accepted version in PR-048. New schema versions must be added explicitly. Unknown versions fail closed; there is no automatic downgrade or partial interpretation of newer documents.

## Secret Restrictions

Git-managed documents must not contain plaintext passwords, tokens, private keys, connection strings, or secret defaults. Use references such as `secretRef`, `accountRef`, or `certificateRef` where the schema permits.
