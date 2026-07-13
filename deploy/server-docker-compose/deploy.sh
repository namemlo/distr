#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"
ENV_FILE="${SCRIPT_DIR}/.env"
BACKUP_DIR="${BACKUP_DIR:-${SCRIPT_DIR}/backups}"
LOCK_FILE="${LOCK_FILE:-${SCRIPT_DIR}/.deploy.lock}"

cd "${REPO_ROOT}"

info() { printf '[distr-deploy] %s\n' "$*"; }
die() { printf '[distr-deploy] ERROR: %s\n' "$*" >&2; exit 1; }

compose() {
  docker compose --env-file "${ENV_FILE}" -f "${COMPOSE_FILE}" "$@"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

load_env() {
  [[ -f "${ENV_FILE}" ]] || die "missing ${ENV_FILE}; run: ${SCRIPT_DIR}/deploy.sh init"
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
}

set_env_var() {
  local key="$1"
  local value="$2"
  if grep -qE "^${key}=" "${ENV_FILE}"; then
    sed -i.bak -E "s#^${key}=.*#${key}=${value}#" "${ENV_FILE}"
  else
    printf '\n%s=%s\n' "${key}" "${value}" >> "${ENV_FILE}"
  fi
  rm -f "${ENV_FILE}.bak"
}

random_b64() {
  openssl rand -base64 "$1" | tr -d '\n'
}

init_env() {
  [[ ! -f "${ENV_FILE}" ]] || die "${ENV_FILE} already exists"
  cp "${SCRIPT_DIR}/.env.example" "${ENV_FILE}"
  chmod 600 "${ENV_FILE}"

  local postgres_password rustfs_secret jwt_secret tag
  postgres_password="$(random_b64 24)"
  rustfs_secret="$(random_b64 24)"
  jwt_secret="$(random_b64 32)"
  tag="$(git rev-parse --short=12 HEAD 2>/dev/null || date -u +%Y%m%dT%H%M%SZ)"

  set_env_var "POSTGRES_PASSWORD" "${postgres_password}"
  set_env_var "DATABASE_URL" "postgres://distr:${postgres_password}@postgres:5432/distr?sslmode=disable"
  set_env_var "REGISTRY_S3_SECRET_ACCESS_KEY" "${rustfs_secret}"
  set_env_var "RUSTFS_SECRET_KEY" "${rustfs_secret}"
  set_env_var "JWT_SECRET" "${jwt_secret}"
  set_env_var "DISTR_IMAGE_TAG" "${tag}"

  info "created ${ENV_FILE}"
  info "edit AWS_REGION, DISTR_IMAGE, DISTR_HOST, REGISTRY_HOST, registration, mail settings, and storage settings before deploy"
}

check_image_env() {
  load_env
  local required=(
    AWS_REGION DISTR_IMAGE DISTR_IMAGE_TAG
  )
  local key value
  for key in "${required[@]}"; do
    value="${!key:-}"
    [[ -n "${value}" ]] || die "${key} is empty in ${ENV_FILE}"
    [[ "${value}" != *CHANGE_ME* ]] || die "${key} still contains CHANGE_ME in ${ENV_FILE}"
  done
  [[ "${DISTR_IMAGE}" == *".dkr.ecr."* ]] || die "DISTR_IMAGE must be an AWS ECR repository URI, for example 123456789012.dkr.ecr.${AWS_REGION}.amazonaws.com/distr-community"
  [[ "${DISTR_IMAGE_TAG}" != "latest" ]] || die "DISTR_IMAGE_TAG must be immutable; do not deploy latest"
}

check_env() {
  check_runtime_env
  [[ "${DISTR_IMAGE_REF}" == *".dkr.ecr."* ]] || die "DISTR_IMAGE_REF must be an AWS ECR image reference"
  [[ "${DISTR_IMAGE_REF}" == *@sha256:* ]] || die "DISTR_IMAGE_REF must pin an ECR digest, for example ${DISTR_IMAGE}@sha256:..."
}

check_runtime_env() {
  load_env
  local required=(
    COMPOSE_PROJECT_NAME POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB DATABASE_URL
    DISTR_HOST REGISTRY_HOST JWT_SECRET RUSTFS_ACCESS_KEY RUSTFS_SECRET_KEY
  )
  local key value
  for key in "${required[@]}"; do
    value="${!key:-}"
    [[ -n "${value}" ]] || die "${key} is empty in ${ENV_FILE}"
    [[ "${value}" != *CHANGE_ME* ]] || die "${key} still contains CHANGE_ME in ${ENV_FILE}"
  done
  [[ "${DISTR_HOST}" != *example.com* ]] || die "DISTR_HOST still uses example.com in ${ENV_FILE}"
  [[ "${REGISTRY_HOST}" != *example.com* ]] || die "REGISTRY_HOST still uses example.com in ${ENV_FILE}"
  if [[ "${USER_EMAIL_VERIFICATION_REQUIRED:-false}" == "true" && -z "${MAILER_TYPE:-}" ]]; then
    die "USER_EMAIL_VERIFICATION_REQUIRED=true requires MAILER_TYPE and mail settings"
  fi
}

ecr_registry() {
  printf '%s' "${DISTR_IMAGE%%/*}"
}

ecr_repository() {
  printf '%s' "${DISTR_IMAGE#*/}"
}

tagged_image_ref() {
  printf '%s:%s' "${DISTR_IMAGE}" "${DISTR_IMAGE_TAG}"
}

resolve_digest_ref_for_tag() {
  local tag="${1:?tag required}"
  local repo digest
  need_cmd aws
  repo="${ECR_REPOSITORY:-$(ecr_repository)}"
  digest="$(
    aws ecr describe-images \
      --region "${AWS_REGION}" \
      --repository-name "${repo}" \
      --image-ids "imageTag=${tag}" \
      --query 'imageDetails[0].imageDigest' \
      --output text
  )"
  [[ "${digest}" == sha256:* ]] || die "could not resolve ECR digest for ${DISTR_IMAGE}:${tag}"
  printf '%s@%s' "${DISTR_IMAGE}" "${digest}"
}

write_release_metadata() {
  check_image_env
  need_cmd aws
  local commit image_ref release_file
  commit="$(git rev-parse HEAD 2>/dev/null || true)"
  image_ref="$(resolve_digest_ref_for_tag "${DISTR_IMAGE_TAG}")"
  set_env_var "DISTR_IMAGE_REF" "${image_ref}"

  mkdir -p dist
  release_file="dist/release-${DISTR_IMAGE_TAG}.env"
  cat > "${release_file}" <<EOF
AWS_REGION=${AWS_REGION}
ECR_REPOSITORY=${ECR_REPOSITORY:-$(ecr_repository)}
DISTR_IMAGE=${DISTR_IMAGE}
DISTR_IMAGE_TAG=${DISTR_IMAGE_TAG}
DISTR_IMAGE_REF=${image_ref}
SOURCE_COMMIT=${commit}
EOF
  info "wrote release metadata ${release_file}"
}

acquire_deploy_lock() {
  need_cmd flock
  exec 9>"${LOCK_FILE}"
  flock -n 9 || die "another deployment is already running; lock: ${LOCK_FILE}"
}

ecr_login() {
  check_image_env
  need_cmd aws
  need_cmd docker
  local registry
  registry="$(ecr_registry)"
  info "logging in to ECR registry ${registry}"
  aws ecr get-login-password --region "${AWS_REGION}" \
    | docker login --username AWS --password-stdin "${registry}"
}

compose_config() {
  check_env
  need_cmd docker
  info "validating Docker Compose configuration"
  compose config --quiet
}

ensure_ecr_repository() {
  check_image_env
  need_cmd aws
  local repo
  repo="${ECR_REPOSITORY:-$(ecr_repository)}"
  info "ensuring ECR repository ${repo} exists in ${AWS_REGION}"
  aws ecr describe-repositories --region "${AWS_REGION}" --repository-names "${repo}" >/dev/null 2>&1 \
    || aws ecr create-repository --region "${AWS_REGION}" --repository-name "${repo}" >/dev/null
}

detect_goarch() {
  local arch
  arch="$(go env GOARCH 2>/dev/null || true)"
  if [[ -z "${arch}" ]]; then
    case "$(uname -m)" in
      x86_64|amd64) arch="amd64" ;;
      aarch64|arm64) arch="arm64" ;;
      *) die "cannot map machine architecture $(uname -m) to Go arch" ;;
    esac
  fi
  printf '%s' "${arch}"
}

ensure_local_sbom() {
  shopt -s nullglob
  local sboms=(dist/*.spdx.json)
  shopt -u nullglob
  if ((${#sboms[@]} > 0)); then
    return
  fi

  cat > dist/local-build.spdx.json <<JSON
{
  "spdxVersion": "SPDX-2.3",
  "dataLicense": "CC0-1.0",
  "SPDXID": "SPDXRef-DOCUMENT",
  "name": "distr-local-source-build",
  "documentNamespace": "https://distr.example.invalid/spdx/local-$(date -u +%Y%m%dT%H%M%SZ)",
  "creationInfo": {
    "created": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "creators": ["Tool: deploy/server-docker-compose/deploy.sh"]
  },
  "packages": []
}
JSON
}

build_image() {
  check_image_env
  need_cmd git
  need_cmd docker
  need_cmd go
  need_cmd mise

  local arch commit image_ref
  arch="$(detect_goarch)"
  commit="$(git rev-parse --short=12 HEAD)"
  image_ref="$(tagged_image_ref)"

  info "installing tool versions from mise.toml"
  mise install

  info "installing root workspace dependencies from the frozen lockfile"
  mise exec -- pnpm install --frozen-lockfile

  info "building community Hub from source for commit ${commit}"
  VERSION="${DISTR_IMAGE_TAG}" mise run build:hub:community
  cp dist/distr "dist/distr-${arch}"
  ensure_local_sbom

  info "building Hub image ${image_ref}"
  docker build \
    --build-arg "TARGETARCH=${arch}" \
    --label "org.opencontainers.image.revision=${commit}" \
    --label "org.opencontainers.image.source=$(git config --get remote.origin.url || true)" \
    -f Dockerfile.hub \
    -t "${image_ref}" \
    .
}

push_image() {
  need_cmd docker
  ecr_login
  info "pushing $(tagged_image_ref) to ECR"
  docker push "$(tagged_image_ref)"
  write_release_metadata
}

pull_image() {
  check_env
  need_cmd docker
  ecr_login
  info "pulling ${DISTR_IMAGE_REF} from ECR"
  compose pull hub
  compose --profile migrate pull migrate
  compose --profile cleanup pull artifact-blob-cleanup
}

start_dependencies() {
  check_runtime_env
  need_cmd docker
  info "starting postgres and storage"
  compose up -d --wait --wait-timeout 180 postgres storage
}

backup_postgres() {
  check_runtime_env
  need_cmd docker
  mkdir -p "${BACKUP_DIR}"
  chmod 700 "${BACKUP_DIR}"

  local stamp backup_file project
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  backup_file="${BACKUP_DIR}/postgres-${stamp}.dump"
  project="${COMPOSE_PROJECT_NAME:-distr-prod}"

  info "writing PostgreSQL backup to ${backup_file}"
  compose exec -T postgres sh -ceu '
    PGPASSWORD="${POSTGRES_PASSWORD}" pg_dump \
      --username="${POSTGRES_USER}" \
      --dbname="${POSTGRES_DB}" \
      --format=custom
  ' > "${backup_file}"

  docker run --rm \
    -v "${BACKUP_DIR}:/backup:ro" \
    postgres:18.4-alpine3.23 \
    pg_restore --list "/backup/$(basename "${backup_file}")" >/dev/null

  info "PostgreSQL backup verified"

  local storage_file storage_volume
  storage_file="${BACKUP_DIR}/rustfs-${stamp}.tar.gz"
  storage_volume="${project}_rustfs"
  if docker volume inspect "${storage_volume}" >/dev/null 2>&1; then
    info "writing RustFS volume backup to ${storage_file}"
    docker run --rm \
      -v "${storage_volume}:/data:ro" \
      -v "${BACKUP_DIR}:/backup" \
      alpine:3.23 \
      tar -C /data -czf "/backup/$(basename "${storage_file}")" .
    docker run --rm \
      -v "${BACKUP_DIR}:/backup" \
      alpine:3.23 \
      chown "$(id -u):$(id -g)" "/backup/$(basename "${storage_file}")"
  else
    info "RustFS volume ${storage_volume} does not exist yet; skipping storage backup"
  fi
}

run_migrations() {
  check_env
  need_cmd docker
  info "running database migrations explicitly"
  compose --profile migrate run --rm migrate
}

start_hub() {
  check_env
  need_cmd docker
  info "starting Hub"
  compose up -d hub
}

stop_hub() {
  check_env
  need_cmd docker
  info "stopping Hub before migration"
  compose stop hub >/dev/null 2>&1 || true
}

health() {
  check_env
  need_cmd curl
  local url="${DISTR_LOCAL_HEALTH_URL:-http://127.0.0.1:${DISTR_HTTP_PORT:-8080}/ready}"
  info "waiting for ${url}"
  for _ in $(seq 1 60); do
    if curl -fsS "${url}" >/dev/null; then
      info "Hub is ready"
      return
    fi
    sleep 2
  done
  compose ps
  compose logs --tail=120 hub
  die "Hub did not become ready at ${url}"
}

deploy_all() {
  build_image
  push_image
  release_from_ecr
}

release_from_ecr() {
  acquire_deploy_lock
  compose_config
  pull_image
  start_dependencies
  backup_postgres
  stop_hub
  run_migrations
  start_hub
  health
}

rollback_app() {
  local ref_or_tag="${1:-}"
  [[ -n "${ref_or_tag}" ]] || die "usage: $0 rollback <previous-image-ref-or-tag>"
  [[ "${ref_or_tag}" != "latest" ]] || die "rollback target must be immutable; do not deploy latest"
  acquire_deploy_lock
  check_env
  local image_ref
  if [[ "${ref_or_tag}" == *@sha256:* ]]; then
    image_ref="${ref_or_tag}"
  else
    image_ref="$(resolve_digest_ref_for_tag "${ref_or_tag}")"
    set_env_var "DISTR_IMAGE_TAG" "${ref_or_tag}"
  fi
  info "switching Hub image to ${image_ref}; this does not roll back database schema"
  set_env_var "DISTR_IMAGE_REF" "${image_ref}"
  load_env
  pull_image
  compose up -d hub
  health
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
  migrate              run distr migrate explicitly
  up                   start Hub with serve --migrate=false
  release              pull from ECR, start deps, backup, stop Hub, migrate, start Hub, health check
  deploy               build, push to ECR, release to Compose, health check
  health               check http://127.0.0.1:\${DISTR_HTTP_PORT:-8080}/ready
  logs [service]       follow compose logs
  ps                   show compose service state
  cleanup-artifacts    run ArtifactBlob cleanup once
  rollback <ref|tag>   application-only rollback to a previous ECR digest ref or tag
EOF
}

case "${1:-}" in
  init) init_env ;;
  image-check) check_image_env ;;
  check) check_env ;;
  config) compose_config ;;
  ecr-login) ecr_login ;;
  ecr-create-repo) ensure_ecr_repository ;;
  build) build_image ;;
  push) push_image ;;
  pull) pull_image ;;
  deps) start_dependencies ;;
  backup) backup_postgres ;;
  migrate) run_migrations ;;
  up) start_hub ;;
  release) release_from_ecr ;;
  deploy) deploy_all ;;
  health) health ;;
  logs)
    shift
    check_env
    if (($# > 0)); then
      compose logs -f --tail=120 "$@"
    else
      compose logs -f --tail=120
    fi
    ;;
  ps) check_env; compose ps ;;
  cleanup-artifacts) check_env; compose --profile cleanup run --rm artifact-blob-cleanup ;;
  rollback) shift; rollback_app "${1:-}" ;;
  ""|-h|--help|help) usage ;;
  *) usage; die "unknown command: $1" ;;
esac
