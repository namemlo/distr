#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DEPLOY_SCRIPT="${REPO_ROOT}/deploy/server-docker-compose/deploy.sh"

info() {
  printf '[hub-image] %s\n' "$*"
}

die() {
  printf '[hub-image] ERROR: %s\n' "$*" >&2
  return 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    die "missing required command: $1"
    return 1
  }
}

require_commit() {
  local commit="${1:-}"
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || {
    die "RELEASE_COMMIT must be exactly 40 lowercase hexadecimal characters"
    return 1
  }
}

candidate_tag() {
  local commit="${1:-}" timestamp
  require_commit "$commit" || return
  timestamp="$(date -u +%Y%m%dt%H%M%Sz)" || return
  [[ "$timestamp" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || return 1
  printf 'candidate-%s-%s\n' "${commit:0:8}" "$timestamp"
}

require_publication_inputs() {
  require_commit "${RELEASE_COMMIT:-}" || return
  [[ "${AWS_REGION:-}" =~ ^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$ ]] || {
    die "AWS_REGION is missing or invalid"
    return 1
  }
  [[ "${ECR_REPOSITORY:-}" =~ ^[a-z0-9][a-z0-9._/-]*[a-z0-9]$ &&
     "$ECR_REPOSITORY" != *'//'*
  ]] || {
    die "ECR_REPOSITORY is missing or invalid"
    return 1
  }
  local image_repository="${DISTR_IMAGE#*/}"
  [[ "$DISTR_IMAGE" == */* && "$image_repository" == "$ECR_REPOSITORY" ]] || {
    die "DISTR_IMAGE repository path must exactly match ECR_REPOSITORY"
    return 1
  }

  local registry="${DISTR_IMAGE%%/*}"
  [[ "$registry" =~ ^[0-9]{12}\.dkr\.ecr(-fips)?\.${AWS_REGION}\.amazonaws\.com(\.cn)?$ ]] || {
    die "DISTR_IMAGE must be an AWS ECR repository URI in AWS_REGION"
    return 1
  }

  local expected_tag="candidate-${RELEASE_COMMIT:0:8}-"
  [[ "${DISTR_IMAGE_TAG:-}" =~ ^candidate-[0-9a-f]{8}-[0-9]{8}t[0-9]{6}z$ &&
     "$DISTR_IMAGE_TAG" == "$expected_tag"*
  ]] || {
    die "DISTR_IMAGE_TAG must be an immutable candidate tag for RELEASE_COMMIT"
    return 1
  }
}

require_exact_checkout() {
  local head dirty
  head="$(git rev-parse HEAD 2>/dev/null)" || {
    die "could not resolve checkout HEAD"
    return 1
  }
  [[ "$head" == "$RELEASE_COMMIT" ]] || {
    die "checkout does not match RELEASE_COMMIT"
    return 1
  }
  dirty="$(git status --porcelain=v1 --untracked-files=all)" || return
  [[ -z "$dirty" ]] || {
    die "checkout must be clean before the image build"
    return 1
  }
}

source_repository() {
  local source
  source="$(git config --get remote.origin.url 2>/dev/null)" || {
    die "checkout has no fixed origin repository"
    return 1
  }
  [[ -n "$source" && "$source" != *$'\n'* && "$source" != *$'\r'* ]] || {
    die "origin repository URL is invalid"
    return 1
  }
  if [[ "$source" =~ ^https?://[^/]*@ ]]; then
    die "origin repository URL must not contain credentials"
    return 1
  fi
  printf '%s' "$source"
}

require_linux_amd64_daemon() {
  local platform
  platform="$(docker info --format '{{.OSType}}/{{.Architecture}}')" || return
  [[ "$platform" == linux/amd64 || "$platform" == linux/x86_64 ]] || {
    die "Jenkins Docker daemon must be linux/amd64"
    return 1
  }
}

require_immutable_repository() {
  local mutability
  mutability="$(
    aws ecr describe-repositories \
      --region "$AWS_REGION" \
      --repository-names "$ECR_REPOSITORY" \
      --query 'repositories[0].imageTagMutability' \
      --output text
  )" || {
    die "could not verify ECR repository tag immutability"
    return 1
  }
  [[ "$mutability" == IMMUTABLE ]] || {
    die "ECR repository must enforce immutable tags"
    return 1
  }
}

assert_remote_tag_absent() {
  local phase="${1:-}" output status error_file
  error_file="${STATE_DIR}/aws-${phase}.stderr"
  set +e
  output="$(
    aws ecr describe-images \
      --region "$AWS_REGION" \
      --repository-name "$ECR_REPOSITORY" \
      --image-ids "imageTag=${DISTR_IMAGE_TAG}" \
      --query 'imageDetails[0].imageDigest' \
      --output text 2>"$error_file"
  )"
  status=$?
  set -e
  if ((status == 0)); then
    die "remote candidate tag already exists; refusing overwrite"
    return 1
  fi
  if grep -q 'ImageNotFoundException' "$error_file"; then
    return 0
  fi
  die "could not prove remote candidate tag is absent during ${phase}"
}

resolve_remote_digest() {
  local digest
  digest="$(
    aws ecr describe-images \
      --region "$AWS_REGION" \
      --repository-name "$ECR_REPOSITORY" \
      --image-ids "imageTag=${DISTR_IMAGE_TAG}" \
      --query 'imageDetails[0].imageDigest' \
      --output text
  )" || return
  [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    die "published image did not resolve to a lowercase SHA-256 digest"
    return 1
  }
  printf '%s' "$digest"
}

inspect_image_identity() {
  local image_ref="$1" expected_source="$2" revision source platform
  revision="$(docker image inspect \
    --format='{{ index .Config.Labels "org.opencontainers.image.revision" }}' \
    "$image_ref")" || return
  [[ "$revision" == "$RELEASE_COMMIT" ]] || {
    die "OCI revision label does not match RELEASE_COMMIT"
    return 1
  }
  source="$(docker image inspect \
    --format='{{ index .Config.Labels "org.opencontainers.image.source" }}' \
    "$image_ref")" || return
  [[ "$source" == "$expected_source" ]] || {
    die "OCI source label does not match the fixed checkout repository"
    return 1
  }
  platform="$(docker image inspect --format='{{.Os}}/{{.Architecture}}' "$image_ref")" || return
  [[ "$platform" == linux/amd64 ]] || {
    die "Hub image platform must be linux/amd64"
    return 1
  }
}

metadata_value() {
  local file="$1" key="$2" line count
  mapfile -t lines < <(grep -E "^${key}=" "$file" || true)
  count="${#lines[@]}"
  ((count == 1)) || {
    die "release metadata must contain exactly one ${key}"
    return 1
  }
  line="${lines[0]}"
  printf '%s' "${line#*=}"
}

write_exact_handoff() {
  local source_file="$1" digest="$2" expected_ref handoff temporary sidecar
  expected_ref="${DISTR_IMAGE}@${digest}"
  [[ "$(metadata_value "$source_file" DISTR_IMAGE_REF)" == "$expected_ref" ]] || {
    die "release metadata image reference differs from resolved ECR digest"
    return 1
  }
  [[ "$(metadata_value "$source_file" DISTR_RELEASE_COMMIT)" == "$RELEASE_COMMIT" ]] || {
    die "release metadata commit differs from RELEASE_COMMIT"
    return 1
  }
  [[ "$(metadata_value "$source_file" DISTR_IMAGE_DIGEST)" == "$digest" ]] || {
    die "release metadata digest differs from resolved ECR digest"
    return 1
  }

  handoff="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.env"
  sidecar="${handoff}.sha256"
  temporary="${STATE_DIR}/handoff.env"
  {
    printf 'DISTR_IMAGE_REF=%s\n' "$expected_ref"
    printf 'DISTR_RELEASE_COMMIT=%s\n' "$RELEASE_COMMIT"
    printf 'DISTR_IMAGE_DIGEST=%s\n' "$digest"
  } >"$temporary" || return
  install -m 0644 "$temporary" "$handoff" || return
  (
    cd "$(dirname "$handoff")" || return
    sha256sum "$(basename "$handoff")" >"$(basename "$sidecar")" || return
    sha256sum -c --status "$(basename "$sidecar")" || return
  ) || return
}

publish() (
  set -Eeuo pipefail
  local expected_source tagged_image digest digest_ref release_file
  need_cmd git || return
  need_cmd aws || return
  need_cmd docker || return
  need_cmd sha256sum || return
  [[ -x "$DEPLOY_SCRIPT" ]] || {
    die "missing executable deployment image helper: $DEPLOY_SCRIPT"
    return 1
  }
  require_publication_inputs || return
  require_exact_checkout || return
  expected_source="$(source_repository)" || return
  require_linux_amd64_daemon || return

  STATE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/distr-hub-image.XXXXXX")" || return
  export STATE_DIR
  chmod 0700 "$STATE_DIR" || return
  trap 'rm -rf -- "${STATE_DIR:-}"' EXIT HUP INT TERM

  local env_file="${STATE_DIR}/image.env"
  umask 077
  {
    printf 'AWS_REGION=%s\n' "$AWS_REGION"
    printf 'ECR_REPOSITORY=%s\n' "$ECR_REPOSITORY"
    printf 'DISTR_IMAGE=%s\n' "$DISTR_IMAGE"
    printf 'DISTR_IMAGE_TAG=%s\n' "$DISTR_IMAGE_TAG"
  } >"$env_file" || return
  chmod 0600 "$env_file" || return

  export ENV_FILE="$env_file"
  export LOCK_FILE="${STATE_DIR}/publish.lock"
  export TIMESTAMP_FENCE_FILE="${STATE_DIR}/timestamp-fence"
  export TIMESTAMP_COMPATIBILITY_FILE="${STATE_DIR}/timestamp-compatibility"
  export RELEASE_METADATA_DIR="${REPO_ROOT}/dist"

  mkdir -p "${REPO_ROOT}/dist" || return
  release_file="${RELEASE_METADATA_DIR}/release-${DISTR_IMAGE_TAG}.env"
  [[ ! -e "$release_file" && ! -e "${release_file}.sha256" ]] || {
    die "release handoff already exists in the workspace"
    return 1
  }

  require_immutable_repository || return
  assert_remote_tag_absent pre-build || return
  "$DEPLOY_SCRIPT" image-check || return
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$DEPLOY_SCRIPT" build || return

  tagged_image="${DISTR_IMAGE}:${DISTR_IMAGE_TAG}"
  inspect_image_identity "$tagged_image" "$expected_source" || return

  assert_remote_tag_absent pre-push || return
  "$DEPLOY_SCRIPT" push || return
  digest="$(resolve_remote_digest)" || return
  digest_ref="${DISTR_IMAGE}@${digest}"

  docker pull "$digest_ref" >/dev/null || return
  inspect_image_identity "$digest_ref" "$expected_source" || return
  write_exact_handoff "$release_file" "$digest" || return

  info "published immutable Hub image ${digest_ref}"
  info "wrote checksummed handoff dist/$(basename "$release_file")"
)

usage() {
  cat <<'EOF'
Usage:
  publish-hub-image.sh candidate-tag <40-lowercase-hex-commit>
  publish-hub-image.sh publish

The publish command requires RELEASE_COMMIT, AWS_REGION, ECR_REPOSITORY,
DISTR_IMAGE, and DISTR_IMAGE_TAG. AWS credentials must come from the process
environment or workload identity; this helper never writes them to disk.
EOF
}

case "${1:-}" in
  candidate-tag)
    [[ $# == 2 ]] || {
      usage >&2
      exit 2
    }
    candidate_tag "$2"
    ;;
  publish)
    [[ $# == 1 ]] || {
      usage >&2
      exit 2
    }
    cd "$REPO_ROOT"
    publish
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
