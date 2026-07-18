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
- an ECR repository configured with tag mutability `IMMUTABLE`.

The build node needs Git, Bash, Docker, AWS CLI v2, `sha256sum`, and the build tools pinned by `mise.toml`. The
Jenkins installation needs Pipeline, Git, Credentials Binding, AWS Credentials, and Timestamper support. The SCM
refspec must fetch the reviewed commit object before the pipeline detaches it.

The AWS identity needs ECR authentication, repository-description, image-description, layer upload, and image-push
permissions. Repository creation and deployment permissions are not required. Prefer a short-lived assumed role or
workload identity. The credentials binding exports credentials only for the publication stage; neither the helper
nor the archived files persist them.

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
the ECR digest, and validates the digest format. Jenkins archives:

```text
dist/release-<candidate-tag>.env
dist/release-<candidate-tag>.env.sha256
```

The `.env` artifact is intentionally non-secret and contains exactly:

```text
DISTR_IMAGE_REF=<repository>@sha256:<digest>
DISTR_RELEASE_COMMIT=<full-source-commit>
DISTR_IMAGE_DIGEST=sha256:<digest>
```

Always verify the sidecar and copy all three values together. Never combine values from separate builds.

## Separate deployment gate

Publication is not deployment approval. The digest must next pass the repository's isolated-server, migration,
backup/restore, health, and acceptance gates. Only after the target environment's authorization is recorded may an
operator copy the exact three-value handoff into that environment and run its documented `check` and `release`
procedure. This Jenkins job has no SSH step and does not call either deployment command.
