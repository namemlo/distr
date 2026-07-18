#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
HELPER="${REPO_ROOT}/deploy/jenkins/publish-hub-image.sh"
JENKINSFILE="${REPO_ROOT}/deploy/jenkins/Jenkinsfile.hub-image"
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

COMMIT='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
OTHER_COMMIT='bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
DIGEST='sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
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
    "$FIXTURE/dist" \
    "$MOCK_BIN"
  cp "$HELPER" "$FIXTURE/deploy/jenkins/publish-hub-image.sh"
  chmod +x "$FIXTURE/deploy/jenkins/publish-hub-image.sh"
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
    case "$3" in
      *revision*) printf '%s\n' "${MOCK_REVISION:-$MOCK_HEAD}" ;;
      *source*) printf '%s\n' "$MOCK_SOURCE_URL" ;;
      *Architecture*) printf 'linux/amd64\n' ;;
      *) printf 'unexpected inspect format: %s\n' "$3" >&2; exit 94 ;;
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

  chmod +x "$MOCK_BIN/git" "$MOCK_BIN/aws" "$MOCK_BIN/docker"
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
    MOCK_EVENTS="$EVENTS" \
    MOCK_AWS_CALLS="$AWS_CALLS" \
    MOCK_PUSHED="$PUSHED" \
    MOCK_TAG_STATE='absent' \
    MOCK_REPOSITORY_MUTABILITY='IMMUTABLE' \
    AWS_ACCESS_KEY_ID='test-access-id' \
    AWS_SECRET_ACCESS_KEY='credential-must-not-appear' \
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
expected_handoff="$(
  printf 'DISTR_IMAGE_REF=%s@%s\n' "$IMAGE" "$DIGEST"
  printf 'DISTR_RELEASE_COMMIT=%s\n' "$COMMIT"
  printf 'DISTR_IMAGE_DIGEST=%s\n' "$DIGEST"
)"
[[ "$(cat "$handoff")" == "$expected_handoff" ]] || {
  cat "$handoff" >&2
  fail "handoff is not the exact three-value contract"
}
(cd "$(dirname "$handoff")" && sha256sum -c "$(basename "$sidecar")" >/dev/null) ||
  fail "handoff checksum is invalid"
grep -q "^docker:pull ${IMAGE}@${DIGEST}$" "$EVENTS" ||
  fail "digest-pinned image was not pulled"
pass "successful publication creates exact checksummed handoff without credential leakage"

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
require_pipeline_text 'archiveArtifacts'
require_pipeline_text 'deleteDir()'
! grep -Eq 'deploy\.sh (deploy|release)|\bssh\b' "$JENKINSFILE" ||
  fail "Jenkinsfile contains a deployment action"
! grep -Eiq 'choice[-_ ]?tp|emlo' "$HELPER" "$JENKINSFILE" ||
  fail "pipeline contains adopter-specific values"
pass "Jenkinsfile is concurrent-safe, exact-checkout, credential-bound, publish-only, and adopter-neutral"

printf '1..%d\n' "$PASS_COUNT"
