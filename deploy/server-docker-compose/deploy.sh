#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/.env}"
[[ "$ENV_FILE" == /* ]] || {
  printf '[distr-deploy] ERROR: ENV_FILE must be absolute: %s\n' "$ENV_FILE" >&2
  return 1 2>/dev/null || exit 1
}
BACKUP_DIR="${BACKUP_DIR:-${SCRIPT_DIR}/backups}"
LOCK_FILE="${LOCK_FILE:-${SCRIPT_DIR}/.deploy.lock}"
TIMESTAMP_FENCE_FILE="${TIMESTAMP_FENCE_FILE:-${SCRIPT_DIR}/.timestamp-expand-fence}"
TIMESTAMP_COMPATIBILITY_FILE="${TIMESTAMP_COMPATIBILITY_FILE:-${SCRIPT_DIR}/.timestamp-expand-compatibility}"

info() { printf '[distr-deploy] %s\n' "$*"; }
die() { printf '[distr-deploy] ERROR: %s\n' "$*" >&2; return 1; }

path_mode_is() {
  local path="$1" expected="$2" actual platform
  actual="$(stat -c '%a' -- "$path")" || return
  if [[ "$actual" == "$expected" ]]; then return 0; fi
  # Git for Windows cannot represent POSIX owner-only mode bits on NTFS. The
  # production host is Linux; keep its check exact while allowing the local
  # sourced harness to exercise all other safety behavior.
  platform="$(uname -s)" || return
  if [[ "$platform" == MINGW* || "$platform" == MSYS* || "$platform" == CYGWIN* ]]; then
    [[ "$expected:$actual" == 600:644 || "$expected:$actual" == 700:755 ]]
    return
  fi
  return 1
}

compose() {
  require_compose_env_parity || return
  DISTR_COMPOSE_ENV_FILE="$ENV_FILE" \
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@" || return
}

require_compose_env_parity() {
  if [[ -n "${DISTR_COMPOSE_ENV_FILE:-}" &&
        "$DISTR_COMPOSE_ENV_FILE" != "$ENV_FILE" ]]; then
    die "DISTR_COMPOSE_ENV_FILE must equal ENV_FILE"
    return 1
  fi
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    die "missing required command: $1"
    return 1
  }
}

load_env() {
  require_secure_env_file || return
  set -a || return
  # shellcheck disable=SC1090
  source "$ENV_FILE" || { set +a || true; return 1; }
  set +a || return
}

set_env_var() {
  local key="${1:-}" value="${2:-}"
  [[ "$key" =~ ^[A-Z][A-Z0-9_]*$ && "$value" != *$'\n'* && "$value" != *$'\r'* ]] || {
    die "invalid environment key/value update"
    return 1
  }
  require_secure_env_file || return
  if grep -qE "^${key}=" "$ENV_FILE"; then
    sed -i.bak -E "s#^${key}=.*#${key}=${value}#" "$ENV_FILE" || return
  else
    printf '\n%s=%s\n' "$key" "$value" >>"$ENV_FILE" || return
  fi
  rm -f -- "$ENV_FILE.bak" || return
}

random_b64() {
  openssl rand -base64 "$1" | tr -d '\n' || return
}

init_env() {
  [[ ! -e "${ENV_FILE}" && ! -L "${ENV_FILE}" ]] || {
    die "${ENV_FILE} already exists"
    return 1
  }
  copy_file_create_new_0600 "${SCRIPT_DIR}/.env.example" "${ENV_FILE}" || return

  local postgres_password rustfs_secret jwt_secret tag
  postgres_password="$(random_b64 24)" || return
  rustfs_secret="$(random_b64 24)" || return
  jwt_secret="$(random_b64 32)" || return
  if ! tag="$(git rev-parse --short=12 HEAD 2>/dev/null)"; then
    tag="$(date -u +%Y%m%dT%H%M%SZ)" || return
  fi

  set_env_var "POSTGRES_PASSWORD" "${postgres_password}" || return
  set_env_var "DATABASE_URL" "postgres://distr:${postgres_password}@postgres:5432/distr?sslmode=disable" || return
  set_env_var "REGISTRY_S3_SECRET_ACCESS_KEY" "${rustfs_secret}" || return
  set_env_var "RUSTFS_SECRET_KEY" "${rustfs_secret}" || return
  set_env_var "JWT_SECRET" "${jwt_secret}" || return
  set_env_var "DISTR_IMAGE_TAG" "${tag}" || return

  info "created ${ENV_FILE}" || return
  info "edit AWS_REGION, DISTR_IMAGE, DISTR_HOST, REGISTRY_HOST, registration, mail settings, and storage settings before deploy" || return
}

check_image_env() {
  load_env || return
  local required=(
    AWS_REGION DISTR_IMAGE DISTR_IMAGE_TAG
  )
  local key value
  for key in "${required[@]}"; do
    value="${!key:-}"
    [[ -n "${value}" ]] || { die "${key} is empty in ${ENV_FILE}"; return 1; }
    [[ "${value}" != *CHANGE_ME* ]] || { die "${key} still contains CHANGE_ME in ${ENV_FILE}"; return 1; }
  done
  [[ "${DISTR_IMAGE}" == *".dkr.ecr."* ]] || {
    die "DISTR_IMAGE must be an AWS ECR repository URI, for example 123456789012.dkr.ecr.${AWS_REGION}.amazonaws.com/distr-community"
    return 1
  }
  [[ "${DISTR_IMAGE_TAG}" != "latest" ]] || {
    die "DISTR_IMAGE_TAG must be immutable; do not deploy latest"
    return 1
  }
}

check_env() {
  check_runtime_env || return
  [[ "$DISTR_IMAGE_REF" == *'.dkr.ecr.'* && "$DISTR_IMAGE_REF" == *@sha256:* ]] || {
    die "DISTR_IMAGE_REF must be an ECR digest reference"
    return 1
  }
  [[ "${DISTR_RELEASE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]] || {
    die "DISTR_RELEASE_COMMIT must be exactly 40 lowercase hexadecimal characters"
    return 1
  }
  [[ "${DISTR_IMAGE_DIGEST:-}" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    die "DISTR_IMAGE_DIGEST must be a lowercase SHA-256 digest"
    return 1
  }
  [[ "$DISTR_IMAGE_REF" == *@"$DISTR_IMAGE_DIGEST" ]] || {
    die "DISTR_IMAGE_DIGEST must match DISTR_IMAGE_REF"
    return 1
  }
  [[ "${DISTR_CALLBACK_PROBE_URL:-}" =~ ^https?://(127\.0\.0\.1|localhost)(:[0-9]+)?/ ]] || {
    die "DISTR_CALLBACK_PROBE_URL must use the local Hub endpoint"
    return 1
  }
  [[ "$DISTR_CALLBACK_PROBE_URL" != *CHANGE_ME* ]] || {
    die "DISTR_CALLBACK_PROBE_URL still contains CHANGE_ME in ${ENV_FILE}"
    return 1
  }
}

audit_probe_execution_id() {
  local source="${1:-}" audit_url="${DISTR_AUDIT_HISTORY_PROBE_URL:-}" execution_id
  [[ "$audit_url" =~ ^https?://(127\.0\.0\.1|localhost)(:[0-9]+)?/api/v1/external-executions/([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})/$ ]] || {
    die "DISTR_AUDIT_HISTORY_PROBE_URL must be an exact loopback history route with trailing slash"
    return 1
  }
  execution_id="${BASH_REMATCH[3]}"
  jq -e --arg executionID "$execution_id" '
    (.eventCount | type == "number" and . > 0) and
    any(.cells[];
      .sourceTable == "externalexecution" and
      .sourceRowId == $executionID)
  ' "$source" >/dev/null || {
    die "audit probe must target a captured historical execution and captured history must be non-empty"
    return 1
  }
  printf '%s' "$execution_id" || return
}

require_audit_probe_execution_history() {
  local execution_id="${1:-}" event_count
  [[ "$execution_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] || return 1
  event_count="$(postgres_scalar \
    "SELECT count(*) FROM ExternalExecutionEvent WHERE external_execution_id = '$execution_id'::uuid")" || return
  [[ "$event_count" =~ ^[0-9]+$ && "$event_count" != 0 ]] || {
    die "audit probe execution has no captured event history"
    return 1
  }
}

check_timestamp_apply_env() {
  local evidence_dir="${1:-}" source execution_id
  check_env || return
  [[ "${DISTR_TIMESTAMP_EVIDENCE_CHECKSUM:-}" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    die "DISTR_TIMESTAMP_EVIDENCE_CHECKSUM must be the captured evidence-bundle checksum"
    return 1
  }
  [[ -n "$evidence_dir" ]] || return 1
  source="$evidence_dir/source-inspection.json"
  verify_sha256_sidecar "$source" || return
  [[ "${DISTR_AUDIT_HISTORY_PROBE_TOKEN:-}" =~ ^[A-Za-z0-9._~+/=-]+$ &&
     "$DISTR_AUDIT_HISTORY_PROBE_TOKEN" != *CHANGE_ME* ]] || {
    die "DISTR_AUDIT_HISTORY_PROBE_TOKEN must be a configured read-only token"
    return 1
  }
  execution_id="$(audit_probe_execution_id "$source")" || return
  require_audit_probe_execution_history "$execution_id" || return
}

check_runtime_env() {
  load_env || return
  local required=(
    COMPOSE_PROJECT_NAME POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB DATABASE_URL
    DISTR_HOST REGISTRY_HOST JWT_SECRET RUSTFS_ACCESS_KEY RUSTFS_SECRET_KEY
  )
  local key value
  for key in "${required[@]}"; do
    value="${!key:-}"
    [[ -n "${value}" ]] || { die "${key} is empty in ${ENV_FILE}"; return 1; }
    [[ "${value}" != *CHANGE_ME* ]] || { die "${key} still contains CHANGE_ME in ${ENV_FILE}"; return 1; }
  done
  [[ "${DISTR_HOST}" != *example.com* ]] || { die "DISTR_HOST still uses example.com in ${ENV_FILE}"; return 1; }
  [[ "${REGISTRY_HOST}" != *example.com* ]] || { die "REGISTRY_HOST still uses example.com in ${ENV_FILE}"; return 1; }
  if [[ "${USER_EMAIL_VERIFICATION_REQUIRED:-false}" == "true" && -z "${MAILER_TYPE:-}" ]]; then
    die "USER_EMAIL_VERIFICATION_REQUIRED=true requires MAILER_TYPE and mail settings"
    return 1
  fi
}

ecr_registry() {
  [[ -n "${DISTR_IMAGE:-}" ]] || return 1
  printf '%s' "${DISTR_IMAGE%%/*}" || return
}

ecr_repository() {
  [[ -n "${DISTR_IMAGE:-}" ]] || return 1
  printf '%s' "${DISTR_IMAGE#*/}" || return
}

tagged_image_ref() {
  printf '%s:%s' "${DISTR_IMAGE}" "${DISTR_IMAGE_TAG}" || return
}

resolve_digest_ref_for_tag() {
  local tag="${1:-}"
  local repo digest
  need_cmd aws || return
  repo="${ECR_REPOSITORY:-$(ecr_repository)}" || return
  digest="$(
    aws ecr describe-images \
      --region "${AWS_REGION}" \
      --repository-name "${repo}" \
      --image-ids "imageTag=${tag}" \
      --query 'imageDetails[0].imageDigest' \
      --output text
  )" || return
  [[ "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    die "could not resolve ECR digest for ${DISTR_IMAGE}:${tag}"
    return 1
  }
  printf '%s@%s' "${DISTR_IMAGE}" "${digest}" || return
}

write_release_metadata() {
  local commit image_ref image_digest release_file release_dir repository
  check_image_env || return
  need_cmd aws || return
  commit="$(git rev-parse HEAD 2>/dev/null)" || return
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || {
    die "release commit must be exactly 40 lowercase hexadecimal characters"
    return 1
  }
  image_ref="$(resolve_digest_ref_for_tag "${DISTR_IMAGE_TAG}")" || return
  image_digest="${image_ref##*@}"
  [[ "$image_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  set_image_identity "$image_ref" "$commit" || return

  release_dir="${RELEASE_METADATA_DIR:-dist}"
  mkdir -p -- "$release_dir" || return
  release_file="$release_dir/release-${DISTR_IMAGE_TAG}.env"
  repository="${ECR_REPOSITORY:-$(ecr_repository)}" || return
  cat >"$release_file" <<EOF || return
AWS_REGION=${AWS_REGION}
ECR_REPOSITORY=${repository}
DISTR_IMAGE=${DISTR_IMAGE}
DISTR_IMAGE_TAG=${DISTR_IMAGE_TAG}
DISTR_IMAGE_REF=${image_ref}
SOURCE_COMMIT=${commit}
DISTR_RELEASE_COMMIT=${commit}
DISTR_IMAGE_DIGEST=${image_digest}
EOF
  info "wrote release metadata ${release_file}"
}

pull_immutable_image_ref() {
  local image_ref="${1:-}"
  [[ "$image_ref" =~ @sha256:[0-9a-f]{64}$ ]] || {
    die "refusing to pull a non-digest image identity"
    return 1
  }
  need_cmd docker || return
  ecr_login || return
  docker pull "$image_ref" || return
}

image_release_commit() {
  local image_ref="${1:-}" revision resolved
  [[ "$image_ref" =~ @sha256:[0-9a-f]{64}$ ]] || return 1
  revision="$(docker image inspect --format \
    '{{ index .Config.Labels "org.opencontainers.image.revision" }}' \
    "$image_ref")" || return
  [[ "$revision" =~ ^[0-9a-f]{7,40}$ ]] || {
    die "image has no trustworthy OCI release revision: $image_ref"
    return 1
  }
  if ((${#revision} == 40)); then
    printf '%s' "$revision" || return
    return 0
  fi
  resolved="$(git rev-parse "${revision}^{commit}" 2>/dev/null)" || {
    die "short OCI revision cannot be resolved to a full commit: $revision"
    return 1
  }
  [[ "$resolved" =~ ^[0-9a-f]{40}$ && "$resolved" == "$revision"* ]] || return 1
  printf '%s' "$resolved" || return
}

path_components_have_no_symlink() {
  local path="${1:-}" current='/' component
  [[ "$path" == /* ]] || return 1
  IFS='/' read -r -a components <<<"${path#/}" || return
  for component in "${components[@]}"; do
    [[ -n "$component" ]] || continue
    current="${current%/}/$component"
    [[ ! -L "$current" ]] || return 1
  done
}

require_secure_state_directory() {
  local directory="${1:-}" owner current_uid mode
  [[ -d "$directory" && ! -L "$directory" ]] || {
    die "state directory must be a real directory: $directory"
    return 1
  }
  path_components_have_no_symlink "$directory" || {
    die "state directory contains a symlink: $directory"
    return 1
  }
  current_uid="$(id -u)" || return
  owner="$(stat -c '%u' -- "$directory")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "state directory must be owned by the deployment user: $directory"
    return 1
  }
  mode="$(stat -c '%a' -- "$directory")" || return
  (( (8#$mode & 8#022) == 0 )) || {
    die "state directory must not be group/world writable: $directory"
    return 1
  }
}

require_secure_env_file() {
  local directory owner current_uid
  [[ "$ENV_FILE" == /* && -f "$ENV_FILE" && ! -L "$ENV_FILE" ]] || {
    die "ENV_FILE must be an absolute regular non-symlink file: $ENV_FILE"
    return 1
  }
  path_components_have_no_symlink "$ENV_FILE" || {
    die "ENV_FILE path contains a symlink: $ENV_FILE"
    return 1
  }
  directory="$(dirname -- "$ENV_FILE")" || return
  require_secure_state_directory "$directory" || return
  current_uid="$(id -u)" || return
  owner="$(stat -c '%u' -- "$ENV_FILE")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "ENV_FILE must be owned by the deployment user: $ENV_FILE"
    return 1
  }
  path_mode_is "$ENV_FILE" 600 || {
    die "ENV_FILE mode must be 0600: $ENV_FILE"
    return 1
  }
}

set_image_identity() (
  set -Eeuo pipefail
  local image_ref="${1:-}" release_commit="${2:-}" image_digest
  local directory temporary='' original_env
  [[ "$image_ref" =~ ^[A-Za-z0-9._/+:-]+@sha256:[0-9a-f]{64}$ ]] || {
    die "image identity requires an immutable digest reference"
    return 1
  }
  [[ "$release_commit" =~ ^[0-9a-f]{40}$ ]] || {
    die "image identity requires a full lowercase release commit"
    return 1
  }
  image_digest="${image_ref##*@}"
  require_secure_env_file || return
  directory="$(dirname -- "$ENV_FILE")" || return
  temporary="$(mktemp "$directory/.env.image-identity.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
  install -m 0600 -- "$ENV_FILE" "$temporary" || return
  original_env="$ENV_FILE"
  ENV_FILE="$temporary"
  set_env_var DISTR_IMAGE_REF "$image_ref" || return
  set_env_var DISTR_RELEASE_COMMIT "$release_commit" || return
  set_env_var DISTR_IMAGE_DIGEST "$image_digest" || return
  sync -f "$temporary" || return
  ENV_FILE="$original_env"
  mv -fT -- "$temporary" "$ENV_FILE" || return
  temporary=''
  sync -f "$directory" || return
)

acquire_deploy_lock() {
  local parent parent_mode owner current_uid
  need_cmd flock || return
  [[ "$LOCK_FILE" == /* ]] || {
    die "deployment lock path must be absolute"
    return 1
  }
  parent="$(dirname -- "$LOCK_FILE")" || return
  require_secure_state_directory "$parent" || return
  current_uid="$(id -u)" || return
  [[ ! -L "$LOCK_FILE" ]] || {
    die "deployment lock may not be a symlink"
    return 1
  }
  if [[ ! -e "$LOCK_FILE" ]]; then
    (umask 077; set -o noclobber; : >"$LOCK_FILE") 2>/dev/null || {
      [[ -f "$LOCK_FILE" && ! -L "$LOCK_FILE" ]] || return 1
    }
  fi
  [[ -f "$LOCK_FILE" && ! -L "$LOCK_FILE" ]] || {
    die "deployment lock must be a regular non-symlink file"
    return 1
  }
  owner="$(stat -c '%u' -- "$LOCK_FILE")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "deployment lock must be owned by the deployment user"
    return 1
  }
  chmod 0600 -- "$LOCK_FILE" || return
  path_mode_is "$LOCK_FILE" 600 || return
  exec 9<>"$LOCK_FILE" || return
  flock -n 9 || {
    die "another deployment is already running; lock: $LOCK_FILE"
    return 1
  }
}

ecr_login() {
  local registry password
  check_image_env || return
  need_cmd aws || return
  need_cmd docker || return
  registry="$(ecr_registry)" || return
  info "logging in to ECR registry ${registry}"
  password="$(aws ecr get-login-password --region "${AWS_REGION}")" || return
  [[ -n "$password" ]] || return 1
  printf '%s' "$password" |
    docker login --username AWS --password-stdin "${registry}" || return
  password=''
}

compose_config() {
  check_env || return
  need_cmd docker || return
  info "validating Docker Compose configuration"
  compose config --quiet || return
}

ensure_ecr_repository() {
  check_image_env || return
  need_cmd aws || return
  local repo
  repo="${ECR_REPOSITORY:-$(ecr_repository)}" || return
  info "ensuring ECR repository ${repo} exists in ${AWS_REGION}" || return
  if ! aws ecr describe-repositories --region "${AWS_REGION}" --repository-names "${repo}" >/dev/null 2>&1; then
    aws ecr create-repository --region "${AWS_REGION}" --repository-name "${repo}" >/dev/null || return
  fi
}

detect_goarch() {
  local arch machine
  arch="$(go env GOARCH 2>/dev/null)" || arch=''
  if [[ -z "${arch}" ]]; then
    machine="$(uname -m)" || return
    case "$machine" in
      x86_64|amd64) arch="amd64" ;;
      aarch64|arm64) arch="arm64" ;;
      *) die "cannot map machine architecture $machine to Go arch"; return 1 ;;
    esac
  fi
  printf '%s' "${arch}" || return
}

ensure_local_sbom() {
  shopt -s nullglob || return
  local sboms=(dist/*.spdx.json)
  shopt -u nullglob || return
  if ((${#sboms[@]} > 0)); then
    return
  fi

  local namespace_stamp created_at
  namespace_stamp="$(date -u +%Y%m%dT%H%M%SZ)" || return
  created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || return

  cat > dist/local-build.spdx.json <<JSON
{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "distr-local-source-build",
  "documentNamespace": "https://distr.example.invalid/spdx/local-${namespace_stamp}",
  "creationInfo": {
    "created": "${created_at}",
    "creators": ["Tool: deploy/server-docker-compose/deploy.sh"]
  },
  "packages": []
}
JSON
}

build_image() {
  check_image_env || return
  need_cmd git || return
  need_cmd docker || return
  need_cmd go || return
  need_cmd mise || return

  local arch commit short_commit image_ref
  arch="$(detect_goarch)" || return
  commit="$(git rev-parse HEAD)" || return
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || return 1
  short_commit="${commit:0:12}"
  image_ref="$(tagged_image_ref)" || return

  info "installing tool versions from mise.toml" || return
  mise install || return

  info "installing root workspace dependencies from the frozen lockfile" || return
  mise exec -- pnpm install --frozen-lockfile || return

  info "building community Hub from source for commit ${short_commit}" || return
  VERSION="${DISTR_IMAGE_TAG}" mise run build:hub:community || return
  cp dist/distr "dist/distr-${arch}" || return
  ensure_local_sbom || return

  info "building Hub image ${image_ref}" || return
  docker build \
    --build-arg "TARGETARCH=${arch}" \
    --label "org.opencontainers.image.revision=${commit}" \
    --label "org.opencontainers.image.source=$(git config --get remote.origin.url || true)" \
    -f Dockerfile.hub \
    -t "${image_ref}" \
    . || return
}

push_image() {
  local image_ref
  need_cmd docker || return
  ecr_login || return
  image_ref="$(tagged_image_ref)" || return
  info "pushing ${image_ref} to ECR" || return
  docker push "$image_ref" || return
  write_release_metadata || return
}

pull_image() {
  local release_commit
  check_env || return
  need_cmd docker || return
  ecr_login || return
  info "pulling ${DISTR_IMAGE_REF} from ECR"
  compose pull hub || return
  compose --profile migrate pull migrate || return
  compose --profile cleanup pull artifact-blob-cleanup || return
  compose --profile timestamp-operator pull timestamp-operator || return
  release_commit="$(image_release_commit "$DISTR_IMAGE_REF")" || return
  [[ "$release_commit" == "$DISTR_RELEASE_COMMIT" ]] || {
    die "configured release commit differs from the pulled image"
    return 1
  }
}

start_dependencies() {
  check_runtime_env || return
  need_cmd docker || return
  info "starting postgres and storage"
  compose up -d --wait --wait-timeout 180 postgres storage || return
}

backup_postgres() {
  local running_ref
  check_env || return
  need_cmd docker || return
  prepare_backup_directory "$BACKUP_DIR" || return
  running_ref="$(running_hub_digest)" || return
  [[ "$running_ref" == "$DISTR_IMAGE_REF" ]] || {
    die "standalone backup refuses configured/running Hub image drift"
    return 1
  }
  stop_hub || return
  assert_hub_writers_stopped || return
  backup_and_restore_release_evidence || return
  start_hub || return
  health || return
}

run_migrations() {
  check_env || return
  need_cmd docker || return
  info "running database migrations explicitly"
  compose --profile migrate run --rm migrate || return
}

migrate_with_fenced_backup() {
  compose_config || return
  pull_image || return
  start_dependencies || return
  run_migration_preflight || return
  prepare_backup_directory "$BACKUP_DIR" || return
  stop_hub || return
  assert_hub_writers_stopped || return
  backup_and_restore_release_evidence || return
  run_migrations || return
  start_hub || return
  health || return
}

start_hub() {
  check_env || return
  need_cmd docker || return
  info "starting Hub"
  compose up -d hub || return
}

stop_hub() {
  check_env || return
  need_cmd docker || return
  info "stopping Hub before migration"
  compose stop hub || return
}

health() {
  check_env || return
  need_cmd curl || return
  local url="${DISTR_LOCAL_HEALTH_URL:-http://127.0.0.1:${DISTR_HTTP_PORT:-8080}/ready}" attempt
  info "waiting for ${url}"
  for ((attempt=1; attempt<=60; attempt++)); do
    if curl -fsS "${url}" >/dev/null; then
      info "Hub is ready"
      return 0
    fi
    sleep 2 || return
  done
  compose ps || true
  compose logs --tail=120 hub || true
  die "Hub did not become ready at ${url}"
  return 1
}

parse_restricted_key_file() {
  local file="${1:-}"
  local expected_csv="${2:-}"
  local output_name="${3:-}"
  local mode key value extra candidate allowed owner current_uid
  local -a expected
  local -n output="$output_name"
  output=()
  [[ -f "$file" && ! -L "$file" ]] || {
    die "unsafe or missing state file: $file"
    return 1
  }
  mode="$(stat -c '%a' -- "$file")" || return
  path_mode_is "$file" 600 || {
    die "state file mode must be 0600: $file"
    return 1
  }
  current_uid="$(id -u)" || return
  owner="$(stat -c '%u' -- "$file")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "state file must be owned by the deployment user: $file"
    return 1
  }
  IFS=',' read -r -a expected <<<"$expected_csv" || return
  while IFS='=' read -r key value extra || [[ -n "$key$value$extra" ]]; do
    [[ -n "$key" && -n "$value" && -z "$extra" ]] || {
      die "invalid state-file record: $file"
      return 1
    }
    [[ "$value" =~ ^[A-Za-z0-9_./:@+-]+$ ]] || {
      die "invalid state-file value for $key"
      return 1
    }
    allowed=0
    for candidate in "${expected[@]}"; do
      [[ "$key" == "$candidate" ]] && allowed=1
    done
    ((allowed == 1)) || {
      die "unknown state-file key: $key"
      return 1
    }
    [[ ! -v "output[$key]" ]] || {
      die "duplicate state-file key: $key"
      return 1
    }
    output["$key"]="$value"
  done <"$file"
  for candidate in "${expected[@]}"; do
    [[ -v "output[$candidate]" ]] || {
      die "missing state-file key: $candidate"
      return 1
    }
  done
  ((${#output[@]} == ${#expected[@]})) || return 1
}

evidence_dir_checksum() {
  local evidence_dir="${1:-}" canonical
  [[ -d "$evidence_dir" && ! -L "$evidence_dir" ]] || {
    die "evidence directory is unsafe: $evidence_dir"
    return 1
  }
  canonical="$(readlink -f -- "$evidence_dir")" || return
  printf '%s' "$canonical" | sha256sum | awk '{print "sha256:"$1}' || return
}

prepare_timestamp_evidence_dir() {
  local evidence_dir="${1:-}"
  local deployment_uid deployment_gid expected_owner actual_owner parent
  [[ "$evidence_dir" == /* ]] || {
    die "timestamp evidence directory must be absolute"
    return 1
  }
  path_components_have_no_symlink "$evidence_dir" || {
    die "timestamp evidence directory path contains a symlink"
    return 1
  }
  parent="$(dirname -- "$evidence_dir")" || return
  require_secure_state_directory "$parent" || return
  deployment_uid="$(id -u)" || return
  deployment_gid="$(id -g)" || return
  expected_owner="$deployment_uid:$deployment_gid"
  if [[ -e "$evidence_dir" || -L "$evidence_dir" ]]; then
    [[ -d "$evidence_dir" && ! -L "$evidence_dir" ]] || {
      die "evidence directory must be a real directory"
      return 1
    }
  else
    (umask 077; mkdir -- "$evidence_dir") || return
    chmod 0700 -- "$evidence_dir" || return
  fi
  path_mode_is "$evidence_dir" 700 || {
    die "evidence directory mode must be 0700"
    return 1
  }
  actual_owner="$(stat -c '%u:%g' -- "$evidence_dir")" || return
  [[ "$actual_owner" == "$expected_owner" ]] || {
    die "evidence directory must be owned by the deployment user"
    return 1
  }
  require_secure_state_directory "$evidence_dir" || return
}

prepare_backup_directory() {
  local directory="${1:-}" owner current_uid parent
  [[ "$directory" == /* ]] || {
    die "backup directory must be absolute"
    return 1
  }
  path_components_have_no_symlink "$directory" || {
    die "backup directory path contains a symlink"
    return 1
  }
  parent="$(dirname -- "$directory")" || return
  require_secure_state_directory "$parent" || return
  if [[ -e "$directory" || -L "$directory" ]]; then
    [[ -d "$directory" && ! -L "$directory" ]] || {
      die "backup directory must be a real directory"
      return 1
    }
  else
    (umask 077; mkdir -- "$directory") || return
    chmod 0700 -- "$directory" || return
  fi
  path_mode_is "$directory" 700 || {
    die "backup directory mode must be 0700"
    return 1
  }
  current_uid="$(id -u)" || return
  owner="$(stat -c '%u' -- "$directory")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "backup directory must be owned by the deployment user"
    return 1
  }
  require_secure_state_directory "$directory" || return
}

active_timestamp_fence() {
  [[ -e "$TIMESTAMP_FENCE_FILE" || -L "$TIMESTAMP_FENCE_FILE" ]]
}

persist_timestamp_fence() (
  local state="${1:-}" fence_id="${2:-}"
  local evidence_dir="${3:-}"
  local source_digest="${4:-}"
  local target_digest="${5:-}"
  local checksum created_at directory temporary='' transition=0
  local keys='STATE,FENCE_ID,EVIDENCE_DIR_CHECKSUM,SOURCE_IMAGE_DIGEST,TARGET_IMAGE_DIGEST,CREATED_AT'
  local -A current=()
  [[ "$state" == PREPARING || "$state" == CAPTURED_WRITERS_STOPPED ]] || {
    die "invalid timestamp fence state: $state"
    return 1
  }
  checksum="$(evidence_dir_checksum "$evidence_dir")" || return
  if [[ -e "$TIMESTAMP_FENCE_FILE" ]]; then
    transition=1
    parse_restricted_key_file "$TIMESTAMP_FENCE_FILE" "$keys" current || return
    [[ "${current[FENCE_ID]}" == "$fence_id" &&
       "${current[EVIDENCE_DIR_CHECKSUM]}" == "$checksum" &&
       "${current[SOURCE_IMAGE_DIGEST]}" == "$source_digest" &&
       "${current[TARGET_IMAGE_DIGEST]}" == "$target_digest" ]] || {
      die "timestamp fence identity changed"
      return 1
    }
    [[ "${current[STATE]}" == PREPARING &&
       "$state" == CAPTURED_WRITERS_STOPPED ]] || {
      die "invalid timestamp fence transition"
      return 1
    }
    created_at="${current[CREATED_AT]}"
  else
    created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || return
  fi
  directory="$(dirname -- "$TIMESTAMP_FENCE_FILE")" || return
  require_secure_state_directory "$directory" || return
  umask 077
  temporary="$(mktemp "$directory/.timestamp-fence.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
  {
    printf 'STATE=%s\n' "$state"
    printf 'FENCE_ID=%s\n' "$fence_id"
    printf 'EVIDENCE_DIR_CHECKSUM=%s\n' "$checksum"
    printf 'SOURCE_IMAGE_DIGEST=%s\n' "$source_digest"
    printf 'TARGET_IMAGE_DIGEST=%s\n' "$target_digest"
    printf 'CREATED_AT=%s\n' "$created_at"
  } >"$temporary" || return
  chmod 0600 -- "$temporary" || return
  sync -f "$temporary" || return
  if ((transition == 1)); then
    mv -fT -- "$temporary" "$TIMESTAMP_FENCE_FILE" || return
    temporary=''
  else
    ln -- "$temporary" "$TIMESTAMP_FENCE_FILE" || {
      die "timestamp fence appeared concurrently"
      return 1
    }
    rm -- "$temporary" || return
    temporary=''
  fi
  sync -f "$directory" || return
)

fence_value() {
  local wanted="${1:-}"
  local keys='STATE,FENCE_ID,EVIDENCE_DIR_CHECKSUM,SOURCE_IMAGE_DIGEST,TARGET_IMAGE_DIGEST,CREATED_AT'
  local -A values=()
  parse_restricted_key_file "$TIMESTAMP_FENCE_FILE" "$keys" values || return
  [[ -v "values[$wanted]" ]] || {
    die "unknown fence key: $wanted"
    return 1
  }
  printf '%s' "${values[$wanted]}" || return
}

require_timestamp_fence() {
  local evidence_dir="${1:-}" actual expected
  local keys='STATE,FENCE_ID,EVIDENCE_DIR_CHECKSUM,SOURCE_IMAGE_DIGEST,TARGET_IMAGE_DIGEST,CREATED_AT'
  local -A values=()
  parse_restricted_key_file "$TIMESTAMP_FENCE_FILE" "$keys" values || return
  [[ "${values[STATE]}" == CAPTURED_WRITERS_STOPPED ]] || {
    die "timestamp fence is not ready for apply/cancel"
    return 1
  }
  actual="$(evidence_dir_checksum "$evidence_dir")" || return
  expected="${values[EVIDENCE_DIR_CHECKSUM]}"
  [[ "$actual" == "$expected" ]] || {
    die "timestamp evidence directory does not match fence"
    return 1
  }
}

clear_timestamp_fence() {
  local evidence_dir="${1:-}" directory
  require_timestamp_fence "$evidence_dir" || return
  rm -- "$TIMESTAMP_FENCE_FILE" || return
  directory="$(dirname -- "$TIMESTAMP_FENCE_FILE")" || return
  sync -f "$directory" || return
}

persist_timestamp_compatibility() (
  local approved_id="${1:-}"
  local keys='SCHEMA_VERSION,EXPAND_IMAGE_DIGEST,PRE_EXPAND_IMAGE_DIGEST,MANIFEST_ID,CREATED_AT'
  local expand_digest pre_expand_digest created_at directory temporary=''
  local -A current=()
  [[ "$approved_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] || return 1
  expand_digest="$(fence_value TARGET_IMAGE_DIGEST)" || return
  pre_expand_digest="$(fence_value SOURCE_IMAGE_DIGEST)" || return
  if [[ -e "$TIMESTAMP_COMPATIBILITY_FILE" ]]; then
    parse_restricted_key_file "$TIMESTAMP_COMPATIBILITY_FILE" "$keys" current || return
    [[ "${current[SCHEMA_VERSION]}" == 138 &&
       "${current[EXPAND_IMAGE_DIGEST]}" == "$expand_digest" &&
       "${current[PRE_EXPAND_IMAGE_DIGEST]}" == "$pre_expand_digest" &&
       "${current[MANIFEST_ID]}" == "$approved_id" ]] || {
      die "existing timestamp compatibility record differs"
      return 1
    }
    return 0
  fi
  created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || return
  directory="$(dirname -- "$TIMESTAMP_COMPATIBILITY_FILE")" || return
  require_secure_state_directory "$directory" || return
  [[ -d "$directory" && ! -L "$directory" ]] || return 1
  umask 077
  temporary="$(mktemp "$directory/.timestamp-compatibility.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
  {
    printf 'SCHEMA_VERSION=138\n'
    printf 'EXPAND_IMAGE_DIGEST=%s\n' "$expand_digest"
    printf 'PRE_EXPAND_IMAGE_DIGEST=%s\n' "$pre_expand_digest"
    printf 'MANIFEST_ID=%s\n' "$approved_id"
    printf 'CREATED_AT=%s\n' "$created_at"
  } >"$temporary" || return
  chmod 0600 -- "$temporary" || return
  sync -f "$temporary" || return
  ln -- "$temporary" "$TIMESTAMP_COMPATIBILITY_FILE" || return
  rm -- "$temporary" || return
  temporary=''
  sync -f "$directory" || return
)

require_rollback_schema_compatibility() {
  local target="${1:-}" status schema dirty
  local keys='SCHEMA_VERSION,EXPAND_IMAGE_DIGEST,PRE_EXPAND_IMAGE_DIGEST,MANIFEST_ID,CREATED_AT'
  local -A values=()
  status="$(current_schema_status)" || return
  [[ "$status" =~ ^([0-9]+):(true|false)$ ]] || {
    die "rollback refused: current schema state is invalid"
    return 1
  }
  schema="${BASH_REMATCH[1]}"
  dirty="${BASH_REMATCH[2]}"
  [[ "$dirty" == false ]] || {
    die "rollback refused: current schema is dirty"
    return 1
  }
  if ((schema < 138)); then return 0; fi
  if ((schema > 138)); then
    die "rollback refused: no exact compatibility metadata exists for schema $schema"
    return 1
  fi
  parse_restricted_key_file "$TIMESTAMP_COMPATIBILITY_FILE" "$keys" values || return
  [[ "${values[SCHEMA_VERSION]}" == "$schema" ]] || {
    die "rollback refused: compatibility metadata schema differs from live schema"
    return 1
  }
  [[ "$target" != "${values[PRE_EXPAND_IMAGE_DIGEST]}" ]] || {
    die "rollback refused: pre-expand image cannot write schema 138"
    return 1
  }
  [[ "$target" == "${values[EXPAND_IMAGE_DIGEST]}" ]] || {
    die "rollback refused: image is not recorded compatible with schema 138"
    return 1
  }
}

copy_file_create_new_0600() (
  set -Eeuo pipefail
  local source="${1:-}"
  local destination="${2:-}"
  local directory temporary destination_name
  [[ -f "$source" && ! -L "$source" ]] || {
    die "source must be a regular non-symlink file"
    return 1
  }
  [[ ! -e "$destination" && ! -L "$destination" ]] || {
    die "destination already exists: $destination"
    return 1
  }
  directory="$(dirname -- "$destination")" || return
  destination_name="$(basename -- "$destination")" || return
  require_secure_state_directory "$directory" || return
  umask 077
  temporary="$(mktemp "$directory/.$destination_name.tmp.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}"' EXIT HUP INT TERM
  install -m 0600 -- "$source" "$temporary" || return
  sync -f "$temporary" || return
  ln -- "$temporary" "$destination" || {
    die "could not create destination without replacement"
    return 1
  }
  rm -f -- "$temporary" || return
  temporary=''
  sync -f "$directory" || return
  trap - EXIT HUP INT TERM || return
)

stage_approved_manifest() {
  local source="${1:-}"
  local evidence_dir="${2:-}"
  local destination="$evidence_dir/approved-manifest.json" mode
  if [[ -e "$destination" ]]; then
    [[ -f "$destination" && ! -L "$destination" ]] || {
      die "staged approved manifest is unsafe"
      return 1
    }
    mode="$(stat -c '%a' -- "$destination")" || return
    path_mode_is "$destination" 600 || {
      die "staged approved manifest mode must be 0600"
      return 1
    }
    cmp -s -- "$source" "$destination" || {
      die "staged approved manifest differs from supplied manifest"
      return 1
    }
    verify_sha256_sidecar "$destination" || return
    return 0
  fi
  copy_file_create_new_0600 "$source" "$destination" || return
  write_sha256_sidecar_create_new "$destination" || return
}

write_sha256_sidecar_create_new() (
  set -Eeuo pipefail
  local file="${1:-}"
  local directory sidecar temporary digest sidecar_name file_name
  directory="$(dirname -- "$file")" || return
  sidecar="$file.sha256"
  sidecar_name="$(basename -- "$sidecar")" || return
  file_name="$(basename -- "$file")" || return
  [[ -f "$file" && ! -L "$file" ]] || {
    die "checksum source must be a regular non-symlink file: $file"
    return 1
  }
  require_secure_state_directory "$directory" || return
  [[ ! -e "$sidecar" && ! -L "$sidecar" ]] || {
    die "checksum sidecar exists: $sidecar"
    return 1
  }
  digest="$(sha256sum -- "$file" | awk '{print $1}')" || return
  umask 077
  temporary="$(mktemp "$directory/.$sidecar_name.tmp.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}"' EXIT HUP INT TERM
  printf '%s  %s\n' "$digest" "$file_name" >"$temporary" || return
  chmod 0600 "$temporary" || return
  sync -f "$temporary" || return
  ln -- "$temporary" "$sidecar" || {
    die "could not create checksum sidecar without replacement"
    return 1
  }
  rm -f -- "$temporary" || return
  temporary=''
  sync -f "$directory" || return
  trap - EXIT HUP INT TERM || return
)

verify_sha256_sidecar() {
  local file="${1:-}" sidecar
  sidecar="$file.sha256"
  local file_mode sidecar_mode directory sidecar_name file_owner sidecar_owner current_uid
  [[ -f "$file" && ! -L "$file" && -f "$sidecar" && ! -L "$sidecar" ]] || return 1
  file_mode="$(stat -c '%a' -- "$file")" || return
  sidecar_mode="$(stat -c '%a' -- "$sidecar")" || return
  path_mode_is "$file" 600 || return
  path_mode_is "$sidecar" 600 || return
  current_uid="$(id -u)" || return
  file_owner="$(stat -c '%u' -- "$file")" || return
  sidecar_owner="$(stat -c '%u' -- "$sidecar")" || return
  [[ "$file_owner" == "$current_uid" && "$sidecar_owner" == "$current_uid" ]] || return 1
  directory="$(dirname -- "$file")" || return
  require_secure_state_directory "$directory" || return
  sidecar_name="$(basename -- "$sidecar")" || return
  (cd "$directory" || return; sha256sum -c --status -- "$sidecar_name" || return)
}

checksum_value() {
  local file="${1:-}" digest recorded expected
  verify_sha256_sidecar "$file" || return
  read -r digest recorded <"$file.sha256" || return
  expected="$(basename -- "$file")" || return
  [[ "$digest" =~ ^[0-9a-f]{64}$ && "$recorded" == "$expected" ]] || return 1
  printf 'sha256:%s' "$digest" || return
}

timestamp_evidence_bundle_checksum() {
  local evidence_dir="${1:-}" database_backup object_backup digest
  local database_checksum object_checksum fence_checksum restore_checksum
  local object_restore_checksum source_checksum draft_checksum
  [[ -n "$evidence_dir" ]] || return 1
  database_backup="$(latest_database_backup "$evidence_dir")" || return
  object_backup="$(latest_object_backup "$evidence_dir")" || return
  database_checksum="$(checksum_value "$database_backup")" || return
  object_checksum="$(checksum_value "$object_backup")" || return
  fence_checksum="$(checksum_value "$evidence_dir/fence-id")" || return
  restore_checksum="$(checksum_value "$evidence_dir/restore-inspection.json")" || return
  object_restore_checksum="$(checksum_value "$evidence_dir/object-restore-inspection.json")" || return
  source_checksum="$(checksum_value "$evidence_dir/source-inspection.json")" || return
  draft_checksum="$(checksum_value "$evidence_dir/draft-manifest.json")" || return
  digest="$({
    printf 'distr.timestamp-expand-evidence/v1\n'
    printf 'postgres-backup=%s\n' "$database_checksum"
    printf 'object-backup=%s\n' "$object_checksum"
    printf 'fence-id=%s\n' "$fence_checksum"
    printf 'restore-inspection=%s\n' "$restore_checksum"
    printf 'object-restore-inspection=%s\n' "$object_restore_checksum"
    printf 'source-inspection=%s\n' "$source_checksum"
    printf 'draft-manifest=%s\n' "$draft_checksum"
  } | sha256sum | awk '{print $1}')" || return
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || return 1
  printf 'sha256:%s' "$digest" || return
}

write_timestamp_evidence_bundle() {
  local evidence_dir="${1:-}" bundle expected temporary recorded label
  [[ -n "$evidence_dir" ]] || return 1
  bundle="$evidence_dir/evidence-bundle.sha256"
  expected="$(timestamp_evidence_bundle_checksum "$evidence_dir")" || return
  if [[ -e "$bundle" || -L "$bundle" ]]; then
    [[ -f "$bundle" && ! -L "$bundle" ]] || return 1
    path_mode_is "$bundle" 600 || return
    read -r recorded label <"$bundle" || return
    [[ "$recorded" == "$expected" &&
       "$label" == timestamp-evidence-bundle-v1 ]] || return 1
    return 0
  fi
  temporary="$(mktemp "$evidence_dir/.evidence-bundle.XXXXXX")" || return
  printf '%s  timestamp-evidence-bundle-v1\n' "$expected" >"$temporary" || {
    rm -f -- "$temporary" || true
    return 1
  }
  chmod 0600 "$temporary" || {
    rm -f -- "$temporary" || true
    return 1
  }
  copy_file_create_new_0600 "$temporary" "$bundle" || {
    rm -f -- "$temporary" || true
    return 1
  }
  rm -f -- "$temporary" || return
}

verify_timestamp_evidence_bundle() {
  local evidence_dir="${1:-}" bundle recorded label expected
  [[ -n "$evidence_dir" ]] || return 1
  bundle="$evidence_dir/evidence-bundle.sha256"
  [[ -f "$bundle" && ! -L "$bundle" ]] || return 1
  path_mode_is "$bundle" 600 || return
  read -r recorded label <"$bundle" || return
  expected="$(timestamp_evidence_bundle_checksum "$evidence_dir")" || return
  [[ "$recorded" == "$expected" &&
     "$label" == timestamp-evidence-bundle-v1 ]] || return 1
  printf '%s' "$expected" || return
}

reviewed_manifest_checksum() {
  local manifest="${1:-}" digest
  [[ -f "$manifest" && ! -L "$manifest" ]] || {
    die "approved manifest must be a regular non-symlink file"
    return 1
  }
  digest="$(checksum_value "$manifest")" || {
    die "approved manifest review checksum is missing, unsafe, or invalid"
    return 1
  }
  printf '%s' "$digest" || return
}

aggregate_volume_checksum() {
  local volume="${1:-}"
  docker run --rm -v "$volume:/data:ro" alpine:3.23 sh -ceu '
    set -o pipefail
    cd /data
    if find . ! -type f ! -type d -print -quit | grep -q .; then
      printf "reject: object volume contains a non-file/non-directory entry\n" >&2
      exit 42
    fi
    find . -mindepth 1 -print0 |
      LC_ALL=C sort -z |
      while IFS= read -r -d "" path; do
        mode="$(stat -c %a "$path")"
        uid="$(stat -c %u "$path")"
        gid="$(stat -c %g "$path")"
        if [ -d "$path" ]; then
          printf "type=D\\0path=%s\\0mode=%s\\0uid=%s\\0gid=%s\\0" \
            "$path" "$mode" "$uid" "$gid"
        else
          size="$(stat -c %s "$path")"
          digest="$(sha256sum "$path" | awk "{print \\$1}")"
          printf "type=F\\0path=%s\\0mode=%s\\0uid=%s\\0gid=%s\\0size=%s\\0sha256=%s\\0" \
            "$path" "$mode" "$uid" "$gid" "$size" "$digest"
        fi
      done |
      sha256sum |
      awk "{print \\$1}"
  ' || return
}

validate_timestamp_inspection_document() {
  local document="${1:-}"
  [[ -n "$document" ]] || return 1
  jq -e '
    type == "object" and
    (.sourceSchemaVersion | type == "number" and floor == . and . >= 0) and
    (.executionCount | type == "number" and floor == . and . >= 0) and
    (.eventCount | type == "number" and floor == . and . >= 0) and
    (.rawCellCount | type == "number" and floor == . and . >= 0) and
    (.rawCellChecksum | type == "string" and test("^sha256:[0-9a-f]{64}$")) and
    (.databaseIdentityChecksum | type == "string" and test("^sha256:[0-9a-f]{64}$")) and
    (.cells | type == "array") and
    (.rawCellCount == (.cells | length))
  ' "$document" >/dev/null || return
}

validate_object_restore_inspection_document() {
  local document="${1:-}"
  [[ -n "$document" ]] || return 1
  jq -e '
    type == "object" and
    (.sourceAggregateChecksum | type == "string" and test("^sha256:[0-9a-f]{64}$")) and
    (.restoredAggregateChecksum | type == "string" and test("^sha256:[0-9a-f]{64}$"))
  ' "$document" >/dev/null || return
}

compare_timestamp_inspections() {
  local source_json="${1:-}"
  local restore_json="${2:-}"
  local object_json="${3:-}"
  local source_fingerprint restore_fingerprint
  validate_timestamp_inspection_document "$source_json" || return
  validate_timestamp_inspection_document "$restore_json" || return
  validate_object_restore_inspection_document "$object_json" || return
  source_fingerprint="$(jq -c '[
    .sourceSchemaVersion,
    .executionCount,
    .eventCount,
    .rawCellCount,
    .rawCellChecksum,
    .databaseIdentityChecksum
  ]' "$source_json")" || return
  restore_fingerprint="$(jq -c '[
    .sourceSchemaVersion,
    .executionCount,
    .eventCount,
    .rawCellCount,
    .rawCellChecksum,
    .databaseIdentityChecksum
  ]' "$restore_json")" || return
  [[ "$source_fingerprint" == "$restore_fingerprint" ]] || {
    die "restored database inspection differs from fenced source"
    return 1
  }
  jq -e '
    .sourceAggregateChecksum ==
    .restoredAggregateChecksum
  ' "$object_json" >/dev/null || {
    die "restored object aggregate differs from fenced source"
    return 1
  }
}

run_timestamp_operator_container() (
  set -Eeuo pipefail
  local evidence_dir="${1:-}" database_url="${2:-}"
  local deployment_uid deployment_gid operator_id operator_name operator_timeout
  local label_key='distr.sh/timestamp-operator' cleanup_status=0
  local -a database_args=()
  shift 2 || return
  [[ -n "$evidence_dir" ]] || return 1
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  require_compose_env_parity || return
  deployment_uid="$(id -u)" || return
  deployment_gid="$(id -g)" || return
  operator_id="$(openssl rand -hex 16)" || return
  operator_name="distr-timestamp-operator-$operator_id"
  operator_timeout="${DISTR_TIMESTAMP_OPERATOR_TIMEOUT:-5m}"
  [[ "$operator_timeout" =~ ^[1-9][0-9]*[smh]$ ]] || {
    die "DISTR_TIMESTAMP_OPERATOR_TIMEOUT must be a positive s/m/h duration"
    return 1
  }
  if [[ -n "$database_url" ]]; then
    database_args=(-e "DATABASE_URL=$database_url")
  fi

  cleanup_timestamp_operator() {
    local status=$? recorded remaining sessions='' attempt sql
    trap - EXIT HUP INT TERM
    if recorded="$(docker inspect --format \
        "{{ index .Config.Labels \"$label_key\" }}" \
        "$operator_name" 2>/dev/null)"; then
      if [[ "$recorded" != "$operator_id" ]]; then
        die "timestamp operator cleanup refused an unowned container" || true
        cleanup_status=1
      else
        docker stop --time 15 "$operator_name" >/dev/null 2>&1 || true
        docker rm -f "$operator_name" >/dev/null 2>&1 || true
      fi
    fi
    remaining="$(docker ps -aq --filter "name=^/${operator_name}$")" || cleanup_status=1
    [[ -z "$remaining" ]] || cleanup_status=1
    if ((status != 0)); then
      sql="SELECT count(*) FROM pg_stat_activity WHERE application_name = 'distr-timestamp-operator-$operator_id' AND pid <> pg_backend_pid()"
      for ((attempt=1; attempt<=15; attempt++)); do
        sessions="$(
          DISTR_COMPOSE_ENV_FILE="$ENV_FILE" \
            timeout --signal=TERM --kill-after=2s 5s \
              docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
                exec -T postgres sh -ceu '
                  PGPASSWORD="$POSTGRES_PASSWORD" psql \
                    --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" \
                    -v ON_ERROR_STOP=1 -AtX -c "$1"
                ' sh "$sql"
        )" || {
          cleanup_status=1
          break
        }
        [[ "$sessions" =~ ^[0-9]+$ ]] || {
          cleanup_status=1
          break
        }
        [[ "$sessions" == 0 ]] && break
        sleep 1 || {
          cleanup_status=1
          break
        }
      done
      [[ "$sessions" == 0 ]] || cleanup_status=1
    fi
    if ((cleanup_status != 0)); then exit 1; fi
    exit "$status"
  }
  trap cleanup_timestamp_operator EXIT HUP INT TERM
  DISTR_COMPOSE_ENV_FILE="$ENV_FILE" \
    DISTR_TIMESTAMP_EVIDENCE_DIR="$evidence_dir" \
    timeout --signal=TERM --kill-after=15s "$operator_timeout" \
      docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
        --profile timestamp-operator run --rm \
        --name "$operator_name" \
        --label "$label_key=$operator_id" \
        --user "$deployment_uid:$deployment_gid" \
        -e "PGAPPNAME=distr-timestamp-operator-$operator_id" \
        "${database_args[@]}" \
        timestamp-operator "$@" || return
)

run_timestamp_operator() {
  local evidence_dir="${1:-}"
  shift || return
  run_timestamp_operator_container "$evidence_dir" '' "$@" || return
}

run_timestamp_operator_with_database() {
  local evidence_dir="${1:-}" database_url="${2:-}"
  shift 2 || return
  run_timestamp_operator_container "$evidence_dir" "$database_url" "$@" || return
}

evidence_fence_id() {
  local evidence_dir="${1:-}" fence_file fence_id
  [[ -n "$evidence_dir" ]] || return 1
  fence_file="$evidence_dir/fence-id"
  verify_sha256_sidecar "$fence_file" || return
  read -r fence_id <"$fence_file" || return
  [[ "$fence_id" =~ ^[A-Za-z0-9_+-]+$ ]] || return 1
  printf '%s' "$fence_id" || return
}

latest_database_backup() {
  local evidence_dir="${1:-}" fence_id path
  if [[ -n "$evidence_dir" ]]; then
    fence_id="$(evidence_fence_id "$evidence_dir")" || return
  else
    fence_id="$(fence_value FENCE_ID)" || return
  fi
  path="$BACKUP_DIR/postgres-$fence_id.dump"
  [[ -f "$path" && ! -L "$path" ]] || return 1
  printf '%s' "$path" || return
}

latest_object_backup() {
  local evidence_dir="${1:-}" fence_id path
  if [[ -n "$evidence_dir" ]]; then
    fence_id="$(evidence_fence_id "$evidence_dir")" || return
  else
    fence_id="$(fence_value FENCE_ID)" || return
  fi
  path="$BACKUP_DIR/rustfs-$fence_id.tar.gz"
  [[ -f "$path" && ! -L "$path" ]] || return 1
  printf '%s' "$path" || return
}

manifest_id() {
  local manifest="${1:-}"
  jq -er '.id | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' \
    "$manifest" || return
}

running_hub_digest() {
  local container configured
  container="$(compose ps -q hub)" || return
  [[ -n "$container" ]] || return 1
  configured="$(docker inspect --format '{{.Config.Image}}' "$container")" || return
  [[ "$configured" == *@sha256:* ]] || return 1
  printf '%s' "$configured" || return
}

verify_running_digest() {
  local actual
  actual="$(running_hub_digest)" || return
  [[ "$actual" == "$DISTR_IMAGE_REF" ]] || {
    die "running Hub image differs from DISTR_IMAGE_REF"
    return 1
  }
}

write_fence_id_evidence() {
  local fence_id="${1:-}"
  local evidence_dir="${2:-}" temporary
  temporary="$(mktemp "$evidence_dir/.fence-id.XXXXXX")" || return
  printf '%s\n' "$fence_id" >"$temporary" || {
    rm -f -- "$temporary" || true
    return 1
  }
  chmod 0600 -- "$temporary" || {
    rm -f -- "$temporary" || true
    return 1
  }
  copy_file_create_new_0600 "$temporary" "$evidence_dir/fence-id" || {
    rm -f -- "$temporary" || true
    return 1
  }
  rm -f -- "$temporary" || return
  write_sha256_sidecar_create_new "$evidence_dir/fence-id" || return
}

assert_hub_writers_stopped() {
  local running sessions callback_url
  need_cmd curl || return
  running="$(compose ps --status running -q hub)" || return
  [[ -z "$running" ]] || {
    die "a Compose Hub container is still running"
    return 1
  }
  sessions="$(compose exec -T postgres sh -ceu '
      PGPASSWORD="$POSTGRES_PASSWORD" psql \
        --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" \
        -v ON_ERROR_STOP=1 -AtX -c \
        "SELECT count(*) FROM pg_stat_activity WHERE application_name = '\''distr-hub'\'' AND pid <> pg_backend_pid()"
    ')" || return
  [[ "$sessions" == 0 ]] || {
    die "distr-hub PostgreSQL sessions remain"
    return 1
  }
  callback_url="${DISTR_CALLBACK_PROBE_URL:-http://127.0.0.1:${DISTR_HTTP_PORT:-8080}/api/v1/external-executions/00000000-0000-4000-8000-000000000000/callbacks}"
  [[ "$callback_url" =~ ^https?://(127\.0\.0\.1|localhost)(:[0-9]+)?/ ]] || {
    die "callback fence probe must use the local Hub endpoint"
    return 1
  }
  if curl --silent --show-error --connect-timeout 2 --max-time 3 \
      --output /dev/null "$callback_url"; then
    die "Hub callback endpoint remains reachable"
    return 1
  fi
}

verify_timestamp_evidence() {
  local approved="${1:-}"
  local evidence_dir="${2:-}"
  local database_backup object_backup file recorded_fence expected_fence
  local state target_commit target_digest target_ref evidence_checksum computed_bundle
  require_timestamp_fence "$evidence_dir" || return
  database_backup="$(latest_database_backup "$evidence_dir")" || return
  object_backup="$(latest_object_backup "$evidence_dir")" || return
  for file in \
      "$database_backup" "$object_backup" \
      "$evidence_dir/fence-id" \
      "$evidence_dir/restore-inspection.json" \
      "$evidence_dir/object-restore-inspection.json" \
      "$evidence_dir/source-inspection.json" \
      "$evidence_dir/draft-manifest.json"; do
    verify_sha256_sidecar "$file" || {
      die "missing, unsafe, or invalid evidence checksum: $file"
      return 1
    }
  done
  [[ -f "$approved" && ! -L "$approved" ]] || {
    die "approved manifest must be a regular non-symlink file"
    return 1
  }
  compare_timestamp_inspections \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/restore-inspection.json" \
    "$evidence_dir/object-restore-inspection.json" || return
  computed_bundle="$(verify_timestamp_evidence_bundle "$evidence_dir")" || return
  read -r recorded_fence <"$evidence_dir/fence-id" || return
  expected_fence="$(fence_value FENCE_ID)" || return
  [[ "$recorded_fence" == "$expected_fence" ]] || return 1
  state="$(jq -er '.state' "$approved")" || return
  target_commit="$(jq -er '.targetReleaseCommit' "$approved")" || return
  target_digest="$(jq -er '.targetImageDigest' "$approved")" || return
  evidence_checksum="$(jq -er '.evidenceBundleChecksum' "$approved")" || return
  target_ref="$(fence_value TARGET_IMAGE_DIGEST)" || return
  [[ "$state" == APPROVED &&
     "$target_commit" == "$DISTR_RELEASE_COMMIT" &&
     "$target_digest" == "$DISTR_IMAGE_DIGEST" &&
     "$target_ref" == "$DISTR_IMAGE_REF" &&
     "$evidence_checksum" == "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" &&
     "$computed_bundle" == "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" ]] || {
    die "approved manifest release/evidence binding differs from deployment"
    return 1
  }
  manifest_id "$approved" >/dev/null || return
}

validate_timestamp_apply_report() {
  local report="${1:-}" manifest="${2:-}" source="${3:-}"
  local expected_dry_run="${4:-}"
  local expected_population_count="${5:-}"
  [[ "$expected_dry_run" == true || "$expected_dry_run" == false ]] || return 1
  [[ -z "$expected_population_count" || "$expected_population_count" =~ ^[0-9]+$ ]] || return 1
  jq -e --argjson expectedDryRun "$expected_dry_run" \
    --arg expectedPopulationCount "$expected_population_count" \
    --slurpfile manifest "$manifest" --slurpfile source "$source" '
    . as $r |
    $manifest[0] as $m |
    $source[0] as $s |
    ($r | type == "object") and
    ($r.manifestId == $m.id) and
    ($r.dryRun == $expectedDryRun) and
    ($r.idempotent | type == "boolean") and
    (["provenCount","attestedCount","unresolvedCount","nullCount",
      "provenanceRows","wouldPopulateCount","populatedShadowCount"] |
      all(. as $key | ($r[$key] | type == "number" and floor == . and . >= 0))) and
    ($r.provenCount == ([$m.cells[] | select(.decision == "PROVEN")] | length)) and
    ($r.attestedCount == ([$m.cells[] | select(.decision == "ATTESTED")] | length)) and
    ($r.unresolvedCount == ([$m.cells[] | select(.decision == "UNRESOLVED")] | length)) and
    ($r.nullCount == ([$m.cells[] | select(.decision == "NULL_VALUE")] | length)) and
    (($r.provenCount + $r.attestedCount + $r.unresolvedCount + $r.nullCount) == $m.rawCellCount) and
    (if $expectedDryRun and (($m.supersedesManifestId // null) == null) then
       ($r.wouldPopulateCount == ($r.provenCount + $r.attestedCount))
     else
       ($r.wouldPopulateCount <= ($r.provenCount + $r.attestedCount))
     end) and
    ($r.rawSetChecksum == $m.rawCellChecksum) and
    ($r.databaseIdentityChecksum == $m.databaseIdentityChecksum) and
    ($r.rawSetChecksum == $s.rawCellChecksum) and
    ($r.databaseIdentityChecksum == $s.databaseIdentityChecksum) and
    (if $expectedDryRun then
       ($r.provenanceRows == 0 and $r.populatedShadowCount == 0)
     elif $r.idempotent then
       ($r.provenanceRows == $m.rawCellCount and
        $r.wouldPopulateCount == 0 and $r.populatedShadowCount == 0)
     else
       ($r.provenanceRows == $m.rawCellCount and
        ($expectedPopulationCount | test("^[0-9]+$")) and
        (($expectedPopulationCount | tonumber) == $r.wouldPopulateCount) and
        $r.populatedShadowCount == $r.wouldPopulateCount)
     end)
  ' "$report" >/dev/null || return
}

run_timestamp_apply_report() (
  set -Eeuo pipefail
  local evidence_dir="${1:-}" manifest="${2:-}" expected_dry_run="${3:-}"
  local output="${4:-}" expected_population_count="${5:-}" temporary=''
  local directory owner current_uid
  shift 5 || return
  [[ -n "$evidence_dir" && -n "$manifest" && -n "$output" ]] || return 1
  directory="$(dirname -- "$output")" || return
  if [[ -e "$output" || -L "$output" ]]; then
    [[ -f "$output" && ! -L "$output" ]] || {
      die "timestamp apply report must be a regular non-symlink file"
      return 1
    }
    path_mode_is "$output" 600 || {
      die "timestamp apply report mode must be 0600"
      return 1
    }
    current_uid="$(id -u)" || return
    owner="$(stat -c '%u' -- "$output")" || return
    [[ "$owner" == "$current_uid" ]] || {
      die "timestamp apply report must be owned by the deployment user"
      return 1
    }
    require_secure_state_directory "$directory" || return
    if [[ -e "$output.sha256" || -L "$output.sha256" ]]; then
      verify_sha256_sidecar "$output" || return
    else
      validate_timestamp_apply_report "$output" "$manifest" \
        "$evidence_dir/source-inspection.json" "$expected_dry_run" \
        "$expected_population_count" || return
      write_sha256_sidecar_create_new "$output" || return
      return 0
    fi
    validate_timestamp_apply_report "$output" "$manifest" \
      "$evidence_dir/source-inspection.json" "$expected_dry_run" \
      "$expected_population_count" || return
    return 0
  fi
  [[ ! -e "$output.sha256" && ! -L "$output.sha256" ]] || {
    die "timestamp apply report checksum exists without its report"
    return 1
  }
  temporary="$(mktemp "$evidence_dir/.timestamp-apply-report.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
  run_timestamp_operator "$evidence_dir" "$@" >"$temporary" || return
  chmod 0600 "$temporary" || return
  validate_timestamp_apply_report "$temporary" "$manifest" \
    "$evidence_dir/source-inspection.json" "$expected_dry_run" \
    "$expected_population_count" || return
  copy_file_create_new_0600 "$temporary" "$output" || return
  write_sha256_sidecar_create_new "$output" || return
)

run_timestamp_idempotent_apply_report() (
  set -Eeuo pipefail
  local evidence_dir="${1:-}" manifest="${2:-}" output="${3:-}"
  local expected_population_count="${4:-}" temporary='' had_report=0
  shift 4 || return
  [[ -n "$evidence_dir" && -n "$manifest" && -n "$output" ]] || return 1
  if [[ -e "$output" || -L "$output" ||
        -e "$output.sha256" || -L "$output.sha256" ]]; then
    had_report=1
  fi
  if ((had_report == 0)); then
    temporary="$(mktemp "$evidence_dir/.timestamp-idempotent-check.XXXXXX")" || return
    trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
    run_timestamp_operator "$evidence_dir" "$@" >"$temporary" || return
    chmod 0600 "$temporary" || return
    validate_timestamp_apply_report "$temporary" "$manifest" \
      "$evidence_dir/source-inspection.json" false \
      "$expected_population_count" || return
    jq -e '.idempotent == true' "$temporary" >/dev/null || {
      die "verified timestamp resume did not produce an idempotent report"
      return 1
    }
    copy_file_create_new_0600 "$temporary" "$output" || return
    write_sha256_sidecar_create_new "$output" || return
    return 0
  fi

  run_timestamp_apply_report "$evidence_dir" "$manifest" false \
    "$output" "$expected_population_count" "$@" || return
  temporary="$(mktemp "$evidence_dir/.timestamp-idempotent-check.XXXXXX")" || return
  trap 'rm -f -- "${temporary:-}" >/dev/null 2>&1 || true' EXIT HUP INT TERM
  run_timestamp_operator "$evidence_dir" "$@" >"$temporary" || return
  chmod 0600 "$temporary" || return
  validate_timestamp_apply_report "$temporary" "$manifest" \
    "$evidence_dir/source-inspection.json" false \
    "$expected_population_count" || return
  jq -e '.idempotent == true' "$temporary" >/dev/null || {
    die "stored timestamp manifest does not exactly match the approved manifest"
    return 1
  }
)

timestamp_apply_expected_population() {
  local report="${1:-}"
  jq -er '.wouldPopulateCount | select(type == "number" and floor == . and . >= 0)' \
    "$report" || return
}

verify_applied_population_count() {
  local manifest="${1:-}" expected="${2:-}" actual
  [[ "$manifest" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ &&
     "$expected" =~ ^[0-9]+$ ]] || return 1
  actual="$(postgres_scalar "SELECT count(*) FROM ExternalExecutionTimestampCellProvenance WHERE manifest_id = '$manifest'::uuid AND decision IN ('PROVEN','ATTESTED') AND converted_value IS NOT NULL")" || return
  [[ "$actual" == "$expected" ]] || {
    die "applied timestamp population count differs from retained dry-run expectation"
    return 1
  }
}

postgres_scalar() {
  local sql="${1:-}"
  compose exec -T postgres sh -ceu '
      PGPASSWORD="$POSTGRES_PASSWORD" psql \
        --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" \
        -v ON_ERROR_STOP=1 -AtX -c "$1"
    ' sh "$sql" || return
}

current_schema_version() {
  postgres_scalar 'SELECT version FROM schema_migrations' || return
}

current_schema_status() {
  postgres_scalar "SELECT version::text || ':' || dirty::text FROM schema_migrations" || return
}

timestamp_expand_apply_phase() {
  local approved_id="${1:-}" expected_execution_count="${2:-}"
  local expected_event_count="${3:-}" expected_raw_cell_count="${4:-}"
  local status phase_row
  [[ "$approved_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ &&
     "$expected_execution_count" =~ ^[0-9]+$ &&
     "$expected_event_count" =~ ^[0-9]+$ &&
     "$expected_raw_cell_count" =~ ^[0-9]+$ ]] || return 1
  status="$(current_schema_status)" || return
  case "$status" in
    137:false)
      printf 'SCHEMA_137'
      return 0
      ;;
    138:false) ;;
    *)
      die "timestamp expand apply requires clean schema 137 or 138"
      return 1
      ;;
  esac
  phase_row="$(postgres_scalar "SELECT
    (SELECT count(*) FROM ExternalExecutionTimestampManifest)::text || ':' ||
    (SELECT count(*) FROM ExternalExecutionTimestampCellProvenance)::text || ':' ||
    (SELECT count(*) FROM ExternalExecutionTimestampManifest WHERE id = '$approved_id'::uuid)::text || ':' ||
    COALESCE((SELECT state FROM ExternalExecutionTimestampManifest WHERE id = '$approved_id'::uuid), 'ABSENT') || ':' ||
    (SELECT count(*) FROM ExternalExecutionTimestampExpandState)::text || ':' ||
    COALESCE((SELECT transition_kind FROM ExternalExecutionTimestampExpandState WHERE singleton), 'ABSENT') || ':' ||
    COALESCE((SELECT source_schema_version::text FROM ExternalExecutionTimestampExpandState WHERE singleton), '0') || ':' ||
    COALESCE((SELECT transition_execution_count::text FROM ExternalExecutionTimestampExpandState WHERE singleton), '0') || ':' ||
    COALESCE((SELECT transition_event_count::text FROM ExternalExecutionTimestampExpandState WHERE singleton), '0') || ':' ||
    COALESCE((SELECT transition_raw_cell_count::text FROM ExternalExecutionTimestampExpandState WHERE singleton), '0')")" || return
  case "$phase_row" in
    "0:0:0:ABSENT:1:MANIFEST_REQUIRED:137:$expected_execution_count:$expected_event_count:$expected_raw_cell_count")
      printf 'SCHEMA_138_EMPTY'
      ;;
    "1:$expected_raw_cell_count:1:VERIFIED:1:MANIFEST_REQUIRED:137:$expected_execution_count:$expected_event_count:$expected_raw_cell_count")
      printf 'SCHEMA_138_VERIFIED'
      ;;
    *)
      die "timestamp expand database phase conflicts with the approved root manifest"
      return 1
      ;;
  esac
}

timestamp_manifest_transition_counts() {
  local manifest="${1:-}"
  jq -er '
    select(
      (.executionCount | type == "number" and floor == . and . >= 0) and
      (.eventCount | type == "number" and floor == . and . >= 0) and
      (.rawCellCount | type == "number" and floor == . and . >= 0) and
      (.rawCellCount == ((5 * .executionCount) + .eventCount))
    ) |
    [.executionCount, .eventCount, .rawCellCount] |
    map(tostring) | join(":")
  ' "$manifest" || return
}

require_clean_schema_137() {
  local status
  status="$(postgres_scalar "SELECT version::text || ':' || dirty::text FROM schema_migrations")" || return
  [[ "$status" == 137:false ]] || {
    die "cancel requires clean schema 137"
    return 1
  }
}

require_clean_schema_138() {
  local status
  status="$(postgres_scalar "SELECT version::text || ':' || dirty::text FROM schema_migrations")" || return
  [[ "$status" == 138:false ]] || {
    die "post-start schema must be 138 and clean"
    return 1
  }
}

require_verified_manifest() {
  local approved_id="${1:-}" state
  [[ "$approved_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] || return 1
  state="$(postgres_scalar "SELECT state FROM ExternalExecutionTimestampManifest WHERE id = '$approved_id'::uuid")" || return
  [[ "$state" == VERIFIED ]] || {
    die "timestamp manifest is not VERIFIED"
    return 1
  }
}

verify_post_start_counts() {
  local source="${1:-}" expected actual
  local expected_executions expected_events actual_executions actual_events
  expected="$(jq -er '[.executionCount,.eventCount] | map(tostring) | join(":")' "$source")" || return
  actual="$(postgres_scalar "SELECT (SELECT count(*) FROM ExternalExecution)::text || ':' || (SELECT count(*) FROM ExternalExecutionEvent)::text")" || return
  [[ "$expected" =~ ^[0-9]+:[0-9]+$ && "$actual" =~ ^[0-9]+:[0-9]+$ ]] || return 1
  IFS=: read -r expected_executions expected_events <<<"$expected" || return
  IFS=: read -r actual_executions actual_events <<<"$actual" || return
  ((actual_executions >= expected_executions &&
    actual_events >= expected_events)) || {
    die "post-start execution/event counts decreased below the fenced source"
    return 1
  }
}

verify_audit_history_visibility() {
  local approved_id="${1:-}"
  local evidence_dir="${2:-}"
  local acceptance_base_url="${3:-}"
  local audit_url="${DISTR_AUDIT_HISTORY_PROBE_URL:-}"
  local audit_token="${DISTR_AUDIT_HISTORY_PROBE_TOKEN:-}" temporary expected_execution_id
  run_timestamp_operator "$evidence_dir" external-execution-timestamps verify \
    --manifest-id "$approved_id" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps readiness >/dev/null || return
  expected_execution_id="$(audit_probe_execution_id \
    "$evidence_dir/source-inspection.json")" || return
  if [[ -n "$acceptance_base_url" ]]; then
    [[ "$acceptance_base_url" =~ ^http://127\.0\.0\.1:[0-9]+$ ]] || return 1
    audit_url="$acceptance_base_url/api/v1/external-executions/$expected_execution_id/"
  fi
  need_cmd curl || return
  need_cmd jq || return
  temporary="$(mktemp "$evidence_dir/.audit-history-probe.XXXXXX")" || return
  printf 'header = "Authorization: Bearer %s"\n' "$audit_token" | \
    curl --config - --fail --silent --show-error --connect-timeout 2 --max-time 10 \
      --output "$temporary" "$audit_url" || {
    rm -f -- "$temporary" || true
    return 1
  }
  jq -e --arg executionID "$expected_execution_id" '
    type == "object" and
    .id == $executionID and
    (.events | type == "array" and length > 0) and
    all(.events[];
      (.id | type == "string") and
      (.sequence | type == "number" and floor == . and . >= 0))
  ' \
    "$temporary" >/dev/null || {
    rm -f -- "$temporary" || true
    return 1
  }
  rm -f -- "$temporary" || return
}

verify_task_lock_integrity() {
  local defects
  defects="$(postgres_scalar "WITH duplicate_active AS (SELECT 1 FROM TaskResourceLock WHERE acquired_at IS NOT NULL AND released_at IS NULL AND concurrency_policy <> 'ALLOW_PARALLEL' GROUP BY organization_id,resource_type,resource_key HAVING count(*) > 1) SELECT (SELECT count(*) FROM duplicate_active)::text || ':' || (SELECT count(*) FROM TaskResourceLock WHERE acquired_at IS NULL AND released_at IS NOT NULL)::text")" || return
  [[ "$defects" == 0:0 ]] || {
    die "task-resource lock integrity check failed"
    return 1
  }
}

verify_no_duplicate_event_sequence() {
  local duplicates
  duplicates="$(postgres_scalar "SELECT count(*) FROM (SELECT external_execution_id,sequence FROM ExternalExecutionEvent GROUP BY external_execution_id,sequence HAVING count(*) > 1) duplicate_sequence")" || return
  [[ "$duplicates" == 0 ]] || {
    die "duplicate external-execution event sequence found"
    return 1
  }
}

TIMESTAMP_ACCEPTANCE_NAME=''
TIMESTAMP_ACCEPTANCE_URL=''
TIMESTAMP_ACCEPTANCE_OWNER=''

stop_fenced_hub_if_running() {
  stop_hub || return
}

timestamp_acceptance_container_owned_by_fence() {
  local container="${1:-}" fence_id="${2:-}" target_ref="${3:-}"
  local label_key='distr.sh/timestamp-acceptance' name configured recorded
  [[ "$container" =~ ^[A-Za-z0-9_.-]+$ &&
     "$fence_id" =~ ^[A-Za-z0-9_+-]+$ &&
     "$target_ref" =~ ^[A-Za-z0-9._/+:-]+@sha256:[0-9a-f]{64}$ ]] || return 1
  name="$(docker inspect --format '{{.Name}}' "$container")" || return
  name="${name#/}"
  [[ "$name" =~ ^distr-timestamp-acceptance-[0-9a-f]{32}$ ]] || {
    die "timestamp acceptance cleanup refused an unexpected container name"
    return 1
  }
  configured="$(docker inspect --format '{{.Config.Image}}' "$container")" || return
  recorded="$(docker inspect --format \
    "{{ index .Config.Labels \"$label_key\" }}" "$container")" || return
  [[ "$configured" == "$target_ref" && "$recorded" == "$fence_id" ]] || {
    die "timestamp acceptance cleanup refused an unowned container"
    return 1
  }
}

cleanup_fenced_acceptance_hubs() {
  local fence_id="${1:-}" target_ref label_key='distr.sh/timestamp-acceptance'
  local container enumeration remaining
  local -a containers=()
  [[ "$fence_id" =~ ^[A-Za-z0-9_+-]+$ ]] || return 1
  need_cmd docker || return
  target_ref="$(fence_value TARGET_IMAGE_DIGEST)" || return
  [[ "$target_ref" =~ ^[A-Za-z0-9._/+:-]+@sha256:[0-9a-f]{64}$ ]] || return 1
  enumeration="$(docker ps -aq --filter \
    "label=$label_key=$fence_id")" || return
  if [[ -n "$enumeration" ]]; then
    mapfile -t containers <<<"$enumeration" || return
  fi
  for container in "${containers[@]}"; do
    timestamp_acceptance_container_owned_by_fence \
      "$container" "$fence_id" "$target_ref" || return
  done
  for container in "${containers[@]}"; do
    timestamp_acceptance_container_owned_by_fence \
      "$container" "$fence_id" "$target_ref" || return
    docker stop --time 45 "$container" >/dev/null || return
    docker rm -f "$container" >/dev/null || return
  done
  remaining="$(docker ps -aq --filter "label=$label_key=$fence_id")" || return
  [[ -z "$remaining" ]] || {
    die "timestamp acceptance containers remain after cleanup"
    return 1
  }
}

start_isolated_acceptance_hub() {
  local nonce fence_id label_key='distr.sh/timestamp-acceptance'
  local published configured aliases alias
  [[ -z "$TIMESTAMP_ACCEPTANCE_NAME" ]] || return 1
  nonce="$(openssl rand -hex 16)" || return
  fence_id="$(fence_value FENCE_ID)" || return
  [[ "$fence_id" =~ ^[A-Za-z0-9_+-]+$ ]] || return 1
  TIMESTAMP_ACCEPTANCE_NAME="distr-timestamp-acceptance-$nonce"
  TIMESTAMP_ACCEPTANCE_OWNER="$fence_id"
  DISTR_COMPOSE_ENV_FILE="$ENV_FILE" \
    docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" \
      run -d --no-deps \
      --name "$TIMESTAMP_ACCEPTANCE_NAME" \
      --label "$label_key=$TIMESTAMP_ACCEPTANCE_OWNER" \
      --publish '127.0.0.1::8080' \
      hub >/dev/null || return
  configured="$(docker inspect --format '{{.Config.Image}}' \
    "$TIMESTAMP_ACCEPTANCE_NAME")" || return
  [[ "$configured" == "$DISTR_IMAGE_REF" ]] || {
    die "isolated acceptance Hub image differs from DISTR_IMAGE_REF"
    return 1
  }
  aliases="$(docker inspect --format \
    '{{range $network := .NetworkSettings.Networks}}{{range $network.Aliases}}{{println .}}{{end}}{{end}}' \
    "$TIMESTAMP_ACCEPTANCE_NAME")" || return
  while IFS= read -r alias; do
    [[ "$alias" != distr-hub ]] || {
      die "isolated acceptance Hub unexpectedly received the public service alias"
      return 1
    }
  done <<<"$aliases"
  published="$(docker port "$TIMESTAMP_ACCEPTANCE_NAME" 8080/tcp)" || return
  [[ "$published" =~ ^127\.0\.0\.1:([0-9]+)$ ]] || {
    die "isolated acceptance Hub must publish one random loopback port"
    return 1
  }
  TIMESTAMP_ACCEPTANCE_URL="http://$published"
}

stop_isolated_acceptance_hub() {
  local label_key='distr.sh/timestamp-acceptance' recorded remaining
  [[ -n "$TIMESTAMP_ACCEPTANCE_NAME" ]] || return 0
  if recorded="$(docker inspect --format \
      "{{ index .Config.Labels \"$label_key\" }}" \
      "$TIMESTAMP_ACCEPTANCE_NAME" 2>/dev/null)"; then
    [[ "$recorded" == "$TIMESTAMP_ACCEPTANCE_OWNER" ]] || {
      die "refusing to remove an unowned acceptance Hub"
      return 1
    }
    docker stop --time 45 "$TIMESTAMP_ACCEPTANCE_NAME" >/dev/null || return
    docker rm -f "$TIMESTAMP_ACCEPTANCE_NAME" >/dev/null || return
  fi
  remaining="$(docker ps -aq --filter \
    "name=^/${TIMESTAMP_ACCEPTANCE_NAME}$")" || return
  [[ -z "$remaining" ]] || return 1
  TIMESTAMP_ACCEPTANCE_NAME=''
  TIMESTAMP_ACCEPTANCE_URL=''
  TIMESTAMP_ACCEPTANCE_OWNER=''
}

health_at_url() {
  local url="${1:-}" attempt
  [[ "$url" =~ ^http://127\.0\.0\.1:[0-9]+/ready$ ]] || return 1
  for ((attempt=1; attempt<=60; attempt++)); do
    if curl -fsS "$url" >/dev/null; then return 0; fi
    sleep 1 || return
  done
  die "isolated acceptance Hub did not become ready"
  return 1
}

verify_isolated_acceptance_digest() {
  local configured
  [[ -n "$TIMESTAMP_ACCEPTANCE_NAME" ]] || return 1
  configured="$(docker inspect --format '{{.Config.Image}}' \
    "$TIMESTAMP_ACCEPTANCE_NAME")" || return
  [[ "$configured" == "$DISTR_IMAGE_REF" ]] || return 1
}

verify_timestamp_expand_integrity() {
  local approved_id="${1:-}"
  local evidence_dir="${2:-}"
  local acceptance_base_url="${3:-}"
  require_clean_schema_138 || return
  require_verified_manifest "$approved_id" || return
  verify_post_start_counts "$evidence_dir/source-inspection.json" || return
  verify_audit_history_visibility "$approved_id" "$evidence_dir" \
    "$acceptance_base_url" || return
  verify_task_lock_integrity || return
  verify_no_duplicate_event_sequence || return
}

start_verify_and_finalize_timestamp_expand() (
  local approved_id="${1:-}"
  local evidence_dir="${2:-}" complete=0
  cleanup_failed_start() {
    local status=$? cleanup_status=0
    trap - EXIT HUP INT TERM
    stop_isolated_acceptance_hub || cleanup_status=1
    if ((complete == 0)); then
      stop_hub || cleanup_status=1
      assert_hub_writers_stopped || cleanup_status=1
    fi
    if ((cleanup_status != 0)); then exit 1; fi
    exit "$status"
  }
  trap cleanup_failed_start EXIT HUP INT TERM
  start_isolated_acceptance_hub || return
  health_at_url "$TIMESTAMP_ACCEPTANCE_URL/ready" || return
  verify_isolated_acceptance_digest || return
  verify_timestamp_expand_integrity "$approved_id" "$evidence_dir" \
    "$TIMESTAMP_ACCEPTANCE_URL" || return
  stop_isolated_acceptance_hub || return
  start_hub || return
  health || return
  verify_running_digest || return
  verify_timestamp_expand_integrity "$approved_id" "$evidence_dir" || return
  persist_timestamp_compatibility "$approved_id" || return
  clear_timestamp_fence "$evidence_dir" || return
  complete=1
)

start_verify_cancel_and_clear() (
  local evidence_dir="${1:-}" complete=0
  cleanup_failed_cancel() {
    local status=$?
    trap - EXIT HUP INT TERM
    if ((complete == 0)); then
      stop_hub || true
      assert_hub_writers_stopped || true
    fi
    exit "$status"
  }
  trap cleanup_failed_cancel EXIT HUP INT TERM
  start_hub || return
  health || return
  verify_running_digest || return
  clear_timestamp_fence "$evidence_dir" || return
  complete=1
)

run_migration_preflight() {
  local evidence_dir="${DISTR_TIMESTAMP_EVIDENCE_DIR:-$BACKUP_DIR/operator-evidence}" status
  prepare_backup_directory "$BACKUP_DIR" || return
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  run_timestamp_operator "$evidence_dir" migrate --check || {
    status=$?
    info 'preflight refused migration; stage the expand workflow first:' || true
    info '  deploy.sh timestamp-expand-capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"' || true
    info '  deploy.sh timestamp-expand-apply "$DISTR_TIMESTAMP_APPROVED_MANIFEST" "$DISTR_TIMESTAMP_EVIDENCE_DIR"' || true
    return "$status"
  }
}

run_timestamp_migration_138() {
  local evidence_dir="${1:-}"
  run_timestamp_operator "$evidence_dir" migrate --to 138 \
    --external-execution-timestamp-manifest \
    /evidence/approved-manifest.json || return
}

backup_and_restore_timestamp_evidence() (
  set -Eeuo pipefail
  local evidence_dir="${1:-}" evidence_id="${2:-}"
  local project source_object_volume host_uid host_gid ready attempt nonce
  local database_backup object_backup database_restore_container
  local database_restore_volume object_restore_volume restore_password
  local source_object_checksum restored_object_checksum object_json
  local database_backup_name object_backup_name restore_owner
  local database_tmp='' object_tmp='' object_archive_tmp=''
  local database_volume_created=0 object_volume_created=0 container_created=0
  local complete=0 cleanup_status=0
  local restore_label='distr.sh/timestamp-restore'
  if [[ -z "$evidence_id" ]]; then
    evidence_id="$(fence_value FENCE_ID)" || return
  fi
  [[ "$evidence_id" =~ ^[A-Za-z0-9_+-]+$ ]] || return 1
  [[ -n "$evidence_dir" ]] || return 1
  project="${COMPOSE_PROJECT_NAME:-distr-prod}"
  source_object_volume="${project}_rustfs"
  database_backup="$BACKUP_DIR/postgres-$evidence_id.dump"
  object_backup="$BACKUP_DIR/rustfs-$evidence_id.tar.gz"
  nonce="$(openssl rand -hex 8)" || return
  restore_owner="${evidence_id}_$nonce"
  database_restore_container="${project}-timestamp-pg-$evidence_id-$nonce"
  database_restore_volume="${project}_timestamp_pg_${evidence_id}_$nonce"
  object_restore_volume="${project}_timestamp_object_${evidence_id}_$nonce"
  restore_password="$(openssl rand -hex 24)" || return
  object_json="$evidence_dir/object-restore-inspection.json"
  database_backup_name="$(basename -- "$database_backup")" || return
  object_backup_name="$(basename -- "$object_backup")" || return
  host_uid="${SUDO_UID:-$(id -u)}" || return
  host_gid="${SUDO_GID:-$(id -g)}" || return

  cleanup_owned_restore_container() {
    local recorded remaining
    ((container_created == 1)) || return 0
    if ! recorded="$(docker inspect --format \
      "{{ index .Config.Labels \"$restore_label\" }}" \
      "$database_restore_container" 2>/dev/null)"; then
      remaining="$(docker ps -aq --filter \
        "name=^/${database_restore_container}$")" || return
      [[ -z "$remaining" ]] || return 1
      return 0
    fi
    [[ "$recorded" == "$restore_owner" ]] || return 1
    docker rm -f "$database_restore_container" >/dev/null 2>&1 || return
  }
  cleanup_owned_restore_volume() {
    local volume="$1" created="$2" recorded remaining
    ((created == 1)) || return 0
    if ! recorded="$(docker volume inspect --format \
      "{{ index .Labels \"$restore_label\" }}" "$volume" 2>/dev/null)"; then
      remaining="$(docker volume ls -q --filter "name=^${volume}$")" || return
      [[ -z "$remaining" ]] || return 1
      return 0
    fi
    [[ "$recorded" == "$restore_owner" ]] || return 1
    docker volume rm -f "$volume" >/dev/null 2>&1 || return
  }
  assert_owned_restore_resources_absent() {
    local remaining
    remaining="$(docker ps -aq --filter \
      "name=^/${database_restore_container}$")" || return
    [[ -z "$remaining" ]] || return 1
    remaining="$(docker volume ls -q --filter \
      "name=^${database_restore_volume}$")" || return
    [[ -z "$remaining" ]] || return 1
    remaining="$(docker volume ls -q --filter \
      "name=^${object_restore_volume}$")" || return
    [[ -z "$remaining" ]] || return 1
  }
  cleanup_timestamp_restore() {
    local status=$? file
    trap - EXIT HUP INT TERM
    cleanup_owned_restore_container || cleanup_status=1
    cleanup_owned_restore_volume "$database_restore_volume" \
      "$database_volume_created" || cleanup_status=1
    cleanup_owned_restore_volume "$object_restore_volume" \
      "$object_volume_created" || cleanup_status=1
    assert_owned_restore_resources_absent || cleanup_status=1
    rm -f -- "${database_tmp:-}" "${object_tmp:-}" \
      "${object_archive_tmp:-}" >/dev/null 2>&1 || cleanup_status=1
    if ((complete == 0)); then
      for file in \
          "$database_backup" "$database_backup.sha256" \
          "$object_backup" "$object_backup.sha256" \
          "$evidence_dir/restore-inspection.json" \
          "$evidence_dir/restore-inspection.json.sha256" \
          "$object_json" "$object_json.sha256"; do
        remove_owned_regular_file_if_present "$file" || cleanup_status=1
      done
    fi
    if ((cleanup_status != 0)); then exit 1; fi
    exit "$status"
  }
  trap cleanup_timestamp_restore EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  prepare_backup_directory "$BACKUP_DIR" || return
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  [[ ! -e "$database_backup" && ! -L "$database_backup" &&
     ! -e "$object_backup" && ! -L "$object_backup" ]] || {
    die "evidence-specific backup already exists"
    return 1
  }
  docker volume inspect "$source_object_volume" >/dev/null || {
    die "source object volume is missing"
    return 1
  }

  database_tmp="$(mktemp "$BACKUP_DIR/.postgres-$evidence_id.XXXXXX")" || return
  chmod 0600 "$database_tmp" || return
  compose exec -T postgres sh -ceu '
    PGPASSWORD="$POSTGRES_PASSWORD" pg_dump \
      --username="$POSTGRES_USER" \
      --dbname="$POSTGRES_DB" \
      --format=custom
  ' >"$database_tmp" || return
  sync -f "$database_tmp" || return
  copy_file_create_new_0600 "$database_tmp" "$database_backup" || return
  rm -- "$database_tmp" || return
  database_tmp=''
  write_sha256_sidecar_create_new "$database_backup" || return

  object_archive_tmp="$(mktemp "$BACKUP_DIR/.rustfs-$evidence_id.XXXXXX")" || return
  local object_archive_tmp_name
  object_archive_tmp_name="$(basename -- "$object_archive_tmp")" || return
  docker run --rm \
    -v "$source_object_volume:/data:ro" \
    -v "$BACKUP_DIR:/backup" \
    alpine:3.23 \
    tar -C /data -czf "/backup/$object_archive_tmp_name" . || return
  docker run --rm -v "$BACKUP_DIR:/backup" alpine:3.23 \
    chown "$host_uid:$host_gid" "/backup/$object_archive_tmp_name" || return
  chmod 0600 "$object_archive_tmp" || return
  sync -f "$object_archive_tmp" || return
  copy_file_create_new_0600 "$object_archive_tmp" "$object_backup" || return
  rm -- "$object_archive_tmp" || return
  object_archive_tmp=''
  write_sha256_sidecar_create_new "$object_backup" || return
  source_object_checksum="$(aggregate_volume_checksum "$source_object_volume")" || return

  docker volume create --label "$restore_label=$restore_owner" \
    "$database_restore_volume" >/dev/null || return
  database_volume_created=1
  docker volume create --label "$restore_label=$restore_owner" \
    "$object_restore_volume" >/dev/null || return
  object_volume_created=1
  docker run -d --name "$database_restore_container" \
    --label "$restore_label=$restore_owner" \
    --network "${project}_default" \
    -v "$database_restore_volume:/var/lib/postgresql" \
    -v "$BACKUP_DIR:/backup:ro" \
    -e POSTGRES_USER=distr_restore \
    -e "POSTGRES_PASSWORD=$restore_password" \
    -e POSTGRES_DB=distr_restore \
    postgres:18.4-alpine3.23 >/dev/null || return
  container_created=1
  ready=0
  for ((attempt=1; attempt<=60; attempt++)); do
    if docker exec "$database_restore_container" \
      pg_isready -U distr_restore -d distr_restore >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1 || return
  done
  ((ready == 1)) || {
    die "restored PostgreSQL did not become ready"
    return 1
  }
  docker exec "$database_restore_container" \
    pg_isready -U distr_restore -d distr_restore >/dev/null || {
    die "restored PostgreSQL did not remain ready"
    return 1
  }
  docker exec -e "PGPASSWORD=$restore_password" \
    "$database_restore_container" \
    pg_restore --exit-on-error --no-owner --no-privileges \
      --username=distr_restore --dbname=distr_restore \
      "/backup/$database_backup_name" || return

  docker run --rm \
    -v "$object_restore_volume:/restore" \
    -v "$BACKUP_DIR:/backup:ro" \
    alpine:3.23 \
    tar -C /restore -xzf "/backup/$object_backup_name" || return
  restored_object_checksum="$(aggregate_volume_checksum "$object_restore_volume")" || return
  [[ "$source_object_checksum" == "$restored_object_checksum" ]] || {
    die "restored object aggregate differs from source"
    return 1
  }

  run_timestamp_operator_with_database \
    "$evidence_dir" \
    "postgres://distr_restore:$restore_password@$database_restore_container:5432/distr_restore?sslmode=disable" \
    external-execution-timestamps inspect \
    --output /evidence/restore-inspection.json || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/restore-inspection.json" || return

  object_tmp="$(mktemp "$evidence_dir/.object-restore.XXXXXX")" || return
  printf '{"sourceAggregateChecksum":"sha256:%s","restoredAggregateChecksum":"sha256:%s"}\n' \
    "$source_object_checksum" "$restored_object_checksum" >"$object_tmp" || return
  chmod 0600 "$object_tmp" || return
  copy_file_create_new_0600 "$object_tmp" "$object_json" || return
  write_sha256_sidecar_create_new "$object_json" || return
  rm -f "$object_tmp" || return
  object_tmp=''
  complete=1
)

backup_and_restore_release_evidence() {
  local stamp random release_id evidence_dir
  stamp="$(date -u +%Y%m%dT%H%M%SZ)" || return
  random="$(openssl rand -hex 8)" || return
  release_id="release_${stamp}_${random}"
  evidence_dir="$BACKUP_DIR/release-evidence-$stamp-$random"
  prepare_backup_directory "$BACKUP_DIR" || return
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  backup_and_restore_timestamp_evidence "$evidence_dir" "$release_id" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps inspect \
    --output /evidence/source-inspection.json || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/source-inspection.json" || return
  compare_timestamp_inspections \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/restore-inspection.json" \
    "$evidence_dir/object-restore-inspection.json" || return
  info "retained verified release evidence: $evidence_dir"
}

validate_source_inspection() {
  local source="${1:-}"
  local evidence_dir current object_document expected_object_checksum
  local current_object_digest current_object_checksum source_object_volume
  local recorded_fence active_fence
  evidence_dir="$(dirname -- "$source")" || return
  current="$evidence_dir/cancel-source-inspection.json"
  object_document="$evidence_dir/object-restore-inspection.json"
  verify_timestamp_evidence_bundle "$evidence_dir" >/dev/null || return
  recorded_fence="$(evidence_fence_id "$evidence_dir")" || return
  active_fence="$(fence_value FENCE_ID)" || return
  [[ "$recorded_fence" == "$active_fence" ]] || {
    die "captured evidence fence differs from the active fence"
    return 1
  }
  if [[ -e "$current" || -L "$current" ]]; then
    verify_sha256_sidecar "$current" || return
  else
    run_timestamp_operator "$evidence_dir" \
      external-execution-timestamps inspect \
      --output /evidence/cancel-source-inspection.json || return
    write_sha256_sidecar_create_new "$current" || return
  fi
  compare_timestamp_inspections "$source" "$current" \
    "$object_document" || return
  expected_object_checksum="$(jq -er '.sourceAggregateChecksum' "$object_document")" || return
  [[ "$expected_object_checksum" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  source_object_volume="${COMPOSE_PROJECT_NAME:-distr-prod}_rustfs"
  current_object_digest="$(aggregate_volume_checksum "$source_object_volume")" || return
  current_object_checksum="sha256:$current_object_digest"
  [[ "$current_object_checksum" == "$expected_object_checksum" ]] || {
    die "live object volume differs from captured source evidence"
    return 1
  }
}

ensure_fence_id_evidence() {
  local fence_id="${1:-}" evidence_dir="${2:-}" recorded
  [[ -n "$fence_id" && -n "$evidence_dir" ]] || return 1
  if [[ -e "$evidence_dir/fence-id" || -L "$evidence_dir/fence-id" ]]; then
    verify_sha256_sidecar "$evidence_dir/fence-id" || return
    read -r recorded <"$evidence_dir/fence-id" || return
    [[ "$recorded" == "$fence_id" ]] || {
      die "retained fence-id evidence differs from active fence"
      return 1
    }
    return 0
  fi
  write_fence_id_evidence "$fence_id" "$evidence_dir" || return
}

capture_evidence_complete() {
  local evidence_dir="${1:-}" database_backup object_backup
  [[ -n "$evidence_dir" ]] || return 1
  database_backup="$(latest_database_backup "$evidence_dir")" || return
  object_backup="$(latest_object_backup "$evidence_dir")" || return
  verify_sha256_sidecar "$database_backup" || return
  verify_sha256_sidecar "$object_backup" || return
  verify_sha256_sidecar "$evidence_dir/restore-inspection.json" || return
  verify_sha256_sidecar "$evidence_dir/object-restore-inspection.json" || return
  verify_sha256_sidecar "$evidence_dir/source-inspection.json" || return
  verify_sha256_sidecar "$evidence_dir/draft-manifest.json" || return
  cmp -s -- "$evidence_dir/source-inspection.json" \
    "$evidence_dir/draft-manifest.json" || return
  compare_timestamp_inspections \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/restore-inspection.json" \
    "$evidence_dir/object-restore-inspection.json" || return
  verify_timestamp_evidence_bundle "$evidence_dir" >/dev/null || return
}

capture_evidence_status() {
  local evidence_dir="${1:-}" fence_id file present=0
  local -a expected_files
  [[ -n "$evidence_dir" ]] || return 1
  fence_id="$(fence_value FENCE_ID)" || return
  expected_files=(
    "$BACKUP_DIR/postgres-$fence_id.dump"
    "$BACKUP_DIR/postgres-$fence_id.dump.sha256"
    "$BACKUP_DIR/rustfs-$fence_id.tar.gz"
    "$BACKUP_DIR/rustfs-$fence_id.tar.gz.sha256"
    "$evidence_dir/restore-inspection.json"
    "$evidence_dir/restore-inspection.json.sha256"
    "$evidence_dir/object-restore-inspection.json"
    "$evidence_dir/object-restore-inspection.json.sha256"
    "$evidence_dir/source-inspection.json"
    "$evidence_dir/source-inspection.json.sha256"
    "$evidence_dir/draft-manifest.json"
    "$evidence_dir/draft-manifest.json.sha256"
    "$evidence_dir/evidence-bundle.sha256"
  )
  for file in "${expected_files[@]}"; do
    if [[ -e "$file" || -L "$file" ]]; then ((present += 1)); fi
  done
  if ((present < ${#expected_files[@]})); then return 2; fi
  capture_evidence_complete "$evidence_dir" || {
    die "complete timestamp evidence failed integrity validation; refusing automatic deletion"
    return 1
  }
  return 0
}

remove_owned_regular_file_if_present() {
  local file="${1:-}" owner current_uid
  [[ -n "$file" ]] || return 1
  if [[ ! -e "$file" && ! -L "$file" ]]; then return 0; fi
  [[ -f "$file" && ! -L "$file" ]] || {
    die "unsafe partial capture path: $file"
    return 1
  }
  current_uid="$(id -u)" || return
  owner="$(stat -c '%u' -- "$file")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "partial capture path is not deployment-owned: $file"
    return 1
  }
  rm -- "$file" || return
}

reset_incomplete_capture_evidence() {
  local evidence_dir="${1:-}" fence_id file
  [[ -n "$evidence_dir" ]] || return 1
  [[ ! -e "$evidence_dir/approved-manifest.json" &&
     ! -L "$evidence_dir/approved-manifest.json" ]] || {
    die "capture cannot reset evidence after an approved manifest was staged"
    return 1
  }
  fence_id="$(fence_value FENCE_ID)" || return
  for file in \
      "$BACKUP_DIR/postgres-$fence_id.dump" \
      "$BACKUP_DIR/postgres-$fence_id.dump.sha256" \
      "$BACKUP_DIR/rustfs-$fence_id.tar.gz" \
      "$BACKUP_DIR/rustfs-$fence_id.tar.gz.sha256" \
      "$evidence_dir/restore-inspection.json" \
      "$evidence_dir/restore-inspection.json.sha256" \
      "$evidence_dir/object-restore-inspection.json" \
      "$evidence_dir/object-restore-inspection.json.sha256" \
      "$evidence_dir/source-inspection.json" \
      "$evidence_dir/source-inspection.json.sha256" \
      "$evidence_dir/draft-manifest.json" \
      "$evidence_dir/draft-manifest.json.sha256" \
      "$evidence_dir/evidence-bundle.sha256"; do
    remove_owned_regular_file_if_present "$file" || return
  done
  sync -f "$BACKUP_DIR" || return
  sync -f "$evidence_dir" || return
}

resume_fenced_target_image() {
  local target_ref="${1:-}" target_digest target_commit
  [[ "$target_ref" =~ ^[A-Za-z0-9._/+:-]+@sha256:[0-9a-f]{64}$ ]] || return 1
  target_digest="${target_ref##*@}"
  load_env || return
  [[ "${DISTR_IMAGE_REF:-}" == "$target_ref" &&
     "${DISTR_IMAGE_DIGEST:-}" == "$target_digest" ]] || {
    die "capture resume image ref/digest differs from the durable fence"
    return 1
  }
  compose_config || return
  pull_image || return
  target_commit="$(image_release_commit "$target_ref")" || return
  [[ "${DISTR_RELEASE_COMMIT:-}" == "$target_commit" ]] || {
    die "capture resume release commit differs from the fenced image"
    return 1
  }
}

timestamp_expand_capture() {
  local evidence_dir="${1:-}" fence_id source_digest target_digest state
  local actual_checksum expected_checksum capture_status
  [[ -n "$evidence_dir" ]] || return 1
  acquire_deploy_lock || return
  if active_timestamp_fence; then
    prepare_timestamp_evidence_dir "$evidence_dir" || return
    state="$(fence_value STATE)" || return
    [[ "$state" == PREPARING || "$state" == CAPTURED_WRITERS_STOPPED ]] || {
      die "timestamp expand fence cannot be resumed from state: $state"
      return 1
    }
    actual_checksum="$(evidence_dir_checksum "$evidence_dir")" || return
    expected_checksum="$(fence_value EVIDENCE_DIR_CHECKSUM)" || return
    [[ "$actual_checksum" == "$expected_checksum" ]] || {
      die "timestamp capture resume evidence directory differs from fence"
      return 1
    }
    fence_id="$(fence_value FENCE_ID)" || return
    source_digest="$(fence_value SOURCE_IMAGE_DIGEST)" || return
    target_digest="$(fence_value TARGET_IMAGE_DIGEST)" || return
    resume_fenced_target_image "$target_digest" || return
    start_dependencies || return
    ensure_fence_id_evidence "$fence_id" "$evidence_dir" || return
  else
    state=NEW
    compose_config || return
    pull_image || return
    start_dependencies || return
    prepare_timestamp_evidence_dir "$evidence_dir" || return
    fence_id="$(openssl rand -hex 16)" || return
    source_digest="$(running_hub_digest)" || return
    target_digest="$DISTR_IMAGE_REF"
    persist_timestamp_fence PREPARING \
      "$fence_id" "$evidence_dir" "$source_digest" "$target_digest" ||
      return
    ensure_fence_id_evidence "$fence_id" "$evidence_dir" || return
  fi
  if [[ "$state" == NEW || "$state" == PREPARING ]]; then
    stop_hub || return
    assert_hub_writers_stopped || return
    persist_timestamp_fence CAPTURED_WRITERS_STOPPED \
      "$fence_id" "$evidence_dir" "$source_digest" "$target_digest" ||
      return
  else
    assert_hub_writers_stopped || return
  fi
  if capture_evidence_status "$evidence_dir"; then return 0; else capture_status=$?; fi
  [[ "$capture_status" == 2 ]] || return "$capture_status"
  reset_incomplete_capture_evidence "$evidence_dir" || return
  backup_and_restore_timestamp_evidence "$evidence_dir" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps inspect \
    --output /evidence/source-inspection.json || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/source-inspection.json" || return
  copy_file_create_new_0600 \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/draft-manifest.json" || return
  write_sha256_sidecar_create_new \
    "$evidence_dir/draft-manifest.json" || return
  compare_timestamp_inspections \
    "$evidence_dir/source-inspection.json" \
    "$evidence_dir/restore-inspection.json" \
    "$evidence_dir/object-restore-inspection.json" || return
  write_timestamp_evidence_bundle "$evidence_dir" || return
}

retain_timestamp_dirty_recovery_checksum() {
  local artifact="${1:-}" reported="${2:-}" actual retained
  local owner current_uid directory
  [[ "$reported" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    die "timestamp dirty recovery operator returned an invalid checksum"
    return 1
  }
  [[ -f "$artifact" && ! -L "$artifact" ]] || {
    die "timestamp dirty recovery artifact is missing or unsafe: $artifact"
    return 1
  }
  path_mode_is "$artifact" 600 || {
    die "timestamp dirty recovery artifact mode must be 0600: $artifact"
    return 1
  }
  current_uid="$(id -u)" || return
  owner="$(stat -c '%u' -- "$artifact")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "timestamp dirty recovery artifact is not deployment-owned: $artifact"
    return 1
  }
  directory="$(dirname -- "$artifact")" || return
  require_secure_state_directory "$directory" || return
  actual="$(sha256sum -- "$artifact" | awk '{print "sha256:"$1}')" || return
  [[ "$actual" == "$reported" ]] || {
    die "timestamp dirty recovery operator checksum differs from retained artifact"
    return 1
  }
  if [[ -e "$artifact.sha256" || -L "$artifact.sha256" ]]; then
    verify_sha256_sidecar "$artifact" || return
  else
    write_sha256_sidecar_create_new "$artifact" || return
  fi
  retained="$(checksum_value "$artifact")" || return
  [[ "$retained" == "$reported" ]] || {
    die "timestamp dirty recovery sidecar checksum differs from operator output"
    return 1
  }
  printf '%s' "$retained" || return
}

timestamp_dirty_recovery_artifact_state() {
  local artifact="${1:-}" sidecar final_present=0 sidecar_present=0
  [[ -n "$artifact" ]] || return 1
  sidecar="$artifact.sha256"
  if [[ -e "$artifact" || -L "$artifact" ]]; then final_present=1; fi
  if [[ -e "$sidecar" || -L "$sidecar" ]]; then sidecar_present=1; fi
  case "$final_present:$sidecar_present" in
    0:0) printf ABSENT ;;
    1:0) printf FINAL_ONLY ;;
    1:1) printf COMPLETE ;;
    0:1)
      die "timestamp dirty recovery checksum exists without its artifact: $sidecar"
      return 1
      ;;
    *) return 1 ;;
  esac
}

require_timestamp_dirty_recovery_file() {
  local file="${1:-}" owner current_uid directory
  [[ -f "$file" && ! -L "$file" ]] || {
    die "timestamp dirty recovery evidence is not a regular non-symlink file: $file"
    return 1
  }
  path_mode_is "$file" 600 || {
    die "timestamp dirty recovery evidence mode must be 0600: $file"
    return 1
  }
  current_uid="$(id -u)" || return
  owner="$(stat -c '%u' -- "$file")" || return
  [[ "$owner" == "$current_uid" ]] || {
    die "timestamp dirty recovery evidence is not deployment-owned: $file"
    return 1
  }
  directory="$(dirname -- "$file")" || return
  require_secure_state_directory "$directory" || return
}

timestamp_dirty_recovery_raw_checksum() {
  local artifact="${1:-}" digest
  require_timestamp_dirty_recovery_file "$artifact" || return
  digest="$(sha256sum -- "$artifact" | awk '{print $1}')" || return
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || return 1
  printf 'sha256:%s' "$digest" || return
}

timestamp_dirty_recovery_existing_checksum() {
  local artifact="${1:-}" checksum
  require_timestamp_dirty_recovery_file "$artifact" || return
  verify_sha256_sidecar "$artifact" || {
    die "timestamp dirty recovery artifact checksum is unsafe or invalid: $artifact"
    return 1
  }
  checksum="$(checksum_value "$artifact")" || return
  [[ "$checksum" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  printf '%s' "$checksum" || return
}

repair_timestamp_dirty_recovery_sidecar() {
  local artifact="${1:-}" expected="${2:-}" actual retained
  [[ "$expected" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  [[ ! -e "$artifact.sha256" && ! -L "$artifact.sha256" ]] || {
    die "timestamp dirty recovery checksum appeared during repair"
    return 1
  }
  actual="$(timestamp_dirty_recovery_raw_checksum "$artifact")" || return
  [[ "$actual" == "$expected" ]] || {
    die "timestamp dirty recovery artifact changed before checksum repair"
    return 1
  }
  write_sha256_sidecar_create_new "$artifact" || return
  retained="$(timestamp_dirty_recovery_existing_checksum "$artifact")" || return
  [[ "$retained" == "$expected" ]] || {
    die "repaired timestamp dirty recovery checksum differs from artifact"
    return 1
  }
  printf '%s' "$retained" || return
}

timestamp_dirty_recovery_partial_members() (
  local directory="${1:-}" member
  local -a members=()
  shopt -s nullglob || return
  members=(
    "$directory"/timestamp-dirty-recovery-result.interrupted-*.partial
    "$directory"/timestamp-dirty-recovery-result.interrupted-*.partial.sha256
  )
  for member in "${members[@]}"; do
    printf '%s\0' "$member" || return
  done
)

archive_interrupted_timestamp_dirty_recovery_result() {
  local result="${1:-}" recovery_id="${2:-}" directory base reservation
  local archive candidate='' suffix source_checksum archive_checksum member name
  local index saw_gap=0 final_present sidecar_present result_checksum
  local -a members=()
  local -A archive_finals=() archive_sidecars=()
  [[ "$recovery_id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ &&
     "$recovery_id" != 00000000-0000-0000-0000-000000000000 ]] || {
    die "invalid timestamp dirty recovery id for interrupted evidence"
    return 1
  }
  directory="$(dirname -- "$result")" || return
  base="$(basename -- "$result")" || return
  reservation="$directory/.$base.$recovery_id.tmp"
  if [[ ! -e "$reservation" && ! -L "$reservation" ]]; then return 0; fi
  require_timestamp_dirty_recovery_file "$reservation" || return
  source_checksum="$(timestamp_dirty_recovery_raw_checksum "$reservation")" || return
  if [[ -e "$result" || -L "$result" ]]; then
    require_timestamp_dirty_recovery_file "$result" || return
    result_checksum="$(timestamp_dirty_recovery_raw_checksum "$result")" || return
    [[ "$source_checksum" == "$result_checksum" ]] &&
      cmp -s -- "$reservation" "$result" || {
      die "interrupted timestamp dirty recovery reservation differs from final result"
      return 1
    }
  fi
  mapfile -d '' -t members < <(
    timestamp_dirty_recovery_partial_members "$directory"
  ) || return
  for member in "${members[@]}"; do
    name="${member##*/}"
    if [[ "$name" =~ ^timestamp-dirty-recovery-result\.interrupted-([0-9]{3})\.partial$ ]]; then
      suffix="${BASH_REMATCH[1]}"
      [[ "$suffix" != 000 && ! -v "archive_finals[$suffix]" ]] || {
        die "invalid or duplicate timestamp dirty recovery partial archive"
        return 1
      }
      archive_finals["$suffix"]=1
    elif [[ "$name" =~ ^timestamp-dirty-recovery-result\.interrupted-([0-9]{3})\.partial\.sha256$ ]]; then
      suffix="${BASH_REMATCH[1]}"
      [[ "$suffix" != 000 && ! -v "archive_sidecars[$suffix]" ]] || {
        die "invalid or duplicate timestamp dirty recovery partial checksum"
        return 1
      }
      archive_sidecars["$suffix"]=1
    else
      die "invalid timestamp dirty recovery partial archive name: $name"
      return 1
    fi
  done
  for ((index=1; index<=999; index++)); do
    printf -v suffix '%03d' "$index" || return
    archive="$directory/timestamp-dirty-recovery-result.interrupted-$suffix.partial"
    final_present="${archive_finals[$suffix]:-0}"
    sidecar_present="${archive_sidecars[$suffix]:-0}"
    [[ "$final_present" == "$sidecar_present" ]] || {
      die "timestamp dirty recovery partial archive/checksum is orphaned: $archive"
      return 1
    }
    if [[ "$final_present" == 1 ]]; then
      ((saw_gap == 0)) || {
        die "timestamp dirty recovery partial archives are not contiguous"
        return 1
      }
      timestamp_dirty_recovery_existing_checksum "$archive" >/dev/null || return
    elif ((saw_gap == 0)); then
      candidate="$archive"
      saw_gap=1
    fi
  done
  [[ -n "$candidate" ]] || {
    die "timestamp dirty recovery partial archive numbering is exhausted"
    return 1
  }
  copy_file_create_new_0600 "$reservation" "$candidate" || return
  write_sha256_sidecar_create_new "$candidate" || return
  archive_checksum="$(timestamp_dirty_recovery_existing_checksum \
    "$candidate")" || return
  [[ "$archive_checksum" == "$source_checksum" ]] || {
    die "archived timestamp dirty recovery partial differs from reservation"
    return 1
  }
  cmp -s -- "$reservation" "$candidate" || {
    die "archived timestamp dirty recovery partial bytes differ"
    return 1
  }
  sync -f "$candidate" || return
  sync -f "$candidate.sha256" || return
  sync -f "$directory" || return
  rm -- "$reservation" || return
  sync -f "$directory" || return
}

validate_timestamp_dirty_recovery_plan() {
  local plan="${1:-}" expected_dirty="${2:-}" operator_identity="${3:-}"
  local reason="${4:-}" fence_id="${5:-}" manifest="${6:--}"
  local staged_manifest staged_checksum=''
  local -a manifest_args=()
  [[ "$expected_dirty" == 137 || "$expected_dirty" == 138 ]] || return 1
  if [[ "$manifest" != - ]]; then
    staged_manifest="$(dirname -- "$plan")/approved-manifest.json" || return
    staged_checksum="$(checksum_value "$staged_manifest")" || return
    manifest_args=(--slurpfile manifest "$staged_manifest")
  else
    manifest_args=(--argjson manifest '[]')
  fi
  jq -e \
    --arg operatorIdentity "$operator_identity" \
    --arg reason "$reason" \
    --arg writerFenceIdentifier "$fence_id" \
    --arg manifestMode "$manifest" \
    --arg manifestChecksum "$staged_checksum" \
    --argjson expectedDirtyVersion "$expected_dirty" \
    "${manifest_args[@]}" '
    type == "object" and
    .formatVersion == "distr.timestamp-dirty-recovery/v1" and
    .recordType == "PLAN" and
    (.recoveryId | type == "string" and
      test("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$") and
      . != "00000000-0000-0000-0000-000000000000") and
    (.createdAt | type == "string" and test("Z$")) and
    .operatorIdentity == $operatorIdentity and
    .reason == $reason and
    .writerFenceIdentifier == $writerFenceIdentifier and
    .expectedDirtyVersion == $expectedDirtyVersion and
    ((.catalogShape == "PREDECESSOR_137" and .forceVersion == 137) or
     (.catalogShape == "EXPAND_138" and .forceVersion == 138)) and
    (.catalogChecksum | type == "string" and
      test("^sha256:[0-9a-f]{64}$")) and
    (if $manifestMode == "-" then
       (has("manifest") | not) and
       (keys == [
         "catalogChecksum","catalogShape","createdAt",
         "expectedDirtyVersion","forceVersion","formatVersion",
         "operatorIdentity","reason","recordType","recoveryId",
         "writerFenceIdentifier"
       ])
     else
       (.manifest | type == "object") and
       ((.manifest | keys) == [
         "databaseIdentityChecksum","decisionContentChecksum",
         "documentChecksum","eventCount","executionCount","id",
         "rawCellCount","rawSetChecksum"
       ]) and
       .manifest.id == $manifest[0].id and
       .manifest.documentChecksum == $manifestChecksum and
       .manifest.decisionContentChecksum ==
         $manifest[0].decisionContentChecksum and
       .manifest.rawSetChecksum == $manifest[0].rawCellChecksum and
       .manifest.databaseIdentityChecksum ==
         $manifest[0].databaseIdentityChecksum and
       .manifest.executionCount == $manifest[0].executionCount and
       .manifest.eventCount == $manifest[0].eventCount and
       .manifest.rawCellCount == $manifest[0].rawCellCount and
       (keys == [
         "catalogChecksum","catalogShape","createdAt",
         "expectedDirtyVersion","forceVersion","formatVersion","manifest",
         "operatorIdentity","reason","recordType","recoveryId",
         "writerFenceIdentifier"
       ])
     end)
  ' "$plan" >/dev/null || {
    die "retained timestamp dirty recovery plan is invalid or has drifted"
    return 1
  }
}

timestamp_dirty_recovery_plan_value() {
  local plan="${1:-}" field="${2:-}"
  case "$field" in
    recoveryId|expectedDirtyVersion|forceVersion|catalogChecksum) ;;
    *) return 1 ;;
  esac
  jq -er --arg field "$field" '.[$field]' "$plan" || return
}

validate_timestamp_dirty_recovery_result() {
  local result="${1:-}" plan="${2:-}" plan_checksum="${3:-}"
  local recovery_id expected_dirty force_version catalog_checksum status
  recovery_id="$(timestamp_dirty_recovery_plan_value "$plan" recoveryId)" || return
  expected_dirty="$(jq -er '.expectedDirtyVersion' "$plan")" || return
  force_version="$(timestamp_dirty_recovery_plan_value "$plan" forceVersion)" || return
  catalog_checksum="$(timestamp_dirty_recovery_plan_value "$plan" catalogChecksum)" || return
  jq -e \
    --arg recoveryId "$recovery_id" \
    --arg planChecksum "$plan_checksum" \
    --arg catalogChecksum "$catalog_checksum" \
    --argjson expectedDirtyVersion "$expected_dirty" \
    --argjson forceVersion "$force_version" '
    type == "object" and
    (keys == [
      "action","catalogChecksum","completedAt","forcedVersion",
      "formatVersion","observedPreApplyStatus","planChecksum",
      "plannedStatus","postStatus","recordType","recoveryId","result"
    ]) and
    .formatVersion == "distr.timestamp-dirty-recovery/v1" and
    .recordType == "RESULT" and
    .recoveryId == $recoveryId and
    .planChecksum == $planChecksum and
    (.completedAt | type == "string" and test("Z$")) and
    .plannedStatus ==
      {"version":$expectedDirtyVersion,"dirty":true} and
    .forcedVersion == $forceVersion and
    .catalogChecksum == $catalogChecksum and
    .result == "SUCCEEDED" and
    .postStatus == {"version":$forceVersion,"dirty":false} and
    ((.action == "FORCED" and
      .observedPreApplyStatus == .plannedStatus) or
     (.action == "OBSERVED_ALREADY_CLEAN" and
      .observedPreApplyStatus ==
        {"version":$forceVersion,"dirty":false}))
  ' "$result" >/dev/null || {
    die "retained timestamp dirty recovery result is invalid or has drifted"
    return 1
  }
  status="$(current_schema_status)" || return
  [[ "$status" == "$force_version:false" ]] || {
    die "timestamp dirty recovery result differs from live schema status"
    return 1
  }
}

timestamp_expand_recover_dirty() {
  local manifest="${1:-}" evidence_dir="${2:-}" operator_identity="${3:-}"
  local reason="${4:-}" fence_id target_ref recorded_fence status dirty_version
  local reviewed_checksum staged_checksum plan_reported plan_checksum
  local result_reported result_checksum lock_timeout
  local bundle_checksum plan_state result_state recovery_id force_version
  local plan="$evidence_dir/timestamp-dirty-recovery-plan.json"
  local result="$evidence_dir/timestamp-dirty-recovery-result.json"
  local -a manifest_args=()
  [[ -n "$manifest" && -n "$evidence_dir" &&
     -n "$operator_identity" && -n "$reason" ]] || return 1
  lock_timeout="${DISTR_TIMESTAMP_DIRTY_RECOVERY_LOCK_TIMEOUT:-2m}"
  [[ "$lock_timeout" =~ ^[1-9][0-9]*[smh]$ ]] || {
    die "DISTR_TIMESTAMP_DIRTY_RECOVERY_LOCK_TIMEOUT must be a positive s/m/h duration"
    return 1
  }
  acquire_deploy_lock || return
  prepare_timestamp_evidence_dir "$evidence_dir" || return
  require_timestamp_fence "$evidence_dir" || return
  fence_id="$(fence_value FENCE_ID)" || return
  target_ref="$(fence_value TARGET_IMAGE_DIGEST)" || return
  resume_fenced_target_image "$target_ref" || return
  start_dependencies || return
  stop_fenced_hub_if_running || return
  cleanup_fenced_acceptance_hubs "$fence_id" || return
  assert_hub_writers_stopped || return
  capture_evidence_complete "$evidence_dir" || return
  bundle_checksum="$(verify_timestamp_evidence_bundle "$evidence_dir")" || return
  [[ "${DISTR_TIMESTAMP_EVIDENCE_CHECKSUM:-}" =~ ^sha256:[0-9a-f]{64}$ &&
     "$bundle_checksum" == "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM" ]] || {
    die "captured timestamp evidence bundle differs from configured checksum"
    return 1
  }
  recorded_fence="$(evidence_fence_id "$evidence_dir")" || return
  [[ "$recorded_fence" == "$fence_id" ]] || {
    die "captured timestamp evidence fence differs from active fence"
    return 1
  }
  if [[ "$manifest" != - ]]; then
    reviewed_checksum="$(reviewed_manifest_checksum "$manifest")" || return
    verify_timestamp_evidence "$manifest" "$evidence_dir" || return
    stage_approved_manifest "$manifest" "$evidence_dir" || return
    staged_checksum="$(checksum_value \
      "$evidence_dir/approved-manifest.json")" || return
    [[ "$staged_checksum" == "$reviewed_checksum" ]] || {
      die "staged recovery manifest checksum differs from reviewed input"
      return 1
    }
    manifest_args=(
      --external-execution-timestamp-manifest
      /evidence/approved-manifest.json
    )
  fi
  plan_state="$(timestamp_dirty_recovery_artifact_state "$plan")" || return
  result_state="$(timestamp_dirty_recovery_artifact_state "$result")" || return
  case "$plan_state" in
    ABSENT)
      [[ "$result_state" == ABSENT ]] || {
        die "timestamp dirty recovery result exists without a retained plan"
        return 1
      }
      status="$(current_schema_status)" || return
      case "$status" in
        137:true) dirty_version=137 ;;
        138:true) dirty_version=138 ;;
        *)
          die "new timestamp dirty recovery requires dirty schema 137 or 138"
          return 1
          ;;
      esac
      plan_reported="$(
        run_timestamp_operator "$evidence_dir" \
          migrate recover-dirty plan \
          --expected-dirty-version "$dirty_version" \
          --operator-identity "$operator_identity" \
          --reason "$reason" \
          --writer-fence-id "$fence_id" \
          "${manifest_args[@]}" \
          --output /evidence/timestamp-dirty-recovery-plan.json \
          --lock-timeout "$lock_timeout"
      )" || return
      plan_checksum="$(retain_timestamp_dirty_recovery_checksum \
        "$plan" "$plan_reported")" || return
      validate_timestamp_dirty_recovery_plan \
        "$plan" "$dirty_version" "$operator_identity" "$reason" \
        "$fence_id" "$manifest" || return
      ;;
    COMPLETE|FINAL_ONLY)
      if [[ "$plan_state" == COMPLETE ]]; then
        plan_checksum="$(timestamp_dirty_recovery_existing_checksum \
          "$plan")" || return
      else
        plan_checksum="$(timestamp_dirty_recovery_raw_checksum \
          "$plan")" || return
      fi
      dirty_version="$(timestamp_dirty_recovery_plan_value \
        "$plan" expectedDirtyVersion)" || return
      validate_timestamp_dirty_recovery_plan \
        "$plan" "$dirty_version" "$operator_identity" "$reason" \
        "$fence_id" "$manifest" || return
      if [[ "$plan_state" == FINAL_ONLY ]]; then
        plan_checksum="$(repair_timestamp_dirty_recovery_sidecar \
          "$plan" "$plan_checksum")" || return
      fi
      status="$(current_schema_status)" || return
      force_version="$(timestamp_dirty_recovery_plan_value \
        "$plan" forceVersion)" || return
      [[ "$status" == "$dirty_version:true" ||
         "$status" == "$force_version:false" ]] || {
        die "live schema does not match the exact retained recovery plan"
        return 1
      }
      ;;
    *) return 1 ;;
  esac
  recovery_id="$(timestamp_dirty_recovery_plan_value \
    "$plan" recoveryId)" || return
  archive_interrupted_timestamp_dirty_recovery_result \
    "$result" "$recovery_id" || return
  if [[ "$result_state" == COMPLETE || "$result_state" == FINAL_ONLY ]]; then
    if [[ "$result_state" == COMPLETE ]]; then
      result_checksum="$(timestamp_dirty_recovery_existing_checksum \
        "$result")" || return
    else
      result_checksum="$(timestamp_dirty_recovery_raw_checksum \
        "$result")" || return
    fi
    validate_timestamp_dirty_recovery_result \
      "$result" "$plan" "$plan_checksum" || return
    if [[ "$result_state" == FINAL_ONLY ]]; then
      result_checksum="$(repair_timestamp_dirty_recovery_sidecar \
        "$result" "$result_checksum")" || return
    fi
    [[ "$result_checksum" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
    assert_hub_writers_stopped || return
    return 0
  fi
  [[ "$result_state" == ABSENT ]] || return 1
  result_reported="$(
    run_timestamp_operator "$evidence_dir" \
      migrate recover-dirty apply \
      --plan /evidence/timestamp-dirty-recovery-plan.json \
      --plan-checksum "$plan_checksum" \
      --writer-fence-id "$fence_id" \
      "${manifest_args[@]}" \
      --output /evidence/timestamp-dirty-recovery-result.json \
      --lock-timeout "$lock_timeout"
  )" || return
  result_checksum="$(retain_timestamp_dirty_recovery_checksum \
    "$result" "$result_reported")" || return
  [[ "$result_checksum" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  validate_timestamp_dirty_recovery_result \
    "$result" "$plan" "$plan_checksum" || return
  assert_hub_writers_stopped || return
}

timestamp_expand_apply() {
  local manifest="${1:-}"
  local evidence_dir="${2:-}"
  local fence_id backup_file backup_reference backup_checksum restore_checksum approved_id
  local reviewed_checksum staged_checksum dry_report apply_report dry_expected_population
  local phase transition_counts expected_execution_count expected_event_count expected_raw_cell_count
  [[ -n "$manifest" && -n "$evidence_dir" ]] || return 1
  acquire_deploy_lock || return
  check_timestamp_apply_env "$evidence_dir" || return
  require_timestamp_fence "$evidence_dir" || return
  fence_id="$(fence_value FENCE_ID)" || return
  stop_fenced_hub_if_running || return
  cleanup_fenced_acceptance_hubs "$fence_id" || return
  assert_hub_writers_stopped || return
  reviewed_checksum="$(reviewed_manifest_checksum "$manifest")" || return
  verify_timestamp_evidence "$manifest" "$evidence_dir" || return
  stage_approved_manifest "$manifest" "$evidence_dir" || return
  staged_checksum="$(checksum_value "$evidence_dir/approved-manifest.json")" || return
  [[ "$staged_checksum" == "$reviewed_checksum" ]] || {
    die "staged manifest checksum differs from reviewed input"
    return 1
  }
  approved_id="$(manifest_id "$evidence_dir/approved-manifest.json")" || return
  transition_counts="$(timestamp_manifest_transition_counts \
    "$evidence_dir/approved-manifest.json")" || return
  IFS=: read -r expected_execution_count expected_event_count \
    expected_raw_cell_count <<<"$transition_counts" || return
  phase="$(timestamp_expand_apply_phase \
    "$approved_id" "$expected_execution_count" "$expected_event_count" \
    "$expected_raw_cell_count")" || return
  if [[ "$phase" != SCHEMA_138_VERIFIED ]]; then
    run_timestamp_operator "$evidence_dir" \
      external-execution-timestamps validate-manifest \
      --manifest /evidence/approved-manifest.json || return
  fi
  dry_report="$evidence_dir/timestamp-apply-dry-run.json"
  apply_report="$evidence_dir/timestamp-apply-result.json"
  case "$phase" in
    SCHEMA_137|SCHEMA_138_EMPTY)
      run_timestamp_apply_report "$evidence_dir" \
        "$evidence_dir/approved-manifest.json" true "$dry_report" '' \
        external-execution-timestamps apply \
        --manifest /evidence/approved-manifest.json || return
      dry_expected_population="$(timestamp_apply_expected_population \
        "$dry_report")" || return
      if [[ "$phase" == SCHEMA_137 ]]; then
        run_timestamp_migration_138 "$evidence_dir" || return
        phase=SCHEMA_138_EMPTY
      fi
      ;;
    SCHEMA_138_VERIFIED)
      if [[ ! -e "$dry_report" && ! -L "$dry_report" &&
            ! -e "$dry_report.sha256" && ! -L "$dry_report.sha256" ]]; then
        die "verified timestamp recovery requires the retained dry-run report"
        return 1
      fi
      run_timestamp_apply_report "$evidence_dir" \
        "$evidence_dir/approved-manifest.json" true "$dry_report" '' \
        external-execution-timestamps apply \
        --manifest /evidence/approved-manifest.json || return
      dry_expected_population="$(timestamp_apply_expected_population \
        "$dry_report")" || return
      ;;
    *) return 1 ;;
  esac
  backup_file="$(latest_database_backup "$evidence_dir")" || return
  backup_reference="$(basename -- "$backup_file")" || return
  backup_checksum="$(checksum_value "$backup_file")" || return
  restore_checksum="$(checksum_value \
    "$evidence_dir/restore-inspection.json")" || return
  if [[ "$phase" == SCHEMA_138_VERIFIED ]]; then
    run_timestamp_idempotent_apply_report "$evidence_dir" \
      "$evidence_dir/approved-manifest.json" "$apply_report" \
      "$dry_expected_population" \
      external-execution-timestamps apply \
      --manifest /evidence/approved-manifest.json --apply \
      --writer-fence-id "$fence_id" \
      --backup-reference "$backup_reference" \
      --backup-checksum "$backup_checksum" \
      --restore-reference restore-inspection.json \
      --restore-checksum "$restore_checksum" || return
  else
    run_timestamp_apply_report "$evidence_dir" \
      "$evidence_dir/approved-manifest.json" false "$apply_report" \
      "$dry_expected_population" \
      external-execution-timestamps apply \
      --manifest /evidence/approved-manifest.json --apply \
      --writer-fence-id "$fence_id" \
      --backup-reference "$backup_reference" \
      --backup-checksum "$backup_checksum" \
      --restore-reference restore-inspection.json \
      --restore-checksum "$restore_checksum" || return
  fi
  verify_applied_population_count "$approved_id" \
    "$dry_expected_population" || return
  run_timestamp_operator "$evidence_dir" \
    external-execution-timestamps verify \
    --manifest-id "$approved_id" || return
  start_verify_and_finalize_timestamp_expand \
    "$approved_id" "$evidence_dir" || return
}

timestamp_expand_cancel() (
  set -Eeuo pipefail
  local evidence_dir="${1:-}" source_digest target_digest source_commit target_commit
  local config_changed=0 complete=0 cleanup_status=0
  cleanup_cancel_configuration() {
    local status=$?
    trap - EXIT HUP INT TERM
    if ((config_changed == 1 && complete == 0)); then
      set_image_identity "$target_digest" "$target_commit" || cleanup_status=1
      load_env || cleanup_status=1
    fi
    if ((cleanup_status != 0)); then exit 1; fi
    exit "$status"
  }
  trap cleanup_cancel_configuration EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  acquire_deploy_lock || return
  require_timestamp_fence "$evidence_dir" || return
  target_digest="$(fence_value TARGET_IMAGE_DIGEST)" || return
  resume_fenced_target_image "$target_digest" || return
  target_commit="${DISTR_RELEASE_COMMIT:-}"
  assert_hub_writers_stopped || return
  require_clean_schema_137 || return
  validate_source_inspection \
    "$evidence_dir/source-inspection.json" || return
  source_digest="$(fence_value SOURCE_IMAGE_DIGEST)" || return
  pull_immutable_image_ref "$source_digest" || return
  source_commit="$(image_release_commit "$source_digest")" || return
  config_changed=1
  set_image_identity "$source_digest" "$source_commit" || return
  load_env || return
  start_verify_cancel_and_clear "$evidence_dir" || return
  complete=1
)

deploy_all() {
  acquire_deploy_lock || return
  require_no_active_timestamp_fence || return
  build_image || return
  push_image || return
  release_from_ecr_locked || return
}

release_from_ecr_locked() {
  require_no_active_timestamp_fence || return
  compose_config || return
  pull_image || return
  start_dependencies || return
  run_migration_preflight || return
  stop_hub || return
  assert_hub_writers_stopped || return
  backup_and_restore_release_evidence || return
  run_migrations || return
  start_hub || return
  health || return
}

release_from_ecr() {
  acquire_deploy_lock || return
  release_from_ecr_locked || return
}

rollback_app() (
  local ref_or_tag="${1:-}"
  local original_ref original_commit verified_original_commit image_ref target_commit
  local config_changed=0 complete=0 cleanup_status=0
  [[ -n "${ref_or_tag}" ]] || {
    die "usage: $0 rollback <previous-image-ref-or-tag>"
    return 1
  }
  [[ "${ref_or_tag}" != "latest" ]] || {
    die "rollback target must be immutable; do not deploy latest"
    return 1
  }
  acquire_deploy_lock || return
  require_no_active_timestamp_fence || return
  check_env || return
  original_ref="${DISTR_IMAGE_REF:-}"
  original_commit="${DISTR_RELEASE_COMMIT:-}"
  cleanup_failed_rollback() {
    local status=$?
    trap - EXIT HUP INT TERM
    if ((config_changed == 1 && complete == 0)); then
      set_image_identity "$original_ref" "$original_commit" || cleanup_status=1
      load_env || cleanup_status=1
      if ((cleanup_status == 0)); then
        stop_hub || cleanup_status=1
        start_hub || cleanup_status=1
        health || cleanup_status=1
      fi
    fi
    if ((cleanup_status != 0)); then exit 1; fi
    exit "$status"
  }
  trap cleanup_failed_rollback EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  if [[ "${ref_or_tag}" == *@sha256:* ]]; then
    image_ref="${ref_or_tag}"
  else
    image_ref="$(resolve_digest_ref_for_tag "${ref_or_tag}")" || return
  fi
  require_rollback_schema_compatibility "$image_ref" || return
  pull_immutable_image_ref "$original_ref" || return
  verified_original_commit="$(image_release_commit "$original_ref")" || return
  [[ "$verified_original_commit" == "$original_commit" ]] || {
    die "rollback refused: configured source commit differs from its image"
    return 1
  }
  pull_immutable_image_ref "$image_ref" || return
  target_commit="$(image_release_commit "$image_ref")" || return
  info "switching Hub image to ${image_ref}; this does not roll back database schema"
  config_changed=1
  set_image_identity "$image_ref" "$target_commit" || return
  load_env || return
  start_hub || return
  health || return
  if [[ "${ref_or_tag}" != *@sha256:* ]]; then
    set_env_var "DISTR_IMAGE_TAG" "${ref_or_tag}" || return
  fi
  complete=1
)

require_no_active_timestamp_fence() {
  if active_timestamp_fence; then
    die "timestamp expand fence is active; only capture resume, apply, dirty recovery, or cancel is allowed"
    return 1
  fi
}

run_locked_ordinary_command() {
  acquire_deploy_lock || return
  require_no_active_timestamp_fence || return
  "$@" || return
}

usage() {
  cat <<EOF
Usage: $0 <command>

Commands:
  init                 create .env with generated local secrets
  image-check          validate only AWS ECR image variables for Jenkins
  check                validate required .env values
  config               validate docker-compose.yml with .env
  ecr-login            log Docker in to AWS ECR
  ecr-create-repo      create the ECR repository if it does not exist
  build                build community Hub image from source
  push                 push the built Hub image to ECR
  pull                 pull the configured Hub image from ECR
  deps                 start postgres and RustFS only
  backup               back up PostgreSQL and local RustFS volume
  migrate              preflight, stop/fence Hub, restore-verify backups, migrate, start, health
  up                   start Hub with serve --migrate=false
  release              pull, preflight, stop/fence Hub, restore-verify backups, migrate, start, health
  deploy               build, push to ECR, release to Compose, health check
  health               check http://127.0.0.1:\${DISTR_HTTP_PORT:-8080}/ready
  logs [service]       follow compose logs
  ps                   show compose service state
  cleanup-artifacts    run ArtifactBlob cleanup once
  rollback <ref|tag>   application-only rollback to a previous ECR digest ref or tag
  timestamp-expand-capture <evidence-dir>
                       stop/fence Hub and retain verified schema-137 evidence
  timestamp-expand-apply <approved-manifest> <evidence-dir>
                       migrate/apply/verify and start the expand Hub
  timestamp-expand-cancel <evidence-dir>
                       restart the pre-expand Hub before migration 138
  timestamp-expand-recover-dirty <approved-manifest-or-> <evidence-dir> <operator-identity> <reason>
                       recover only an exactly proven dirty migration 137/138
EOF
}

dispatch_ordinary_command() {
  local command_name="${1:-}"
  shift || true
  case "$command_name" in
    init) run_locked_ordinary_command init_env || return ;;
    ecr-login) run_locked_ordinary_command ecr_login || return ;;
    ecr-create-repo) run_locked_ordinary_command ensure_ecr_repository || return ;;
    build) run_locked_ordinary_command build_image || return ;;
    push) run_locked_ordinary_command push_image || return ;;
    pull) run_locked_ordinary_command pull_image || return ;;
    deps) run_locked_ordinary_command start_dependencies || return ;;
    backup) run_locked_ordinary_command backup_postgres || return ;;
    migrate) run_locked_ordinary_command migrate_with_fenced_backup || return ;;
    up) run_locked_ordinary_command start_hub || return ;;
    release) release_from_ecr || return ;;
    deploy) deploy_all || return ;;
    cleanup-artifacts)
      run_locked_ordinary_command cleanup_artifacts || return
      ;;
    rollback) rollback_app "$@" || return ;;
    *) die "unknown mutating command: $command_name"; return 1 ;;
  esac
}

cleanup_artifacts() {
  check_env || return
  compose --profile cleanup run --rm artifact-blob-cleanup || return
}

dispatch_read_only_command() {
  local command_name="${1:-}"
  shift || true
  case "$command_name" in
    image-check) check_image_env || return ;;
    check) check_env || return ;;
    config) compose_config || return ;;
    health) health || return ;;
    logs)
      check_env || return
      if (($# > 0)); then
        compose logs -f --tail=120 "$@" || return
      else
        compose logs -f --tail=120 || return
      fi
      ;;
    ps) check_env || return; compose ps || return ;;
    ""|-h|--help|help) usage || return ;;
    *) usage || return; die "unknown command: $command_name"; return 1 ;;
  esac
}

dispatch_command() {
  local command_name="${1:-}"
  cd "$REPO_ROOT" || return
  case "$command_name" in
    timestamp-expand-apply)
      [[ $# == 3 ]] || {
        die "usage: $0 timestamp-expand-apply <approved-manifest> <evidence-dir>"
        return 1
      }
      shift
      timestamp_expand_apply "$@" || return
      ;;
    timestamp-expand-cancel)
      [[ $# == 2 ]] || {
        die "usage: $0 timestamp-expand-cancel <evidence-dir>"
        return 1
      }
      shift
      timestamp_expand_cancel "$@" || return
      ;;
    timestamp-expand-capture)
      [[ $# == 2 ]] || {
        die "usage: $0 timestamp-expand-capture <evidence-dir>"
        return 1
      }
      shift
      timestamp_expand_capture "$@" || return
      ;;
    timestamp-expand-recover-dirty)
      [[ $# == 5 ]] || {
        die "usage: $0 timestamp-expand-recover-dirty <approved-manifest-or-> <evidence-dir> <operator-identity> <reason>"
        return 1
      }
      shift
      timestamp_expand_recover_dirty "$@" || return
      ;;
    init|ecr-login|ecr-create-repo|build|push|pull|deps|backup|migrate|up|release|deploy|cleanup-artifacts|rollback)
      if [[ "$command_name" == rollback ]]; then
        [[ $# == 2 ]] || {
          die "usage: $0 rollback <previous-image-ref-or-tag>"
          return 1
        }
      else
        [[ $# == 1 ]] || {
          die "command does not accept arguments: $command_name"
          return 1
        }
      fi
      require_no_active_timestamp_fence || return
      dispatch_ordinary_command "$@" || return
      ;;
    *)
      dispatch_read_only_command "$@" || return
      ;;
  esac
}

if [[ "${DISTR_DEPLOY_LIB_ONLY:-0}" == 1 ]]; then return 0 2>/dev/null || exit 0; fi
dispatch_command "$@" || exit $?
