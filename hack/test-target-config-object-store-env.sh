#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

source_deploy_library() {
  export DISTR_DEPLOY_LIB_ONLY=1
  export ENV_FILE="$1"
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
}

set_valid_target_config_env() {
  TARGET_CONFIG_OBJECT_STORE_ENABLED=true
  TARGET_CONFIG_S3_REGION=local
  TARGET_CONFIG_S3_ENDPOINT=http://storage:9000
  TARGET_CONFIG_S3_BUCKET=distr
  TARGET_CONFIG_S3_ACCESS_KEY_ID=local
  TARGET_CONFIG_S3_SECRET_ACCESS_KEY=generated-secret
  export TARGET_CONFIG_OBJECT_STORE_ENABLED TARGET_CONFIG_S3_REGION
  export TARGET_CONFIG_S3_ENDPOINT TARGET_CONFIG_S3_BUCKET
  export TARGET_CONFIG_S3_ACCESS_KEY_ID TARGET_CONFIG_S3_SECRET_ACCESS_KEY
}

test_init_generates_matching_target_config_storage_secret() (
  local env_file="$TMP/init.env" rustfs_secret target_secret
  source_deploy_library "$env_file"
  random_b64() { printf 'generated-%s-byte-secret' "$1"; }

  init_env >/dev/null

  rustfs_secret="$(sed -n 's/^RUSTFS_SECRET_KEY=//p' "$env_file")"
  target_secret="$(sed -n 's/^TARGET_CONFIG_S3_SECRET_ACCESS_KEY=//p' "$env_file")"
  [[ -n "$rustfs_secret" && "$rustfs_secret" != *CHANGE_ME* ]]
  [[ "$target_secret" == "$rustfs_secret" ]]
  grep -Eq '^TARGET_CONFIG_S3_BUCKET=[A-Za-z0-9][A-Za-z0-9._-]{2,62}$' "$env_file"
)

test_example_requires_init_before_verifier_is_configured() (
  local env_file="$TMP/example.env"
  cp "$ROOT/deploy/server-docker-compose/.env.example" "$env_file"
  chmod 0600 "$env_file"
  source_deploy_library "$env_file"
  load_env

  if check_target_config_object_store_env >/dev/null 2>&1; then
    printf '.env.example falsely configured the target-config verifier before init\n' >&2
    return 1
  fi
)

test_init_secret_helper_supports_iam_role() (
  local env_file="$TMP/iam-init.env"
  printf '%s\n' \
    'TARGET_CONFIG_S3_ACCESS_KEY_ID=' \
    'TARGET_CONFIG_S3_SECRET_ACCESS_KEY=' >"$env_file"
  chmod 0600 "$env_file"
  source_deploy_library "$env_file"

  initialize_target_config_object_store_secret generated-local-secret

  grep -Eq '^TARGET_CONFIG_S3_ACCESS_KEY_ID=$' "$env_file"
  grep -Eq '^TARGET_CONFIG_S3_SECRET_ACCESS_KEY=$' "$env_file"
)

test_validation_accepts_aws_defaults_and_iam_role() (
  local env_file="$TMP/aws-defaults.env"
  : >"$env_file"
  chmod 0600 "$env_file"
  source_deploy_library "$env_file"
  set_valid_target_config_env

  unset TARGET_CONFIG_S3_ENDPOINT
  unset TARGET_CONFIG_S3_ACCESS_KEY_ID
  unset TARGET_CONFIG_S3_SECRET_ACCESS_KEY
  check_target_config_object_store_env
)

test_validation_rejects_incomplete_or_placeholder_enabled_config() (
  local env_file="$TMP/validation.env" key original
  : >"$env_file"
  chmod 0600 "$env_file"
  source_deploy_library "$env_file"
  set_valid_target_config_env

  check_target_config_object_store_env

  for key in \
    TARGET_CONFIG_S3_REGION \
    TARGET_CONFIG_S3_BUCKET; do
    original="${!key}"
    printf -v "$key" '%s' ''
    export "$key"
    if check_target_config_object_store_env >/dev/null 2>&1; then
      printf 'enabled target-config storage accepted missing %s\n' "$key" >&2
      return 1
    fi
    printf -v "$key" '%s' "$original"
    export "$key"

    printf -v "$key" '%s' 'CHANGE_ME_UNSAFE'
    export "$key"
    if check_target_config_object_store_env >/dev/null 2>&1; then
      printf 'enabled target-config storage accepted placeholder %s\n' "$key" >&2
      return 1
    fi
    printf -v "$key" '%s' "$original"
    export "$key"
  done

  for key in \
    TARGET_CONFIG_S3_ENDPOINT \
    TARGET_CONFIG_S3_ACCESS_KEY_ID \
    TARGET_CONFIG_S3_SECRET_ACCESS_KEY; do
    original="${!key}"
    printf -v "$key" '%s' 'CHANGE_ME_UNSAFE'
    export "$key"
    if check_target_config_object_store_env >/dev/null 2>&1; then
      printf 'enabled target-config storage accepted placeholder %s\n' "$key" >&2
      return 1
    fi
    printf -v "$key" '%s' "$original"
    export "$key"
  done

  TARGET_CONFIG_S3_SECRET_ACCESS_KEY=''
  export TARGET_CONFIG_S3_SECRET_ACCESS_KEY
  if check_target_config_object_store_env >/dev/null 2>&1; then
    printf 'enabled target-config storage accepted partial static credentials\n' >&2
    return 1
  fi
  TARGET_CONFIG_S3_SECRET_ACCESS_KEY=generated-secret
  export TARGET_CONFIG_S3_SECRET_ACCESS_KEY

  TARGET_CONFIG_S3_ENDPOINT='https://user:password@objects.example.invalid'
  export TARGET_CONFIG_S3_ENDPOINT
  if check_target_config_object_store_env >/dev/null 2>&1; then
    printf 'enabled target-config storage accepted invalid optional endpoint\n' >&2
    return 1
  fi

  TARGET_CONFIG_OBJECT_STORE_ENABLED=false
  unset TARGET_CONFIG_S3_ENDPOINT TARGET_CONFIG_S3_BUCKET
  unset TARGET_CONFIG_S3_ACCESS_KEY_ID TARGET_CONFIG_S3_SECRET_ACCESS_KEY
  check_target_config_object_store_env
)

test_development_compose_has_real_target_config_bucket() (
  local env_file="$ROOT/deploy/docker/.env" bucket
  bucket="$(sed -n 's/^TARGET_CONFIG_S3_BUCKET=//p' "$env_file" | tr -d '"')"
  [[ "$bucket" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]]
  [[ "${bucket^^}" != *CHANGE_ME* ]]
)

test_image_and_runtime_checks_enforce_target_config_storage() (
  local env_file="$TMP/checks.env"
  : >"$env_file"
  chmod 0600 "$env_file"
  source_deploy_library "$env_file"
  load_env() { :; }
  set_valid_target_config_env

  AWS_REGION=ap-southeast-1
  DISTR_IMAGE=821392278328.dkr.ecr.ap-southeast-1.amazonaws.com/distr
  DISTR_IMAGE_TAG=immutable-tag
  export AWS_REGION DISTR_IMAGE DISTR_IMAGE_TAG

  COMPOSE_PROJECT_NAME=distr-test
  POSTGRES_USER=distr
  POSTGRES_PASSWORD=generated-password
  POSTGRES_DB=distr
  DATABASE_URL=postgres://distr:generated-password@postgres:5432/distr
  DISTR_HOST=https://distr.invalid
  REGISTRY_HOST=registry.invalid
  JWT_SECRET=generated-jwt
  RUSTFS_ACCESS_KEY=local
  RUSTFS_SECRET_KEY=generated-secret
  export COMPOSE_PROJECT_NAME POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB DATABASE_URL
  export DISTR_HOST REGISTRY_HOST JWT_SECRET RUSTFS_ACCESS_KEY RUSTFS_SECRET_KEY

  TARGET_CONFIG_S3_SECRET_ACCESS_KEY=''
  export TARGET_CONFIG_S3_SECRET_ACCESS_KEY
  if check_image_env >/dev/null 2>&1; then
    printf 'image-check accepted incomplete target-config storage\n' >&2
    return 1
  fi
  if check_runtime_env >/dev/null 2>&1; then
    printf 'runtime check accepted incomplete target-config storage\n' >&2
    return 1
  fi
)

test_init_generates_matching_target_config_storage_secret
test_example_requires_init_before_verifier_is_configured
test_init_secret_helper_supports_iam_role
test_validation_accepts_aws_defaults_and_iam_role
test_validation_rejects_incomplete_or_placeholder_enabled_config
test_development_compose_has_real_target_config_bucket
test_image_and_runtime_checks_enforce_target_config_storage
printf 'target-config object-store environment tests passed\n'
