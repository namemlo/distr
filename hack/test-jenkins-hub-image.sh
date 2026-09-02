#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
HELPER="${REPO_ROOT}/deploy/jenkins/publish-hub-image.sh"
JENKINSFILE="${REPO_ROOT}/deploy/jenkins/Jenkinsfile.hub-image"
DEPLOY_SCRIPT="${REPO_ROOT}/deploy/server-docker-compose/deploy.sh"
RELEASE_WORKFLOW="${REPO_ROOT}/.github/workflows/community-release-hardening.yaml"
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT

PASS_COUNT=0

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  printf 'ok %d - %s\n' "$PASS_COUNT" "$1"
}

fail() {
  printf 'not ok - %s\n' "$1" >&2
  exit 1
}

expect_failure() {
  local name="$1" expected="$2"
  shift 2
  local output status
  set +e
  output="$("$@" 2>&1)"
  status=$?
  set -e
  ((status != 0)) || fail "$name unexpectedly succeeded"
  [[ "$output" == *"$expected"* ]] || {
    printf '%s\n' "$output" >&2
    fail "$name did not report: $expected"
  }
  pass "$name"
}

[[ -x "$HELPER" ]] || fail "missing executable helper: $HELPER"
[[ -f "$JENKINSFILE" ]] || fail "missing Jenkins pipeline: $JENKINSFILE"
[[ -x "$DEPLOY_SCRIPT" ]] || fail "missing executable deployment helper: $DEPLOY_SCRIPT"
[[ -f "$RELEASE_WORKFLOW" ]] || fail "missing release workflow: $RELEASE_WORKFLOW"

COMMIT='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
OTHER_COMMIT='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
DIGEST='sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
LOCAL_IMAGE_ID='sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'
COMMIT_EPOCH='1784376000'
SOURCE_URL='https://git.example.invalid/distr/project'
ACCOUNT_ID="$(printf '1%.0s' {1..12})"
IMAGE="${ACCOUNT_ID}.dkr.ecr.test-region-1.amazonaws.com/distr-hub"
TAG='candidate-aaaaaaaa-20260718t120000z'

expect_failure \
  "candidate tag rejects a non-full commit" \
  "40 lowercase hexadecimal" \
  "$HELPER" candidate-tag abc123

new_fixture() {
  local name="$1"
  FIXTURE="$TMP/$name"
  MOCK_BIN="$FIXTURE/mock-bin"
  EVENTS="$FIXTURE/events"
  AWS_CALLS="$FIXTURE/aws-calls"
  PUSHED="$FIXTURE/pushed"
  mkdir -p \
    "$FIXTURE/deploy/jenkins" \
    "$FIXTURE/deploy/server-docker-compose" \
    "$FIXTURE/hack" \
    "$FIXTURE/dist" \
    "$MOCK_BIN"
  cp "$HELPER" "$FIXTURE/deploy/jenkins/publish-hub-image.sh"
  cp "$REPO_ROOT/hack/pr050-validate-control-plane-evidence.mjs" "$FIXTURE/hack/"
  chmod +x "$FIXTURE/deploy/jenkins/publish-hub-image.sh"
  printf 'test private key\n' >"$FIXTURE/provenance.key"
  printf 'test public key\n' >"$FIXTURE/provenance.pub"
  : >"$EVENTS"
  : >"$AWS_CALLS"

  cat >"$FIXTURE/deploy/server-docker-compose/deploy.sh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'deploy:%s\n' "$1" >>"$MOCK_EVENTS"
case "$1" in
  image-check)
    ;;
  build)
    printf 'binary\n' >dist/distr-amd64
    node <<'NODE'
const fs = require('node:fs');
const checksum = 'e'.repeat(64);
const rootId = 'SPDXRef-Package-distr-hub';
const document = {
  SPDXID: 'SPDXRef-DOCUMENT',
  creationInfo: {created: '2026-07-18T12:00:00Z'},
  documentDescribes: [rootId],
  documentNamespace: `https://distr.sh/spdx/hub-image/${process.env.RELEASE_COMMIT}/${process.env.DISTR_IMAGE_TAG}/${process.env.MOCK_LOCAL_IMAGE_ID.slice(7)}`,
  packages: [{
    SPDXID: rootId,
    checksums: [{algorithm: 'SHA256', checksumValue: checksum}],
    externalRefs: [{
      referenceCategory: 'PACKAGE-MANAGER',
      referenceLocator: `pkg:oci/${encodeURIComponent(process.env.DISTR_IMAGE)}@${encodeURIComponent(`sha256:${checksum}`)}?arch=amd64&tag=${encodeURIComponent(process.env.DISTR_IMAGE_TAG)}`,
      referenceType: 'purl',
    }],
    name: process.env.DISTR_IMAGE,
    primaryPackagePurpose: 'CONTAINER',
    versionInfo: process.env.DISTR_IMAGE_TAG,
  }],
  relationships: [{relatedSpdxElement: rootId, relationshipType: 'DESCRIBES', spdxElementId: 'SPDXRef-DOCUMENT'}],
  spdxVersion: 'SPDX-2.3',
};
const canonicalize = (value) => {
  if (Array.isArray(value)) return value.map(canonicalize).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)));
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonicalize(value[key])]));
  return value;
};
const sbom = `dist/release-${process.env.DISTR_IMAGE_TAG}.spdx.json`;
fs.writeFileSync(sbom, `${JSON.stringify(canonicalize(document))}\n`);
const sbomSha = require('node:crypto').createHash('sha256').update(fs.readFileSync(sbom)).digest('hex');
const binding = {
  schemaVersion: 'distr.image-sbom-binding/v1',
  sourceCommit: process.env.RELEASE_COMMIT,
  taggedImageRef: `${process.env.DISTR_IMAGE}:${process.env.DISTR_IMAGE_TAG}`,
  localImageId: process.env.MOCK_LOCAL_IMAGE_ID,
  platform: 'linux/amd64',
  sbomFile: `dist/release-${process.env.DISTR_IMAGE_TAG}.spdx.json`,
  sbomSha256: `sha256:${sbomSha}`,
};
fs.writeFileSync(`dist/release-${process.env.DISTR_IMAGE_TAG}.image-binding.json`, `${JSON.stringify(binding)}\n`);
NODE
    (
      cd dist
      printf '%s  %s\n' \
        "$(sha256sum "release-${DISTR_IMAGE_TAG}.spdx.json" | awk '{print $1}')" \
        "release-${DISTR_IMAGE_TAG}.spdx.json" >"release-${DISTR_IMAGE_TAG}.spdx.json.sha256"
      printf '%s  %s\n' \
        "$(sha256sum "release-${DISTR_IMAGE_TAG}.image-binding.json" | awk '{print $1}')" \
        "release-${DISTR_IMAGE_TAG}.image-binding.json" >"release-${DISTR_IMAGE_TAG}.image-binding.json.sha256"
    )
    ;;
  push)
    touch "$MOCK_PUSHED"
    mkdir -p "$RELEASE_METADATA_DIR"
    cat >"$RELEASE_METADATA_DIR/release-${DISTR_IMAGE_TAG}.env" <<HANDOFF
AWS_REGION=${AWS_REGION}
ECR_REPOSITORY=${ECR_REPOSITORY}
DISTR_IMAGE=${DISTR_IMAGE}
DISTR_IMAGE_TAG=${DISTR_IMAGE_TAG}
DISTR_IMAGE_REF=${DISTR_IMAGE}@${MOCK_DIGEST}
SOURCE_COMMIT=${RELEASE_COMMIT}
DISTR_RELEASE_COMMIT=${RELEASE_COMMIT}
DISTR_IMAGE_DIGEST=${MOCK_DIGEST}
DISTR_LOCAL_IMAGE_ID=${MOCK_LOCAL_IMAGE_ID}
DISTR_IMAGE_PLATFORM=linux/amd64
DISTR_SBOM_FILE=dist/release-${DISTR_IMAGE_TAG}.spdx.json
DISTR_SBOM_SHA256=$(sha256sum "dist/release-${DISTR_IMAGE_TAG}.spdx.json" | awk '{print "sha256:" $1}')
DISTR_SBOM_BINDING_FILE=dist/release-${DISTR_IMAGE_TAG}.image-binding.json
DISTR_SBOM_BINDING_SHA256=$(sha256sum "dist/release-${DISTR_IMAGE_TAG}.image-binding.json" | awk '{print "sha256:" $1}')
HANDOFF
    ;;
  *)
    printf 'unexpected deploy command: %s\n' "$1" >&2
    exit 91
    ;;
esac
EOF
  chmod +x "$FIXTURE/deploy/server-docker-compose/deploy.sh"

  cat >"$MOCK_BIN/git" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case "$*" in
  "rev-parse HEAD") printf '%s\n' "$MOCK_HEAD" ;;
  "status --porcelain=v1 --untracked-files=all") ;;
  "config --get remote.origin.url") printf '%s\n' "$MOCK_SOURCE_URL" ;;
  "show -s --format=%ct $MOCK_HEAD") printf '%s\n' "$MOCK_COMMIT_EPOCH" ;;
  *) printf 'unexpected git command: %s\n' "$*" >&2; exit 92 ;;
esac
EOF

cat >"$MOCK_BIN/aws" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'aws:%s\n' "$*" >>"$MOCK_EVENTS"
if [[ "$*" == ecr\ describe-repositories* ]]; then
  printf '%s\n' "$MOCK_REPOSITORY_MUTABILITY"
  exit 0
fi
if [[ "$*" != ecr\ describe-images* ]]; then
  printf 'unexpected aws command\n' >&2
  exit 93
fi
count="$(wc -l <"$MOCK_AWS_CALLS")"
printf 'call\n' >>"$MOCK_AWS_CALLS"
case "$MOCK_TAG_STATE" in
  existing)
    printf '%s\n' "$MOCK_DIGEST"
    ;;
  race)
    if ((count == 0)); then
      printf 'ImageNotFoundException: tag is absent\n' >&2
      exit 254
    fi
    printf '%s\n' "$MOCK_DIGEST"
    ;;
  absent)
    if [[ -e "$MOCK_PUSHED" ]]; then
      printf '%s\n' "$MOCK_DIGEST"
    else
      printf 'ImageNotFoundException: tag is absent\n' >&2
      exit 254
    fi
    ;;
  *)
    printf 'AccessDeniedException: fail closed\n' >&2
    exit 254
    ;;
esac
EOF

  cat >"$MOCK_BIN/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'docker:%s\n' "$*" >>"$MOCK_EVENTS"
case "$1 $2" in
  "info --format")
    printf 'linux/amd64\n'
    ;;
  "image inspect")
    case "$3:$4" in
      *revision*) printf '%s\n' "${MOCK_REVISION:-$MOCK_HEAD}" ;;
      *source*) printf '%s\n' "$MOCK_SOURCE_URL" ;;
      --format:*Id*Architecture*) printf '%s|linux/amd64\n' "$MOCK_LOCAL_IMAGE_ID" ;;
      *Architecture*) printf 'linux/amd64\n' ;;
      *) printf 'unexpected inspect format: %s %s\n' "$3" "${4:-}" >&2; exit 94 ;;
    esac
    ;;
  *)
    if [[ "$1" == pull ]]; then
      [[ "$2" == *@"$MOCK_DIGEST" ]] || exit 95
    else
      printf 'unexpected docker command: %s\n' "$*" >&2
      exit 96
    fi
    ;;
esac
EOF

  cat >"$MOCK_BIN/cosign" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case "$1" in
  sign-blob)
    output=''
    while (($#)); do
      if [[ "$1" == --output-signature ]]; then
        output="$2"
        shift 2
        continue
      fi
      shift
    done
    [[ -n "$output" ]]
    printf 'signed-provenance\n' >"$output"
    ;;
  verify-blob)
    [[ "${MOCK_COSIGN_VERIFY_FAIL:-false}" != true ]]
    ;;
  *)
    printf 'unexpected cosign command: %s\n' "$1" >&2
    exit 97
    ;;
esac
EOF

  chmod +x "$MOCK_BIN/git" "$MOCK_BIN/aws" "$MOCK_BIN/docker" "$MOCK_BIN/cosign"
}

run_publish() {
  local root="$1"
  shift
  env \
    PATH="$MOCK_BIN:$PATH" \
    RELEASE_COMMIT="$COMMIT" \
    AWS_REGION='test-region-1' \
    ECR_REPOSITORY='distr-hub' \
    DISTR_IMAGE="$IMAGE" \
    DISTR_IMAGE_TAG="$TAG" \
    MOCK_HEAD="$COMMIT" \
    MOCK_SOURCE_URL="$SOURCE_URL" \
    MOCK_DIGEST="$DIGEST" \
    MOCK_LOCAL_IMAGE_ID="$LOCAL_IMAGE_ID" \
    MOCK_COMMIT_EPOCH="$COMMIT_EPOCH" \
    MOCK_EVENTS="$EVENTS" \
    MOCK_AWS_CALLS="$AWS_CALLS" \
    MOCK_PUSHED="$PUSHED" \
    MOCK_TAG_STATE='absent' \
    MOCK_REPOSITORY_MUTABILITY='IMMUTABLE' \
    AWS_ACCESS_KEY_ID='test-access-id' \
    AWS_SECRET_ACCESS_KEY='credential-must-not-appear' \
    PROVENANCE_SIGNING_KEY="$root/provenance.key" \
    PROVENANCE_SIGNING_PUBLIC_KEY="$root/provenance.pub" \
    COSIGN_PASSWORD='placeholder' \
    "$@" \
    "$root/deploy/jenkins/publish-hub-image.sh" publish
}

new_fixture invalid-tag
expect_failure \
  "publish rejects mutable tag" \
  "immutable candidate tag" \
  env \
    PATH="$MOCK_BIN:$PATH" \
    RELEASE_COMMIT="$COMMIT" \
    AWS_REGION='test-region-1' \
    ECR_REPOSITORY='distr-hub' \
    DISTR_IMAGE="$IMAGE" \
    DISTR_IMAGE_TAG='latest' \
    "$FIXTURE/deploy/jenkins/publish-hub-image.sh" publish

new_fixture wrong-checkout
expect_failure \
  "publish rejects checkout at a different commit" \
  "checkout does not match RELEASE_COMMIT" \
  run_publish "$FIXTURE" MOCK_HEAD="$OTHER_COMMIT"

new_fixture mismatched-repository
expect_failure \
  "publish rejects an image URI for a different repository path" \
  "must exactly match" \
  run_publish "$FIXTURE" DISTR_IMAGE="${IMAGE%/distr-hub}/other/distr-hub"
! grep -q '^deploy:build$' "$EVENTS" || fail "mismatched repository reached build"
pass "mismatched repository performs no build"

new_fixture existing-tag
expect_failure \
  "publish refuses an existing remote tag before build" \
  "already exists" \
  run_publish "$FIXTURE" MOCK_TAG_STATE=existing
! grep -q '^deploy:build$' "$EVENTS" || fail "existing tag reached build"
! grep -q '^deploy:push$' "$EVENTS" || fail "existing tag reached push"
pass "existing tag performs no build or push"

new_fixture mutable-repository
expect_failure \
  "publish refuses a repository that allows tag replacement" \
  "must enforce immutable tags" \
  run_publish "$FIXTURE" MOCK_REPOSITORY_MUTABILITY=MUTABLE
! grep -q '^deploy:build$' "$EVENTS" || fail "mutable repository reached build"
pass "mutable repository performs no build"

new_fixture collision-before-push
expect_failure \
  "publish rechecks and refuses a tag created during build" \
  "already exists" \
  run_publish "$FIXTURE" MOCK_TAG_STATE=race
grep -qx 'deploy:build' "$EVENTS" || fail "race case did not build once"
! grep -q '^deploy:push$' "$EVENTS" || fail "race case pushed after collision"
pass "pre-push collision performs no push"

new_fixture wrong-revision
expect_failure \
  "publish rejects an image with the wrong OCI revision" \
  "OCI revision label does not match" \
  run_publish "$FIXTURE" MOCK_REVISION="$OTHER_COMMIT"
! grep -q '^deploy:push$' "$EVENTS" || fail "wrong revision reached push"
pass "wrong OCI identity performs no push"

new_fixture invalid-provenance-signature
expect_failure \
  "publish rejects provenance that cosign cannot verify" \
  "cosign could not verify the provenance signature" \
  run_publish "$FIXTURE" MOCK_COSIGN_VERIFY_FAIL=true
pass "provenance signing fails closed before handoff"

new_fixture success
success_output="$(run_publish "$FIXTURE" 2>&1)"
[[ "$success_output" != *'credential-must-not-appear'* ]] || fail "credential leaked to output"
mapfile -t deploy_events < <(grep '^deploy:' "$EVENTS")
[[ "${deploy_events[*]}" == 'deploy:image-check deploy:build deploy:push' ]] || {
  printf 'deploy events: %s\n' "${deploy_events[*]}" >&2
  fail "helper did not use image-check/build/push exactly once"
}
! grep -Eq '^deploy:(deploy|release)$' "$EVENTS" || fail "helper invoked deployment"
handoff="$FIXTURE/dist/release-${TAG}.env"
sidecar="${handoff}.sha256"
[[ -f "$handoff" && -f "$sidecar" ]] || fail "handoff or checksum missing"
sbom="$FIXTURE/dist/release-${TAG}.spdx.json"
binding="$FIXTURE/dist/release-${TAG}.image-binding.json"
provenance="$FIXTURE/dist/release-${TAG}.intoto.json"
signature="${provenance}.sig"
[[ -s "$sbom" && -s "${sbom}.sha256" ]] || fail "real image SBOM or checksum missing"
[[ -s "$binding" && -s "${binding}.sha256" ]] || fail "image SBOM binding or checksum missing"
[[ -s "$provenance" && -s "${provenance}.sha256" ]] || fail "provenance attestation or checksum missing"
[[ -s "$signature" && -s "${signature}.sha256" ]] || fail "provenance signature or checksum missing"
sbom_sha="$(sha256sum "$sbom" | awk '{print $1}')"
binding_sha="$(sha256sum "$binding" | awk '{print $1}')"
provenance_sha="$(sha256sum "$provenance" | awk '{print $1}')"
signature_sha="$(sha256sum "$signature" | awk '{print $1}')"
expected_handoff="$(
  printf 'DISTR_IMAGE_REF=%s@%s\n' "$IMAGE" "$DIGEST"
  printf 'DISTR_RELEASE_COMMIT=%s\n' "$COMMIT"
  printf 'DISTR_IMAGE_DIGEST=%s\n' "$DIGEST"
  printf 'DISTR_LOCAL_IMAGE_ID=%s\n' "$LOCAL_IMAGE_ID"
  printf 'DISTR_IMAGE_PLATFORM=linux/amd64\n'
  printf 'DISTR_SBOM_FILE=dist/release-%s.spdx.json\n' "$TAG"
  printf 'DISTR_SBOM_SHA256=sha256:%s\n' "$sbom_sha"
  printf 'DISTR_SBOM_BINDING_FILE=dist/release-%s.image-binding.json\n' "$TAG"
  printf 'DISTR_SBOM_BINDING_SHA256=sha256:%s\n' "$binding_sha"
  printf 'DISTR_PROVENANCE_REF=dist/release-%s.intoto.json\n' "$TAG"
  printf 'DISTR_PROVENANCE_SHA256=sha256:%s\n' "$provenance_sha"
  printf 'DISTR_PROVENANCE_SIGNATURE_REF=dist/release-%s.intoto.json.sig\n' "$TAG"
  printf 'DISTR_PROVENANCE_SIGNATURE_SHA256=sha256:%s\n' "$signature_sha"
  printf 'DISTR_ATTESTATION_REF=dist/release-%s.intoto.json.sig@sha256:%s\n' "$TAG" "$signature_sha"
)"
[[ "$(cat "$handoff")" == "$expected_handoff" ]] || {
  cat "$handoff" >&2
  fail "handoff is not the exact image-evidence contract"
}
(cd "$(dirname "$handoff")" && sha256sum -c "$(basename "$sidecar")" >/dev/null) ||
  fail "handoff checksum is invalid"
grep -q "^docker:pull ${IMAGE}@${DIGEST}$" "$EVENTS" ||
  fail "digest-pinned image was not pulled"
node - "$provenance" "$COMMIT" "$DIGEST" "$LOCAL_IMAGE_ID" "$binding_sha" <<'NODE' ||
const fs = require('node:fs');
const [file, commit, digest, localImageId, bindingSha] = process.argv.slice(2);
const statement = JSON.parse(fs.readFileSync(file, 'utf8'));
if (
  statement._type !== 'https://in-toto.io/Statement/v1' ||
  statement.predicateType !== 'https://slsa.dev/provenance/v1' ||
  statement.subject?.[0]?.digest?.sha256 !== digest.slice('sha256:'.length) ||
  statement.predicate?.buildDefinition?.resolvedDependencies?.[0]?.digest?.sha1 !== commit ||
  statement.predicate?.buildDefinition?.externalParameters?.localImageId !== localImageId ||
  statement.predicate?.buildDefinition?.resolvedDependencies?.[2]?.digest?.sha256 !== bindingSha
) {
  process.exit(1);
}
NODE
  fail "provenance statement is not bound to commit and image digest"
pass "successful publication co-binds image ID, digest, SBOM, binding, provenance, and handoff"

write_evidence_fixtures() {
  local root="$1"
  node - "$root" "$COMMIT" <<'NODE'
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const [root, commit] = process.argv.slice(2);
const sha = (text) => `sha256:${crypto.createHash('sha256').update(text).digest('hex')}`;
const stable = (value) => {
  if (Array.isArray(value)) return `[${value.map(stable).join(',')}]`;
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stable(value[key])}`).join(',')}}`;
  }
  return JSON.stringify(value);
};
const scenarioIds = [
  'migration-file-integrity',
  'postgres-runtime-version',
  'migration-138-to-170-upgrade',
  'clean-install',
  'single-step-down-and-refusal-contracts',
  'checkpoint-idempotency-and-cursor-resume',
  'v1-flags-off',
  'mixed-v1-v2',
  'v2-history-flags-off',
  'upstream-compatibility',
];
function migration(version) {
  const report = {
    schemaVersion: 'distr.control-plane-migration-matrix-report/v1',
    status: 'PASS',
    planOnly: false,
    startedAt: '2026-07-29T00:00:00Z',
    completedAt: '2026-07-29T00:01:00Z',
    source: {commit, workingTreeDirty: false},
    range: {from: 138, to: 170},
    database: {
      scheme: 'postgres',
      host: '127.0.0.1',
      port: 5432,
      name: 'control_plane_ci',
      user: 'release_ci',
      passwordPresent: true,
      sslMode: 'disable',
      expectedServerVersion: version,
      observedServerVersion: version,
    },
    migrationFiles: Array.from({length: 33}, (_, index) => ({
      version: 138 + index,
      upFile: `${138 + index}.up.sql`,
      upSha256: 'sha256:' + 'b'.repeat(64),
      downFile: `${138 + index}.down.sql`,
      downSha256: 'sha256:' + 'c'.repeat(64),
    })),
    scenarios: scenarioIds.map((id) => {
      const output = `${id} complete\n`;
      return {
        id,
        status: 'PASS',
        startedAt: '2026-07-29T00:00:00Z',
        durationMs: 1,
        checks: id === 'migration-file-integrity'
          ? [{description: 'migration inventory', count: 33, checksum: sha('inventory')}]
          : [{
              description: `${id} executed`,
              exitCode: 0,
              durationMs: 1,
              outputSha256: sha(output),
              output,
              diagnostic: output.trim(),
            }],
        diagnostic: '',
      };
    }),
    coverage: {
      schemaUpgrade: {from: 138, to: 170},
      schemaDown: {mode: 'single-step', from: 170, to: 169},
      checkpoint: 'idempotency-and-cursor-resume-tests',
      notExecuted: ['process-interruption-and-restart', 'binary-rollback'],
    },
    integrity: {
      algorithm: 'sha256',
      encoding: 'utf8',
      serialization: 'compact-json-preserving-property-order',
      scope: 'complete-report-excluding-reportChecksum',
      commandEvidence: 'complete-redacted-output',
    },
    cleanup: {attemptedSchemas: 3, droppedSchemas: 3, complete: true},
  };
  report.reportChecksum = sha(JSON.stringify(report));
  return report;
}
const targets = [
  {id: 'target-alpha', hubTargetId: 'hub-alpha', activeRelease: 'A', observerId: 'observer-alpha'},
  {id: 'target-beta', hubTargetId: 'hub-beta', activeRelease: 'A', observerId: 'observer-beta'},
];
const evidence = targets.flatMap((target) => ['A', 'B', 'A'].map((release, index) => ({
  targetId: target.id,
  component: 'api',
  release,
  attempts: 2,
  executedStepKeys: release === 'B' ? ['release-b:migration:migration-v2'] : [`release-${release}:deploy`],
  observed: {
    id: `${target.id}-${release}-${index}`,
    observerId: `${target.observerId}-registration`,
    deploymentUnitId: `${target.id}-unit`,
    componentInstanceId: `${target.id}-instance`,
    componentKey: 'api',
    sourceSequence: index + 1,
    evidenceChecksum: sha(`${target.id}-${release}-${index}`),
    evidenceReference: `fixture://${target.id}/${release}/api`,
    artifactDigest: sha(`artifact-${release}`),
    configChecksum: sha(`config-${release}`),
    capabilityChecksum: sha(`capability-${target.id}`),
    topologyChecksum: sha(`topology-${target.id}`),
    health: 'HEALTHY',
    outcome: 'COMPLETE',
    disposition: 'ACCEPTED',
    trusted: true,
    current: true,
    stateChecksum: sha(`state-${target.id}-${release}-${index}`),
    runtimeStateChecksum: sha(`runtime-${target.id}-${release}-${index}`),
  },
})));
const postdeploy = {
  ok: true,
  proofMode: 'live-hub-api',
  targets,
  releaseHistory: ['A', 'B', 'A'],
  migration: {
    id: 'migration-v2',
    appliedCount: 2,
    attempts: targets.map((target) => ({
      targetId: target.id,
      stepKey: 'release-b:migration:migration-v2',
      result: 'SUCCEEDED_VIA_V2',
    })),
  },
  evidence,
  fleet: {items: targets.map((target) => ({deploymentTargetId: target.hubTargetId, activeRelease: '1.0.0'}))},
  flowChecksum: '',
  secretLeaks: 0,
  liveStack: {
    started: true,
    loopbackOnly: true,
    services: ['postgres', 'hub', 'external-executor', 'reference-executor', 'observer-alpha', 'observer-beta'],
    nonLocalCalls: 0,
  },
  cleanup: {completed: true, retainedResources: [], inspectionFailures: []},
};
postdeploy.flowChecksum = sha(stable({
  releaseHistory: postdeploy.releaseHistory,
  evidence: postdeploy.evidence,
  fleet: postdeploy.fleet,
}));
fs.writeFileSync(path.join(root, 'migration-16.json'), `${JSON.stringify(migration('16.14'), null, 2)}\n`);
fs.writeFileSync(path.join(root, 'migration-18.json'), `${JSON.stringify(migration('18.4'), null, 2)}\n`);
fs.writeFileSync(path.join(root, 'post-deploy.json'), `${JSON.stringify(postdeploy, null, 2)}\n`);
fs.writeFileSync(path.join(root, 'ui.json'), `${JSON.stringify({
  schemaVersion: 'distr.control-plane-ui-report/v1',
  status: 'PASS',
  stats: {expected: 7, unexpected: 0, flaky: 0, skipped: 0},
}, null, 2)}\n`);
NODE
}

write_evidence_fixtures "$FIXTURE"
migration16_input="$FIXTURE/migration-16.json"
migration18_input="$FIXTURE/migration-18.json"
postdeploy_input="$FIXTURE/post-deploy.json"
ui_input="$FIXTURE/ui.json"
env \
  PATH="$MOCK_BIN:$PATH" \
  RELEASE_COMMIT="$COMMIT" \
  DISTR_IMAGE="$IMAGE" \
  DISTR_IMAGE_TAG="$TAG" \
  MOCK_HEAD="$COMMIT" \
  MOCK_COMMIT_EPOCH="$COMMIT_EPOCH" \
  MOCK_LOCAL_IMAGE_ID="$LOCAL_IMAGE_ID" \
  MOCK_SOURCE_URL="$SOURCE_URL" \
  MOCK_DIGEST="$DIGEST" \
  MOCK_EVENTS="$EVENTS" \
  PROVENANCE_SIGNING_PUBLIC_KEY="$FIXTURE/provenance.pub" \
  "$FIXTURE/${HELPER#"$REPO_ROOT/"}" \
  finalize-evidence "$migration16_input" "$migration18_input" "$postdeploy_input" "$ui_input"
acceptance="$FIXTURE/dist/release-${TAG}-acceptance.json"
migration16="$FIXTURE/dist/release-${TAG}-migration-postgresql-16.14.json"
migration18="$FIXTURE/dist/release-${TAG}-migration-postgresql-18.4.json"
[[ -s "$acceptance" && -s "${acceptance}.sha256" ]] || fail "acceptance bundle or checksum missing"
[[ -s "$migration16" && -s "${migration16}.sha256" ]] || fail "PostgreSQL 16 migration report missing"
[[ -s "$migration18" && -s "${migration18}.sha256" ]] || fail "PostgreSQL 18 migration report missing"
for category in operator api ui flag audit; do
  node -e '
    const report = require(process.argv[1]);
    if (report.evidence?.[process.argv[2]]?.status !== "PASS") process.exit(1);
  ' "$acceptance" "$category" || fail "acceptance bundle is missing passing $category evidence"
done
grep -Fq "DISTR_MIGRATION_POSTGRESQL_16_REPORT_REF=dist/release-${TAG}-migration-postgresql-16.14.json" "$handoff" ||
  fail "final handoff does not reference PostgreSQL 16 migration report"
grep -Fq "DISTR_MIGRATION_POSTGRESQL_18_REPORT_REF=dist/release-${TAG}-migration-postgresql-18.4.json" "$handoff" ||
  fail "final handoff does not reference PostgreSQL 18 migration report"
grep -Fq "DISTR_ACCEPTANCE_BUNDLE_REF=dist/release-${TAG}-acceptance.json" "$handoff" ||
  fail "final handoff does not reference acceptance bundle"
(cd "$(dirname "$handoff")" && sha256sum -c "$(basename "$sidecar")" >/dev/null) ||
  fail "final handoff checksum is invalid"
pass "evidence finalization validates reports and binds five acceptance domains"

new_fixture empty-sbom
sed -i 's/packages: \[{/packages: [], ignoredPackages: [{/' \
  "$FIXTURE/deploy/server-docker-compose/deploy.sh"
expect_failure \
  "publish rejects an empty-package SPDX document" \
  "image SBOM or binding does not match" \
  run_publish "$FIXTURE"

new_fixture blocked-migration
run_publish "$FIXTURE" >/dev/null
write_evidence_fixtures "$FIXTURE"
migration16_input="$FIXTURE/migration-16.json"
migration18_input="$FIXTURE/migration-18.json"
postdeploy_input="$FIXTURE/post-deploy.json"
ui_input="$FIXTURE/ui.json"
node - "$migration16_input" <<'NODE'
const fs = require('node:fs');
const file = process.argv[2];
const report = JSON.parse(fs.readFileSync(file, 'utf8'));
report.status = 'FAIL';
fs.writeFileSync(file, JSON.stringify(report));
NODE
expect_failure \
  "finalization rejects a failed migration report" \
  "migration report checksum does not match" \
  env \
    PATH="$MOCK_BIN:$PATH" \
    RELEASE_COMMIT="$COMMIT" \
    DISTR_IMAGE="$IMAGE" \
    DISTR_IMAGE_TAG="$TAG" \
    MOCK_HEAD="$COMMIT" \
    MOCK_COMMIT_EPOCH="$COMMIT_EPOCH" \
    MOCK_LOCAL_IMAGE_ID="$LOCAL_IMAGE_ID" \
    MOCK_SOURCE_URL="$SOURCE_URL" \
    MOCK_DIGEST="$DIGEST" \
    MOCK_EVENTS="$EVENTS" \
    PROVENANCE_SIGNING_PUBLIC_KEY="$FIXTURE/provenance.pub" \
    "$FIXTURE/${HELPER#"$REPO_ROOT/"}" \
    finalize-evidence "$migration16_input" "$migration18_input" "$postdeploy_input" "$ui_input"

require_pipeline_text() {
  local text="$1"
  grep -Fq "$text" "$JENKINSFILE" || fail "Jenkinsfile missing contract: $text"
}

require_pipeline_text 'disableConcurrentBuilds'
require_pipeline_text 'skipDefaultCheckout'
require_pipeline_text 'timeout('
require_pipeline_text 'timestamps()'
require_pipeline_text 'checkout scm'
require_pipeline_text 'git checkout --detach "$RELEASE_COMMIT"'
require_pipeline_text 'AmazonWebServicesCredentialsBinding'
require_pipeline_text 'publish-hub-image.sh publish'
require_pipeline_text 'control-plane-migration-matrix.ps1'
require_pipeline_text 'postgres:16.14-alpine3.23'
require_pipeline_text 'postgres:18.4-alpine3.23'
require_pipeline_text 'work/release-evidence-${DISTR_IMAGE_TAG}'
require_pipeline_text 'node hack/pr050-validate-control-plane-evidence.mjs migration'
require_pipeline_text 'examples/control-plane-e2e/run.mjs --mode clean --json'
require_pipeline_text 'node hack/pr050-validate-control-plane-evidence.mjs postdeploy'
require_pipeline_text 'playwright.control-plane.config.ts'
require_pipeline_text 'finalize-evidence'
require_pipeline_text 'node --test hack/control-plane-adopter-term-scan.test.mjs'
require_pipeline_text 'node hack/control-plane-adopter-term-scan.mjs --base'
require_pipeline_text 'docs/api/operator-control-plane-api.md'
require_pipeline_text 'archiveArtifacts'
require_pipeline_text 'onlyIfSuccessful: false'
require_pipeline_text 'deleteDir()'
! grep -Eq 'deploy\.sh (deploy|release)|\bssh\b' "$JENKINSFILE" ||
  fail "Jenkinsfile contains a deployment action"
! grep -Eiq 'choice[-_ ]?tp|emlo' "$HELPER" "$JENKINSFILE" ||
  fail "pipeline contains adopter-specific values"
for release_contract in "$HELPER" "$RELEASE_WORKFLOW"; do
  grep -Fq 'range=138..170' "$release_contract" ||
    fail "release evidence contract is not aligned to migration 170: $release_contract"
  ! grep -Fq 'range=138..164' "$release_contract" ||
    fail "release evidence contract retains stale migration 164 selector: $release_contract"
  ! grep -Fq 'range=138..166' "$release_contract" ||
    fail "release evidence contract retains stale migration 166 selector: $release_contract"
done
pass "Jenkinsfile is concurrent-safe, exact-checkout, credential-bound, publish-only, and adopter-neutral"

clean_gate_fixture="$TMP/tracked-source-clean"
mkdir -p "$clean_gate_fixture"
git -C "$clean_gate_fixture" init --quiet
git -C "$clean_gate_fixture" -c user.name=test -c user.email=test@example.invalid \
  -c commit.gpgsign=false commit --quiet --allow-empty -m init
printf 'tracked\n' >"$clean_gate_fixture/tracked.txt"
git -C "$clean_gate_fixture" add tracked.txt
git -C "$clean_gate_fixture" -c user.name=test -c user.email=test@example.invalid \
  -c commit.gpgsign=false commit --quiet -m tracked
(
  cd "$clean_gate_fixture"
  ENV_FILE="$clean_gate_fixture/unused.env" DISTR_DEPLOY_LIB_ONLY=1 source "$DEPLOY_SCRIPT"
  require_source_clean before
  mkdir -p ignored
  printf 'ignored/\n' >.gitignore
  git add .gitignore
  git -c user.name=test -c user.email=test@example.invalid -c commit.gpgsign=false \
    commit --quiet -m ignore-build-output
  printf 'ignored build output\n' >ignored/artifact.txt
  require_source_clean after
  printf 'untracked source\n' >artifact.go
  if output="$(require_source_clean before 2>&1)"; then
    fail "non-ignored untracked source unexpectedly passed the clean gate"
  fi
  [[ "$output" == *'source worktree must be clean before release build'* ]] ||
    fail "untracked source clean gate did not report its phase"
  rm -f artifact.go
  printf 'dirty\n' >>tracked.txt
  for phase in before after; do
    if output="$(require_source_clean "$phase" 2>&1)"; then
      fail "source dirtiness unexpectedly passed the $phase gate"
    fi
    [[ "$output" == *"source worktree must be clean ${phase} release build"* ]] ||
      fail "source $phase gate did not report its phase"
  done
)
pass "deployment helper allows ignored output and rejects untracked or tracked pre/post build dirtiness"

new_syft_fixture() {
  local name="$1"
  SYFT_FIXTURE="$TMP/syft-$name"
  SYFT_MOCK_BIN="$SYFT_FIXTURE/mock-bin"
  mkdir -p "$SYFT_FIXTURE/dist" "$SYFT_MOCK_BIN"
  git -C "$SYFT_FIXTURE" init --quiet
  printf 'fixture\n' >"$SYFT_FIXTURE/source.txt"
  git -C "$SYFT_FIXTURE" add source.txt
  GIT_AUTHOR_DATE='2026-07-18T12:00:00Z' GIT_COMMITTER_DATE='2026-07-18T12:00:00Z' \
    git -C "$SYFT_FIXTURE" -c user.name=test -c user.email=test@example.invalid \
      -c commit.gpgsign=false commit --quiet -m fixture
  SYFT_COMMIT="$(git -C "$SYFT_FIXTURE" rev-parse HEAD)"
  printf '{}\n' >"$SYFT_FIXTURE/dist/release-test.spdx.json"
  (
    cd "$SYFT_FIXTURE/dist"
    sha256sum release-test.spdx.json >release-test.spdx.json.sha256
  )
  : >"$SYFT_FIXTURE/docker-inspects"
  : >"$SYFT_FIXTURE/syft-scans"
  cat >"$SYFT_MOCK_BIN/syft" <<'EOF'
#!/usr/bin/env bash
printf 'poison syft invoked\n' >"$MOCK_POISON_MARKER"
exit 99
EOF
  cat >"$SYFT_FIXTURE/mock-syft" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "$*" == 'version -o json' ]]; then
  printf '{"application":"syft","version":"%s"}\n' "${MOCK_SYFT_VERSION:-1.51.1}"
  exit 0
fi
[[ "$1" == "docker:${MOCK_IMAGE_ID}" ]] || exit 91
shift
[[ "$1" == --source-name && "$2" == "$MOCK_REPOSITORY" ]] || exit 92
shift 2
[[ "$1" == --source-version && "$2" == "$MOCK_TAG" ]] || exit 93
shift 2
[[ "$1" == --platform && "$2" == linux/amd64 ]] || exit 94
shift 2
[[ "$1" == -o && "$2" == spdx-json=* ]] || exit 95
output="${2#spdx-json=}"
printf 'scan\n' >>"$MOCK_SYFT_SCANS"
scan="$(wc -l <"$MOCK_SYFT_SCANS")"
MOCK_SCAN="$scan" MOCK_OUTPUT="$output" node <<'NODE'
const fs = require('node:fs');
const repository = process.env.MOCK_REPOSITORY;
const tag = process.env.MOCK_TAG;
const imageId = process.env.MOCK_IMAGE_ID;
const mode = process.env.MOCK_SYFT_MODE || 'valid';
const scan = Number(process.env.MOCK_SCAN);
const checksum = '2'.repeat(64);
const purl = `pkg:oci/${encodeURIComponent(repository)}@${encodeURIComponent(`sha256:${checksum}`)}?arch=amd64&tag=${imageId.slice(7)}`;
const root = {
  SPDXID: 'SPDXRef-Package-image',
  name: mode === 'wrong-repo' ? `${repository}-wrong` : repository,
  versionInfo: mode === 'wrong-tag' ? `${tag}-wrong` : tag,
  primaryPackagePurpose: mode === 'wrong-purpose' ? 'APPLICATION' : 'CONTAINER',
  checksums: mode === 'invalid-checksum'
    ? [{algorithm: 'SHA256', checksumValue: 'bad'}]
    : mode === 'duplicate-checksum'
      ? [{algorithm: 'SHA256', checksumValue: checksum}, {algorithm: 'SHA256', checksumValue: checksum}]
      : [{algorithm: 'SHA256', checksumValue: checksum}],
  externalRefs: mode === 'missing-purl'
    ? []
    : mode === 'duplicate-purl'
      ? [
          {referenceCategory: 'PACKAGE-MANAGER', referenceType: 'purl', referenceLocator: purl},
          {referenceCategory: 'PACKAGE-MANAGER', referenceType: 'purl', referenceLocator: purl},
        ]
      : [{
          referenceCategory: 'PACKAGE-MANAGER',
          referenceType: 'purl',
          referenceLocator: mode === 'wrong-arch'
            ? purl.replace('arch=amd64', 'arch=arm64')
            : mode === 'duplicate-qualifier' ? `${purl}&arch=arm64` : purl,
        }],
};
const aux = {SPDXID: 'SPDXRef-Package-aux', name: 'aux', versionInfo: mode === 'fact-diff' && scan === 2 ? '2' : '1'};
const describes = {spdxElementId: 'SPDXRef-DOCUMENT', relationshipType: 'DESCRIBES', relatedSpdxElement: root.SPDXID};
const contains = {spdxElementId: root.SPDXID, relationshipType: 'CONTAINS', relatedSpdxElement: aux.SPDXID};
const relationships = mode === 'missing-describes'
  ? [contains]
  : mode === 'duplicate-describes'
    ? [describes, {...describes}, contains]
    : scan === 1 ? [describes, contains] : [contains, describes];
const document = scan === 1
  ? {
      spdxVersion: 'SPDX-2.3', SPDXID: 'SPDXRef-DOCUMENT',
      documentNamespace: 'https://volatile.invalid/one', creationInfo: {created: '2020-01-01T00:00:00Z'},
      relationships, packages: [root, aux],
    }
  : {
      packages: [aux, root], relationships,
      creationInfo: {created: '2030-01-01T00:00:00Z'}, documentNamespace: 'https://volatile.invalid/two',
      SPDXID: 'SPDXRef-DOCUMENT', spdxVersion: 'SPDX-2.3',
    };
fs.writeFileSync(process.env.MOCK_OUTPUT, `${JSON.stringify(document)}\n`);
NODE
EOF
  cat >"$SYFT_MOCK_BIN/mise" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
[[ "$1" == exec && "$2" == -- && "$3" == syft ]] || exit 96
shift 3
exec "$MOCK_REAL_SYFT" "$@"
EOF
  cat >"$SYFT_MOCK_BIN/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
[[ "$1 $2 $3" == "image inspect --format" && "$4" == '{{.Id}}|{{.Os}}/{{.Architecture}}' ]] || exit 97
printf 'inspect\n' >>"$MOCK_DOCKER_INSPECTS"
count="$(wc -l <"$MOCK_DOCKER_INSPECTS")"
case "${MOCK_DOCKER_MODE:-stable}:$count" in
  retag-first:2|retag-second:3) printf 'sha256:%064d|linux/amd64\n' 9 ;;
  *) printf '%s|linux/amd64\n' "$MOCK_IMAGE_ID" ;;
esac
EOF
  chmod +x "$SYFT_MOCK_BIN/syft" "$SYFT_MOCK_BIN/mise" "$SYFT_MOCK_BIN/docker" "$SYFT_FIXTURE/mock-syft"
}

new_syft_fixture valid
(
  cd "$SYFT_FIXTURE"
  export PATH="$SYFT_MOCK_BIN:$PATH"
  export MOCK_REAL_SYFT="$SYFT_FIXTURE/mock-syft" MOCK_REPOSITORY='example.invalid/image' MOCK_TAG=test
  export MOCK_IMAGE_ID="sha256:$(printf '1%.0s' {1..64})" MOCK_POISON_MARKER="$SYFT_FIXTURE/poison"
  export MOCK_SYFT_SCANS="$SYFT_FIXTURE/syft-scans" MOCK_DOCKER_INSPECTS="$SYFT_FIXTURE/docker-inspects"
  ENV_FILE="$SYFT_FIXTURE/unused.env" DISTR_DEPLOY_LIB_ONLY=1 source "$DEPLOY_SCRIPT"
  DISTR_IMAGE='example.invalid/image' DISTR_IMAGE_TAG=test \
    generate_image_sbom example.invalid/image:test "$SYFT_COMMIT"
  [[ ! -e "$MOCK_POISON_MARKER" ]]
  DISTR_IMAGE='example.invalid/image' DISTR_IMAGE_TAG=test \
    verify_image_sbom_binding example.invalid/image:test "$SYFT_COMMIT" >/dev/null
  node - dist/release-test.spdx.json dist/release-test.image-binding.json "$SYFT_COMMIT" <<'NODE'
const fs = require('node:fs');
const [sbomFile, bindingFile, commit] = process.argv.slice(2);
const document = JSON.parse(fs.readFileSync(sbomFile, 'utf8'));
const binding = JSON.parse(fs.readFileSync(bindingFile, 'utf8'));
if (JSON.stringify(document.documentDescribes) !== JSON.stringify(['SPDXRef-Package-image'])) process.exit(1);
if (binding.sourceCommit !== commit || binding.localImageId !== `sha256:${'1'.repeat(64)}`) process.exit(1);
NODE
)
pass "deployment helper uses pinned mise Syft, immutable image ID, deterministic scans, and exact binding"

for sidecar_mode in appended-entry wrong-name; do
  new_syft_fixture "$sidecar_mode"
  output="$( (
    cd "$SYFT_FIXTURE"
    export PATH="$SYFT_MOCK_BIN:$PATH"
    export MOCK_REAL_SYFT="$SYFT_FIXTURE/mock-syft" MOCK_REPOSITORY='example.invalid/image' MOCK_TAG=test
    export MOCK_IMAGE_ID="sha256:$(printf '1%.0s' {1..64})" MOCK_POISON_MARKER="$SYFT_FIXTURE/poison"
    export MOCK_SYFT_SCANS="$SYFT_FIXTURE/syft-scans" MOCK_DOCKER_INSPECTS="$SYFT_FIXTURE/docker-inspects"
    ENV_FILE="$SYFT_FIXTURE/unused.env" DISTR_DEPLOY_LIB_ONLY=1 source "$DEPLOY_SCRIPT"
    DISTR_IMAGE='example.invalid/image' DISTR_IMAGE_TAG=test \
      generate_image_sbom example.invalid/image:test "$SYFT_COMMIT"
    sidecar='dist/release-test.spdx.json.sha256'
    if [[ "$sidecar_mode" == appended-entry ]]; then
      printf '%064d  unrelated.file\n' 0 >>"$sidecar"
    else
      checksum="$(sha256sum dist/release-test.spdx.json | awk '{print $1}')"
      printf '%s  wrong-name.spdx.json\n' "$checksum" >"$sidecar"
    fi
    DISTR_IMAGE='example.invalid/image' DISTR_IMAGE_TAG=test \
      verify_image_sbom_binding example.invalid/image:test "$SYFT_COMMIT"
  ) 2>&1)" && fail "$sidecar_mode checksum sidecar unexpectedly passed"
  [[ "$output" == *'checksum sidecar is not exact'* ]] ||
    fail "$sidecar_mode checksum sidecar did not fail the exact contract"
done
pass "deployment helper rejects appended and wrong-name checksum sidecars"

for mode in wrong-repo wrong-tag wrong-purpose wrong-arch duplicate-qualifier missing-purl duplicate-purl \
  missing-describes duplicate-describes invalid-checksum duplicate-checksum fact-diff; do
  new_syft_fixture "$mode"
  if output="$( (
    cd "$SYFT_FIXTURE"
    export PATH="$SYFT_MOCK_BIN:$PATH"
    export MOCK_REAL_SYFT="$SYFT_FIXTURE/mock-syft" MOCK_REPOSITORY='example.invalid/image' MOCK_TAG=test
    export MOCK_IMAGE_ID="sha256:$(printf '1%.0s' {1..64})" MOCK_POISON_MARKER="$SYFT_FIXTURE/poison"
    export MOCK_SYFT_SCANS="$SYFT_FIXTURE/syft-scans" MOCK_DOCKER_INSPECTS="$SYFT_FIXTURE/docker-inspects"
    ENV_FILE="$SYFT_FIXTURE/unused.env" DISTR_DEPLOY_LIB_ONLY=1 source "$DEPLOY_SCRIPT"
    MOCK_SYFT_MODE="$mode" DISTR_IMAGE='example.invalid/image' DISTR_IMAGE_TAG=test \
      generate_image_sbom example.invalid/image:test "$SYFT_COMMIT"
  ) 2>&1)"; then
    fail "invalid Syft $mode document unexpectedly passed"
  fi
  [[ "$output" == *'SBOM'* || "$output" == *'SPDX'* ]] || fail "invalid Syft $mode document did not fail closed"
done
pass "deployment helper rejects incorrect root identity, purl, checksum, relationship, and scan facts"

for mode in retag-first retag-second; do
  new_syft_fixture "$mode"
  if output="$( (
    cd "$SYFT_FIXTURE"
    export PATH="$SYFT_MOCK_BIN:$PATH"
    export MOCK_REAL_SYFT="$SYFT_FIXTURE/mock-syft" MOCK_REPOSITORY='example.invalid/image' MOCK_TAG=test
    export MOCK_IMAGE_ID="sha256:$(printf '1%.0s' {1..64})" MOCK_POISON_MARKER="$SYFT_FIXTURE/poison"
    export MOCK_SYFT_SCANS="$SYFT_FIXTURE/syft-scans" MOCK_DOCKER_INSPECTS="$SYFT_FIXTURE/docker-inspects"
    ENV_FILE="$SYFT_FIXTURE/unused.env" DISTR_DEPLOY_LIB_ONLY=1 source "$DEPLOY_SCRIPT"
    MOCK_DOCKER_MODE="$mode" DISTR_IMAGE='example.invalid/image' DISTR_IMAGE_TAG=test \
      generate_image_sbom example.invalid/image:test "$SYFT_COMMIT"
  ) 2>&1)"; then
    fail "$mode unexpectedly passed"
  fi
  [[ "$output" == *'local image tag changed'* ]] || fail "$mode did not fail closed on the retag race"
done
pass "deployment helper rejects image retag races after either immutable scan"

new_syft_fixture wrong-version
if output="$( (
  cd "$SYFT_FIXTURE"
  export PATH="$SYFT_MOCK_BIN:$PATH"
  export MOCK_REAL_SYFT="$SYFT_FIXTURE/mock-syft" MOCK_REPOSITORY='example.invalid/image' MOCK_TAG=test
  export MOCK_IMAGE_ID="sha256:$(printf '1%.0s' {1..64})" MOCK_POISON_MARKER="$SYFT_FIXTURE/poison"
  export MOCK_SYFT_SCANS="$SYFT_FIXTURE/syft-scans" MOCK_DOCKER_INSPECTS="$SYFT_FIXTURE/docker-inspects"
  ENV_FILE="$SYFT_FIXTURE/unused.env" DISTR_DEPLOY_LIB_ONLY=1 source "$DEPLOY_SCRIPT"
  MOCK_SYFT_VERSION=1.50.0 DISTR_IMAGE='example.invalid/image' DISTR_IMAGE_TAG=test \
    generate_image_sbom example.invalid/image:test "$SYFT_COMMIT"
) 2>&1)"; then
  fail "wrong Syft version unexpectedly passed"
fi
[[ "$output" == *'exactly 1.51.1'* ]] || fail "wrong Syft version did not fail closed"
pass "deployment helper enforces the exact mise-managed Syft version"

for required_deploy_text in \
  'mise exec -- syft "docker:${image_id}"' \
  'require_source_clean before' \
  'require_source_clean after' \
  'verify_image_sbom_binding' \
  'distr.image-sbom-binding/v1' \
  'DISTR_SBOM_BINDING_SHA256'; do
  grep -Fq "$required_deploy_text" "$DEPLOY_SCRIPT" ||
    fail "deployment helper missing real SBOM contract: $required_deploy_text"
done
! grep -Fq '"packages": []' "$DEPLOY_SCRIPT" ||
  fail "deployment helper still contains empty-package SPDX fallback"
content_line="$(grep -n 'generate_hub_content_sbom "dist/distr-${arch}" "$commit"' "$DEPLOY_SCRIPT" | cut -d: -f1)"
build_line="$(grep -n '^  docker build \\' "$DEPLOY_SCRIPT" | cut -d: -f1)"
image_sbom_line="$(grep -n 'generate_image_sbom "$image_ref"' "$DEPLOY_SCRIPT" | cut -d: -f1)"
clean_before_line="$(grep -n 'require_source_clean before' "$DEPLOY_SCRIPT" | cut -d: -f1)"
clean_after_line="$(grep -n 'require_source_clean after' "$DEPLOY_SCRIPT" | cut -d: -f1)"
[[ "$clean_before_line" -lt "$content_line" && "$content_line" -lt "$build_line" && \
   "$build_line" -lt "$image_sbom_line" && "$image_sbom_line" -lt "$clean_after_line" ]] ||
  fail "content SBOM, one image build, and image SBOM are not in the required order"
pass "deployment helper generates and validates a non-empty image SBOM"

for required_workflow_text in \
  'anchore/sbom-action' \
  'cosign sign-blob' \
  'cosign verify-blob' \
  'control-plane-migration-matrix.ps1' \
  "work/release-evidence/migration-postgresql-16.14.json" \
  "work/release-evidence/migration-postgresql-18.4.json" \
  'node hack/pr050-validate-control-plane-evidence.mjs migration' \
  'examples/control-plane-e2e/run.mjs --mode clean --json' \
  'node hack/pr050-validate-control-plane-evidence.mjs postdeploy' \
  'playwright.control-plane.config.ts' \
  'node --test hack/control-plane-adopter-term-scan.test.mjs' \
  'docs/api/operator-control-plane-api.md' \
  'distr.control-plane-release-acceptance/v1' \
  'if-no-files-found: error'; do
  grep -Fq "$required_workflow_text" "$RELEASE_WORKFLOW" ||
    fail "release workflow missing enforced evidence contract: $required_workflow_text"
done
pass "release workflow fails closed on real SBOM, migration, and five-domain acceptance evidence"

printf '1..%d\n' "$PASS_COUNT"
