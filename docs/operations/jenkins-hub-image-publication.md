# Jenkins Hub Image Publication

`deploy/jenkins/Jenkinsfile.hub-image` is a publish-only Jenkins pipeline for the Distr Hub image. It checks out one
reviewed source commit, builds that commit once for `linux/amd64`, pushes one immutable ECR candidate, and archives
the digest-pinned deployment identity. It never connects to a server or starts a deployment.

## Jenkins job configuration

Create a Pipeline-from-SCM job with:

- the fixed Distr repository and credentials configured in Jenkins SCM, not as build parameters;
- script path `deploy/jenkins/Jenkinsfile.hub-image`;
- a Linux AMD64 agent labeled `linux-amd64-docker`;
- `AWS_REGION`, `ECR_REPOSITORY`, and the complete untagged `DISTR_IMAGE` ECR repository URI as non-secret job
  environment values;
- `AWS_CREDENTIALS_ID` as the ID of an AWS Credentials plugin binding;
- `PROVENANCE_SIGNING_KEY_CREDENTIALS_ID` as the ID of the cosign private-key secret file;
- `PROVENANCE_SIGNING_PUBLIC_KEY_CREDENTIALS_ID` as the ID of the cosign public-key secret file;
- `PROVENANCE_SIGNING_PASSWORD_CREDENTIALS_ID` as the ID of the cosign password secret text;
- an ECR repository configured with tag mutability `IMMUTABLE`.

The build node needs Git, Bash, Docker, AWS CLI v2, cosign, `sha256sum`, and the build tools pinned by `mise.toml`. The
Jenkins installation needs Pipeline, Git, Credentials Binding, AWS Credentials, and Timestamper support. The SCM
refspec must fetch the reviewed commit object before the pipeline detaches it.

The AWS identity needs ECR authentication, repository-description, image-description, layer upload, and image-push
permissions. Repository creation and deployment permissions are not required. Prefer a short-lived assumed role or
workload identity. The credentials binding exports credentials only for the publication stage; neither the helper
nor the archived files persist them. The private key and public key bindings must be Jenkins secret files; the
password binding must be Jenkins secret text and is exposed to cosign only as `COSIGN_PASSWORD`.

## Build parameter

`RELEASE_COMMIT` is required and must be exactly 40 lowercase hexadecimal characters. The pipeline verifies that
the configured SCM checkout contains this commit, detaches `HEAD` at it, removes workspace residue, and confirms the
resulting tree is clean.

The candidate tag is generated as:

```text
candidate-<first-8-commit-characters>-<YYYYMMDD>t<HHMMSS>z
```

Before both build and push, the helper proves that the candidate tag is absent. It also requires repository-level
tag immutability, so a concurrent publisher cannot replace an existing candidate.

## Publication and archived handoff

The helper reuses the repository's existing release-engineering contract:

```text
deploy.sh image-check
deploy.sh build
deploy.sh push
```

It forces the build inputs to Linux AMD64, verifies the local and digest-pulled OCI revision/source labels, resolves
the ECR digest, and validates the digest format. This job publishes one Linux AMD64 image, not a multi-platform
manifest list. An ARM64 server requires a separately implemented and proven ARM64 publication path.

Jenkins archives:

```text
dist/release-<candidate-tag>.env
dist/release-<candidate-tag>.env.sha256
dist/release-<candidate-tag>.spdx.json*
dist/release-<candidate-tag>.intoto.json*
dist/release-<candidate-tag>-migration-postgresql-16.14.json*
dist/release-<candidate-tag>-migration-postgresql-18.4.json*
dist/release-<candidate-tag>-acceptance.json*
dist/release-evidence-<candidate-tag>/**
```

After evidence finalization, the non-secret `.env` handoff contains:

```text
DISTR_IMAGE_REF=<repository>@sha256:<digest>
DISTR_RELEASE_COMMIT=<full-source-commit>
DISTR_IMAGE_DIGEST=sha256:<digest>
DISTR_SBOM_REF=dist/release-<candidate-tag>.spdx.json
DISTR_SBOM_SHA256=sha256:<digest>
DISTR_PROVENANCE_REF=dist/release-<candidate-tag>.intoto.json
DISTR_PROVENANCE_SHA256=sha256:<digest>
DISTR_PROVENANCE_SIGNATURE_REF=dist/release-<candidate-tag>.intoto.json.sig
DISTR_PROVENANCE_SIGNATURE_SHA256=sha256:<digest>
DISTR_ATTESTATION_REF=dist/release-<candidate-tag>.intoto.json.sig@sha256:<digest>
DISTR_MIGRATION_POSTGRESQL_16_REPORT_REF=dist/release-<candidate-tag>-migration-postgresql-16.14.json
DISTR_MIGRATION_POSTGRESQL_16_REPORT_SHA256=sha256:<digest>
DISTR_MIGRATION_POSTGRESQL_18_REPORT_REF=dist/release-<candidate-tag>-migration-postgresql-18.4.json
DISTR_MIGRATION_POSTGRESQL_18_REPORT_SHA256=sha256:<digest>
DISTR_ACCEPTANCE_BUNDLE_REF=dist/release-<candidate-tag>-acceptance.json
DISTR_ACCEPTANCE_BUNDLE_SHA256=sha256:<digest>
```

Always verify the handoff sidecar and retain the complete handoff with its referenced evidence. The server runtime
uses the first three image-identity values; copy that triplet together from one verified handoff. Never combine
identity or evidence values from separate builds.

## Separate deployment gate

Publication is not deployment approval. The digest must next pass the repository's isolated-server, migration,
backup/restore, health, and acceptance gates. Only after the target environment's authorization is recorded may an
operator copy the runtime identity triplet from the verified complete handoff into that environment and run its
documented `check` and `release` procedure. This Jenkins job has no SSH step and does not call either deployment
command.
