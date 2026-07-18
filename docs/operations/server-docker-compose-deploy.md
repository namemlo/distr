# Server Docker Compose Deployment With AWS ECR

This guide deploys the community Distr Hub from this source tree to one Linux server with Docker Compose.
The Hub image is built from source, pushed to a private AWS ECR repository, then pulled by Docker Compose on the server.

For production releases, Jenkins builds an immutable image, pushes it to ECR, resolves the pushed image digest, and the
server deploys that exact `repository@sha256:...` image reference. The helper script also supports building directly on
the server for the first controlled deployment.

## What This Package Adds

Files:

- `deploy/server-docker-compose/deploy.sh` - build, ECR login, push, pull, backup, migrate, start, health-check, and rollback helper.
- `deploy/server-docker-compose/docker-compose.yml` - production-oriented Compose stack that runs the configured ECR image.
- `deploy/server-docker-compose/.env.example` - environment template with no real secrets.
- `deploy/server-docker-compose/nginx.example.conf` - reverse-proxy example for Hub and registry hostnames.
- `deploy/server-docker-compose/nginx.single-domain.example.conf` - reverse-proxy example when one hostname serves both Hub and `/v2/` registry traffic.

The package intentionally does not modify `deploy/docker/docker-compose.yaml`, which is the upstream-style sample that
uses the published `ghcr.io/distr-sh/distr-ce:2.24.1` image.

## Server Requirements

Install these on the server:

- Linux server with at least 2 CPU, 4 GB RAM, and enough disk for PostgreSQL, images, logs, and registry artifacts.
- Docker Engine with the `docker compose` plugin.
- AWS CLI v2 authenticated with permission to pull from ECR.
- Git.
- `curl`, `openssl`, `jq`, `sha256sum`, `bash`, and `flock`.
- `mise`, only if the server will build from source. If Jenkins builds and pushes to ECR, the server does not need the build toolchain.

Recommended firewall:

- Allow inbound `22`, `80`, and `443`.
- Do not expose PostgreSQL, RustFS, `8080`, `8585`, or metrics ports directly.

Minimum ECR permissions:

- Jenkins build/push identity: `ecr:GetAuthorizationToken`, `ecr:DescribeRepositories`, `ecr:DescribeImages`,
  `ecr:BatchCheckLayerAvailability`, `ecr:InitiateLayerUpload`, `ecr:UploadLayerPart`, `ecr:CompleteLayerUpload`,
  and `ecr:PutImage`.
- Add `ecr:CreateRepository` only if you want `deploy.sh ecr-create-repo` to create the repository.
- Server pull identity: `ecr:GetAuthorizationToken`, `ecr:BatchGetImage`, `ecr:GetDownloadUrlForLayer`, and
  `ecr:DescribeImages` if rollback by tag is allowed.

## Server Setup

Clone the fork on the production server so the server has the Compose file and release helper:

```bash
git clone https://github.com/namemlo/distr.git /opt/distr
cd /opt/distr
git checkout 236a463d
```

Create the production environment file:

```bash
./deploy/server-docker-compose/deploy.sh init
```

Edit `deploy/server-docker-compose/.env`:

```bash
nano deploy/server-docker-compose/.env
```

Set at least:

- `AWS_REGION=ap-southeast-1`
- `ECR_REPOSITORY=distr-community`
- `DISTR_IMAGE=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community`
- `DISTR_IMAGE_TAG=<immutable-release-tag-or-git-sha>` from Jenkins; do not use `latest`.
- `DISTR_IMAGE_REF=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community@sha256:<image-digest>` from Jenkins.
- `DISTR_RELEASE_COMMIT=<40-lowercase-hex-source-commit>` from the same archived Jenkins release artifact.
- `DISTR_IMAGE_DIGEST=sha256:<64-lowercase-hex-image-digest>` from that same artifact; it must match `DISTR_IMAGE_REF`.
- `DISTR_CALLBACK_PROBE_URL` set to a non-`CHANGE_ME` loopback callbacks route for one known historical execution.
- `DISTR_HOST=https://distr.example.com`
- `REGISTRY_HOST=registry.example.com`
- `REGISTRATION=enabled` for first admin setup, then change to `hidden` or `disabled`.
- SMTP/SES settings before setting `USER_EMAIL_VERIFICATION_REQUIRED=true`.
- Keep the local RustFS settings for the first deploy, or adapt the Compose file before using external S3.

Confirm AWS auth before deploy:

```bash
aws sts get-caller-identity
./deploy/server-docker-compose/deploy.sh ecr-login
```

Do not put production runtime secrets in Jenkins. Keep the full `.env` on the server.

## Jenkins Build And Server Deploy

Use this as the normal deployment path for our infrastructure: Jenkins builds the image and pushes it to ECR, while the
server only pulls and runs it.

For a production Pipeline-from-SCM job, use
[`deploy/jenkins/Jenkinsfile.hub-image`](jenkins-hub-image-publication.md) and its immutable publication contract.

The existing `examples/ci/jenkins/Jenkinsfile` demonstrates Distr release API publishing. This section is separate: it
builds and publishes the Distr Hub deployment image itself.

On the Jenkins agent, create a minimal `deploy/server-docker-compose/.env` for the image job only:

```bash
cat > deploy/server-docker-compose/.env <<EOF
AWS_REGION=ap-southeast-1
ECR_REPOSITORY=distr-community
DISTR_IMAGE=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community
DISTR_IMAGE_TAG=$(git rev-parse --short=12 HEAD)
EOF

./deploy/server-docker-compose/deploy.sh image-check
./deploy/server-docker-compose/deploy.sh ecr-create-repo
./deploy/server-docker-compose/deploy.sh build
./deploy/server-docker-compose/deploy.sh push
```

Jenkins does not need the production database password, JWT secret, domain settings, or RustFS secrets for `build` and
`push`. After `push`, archive `dist/release-${DISTR_IMAGE_TAG}.env`; it contains the non-secret release identity that
the server should deploy.

Treat that file as one immutable handoff artifact and never mix values from different archived artifacts. At the
isolated-validation checkpoint, copy its `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and
`DISTR_IMAGE_DIGEST` values together into only the isolated server environment and use a clone-scoped callback probe.
Do not change a live or client environment. Copy the same three values together into the full production `.env` only
as part of an authorized live-promotion checkpoint.
Set `DISTR_CALLBACK_PROBE_URL` to a non-`CHANGE_ME` loopback callbacks route for one known historical execution only
at that checkpoint.

## Authoritative Release-Execution Matrix

The checkpoint cadence is once per completed checkpoint/PR, not once per edit. A candidate advances only through this
sequence:

| Checkpoint                 | Required action and retained proof                                                                                                                                                                                                                           | Live effect                                            |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------ |
| 1. Focused local preflight | Run the focused validators, tests, build, migration/recovery checks, and diff/privacy checks applicable to the completed checkpoint.                                                                                                                         | None.                                                  |
| 2. Exact tree              | Commit the exact complete tree that passed preflight. Record the commit and reviewed range.                                                                                                                                                                  | None.                                                  |
| 3. Immutable ECR candidate | Build once from that commit and publish one immutable ECR digest tied to the source commit, OCI revision, and target platform. Retain the archived three-value handoff.                                                                                      | None. Publishing a candidate is not promotion.         |
| 4. Isolated server proof   | Run that exact digest on an isolated server runtime against a PostgreSQL 18 clone. Retain checksummed database/object backup, restore, schema, migration, recovery, health, audit, image, and platform evidence. Do not mutate a live or client environment. | None. Isolated validation is not live promotion.       |
| 5. Acceptance gates        | Require the functional, migration, dirty-recovery, backup/restore, platform, and dependency gates to pass for that same commit and digest.                                                                                                                   | None.                                                  |
| 6. Live promotion          | Treat promotion as a distinct recorded action. Use only the already proven digest, and proceed only when environment policy, required authorization, and every live precondition pass. This runbook grants no promotion authority.                           | The approved environment changes to the proven digest. |
| 7. Post-promotion proof    | Verify and retain the running digest, clean schema version, health/readiness, and expected audit state.                                                                                                                                                      | Promotion is accepted only after these checks pass.    |

The current candidate is **NOT LIVE PROMOTED**. If any checkpoint fails and a source or packaging change is required,
create a new commit, build a new digest, and repeat the sequence. Never overwrite a candidate tag or reuse a tag as
release identity; the immutable digest and its matching commit/OCI revision/platform tuple are authoritative.

The commands below are the live-promotion action, not an isolated-validation shortcut. Run them only at checkpoint 6
after the exact digest has passed checkpoints 1-5 and the required policy and authorization have been recorded:

```bash
./deploy/server-docker-compose/deploy.sh check
./deploy/server-docker-compose/deploy.sh release
```

`release` acquires the deployment lock, refuses an active timestamp fence, validates Compose and the immutable
release identity, pulls the digest-pinned image, and starts dependencies. It then runs the read-only migration
preflight while the existing Hub writers remain online. A non-empty schema 137 database is refused at that point,
before the Hub is stopped, and must use the staged migration-138 procedure below. Only when preflight allows an
ordinary release does the script stop and fence Hub writers, verify they are stopped, back up PostgreSQL and object
storage, migrate explicitly, start Hub with `serve --migrate=false`, and run the health check.

## External-Execution Timestamp Expand (Migration 138)

Set `DISTR_TIMESTAMP_EVIDENCE_DIR` to a protected empty directory on the deployment host, create it with mode
`0700`, and retain it through release acceptance:

```bash
install -d -m 0700 "$DISTR_TIMESTAMP_EVIDENCE_DIR"
./deploy/server-docker-compose/deploy.sh timestamp-expand-capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"
```

Capture acquires the deployment lock, persists the writer fence, stops every Hub writer, creates checksummed
PostgreSQL and object-store backups, restores both into isolated temporary resources, compares restored and source
identity, writes the complete draft manifest, removes temporary restore resources, and leaves Hub stopped.

An independent reviewer copies `draft-manifest.json` to `reviewed-draft.json`, records one decision for every cell,
and supplies named author/reviewer identities plus a checksummed evidence reference. Seal the reviewed file with
the same digest-pinned image:

Compose `--env-file` does not export those values into the host shell.
Load the release identity explicitly from the restricted Compose environment, derive the captured evidence checksum
from its canonical sidecar, and replace the three non-secret `CHANGE_ME` identity/reference placeholders before
sealing:

```bash
set -Eeuo pipefail
export DISTR_COMPOSE_ENV_FILE="$(realpath deploy/server-docker-compose/.env)"
export DISTR_TIMESTAMP_AUTHOR="CHANGE_ME_NON_SECRET_AUTHOR_IDENTITY"
export DISTR_TIMESTAMP_REVIEWER="CHANGE_ME_DISTINCT_NON_SECRET_REVIEWER_IDENTITY"
export DISTR_TIMESTAMP_EVIDENCE_REFERENCE="CHANGE_ME_OPAQUE_NON_SECRET_EVIDENCE_REFERENCE"

[[ "$DISTR_COMPOSE_ENV_FILE" = /* ]]
[[ -f "$DISTR_COMPOSE_ENV_FILE" && ! -L "$DISTR_COMPOSE_ENV_FILE" ]]
[[ "$(stat -c '%a' -- "$DISTR_COMPOSE_ENV_FILE")" == 600 ]]

read_compose_env_value() {
  local key="$1"
  local -a matches=()
  mapfile -t matches < <(grep -E "^${key}=" "$DISTR_COMPOSE_ENV_FILE")
  ((${#matches[@]} == 1)) || return 1
  printf '%s' "${matches[0]#*=}"
}

export DISTR_TIMESTAMP_EVIDENCE_DIR="$(read_compose_env_value DISTR_TIMESTAMP_EVIDENCE_DIR)"
[[ "$DISTR_TIMESTAMP_EVIDENCE_DIR" = /* ]]
[[ -d "$DISTR_TIMESTAMP_EVIDENCE_DIR" && ! -L "$DISTR_TIMESTAMP_EVIDENCE_DIR" ]]
[[ "$DISTR_TIMESTAMP_AUTHOR" != CHANGE_ME_* ]]
[[ "$DISTR_TIMESTAMP_REVIEWER" != CHANGE_ME_* ]]
[[ "$DISTR_TIMESTAMP_EVIDENCE_REFERENCE" != CHANGE_ME_* ]]
[[ "$DISTR_TIMESTAMP_AUTHOR" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{1,127}$ ]]
[[ "$DISTR_TIMESTAMP_REVIEWER" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{1,127}$ ]]
[[ "$DISTR_TIMESTAMP_EVIDENCE_REFERENCE" =~ ^evidence:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]]
[[ "$DISTR_TIMESTAMP_AUTHOR" != "$DISTR_TIMESTAMP_REVIEWER" ]]

export DISTR_RELEASE_COMMIT="$(read_compose_env_value DISTR_RELEASE_COMMIT)"
export DISTR_IMAGE_DIGEST="$(read_compose_env_value DISTR_IMAGE_DIGEST)"
[[ "$DISTR_RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]]
[[ "$DISTR_IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]

evidence_checksum_file="$DISTR_TIMESTAMP_EVIDENCE_DIR/evidence-bundle.sha256"
[[ -f "$evidence_checksum_file" && ! -L "$evidence_checksum_file" ]]
[[ "$(stat -c '%a' -- "$evidence_checksum_file")" == 600 ]]
mapfile -t evidence_checksum_lines <"$evidence_checksum_file"
((${#evidence_checksum_lines[@]} == 1))
[[ "${evidence_checksum_lines[0]}" =~ ^(sha256:[0-9a-f]{64})\ \ timestamp-evidence-bundle-v1$ ]]
export DISTR_TIMESTAMP_EVIDENCE_CHECKSUM="${BASH_REMATCH[1]}"
[[ "$(read_compose_env_value DISTR_TIMESTAMP_EVIDENCE_CHECKSUM)" == "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" ]]

docker compose \
  --env-file "$DISTR_COMPOSE_ENV_FILE" \
  -f deploy/server-docker-compose/docker-compose.yml \
  --profile timestamp-operator \
  run --rm --user "$(id -u):$(id -g)" timestamp-operator \
  external-execution-timestamps seal-manifest \
  --input /evidence/reviewed-draft.json \
  --output /evidence/approved-manifest.json \
  --author "$DISTR_TIMESTAMP_AUTHOR" \
  --reviewer "$DISTR_TIMESTAMP_REVIEWER" \
  --evidence-reference "$DISTR_TIMESTAMP_EVIDENCE_REFERENCE" \
  --evidence-checksum "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" \
  --target-commit "$DISTR_RELEASE_COMMIT" \
  --target-image-digest "$DISTR_IMAGE_DIGEST"
```

The sealing command writes only the approved JSON.
The checksum equality above keeps the sealed manifest binding identical to the value that `timestamp-expand-apply`
later reloads from the restricted Compose environment.

Before apply, create its restricted checksum sidecar on the host:

```bash
export DISTR_TIMESTAMP_APPROVED_MANIFEST="$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json"
(
  set -Eeuo pipefail
  approved_name="$(basename -- "$DISTR_TIMESTAMP_APPROVED_MANIFEST")"
  sidecar_name="${approved_name}.sha256"
  cd -- "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  [[ -f "$approved_name" && ! -L "$approved_name" ]]
  [[ ! -e "$sidecar_name" && ! -L "$sidecar_name" ]]
  chmod 0600 -- "$approved_name"
  umask 077
  set -o noclobber
  sha256sum --text -- "$approved_name" >"$sidecar_name"
  chmod 0600 -- "$sidecar_name"
)
```

The explicit text-mode, relative `sha256sum` input writes the canonical `approved-manifest.json` basename expected by
apply. `noclobber` refuses a pre-existing sidecar.

Timestamp-expand apply additionally requires `DISTR_AUDIT_HISTORY_PROBE_URL` for that captured historical execution
and its read-only `DISTR_AUDIT_HISTORY_PROBE_TOKEN`. Keep the URL and token only in the restricted server `.env`; the
token is not part of the Jenkins artifact, and this guide intentionally contains no real credential assignment.

Apply only the sealed manifest and its pre-existing valid sidecar from the active evidence directory:

```bash
./deploy/server-docker-compose/deploy.sh \
  timestamp-expand-apply \
  "$DISTR_TIMESTAMP_APPROVED_MANIFEST" \
  "$DISTR_TIMESTAMP_EVIDENCE_DIR"
```

### Apply and Resume Contract

If apply is interrupted, rerun the identical command with the same active evidence directory and approved manifest.
Apply acquires the deployment lock, rechecks the fence, backup and isolated-restore evidence, target commit, image
digest, and manifest identity, and stops the fenced Hub even if Docker is restarting it. Before changing a discovered
fence-labelled acceptance container, apply validates its immutable container ID, derived name, fence label, and exact
target image, then revalidates them immediately before stop/removal. It removes only acceptance containers owned by
the active fence and asserts that every Hub writer is stopped before database work.

Resume is fail closed and accepts only these database phases:

| Phase                       | Exact accepted state                                                                                                                                                                                                 | Resume action                                                                                                                                                         |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Clean schema `137`          | `schema_migrations` is exactly `137:false`.                                                                                                                                                                          | Validate the sealed manifest, create or reuse the retained dry-run report, and run bounded `migrate --to 138` once.                                                   |
| Empty clean schema `138`    | No manifest or provenance rows; exactly one `MANIFEST_REQUIRED` expand-state row bound to source schema 137 and the approved execution, event, and raw-cell counts.                                                  | Validate the sealed manifest, create or reuse the retained dry-run report, and continue without repeating migration.                                                  |
| Verified clean schema `138` | Exactly one manifest matching the approved ID in `VERIFIED` state; provenance count equals the approved raw-cell count; the same single `MANIFEST_REQUIRED` row and exact source-schema and transition counts exist. | Require and validate the retained dry-run report, skip live `validate-manifest`, and prove the real apply is exactly idempotent before continuing startup acceptance. |

Dirty schemas, unsupported versions, extra or partial manifest/provenance/state rows, a different approved root, or
count conflicts are refused before preview, migration, apply, or Hub start. Apply then verifies the immutable ledger,
proves final counts are not below the fenced source counts, checks every original/raw-and-instant lifecycle pair,
checks expand startup compatibility, starts only `serve --migrate=false`, and clears the fence only after post-start
checks pass.

Dry-run and real-apply reports are protected recovery records:

- A report with its sidecar is checksum-verified and content-validated before reuse.
- A valid deployment-user-owned `0600` report whose sidecar is missing is content-validated before a new sidecar is
  created.
- A sidecar without its report, a symlink, corrupt content, ownership/mode mismatch, or manifest/count conflict is
  refused.
- When no real-apply report exists for an already verified database, apply writes a temporary result, validates its
  exact approved-manifest binding and `idempotent: true`, and only then publishes the report and sidecar. The retained
  dry-run report is mandatory in this phase; if both it and its sidecar are missing, recovery is refused.

Rerunning `timestamp-expand-apply` is also the recovery path for a fence-owned orphan acceptance container. The
`timestamp-expand-cancel` command does not remove those containers and remains a schema-137-only escape path.

### Audited Dirty-Marker Recovery

Use dirty-marker recovery only when migration 138 was interrupted and `schema_migrations` is dirty at version 137
or 138. It repairs the migration marker after proving the exact catalog; it does not run DDL, change
external-execution data, start Hub, persist timestamp compatibility, or clear the writer fence. Never edit
`schema_migrations` directly and never run raw `migrate force`.

The supported command is:

```bash
./deploy/server-docker-compose/deploy.sh \
  timestamp-expand-recover-dirty \
  <approved-manifest-or-> \
  <evidence-dir> \
  <operator-identity> \
  <reason>
```

The literal `-` is the no-manifest sentinel, not standard input. Otherwise, pass the exact `APPROVED` document
required by the catalog: either the root document or, for already verified evidence, the approved form whose content
matches the stored `VERIFIED` tip. Do not pass a document whose file state is `VERIFIED`; the recovery CLI accepts an
`APPROVED` manifest document and independently proves it matches the stored tip. The evidence directory and active
fence must be the same ones used by the interrupted timestamp-expand operation.

No-manifest recovery is available only when a complete captured evidence bundle and its active
`CAPTURED_WRITERS_STOPPED` fence already existed before the interrupted migration. The wrapper cannot retrofit an
interrupted ordinary zero-history `release`, because that path creates neither the timestamp fence nor the capture
bundle. Never manufacture those records after failure. Keep writers stopped and restore the verified backup or
escalate when the pre-existing fence and bundle are absent.

Choose the argument and next step from the proven catalog and evidence state, not from the observed dirty marker:

| Exact proven catalog/evidence state                                              | Manifest argument                                                                   | Marker repaired to | Required next step                                                                                                                                                                                                                                                                                                                                            |
| -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PREDECESSOR_137`, empty history, with the pre-existing staged fence/capture     | `-`                                                                                 | clean 137          | Retain the original immutable target handoff, then run normal `timestamp-expand-cancel` after its unchanged-schema checks pass. Stop there for rollback. To proceed forward, copy the target handoff's exact `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and `DISTR_IMAGE_DIGEST` back into `.env`, run `check`, then restart ordinary zero-history `release`. |
| `PREDECESSOR_137`, non-empty history                                             | Exact approved root manifest                                                        | clean 137          | Rerun normal `timestamp-expand-apply` with the same approved manifest and evidence.                                                                                                                                                                                                                                                                           |
| `EXPAND_138`, durable `ZERO_HISTORY`, with the pre-existing staged fence/capture | `-`                                                                                 | clean 138          | Stop and escalate. Hub remains stopped and fenced because no current no-manifest finalizer can safely complete this state.                                                                                                                                                                                                                                    |
| `EXPAND_138`, `MANIFEST_REQUIRED` before apply or a stored `VERIFIED` tip        | Exact `APPROVED` root document or exact `APPROVED` document matching the stored tip | clean 138          | Rerun normal `timestamp-expand-apply` with the identical approved content and evidence so its normal verification and finalization gates run.                                                                                                                                                                                                                 |

Partial, mixed, unknown, or contract-gated catalogs are refused. The observed dirty marker may be 137 or 138 in
either accepted catalog shape; it is evidence of interruption, not the force target. `PREDECESSOR_137` alone selects
marker 137, and `EXPAND_138` alone selects marker 138.

Use a stable non-secret operator identity of 1-128 characters matching
`[A-Za-z0-9][A-Za-z0-9._@:+-]{0,127}`. Supply the reason as one quoted, trimmed, printable, non-secret argument of
1-256 characters. The reason must be single-line and must not include a URL, connection string, credential,
token-like assignment, or local/remote path.

The recovery database-lock timeout defaults to `2m`. To change it, export a positive whole-number `s`, `m`, or `h`
duration in the host shell before invoking the wrapper:

```bash
export DISTR_TIMESTAMP_DIRTY_RECOVERY_LOCK_TIMEOUT=2m
```

The wrapper reads this value before loading the Compose `.env`, so setting it only in `.env` does not configure the
recovery command.

Recovery runs in this fixed order:

1. Acquire the deployment lock and revalidate the active `CAPTURED_WRITERS_STOPPED` fence and exact evidence
   directory.
2. Revalidate the fenced target image reference, digest, release commit, Compose configuration, and stopped Hub and
   acceptance writers.
3. Reproduce the checksummed evidence bundle and fence identifier; when a manifest is supplied, revalidate and
   stage that exact checksummed document.
4. Create or validate the retained recovery plan, binding the catalog checksum, selected force target, fence,
   manifest mode, operator identity, and reason.
5. Durably archive any exact interrupted result reservation before reuse or retry.
6. Reuse a valid retained result without another operator call, or invoke recovery Apply exactly once and retain
   and validate its result.
7. Assert again that all Hub writers remain stopped, then return without finalization.

Retain these deployment-user-owned recovery records in the mode-`0700` evidence directory:

- `timestamp-dirty-recovery-plan.json` and `timestamp-dirty-recovery-plan.json.sha256`;
- `timestamp-dirty-recovery-result.json` and `timestamp-dirty-recovery-result.json.sha256`; and
- `timestamp-dirty-recovery-result.interrupted-NNN.partial` and its `.sha256` sidecar for every interrupted result
  reservation.

Every recovery artifact and sidecar must remain a regular non-symlink file with mode `0600`. A validated final plan
or result that is missing only its sidecar may have that sidecar repaired create-new. An orphan sidecar, invalid
checksum, ownership/mode mismatch, unsafe path, noncontiguous partial archive, or temp/final mismatch is refused.
Before removing an interrupted temp, the wrapper publishes and verifies a create-new numbered archive and sidecar,
syncs both files and the directory, removes the temp, and syncs the directory again.

A retry must use the same manifest mode (`-` or a file) and exact approved manifest content/checksum, plus the same
evidence directory, operator identity, reason, and active fence. The source pathname is not part of the recovery
plan; a different pathname is acceptable only when it supplies the byte-identical approved document and therefore
the same staged checksum. A valid retained plan can continue when the marker is either still at its exact planned
dirty value or is already clean at the catalog-selected force target. A valid retained result is checksum- and
content-validated against the live clean marker and reused without another recovery Apply.

When Apply observes the exact planned dirty status and repairs it, the retained result records `action: "FORCED"`.
When an exact retained-plan retry finds the catalog-selected marker already clean, Apply performs no Force and
records `action: "OBSERVED_ALREADY_CLEAN"`. Both results record `result: "SUCCEEDED"` and an exact clean
`postStatus` whose version equals `forcedVersion`; neither result authorizes release finalization.

Successful recovery is only marker repair. Keep Hub stopped and the fence active until normal
`timestamp-expand-apply` or `timestamp-expand-cancel` completes its own gates. After clean empty-history
`PREDECESSOR_137`, guarded cancel exits the staged fence and persists the previous/source image identity. Before a
forward ordinary zero-history release, restore all three target identity values from the same archived immutable
handoff artifact described above; never mix artifacts or rely on the now-cleared fence. For `EXPAND_138`/`ZERO_HISTORY`,
no finalizer currently exists: do not start Hub or clear the fence manually; retain all evidence and escalate for an
independently reviewed finalization procedure.

Cancel is allowed for unchanged clean schema 137 before migration starts. After audited recovery, it is allowed only
for an exact empty-history `PREDECESSOR_137` outcome when the normal unchanged-schema checks still pass:

```bash
./deploy/server-docker-compose/deploy.sh timestamp-expand-cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"
```

After manifest application, do not run a schema down migration. The legacy columns remain the expand release's
read path, but operational recovery uses the retained previous image only with a compatible schema or restores the
verified PostgreSQL and object-store backups first. Timestamp migration, audit retention, and tutorial/demo cleanup
are separate operations.

## Optional Server Build

The matrix above remains authoritative. Use this combined command only on an isolated, non-client server when Jenkins
is unavailable for candidate creation. It builds on that server, pushes to ECR, resolves the digest, and releases the
same digest-pinned image into that isolated runtime:

```bash
./deploy/server-docker-compose/deploy.sh deploy
```

This command is not a live-promotion shortcut and is not permitted on a live/client environment for the current
candidate. Retain its immutable handoff and checksummed isolated proof; any later live promotion uses the already
proven digest through the separately authorized `check` and `release` action.

That optional command does this:

1. Acquires the deployment lock and refuses an active timestamp-expand fence.
2. Installs pinned build tools with `mise install`.
3. Builds the community frontend and Hub from source.
4. Copies `dist/distr` to the architecture-specific name required by `Dockerfile.hub`.
5. Builds the Docker image tagged as `DISTR_IMAGE:DISTR_IMAGE_TAG`.
6. Logs in to AWS ECR and pushes the image.
7. Resolves the pushed tag to an ECR digest.
8. Atomically updates `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and `DISTR_IMAGE_DIGEST` in the deployment environment.
9. Writes `dist/release-${DISTR_IMAGE_TAG}.env` from that same resolved image identity.
10. Validates the Docker Compose config and immutable release identity.
11. Pulls the digest-pinned image from ECR for Compose.
12. Starts PostgreSQL and RustFS.
13. Runs the read-only migration preflight while the existing Hub writers remain online.
14. Stops Hub and verifies that its writers are stopped.
15. Creates and restore-verifies PostgreSQL and RustFS backups when data already exists.
16. Runs `distr migrate` explicitly.
17. Starts Hub with `serve --migrate=false`.
18. Waits for `http://127.0.0.1:${DISTR_HTTP_PORT}/ready`.

## Reverse Proxy

The Compose file binds Hub and registry to localhost only:

```text
127.0.0.1:8080 -> Hub
127.0.0.1:8585 -> built-in OCI registry
```

For the current `distr.emlotech.com` setup, use one Nginx UI host with:

- `/v2/` -> `http://distr-hub:8585`
- `/` -> `http://distr-hub:8080`

`distr-hub` is a Docker network alias. Set `NGINX_UI_NETWORK=services` so the Hub joins the same Docker network as
the existing `nginx-ui` container.

Use Nginx, Caddy, Traefik, or another TLS proxy in front. For Nginx, start from:

```bash
sudo cp deploy/server-docker-compose/nginx.example.conf /etc/nginx/sites-available/distr.conf
sudo ln -s /etc/nginx/sites-available/distr.conf /etc/nginx/sites-enabled/distr.conf
sudo nginx -t
sudo systemctl reload nginx
```

Update certificates and hostnames before reloading. Enable HSTS only after HTTPS is verified.

## Day-2 Commands

Log in to ECR:

```bash
./deploy/server-docker-compose/deploy.sh ecr-login
```

Pull the configured ECR image:

```bash
./deploy/server-docker-compose/deploy.sh pull
```

Validate Compose config:

```bash
./deploy/server-docker-compose/deploy.sh config
```

Run the server-side release flow after Jenkins has published the digest and `DISTR_IMAGE_REF` is updated:

```bash
./deploy/server-docker-compose/deploy.sh release
```

Check service status:

```bash
./deploy/server-docker-compose/deploy.sh ps
```

Follow logs:

```bash
./deploy/server-docker-compose/deploy.sh logs hub
```

Run a manual health check:

```bash
./deploy/server-docker-compose/deploy.sh health
```

Run only backup with a deployment-user-owned directory. Provisioning the directory may require an administrator, but
run the deployment command as the same non-root deployment user used for releases:

```bash
sudo install -d -m 0700 -o "$(id -un)" -g "$(id -gn)" /var/backups/distr
BACKUP_DIR=/var/backups/distr ./deploy/server-docker-compose/deploy.sh backup
```

Run only migrations:

```bash
./deploy/server-docker-compose/deploy.sh migrate
```

Run artifact cleanup once:

```bash
./deploy/server-docker-compose/deploy.sh cleanup-artifacts
```

## Upgrade

For a later commit or tag, Jenkins should checkout the reviewed source and run the Jenkins build/push flow again:

```bash
git fetch --all --tags
git checkout <new-tag-or-commit>
./deploy/server-docker-compose/deploy.sh image-check
./deploy/server-docker-compose/deploy.sh build
./deploy/server-docker-compose/deploy.sh push
```

On the server, edit `deploy/server-docker-compose/.env` and copy the new release identity from one archived Jenkins
release artifact:

```text
DISTR_IMAGE_TAG=2026-06-24-rc1
DISTR_IMAGE_REF=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community@sha256:<image-digest>
DISTR_RELEASE_COMMIT=<40-lowercase-hex-source-commit>
DISTR_IMAGE_DIGEST=sha256:<64-lowercase-hex-image-digest>
```

```bash
./deploy/server-docker-compose/deploy.sh release
```

The script backs up before migrations. Keep the backup files and the previous ECR image digest until the release is accepted.

If Jenkins is unavailable and you must build directly on the server, use the optional `deploy` command instead.

## Rollback

Application-only rollback to a previous ECR digest reference:

```bash
./deploy/server-docker-compose/deploy.sh rollback <previous-image-ref>
```

This updates `DISTR_IMAGE_REF`, pulls the previous image from ECR, restarts Hub, and runs the health check.
Use it only when the new database schema is backward-compatible with the previous binary.

If a migration is incompatible:

1. Stop Hub.
2. Restore the pre-upgrade PostgreSQL backup.
3. Restore RustFS or external object storage if the release changed artifact data.
4. Start the previous image digest.
5. Run health checks and a smoke test.

Do not automate `distr migrate --down`; the command explicitly warns that it purges the database.

## Backup Notes

The script writes PostgreSQL custom-format dumps and RustFS volume tarballs to
`deploy/server-docker-compose/backups` by default. For a separate server path, create `BACKUP_DIR` as a real,
non-symlink directory with mode `0700`, owned by the deployment user, beneath a secure parent directory. The script
refuses a root-owned directory when the deployment command runs as a non-root deployment user.

Database backup alone is not enough if you use the local RustFS registry. Back up both PostgreSQL and object storage,
or use provider snapshots. External S3-compatible storage with versioning is safer than keeping database and artifacts
on the same physical server.

## Security Notes

- `.env` stores plaintext secrets; keep it mode `0600`, owned by the deployment user, and out of Git.
- Prefer an EC2 instance role, ECS task role, or Jenkins/OIDC role over long-lived AWS access keys on the server.
- Keep the ECR repository private and deploy digest-pinned image references. The helper refuses `DISTR_IMAGE_TAG=latest`.
- Replace all generated local secrets before production if your organization requires centrally managed credentials.
- Use external S3 and managed PostgreSQL for stronger durability; this starter Compose stack uses local RustFS by default.
- The included RustFS image is beta; evaluate it before production.
- Building on the server is less controlled than building in Jenkins and deploying by digest.
- `USER_EMAIL_VERIFICATION_REQUIRED=false` is acceptable only until mail is configured.
- Keep `REGISTRATION=enabled` only long enough to create the first admin account.
