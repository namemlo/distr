#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export DISTR_DEPLOY_LIB_ONLY=1
export ENV_FILE="$TMP/.env"
export LOCK_FILE="$TMP/deploy.lock"
export TIMESTAMP_FENCE_FILE="$TMP/timestamp-fence"
export BACKUP_DIR="$TMP/backups"
mkdir -p "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"
printf 'COMPOSE_PROJECT_NAME=distr-test\n' >"$ENV_FILE"
chmod 0600 "$ENV_FILE"
source "$ROOT/deploy/server-docker-compose/deploy.sh"

record() { printf '%s\n' "$1" >>"$TMP/events"; }
reset_events() { : >"$TMP/events"; }
assert_events() {
  local expected="$1" actual
  actual="$(paste -sd' ' "$TMP/events")"
  [[ "$actual" == "$expected" ]] || {
    printf 'want: %s\n got: %s\n' "$expected" "$actual" >&2
    return 1
  }
}

test_restore_identity_updates_image_and_volumes_together() {
  local image='registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  set_restore_runtime_identity \
    "$image" bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
    distr_restore_pg distr_restore_object
  [[ "$(grep -c '^DISTR_IMAGE_REF=' "$ENV_FILE")" == 1 ]]
  [[ "$(grep -c '^DISTR_RELEASE_COMMIT=' "$ENV_FILE")" == 1 ]]
  [[ "$(grep -c '^DISTR_IMAGE_DIGEST=' "$ENV_FILE")" == 1 ]]
  [[ "$(grep -c '^POSTGRES_VOLUME_NAME=' "$ENV_FILE")" == 1 ]]
  [[ "$(grep -c '^RUSTFS_VOLUME_NAME=' "$ENV_FILE")" == 1 ]]
  grep -Fxq "DISTR_IMAGE_REF=$image" "$ENV_FILE"
  grep -Fxq 'POSTGRES_VOLUME_NAME=distr_restore_pg' "$ENV_FILE"
  grep -Fxq 'RUSTFS_VOLUME_NAME=distr_restore_object' "$ENV_FILE"
}

stub_restore_plan_dependencies() {
  acquire_deploy_lock() { record lock; }
  require_no_active_timestamp_fence() { record fence; }
  check_env() {
    COMPOSE_PROJECT_NAME=distr-test
    POSTGRES_USER=distr
    POSTGRES_PASSWORD=secret
    POSTGRES_DB=distr
    DISTR_IMAGE_REF='registry.invalid/operator@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
    DISTR_RELEASE_COMMIT=dddddddddddddddddddddddddddddddddddddddd
  }
  verify_checksum_bound_input() { :; }
  checksum_bound_input_value() { printf 'sha256:%064d' 0; }
  prepare_restore_plan_directory() { mkdir -p "$1"; chmod 0700 "$1"; }
  metadata_value() {
    case "$2" in
      DISTR_IMAGE_REF) printf 'registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' ;;
      DISTR_RELEASE_COMMIT) printf 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' ;;
      DISTR_IMAGE_DIGEST) printf 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' ;;
    esac
  }
  jq() { printf '138\n'; }
  openssl() { printf '0123456789abcdef\n'; }
  copy_file_create_new_0600() { :; }
  write_sha256_sidecar_create_new() { :; }
  pull_immutable_image_ref() { record pull; }
  image_release_commit() {
    case "$1" in
      *prior*) printf 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' ;;
      *) printf 'dddddddddddddddddddddddddddddddddddddddd' ;;
    esac
  }
}

test_restore_plan_validates_before_sealing_without_switch() (
  reset_events
  local plan="$TMP/plan-success"
  stub_restore_plan_dependencies
  docker() {
    case "$1 $2" in
      'volume create') record volume ;;
      'network create') record network ;;
      'run -d') record postgres ;;
      'run --rm') record docker-run ;;
      'rm -f'|'network rm') : ;;
    esac
  }
  wait_for_restore_postgres() { record ready; }
  aggregate_volume_checksum() { printf '%064d' 0; }
  run_protected_history_export() { record history-export; }
  run_protected_history_exact_compare() { record history-compare; }
  write_restore_plan_state() { record seal; }
  write_restore_failure_state() { record failure; }

  restore_plan \
    "$TMP/handoff" "$TMP/postgres.dump" "$TMP/rustfs.tar.gz" \
    "$TMP/history.json" "$plan" -
  assert_events \
    'lock fence pull pull volume volume network postgres ready docker-run docker-run history-export history-compare seal'
  ! grep -q '^failure$' "$TMP/events"
)

test_failed_restore_plan_retains_candidate_volumes() (
  reset_events
  : >"$TMP/docker-events"
  local plan="$TMP/plan-failure"
  stub_restore_plan_dependencies
  docker() {
    printf '%s\n' "$*" >>"$TMP/docker-events"
    [[ "$1 $2" != 'run -d' ]]
  }
  write_restore_failure_state() { record "retained:$3:$4:$5"; }

  if restore_plan \
      "$TMP/handoff" "$TMP/postgres.dump" "$TMP/rustfs.tar.gz" \
      "$TMP/history.json" "$plan" -; then
    printf 'failed restore plan unexpectedly succeeded\n' >&2
    return 1
  fi
  grep -q '^retained:.*:.*:RESTORE_VOLUMES$' "$TMP/events"
  ! grep -q '^volume rm' "$TMP/docker-events"
)

test_restore_apply_revalidates_before_atomic_switch() (
  reset_events
  local plan_dir="$TMP/apply-plan"
  mkdir -p "$plan_dir"
  acquire_deploy_lock() { record lock; }
  require_no_active_timestamp_fence() { record fence; }
  check_env() {
    COMPOSE_PROJECT_NAME=distr-test
    POSTGRES_USER=distr
    POSTGRES_PASSWORD=secret
    POSTGRES_DB=distr
    DATABASE_URL='postgres://distr:secret@postgres:5432/distr?sslmode=disable'
    DISTR_IMAGE_REF='registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
    DISTR_RELEASE_COMMIT=dddddddddddddddddddddddddddddddddddddddd
  }
  validate_restore_plan() {
    local -n values="$2"
    values=(
      [PLAN_ID]=restore_20260901T000000Z_0123456789abcdef
      [TARGET_IMAGE_REF]=registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      [TARGET_RELEASE_COMMIT]=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      [OPERATOR_IMAGE_REF]=registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
      [OPERATOR_RELEASE_COMMIT]=dddddddddddddddddddddddddddddddddddddddd
      [POSTGRES_VOLUME]=distr_restore_pg
      [RUSTFS_VOLUME]=distr_restore_object
      [RETIREMENT_ALLOWLIST_SHA256]=NONE
      [OBJECT_AGGREGATE_SHA256]=sha256:0000000000000000000000000000000000000000000000000000000000000000
      [SOURCE_SCHEMA_VERSION]=138
    )
  }
  configured_postgres_volume() { printf distr_old_pg; }
  configured_rustfs_volume() { printf distr_old_object; }
  require_restore_volume_binding() { record volume-check; }
  pull_immutable_image_ref() { record pull; }
  image_release_commit() {
    case "$1" in
      *prior*) printf 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' ;;
      *) printf 'dddddddddddddddddddddddddddddddddddddddd' ;;
    esac
  }
  openssl() { printf '0123456789abcdef\n'; }
  docker() { :; }
  wait_for_restore_postgres() { record ready; }
  run_protected_history_export() {
    case "$6" in
      *apply-validation*) record pre-export ;;
      *post-switch*) record post-export ;;
    esac
  }
  run_protected_history_exact_compare() {
    case "$4" in
      *apply-validation*) record pre-compare ;;
      *post-switch*) record post-compare ;;
    esac
  }
  aggregate_volume_checksum() { record aggregate; printf '%064d' 0; }
  stop_restore_runtime() { record stop; }
  set_restore_runtime_identity() { record switch; }
  load_env() { check_env; }
  compose_config() { record config; }
  start_dependencies() { record deps; }
  start_hub() { record start; }
  health() { record health; }
  verify_running_release_identity() { record identity; }
  current_schema_status() { printf '138:false'; }
  write_restore_applied_state() { record applied; }
  write_restore_failure_state() { record failure; }

  restore_apply "$plan_dir"
  assert_events \
    'lock fence volume-check volume-check pull pull ready pre-export pre-compare aggregate stop switch config deps start health identity post-export post-compare aggregate applied'
  ! grep -q '^failure$' "$TMP/events"
)

test_restore_dispatch_rejects_wrong_arity() {
  if dispatch_command restore-plan one two three four; then return 1; fi
  if dispatch_command restore-apply; then return 1; fi
  if dispatch_command restore-apply one two; then return 1; fi
}

test_restore_functions_never_delete_candidate_volumes() {
  local body
  body="$(sed -n '/^restore_plan()/,/^)/p' "$ROOT/deploy/server-docker-compose/deploy.sh")"
  body+="$(sed -n '/^restore_apply()/,/^)/p' "$ROOT/deploy/server-docker-compose/deploy.sh")"
  ! grep -Eq 'docker volume rm|compose down .*--volumes' <<<"$body"
  grep -q 'write_restore_failure_state' <<<"$body"
}

test_restore_identity_updates_image_and_volumes_together
test_restore_plan_validates_before_sealing_without_switch
test_failed_restore_plan_retains_candidate_volumes
test_restore_apply_revalidates_before_atomic_switch
test_restore_dispatch_rejects_wrong_arity
test_restore_functions_never_delete_candidate_volumes
printf 'server Compose guarded restore tests passed\n'
