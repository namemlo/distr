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

test_restore_identity_updates_image_and_volumes_together() (
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
)

test_restore_snapshot_manifest_binds_all_recovery_inputs() (
  local plan_dir="$TMP/snapshot-plan"
  local handoff_checksum="sha256:$(printf '1%.0s' {1..64})"
  local database_checksum="sha256:$(printf '2%.0s' {1..64})"
  local object_checksum="sha256:$(printf '3%.0s' {1..64})"
  local history_checksum="sha256:$(printf '4%.0s' {1..64})"
  local image='registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  local commit=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  local plan_id=restore_20260901T000000Z_0123456789abcdef
  local snapshot_checksum
  local -A plan=()
  mkdir -p "$plan_dir"
  chmod 0700 "$plan_dir"
  sync() { :; }
  jq() {
    local mode="${1:-}" key value expression file
    local -A values=()
    shift || return
    while [[ "${1:-}" == --arg ]]; do
      key="$2"
      value="$3"
      values["$key"]="$value"
      shift 3
    done
    if [[ "$mode" == -n ]]; then
      printf 'schema=%s\n' "${values[schema]}"
      printf 'planId=%s\n' "${values[planId]}"
      printf 'priorHandoff=%s\n' "${values[handoffChecksum]}"
      printf 'postgresBackup=%s\n' "${values[postgresBackupChecksum]}"
      printf 'rustfsBackup=%s\n' "${values[rustfsBackupChecksum]}"
      printf 'protectedHistory=%s\n' "${values[protectedHistoryChecksum]}"
      printf 'targetImage=%s\n' "${values[targetImageRef]}"
      printf 'targetCommit=%s\n' "${values[targetReleaseCommit]}"
      return
    fi
    [[ "$mode" == -e ]] || return 1
    expression="${1:-}"
    file="${2:-}"
    [[ -n "$expression" && -f "$file" ]] || return 1
    grep -Fxq "planId=${values[planId]}" "$file" &&
      grep -Fxq "priorHandoff=${values[handoffChecksum]}" "$file" &&
      grep -Fxq "postgresBackup=${values[postgresBackupChecksum]}" "$file" &&
      grep -Fxq "rustfsBackup=${values[rustfsBackupChecksum]}" "$file" &&
      grep -Fxq "protectedHistory=${values[protectedHistoryChecksum]}" "$file" &&
      grep -Fxq "targetImage=${values[targetImageRef]}" "$file" &&
      grep -Fxq "targetCommit=${values[targetReleaseCommit]}" "$file"
  }

  write_restore_snapshot_manifest \
    "$plan_dir" "$plan_id" "$handoff_checksum" "$database_checksum" \
    "$object_checksum" "$history_checksum" "$image" "$commit"
  snapshot_checksum="$(checksum_value "$plan_dir/release-restore-snapshot.json")"
  plan=(
    [PLAN_ID]="$plan_id"
    [RESTORE_SNAPSHOT_SHA256]="$snapshot_checksum"
    [PRIOR_HANDOFF_SHA256]="$handoff_checksum"
    [DATABASE_BACKUP_SHA256]="$database_checksum"
    [OBJECT_BACKUP_SHA256]="$object_checksum"
    [PROTECTED_HISTORY_BASELINE_SHA256]="$history_checksum"
    [TARGET_IMAGE_REF]="$image"
    [TARGET_RELEASE_COMMIT]="$commit"
  )
  validate_restore_snapshot_manifest "$plan_dir" plan

  plan[DATABASE_BACKUP_SHA256]="sha256:$(printf '9%.0s' {1..64})"
  if validate_restore_snapshot_manifest "$plan_dir" plan; then
    printf 'snapshot manifest accepted a different database backup binding\n' >&2
    return 1
  fi
)

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
  checksum_value() { printf 'sha256:%064d' 0; }
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
  write_restore_snapshot_manifest() {
    record snapshot
    : >"$1/release-restore-snapshot.json"
  }
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
    'lock fence pull pull volume volume network postgres ready docker-run docker-run history-export history-compare snapshot seal'
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

stub_restore_apply_dependencies() {
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
      [RESTORE_SNAPSHOT_SHA256]=sha256:0000000000000000000000000000000000000000000000000000000000000000
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
  docker() {
    case "$1 $2" in
      'network create') record network ;;
      'run -d')
        if [[ "$*" == *'distr_restore_pg:/var/lib/postgresql'* ]]; then
          record candidate-db
        elif [[ "$*" == *'distr_old_pg:/var/lib/postgresql'* ]]; then
          record source-db
        else
          record unexpected-db
        fi
        ;;
      'stop --time'|'rm distr-restore'*|'rm -f'|'network rm') : ;;
    esac
  }
  wait_for_restore_postgres() {
    case "$1" in
      distr-restore-apply-*) record candidate-ready ;;
      distr-restore-source-*) record source-ready ;;
      *) record unexpected-ready ;;
    esac
  }
  run_protected_history_export() {
    case "$6" in
      *apply-validation*) record pre-export ;;
      *live-source-fence*) record source-export ;;
      *post-switch*) record post-export ;;
    esac
  }
  run_protected_history_exact_compare() {
    case "$8" in
      *apply-compare*) record pre-compare ;;
      *live-source-fence*) record source-compare ;;
      *post-switch*) record post-compare ;;
    esac
  }
  aggregate_volume_checksum() { record aggregate; printf '%064d' 0; }
  restore_runtime_identity_matches() { record configured-source; }
  running_service_volume_name() {
    record "volume:$1"
    case "$1" in
      postgres) printf distr_old_pg ;;
      storage) printf distr_old_object ;;
      *) return 1 ;;
    esac
  }
  stop_restore_runtime() { record stop; }
  set_restore_runtime_identity() { record switch; }
  write_restore_switch_journal() { record "journal:$2"; }
  restore_source_runtime() { record recover-source; }
  load_env() { check_env; }
  compose_config() { record config; }
  start_dependencies() { record deps; }
  start_hub() { record start; }
  health() { record health; }
  verify_running_release_identity() { record running-identity; }
  current_schema_status() { record schema; printf '138:false'; }
  write_restore_applied_state() { record applied; }
  write_restore_failure_state() { record failure; }
}

test_restore_apply_fences_stopped_source_before_switch() (
  reset_events
  local plan_dir="$TMP/apply-plan"
  mkdir -p "$plan_dir"
  stub_restore_apply_dependencies

  restore_apply "$plan_dir"
  assert_events \
    'lock fence volume-check volume-check pull pull network candidate-db candidate-ready pre-export pre-compare aggregate configured-source running-identity volume:postgres volume:storage journal:PREPARED stop journal:SOURCE_STOPPED source-db source-ready source-export source-compare switch journal:IDENTITY_SWITCHED config deps start health running-identity schema post-export post-compare aggregate journal:TARGET_VERIFIED applied journal:COMMITTED'
  ! grep -q '^failure$' "$TMP/events"
)

test_restore_apply_refuses_changed_live_history_and_restores_source() (
  reset_events
  local plan_dir="$TMP/apply-changed-source-plan"
  mkdir -p "$plan_dir"
  stub_restore_apply_dependencies
  run_protected_history_exact_compare() {
    case "$8" in
      *apply-compare*) record pre-compare ;;
      *live-source-fence*) record source-compare; return 1 ;;
      *post-switch*) record post-compare ;;
    esac
  }

  if restore_apply "$plan_dir"; then
    printf 'restore apply switched despite changed live source history\n' >&2
    return 1
  fi
  assert_events \
    'lock fence volume-check volume-check pull pull network candidate-db candidate-ready pre-export pre-compare aggregate configured-source running-identity volume:postgres volume:storage journal:PREPARED stop journal:SOURCE_STOPPED source-db source-ready source-export source-compare recover-source journal:RECOVERED failure'
  ! grep -q '^switch$' "$TMP/events"
)

test_restore_apply_refuses_running_source_volume_drift_before_prepared() (
  reset_events
  local plan_dir="$TMP/apply-source-volume-drift-plan"
  mkdir -p "$plan_dir"
  stub_restore_apply_dependencies
  running_service_volume_name() {
    record "volume:$1"
    case "$1" in
      postgres) printf distr_unexpected_pg ;;
      storage) printf distr_old_object ;;
      *) return 1 ;;
    esac
  }

  if restore_apply "$plan_dir"; then
    printf 'restore apply accepted drifted running source volumes\n' >&2
    return 1
  fi
  assert_events \
    'lock fence volume-check volume-check pull pull network candidate-db candidate-ready pre-export pre-compare aggregate configured-source running-identity volume:postgres volume:storage failure'
  ! grep -Eq '^journal:PREPARED$|^stop$|^switch$|^recover-source$' "$TMP/events"
)

test_restore_switch_journal_transitions_are_checksummed() (
  local plan_dir="$TMP/journal-plan" recorded computed
  local -A plan=(
    [PLAN_ID]=restore_20260901T000000Z_0123456789abcdef
    [RESTORE_SNAPSHOT_SHA256]=sha256:0000000000000000000000000000000000000000000000000000000000000000
    [TARGET_IMAGE_REF]=registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    [TARGET_RELEASE_COMMIT]=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    [POSTGRES_VOLUME]=distr_restore_pg
    [RUSTFS_VOLUME]=distr_restore_object
  )
  local -A journal=()
  mkdir -p "$plan_dir"
  chmod 0700 "$plan_dir"
  sync() { :; }

  write_restore_switch_journal \
    "$plan_dir" PREPARED plan \
    registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
    dddddddddddddddddddddddddddddddddddddddd distr_old_pg distr_old_object
  read_restore_switch_journal "$plan_dir" journal
  [[ "${journal[STATE]}" == PREPARED ]]
  recorded="${journal[JOURNAL_CHECKSUM]}"
  computed="sha256:$(sed '$d' "$plan_dir/restore-switch-journal.env" | sha256sum | awk '{print $1}')"
  [[ "$recorded" == "$computed" ]]
  [[ "$(tail -n 1 "$plan_dir/restore-switch-journal.env")" == "JOURNAL_CHECKSUM=$recorded" ]]

  write_restore_switch_journal \
    "$plan_dir" SOURCE_STOPPED plan \
    registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
    dddddddddddddddddddddddddddddddddddddddd distr_old_pg distr_old_object
  journal=()
  read_restore_switch_journal "$plan_dir" journal
  [[ "${journal[STATE]}" == SOURCE_STOPPED ]]
  if write_restore_switch_journal \
      "$plan_dir" COMMITTED plan \
      registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
      dddddddddddddddddddddddddddddddddddddddd distr_old_pg distr_old_object; then
    printf 'journal accepted an invalid SOURCE_STOPPED to COMMITTED transition\n' >&2
    return 1
  fi

  sed -i 's/^SOURCE_RELEASE_COMMIT=.*/SOURCE_RELEASE_COMMIT=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee/' \
    "$plan_dir/restore-switch-journal.env"
  if read_restore_switch_journal "$plan_dir" journal; then
    printf 'journal accepted content changed without a new checksum\n' >&2
    return 1
  fi
)

stub_restore_recover_dependencies() {
  acquire_deploy_lock() { record lock; }
  require_no_active_timestamp_fence() { record fence; }
  check_env() { :; }
  validate_restore_plan() {
    local -n values="$2"
    values=(
      [PLAN_ID]=restore_20260901T000000Z_0123456789abcdef
      [RESTORE_SNAPSHOT_SHA256]=sha256:0000000000000000000000000000000000000000000000000000000000000000
      [TARGET_IMAGE_REF]=registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      [TARGET_RELEASE_COMMIT]=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      [POSTGRES_VOLUME]=distr_restore_pg
      [RUSTFS_VOLUME]=distr_restore_object
    )
  }
  read_restore_switch_journal() {
    local -n values="$2"
    values=(
      [STATE]="$RECOVERY_TEST_STATE"
      [PLAN_ID]=restore_20260901T000000Z_0123456789abcdef
      [RESTORE_SNAPSHOT_SHA256]=sha256:0000000000000000000000000000000000000000000000000000000000000000
      [SOURCE_IMAGE_REF]=registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
      [SOURCE_RELEASE_COMMIT]=dddddddddddddddddddddddddddddddddddddddd
      [SOURCE_POSTGRES_VOLUME]=distr_old_pg
      [SOURCE_RUSTFS_VOLUME]=distr_old_object
      [TARGET_IMAGE_REF]=registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      [TARGET_RELEASE_COMMIT]=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      [TARGET_POSTGRES_VOLUME]=distr_restore_pg
      [TARGET_RUSTFS_VOLUME]=distr_restore_object
    )
  }
  restore_runtime_identity_matches() {
    case "$RECOVERY_TEST_STATE" in
      TARGET_VERIFIED|COMMITTED)
        [[ "$1" == registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa &&
           "$2" == bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb &&
           "$3" == distr_restore_pg && "$4" == distr_restore_object ]] || return 1
        ;;
      *)
        [[ "$1" == registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc &&
           "$2" == dddddddddddddddddddddddddddddddddddddddd &&
           "$3" == distr_old_pg && "$4" == distr_old_object ]] || return 1
        ;;
    esac
    record identity-check
  }
  restore_source_runtime() { record restore-source; }
  validate_restore_applied_state() {
    [[ "$2" == restore_20260901T000000Z_0123456789abcdef ]] || return 1
    record applied-check
  }
  verify_running_release_identity() { record verify-running; }
  write_restore_switch_journal() { record "journal:$2"; }
}

test_restore_recover_source_stopped_restores_source() (
  reset_events
  local plan_dir="$TMP/recover-source-stopped-plan"
  RECOVERY_TEST_STATE=SOURCE_STOPPED
  mkdir -p "$plan_dir"
  stub_restore_recover_dependencies

  restore_recover "$plan_dir"
  assert_events 'lock fence identity-check restore-source journal:RECOVERED'
)

test_restore_recover_source_stopped_accepts_target_config_and_restores_source() (
  reset_events
  local plan_dir="$TMP/recover-source-stopped-target-config-plan"
  RECOVERY_TEST_STATE=SOURCE_STOPPED
  mkdir -p "$plan_dir"
  stub_restore_recover_dependencies
  restore_runtime_identity_matches() {
    if [[ "$1" == registry.invalid/current@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc ]]; then
      record source-identity-miss
      return 1
    fi
    [[ "$1" == registry.invalid/prior@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa &&
       "$2" == bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb &&
       "$3" == distr_restore_pg && "$4" == distr_restore_object ]] || return 1
    record target-identity-match
  }

  restore_recover "$plan_dir"
  assert_events \
    'lock fence source-identity-miss target-identity-match restore-source journal:RECOVERED'
)

test_restore_recover_target_verified_finalizes_committed() (
  reset_events
  local plan_dir="$TMP/recover-target-verified-plan"
  RECOVERY_TEST_STATE=TARGET_VERIFIED
  mkdir -p "$plan_dir"
  stub_restore_recover_dependencies

  restore_recover "$plan_dir"
  assert_events 'lock fence applied-check identity-check verify-running journal:COMMITTED'
)

test_restore_recover_committed_is_idempotent() (
  reset_events
  local plan_dir="$TMP/recover-committed-plan"
  RECOVERY_TEST_STATE=COMMITTED
  mkdir -p "$plan_dir"
  stub_restore_recover_dependencies

  restore_recover "$plan_dir"
  assert_events 'lock fence applied-check identity-check verify-running'
)

test_restore_recover_recovered_is_idempotent() (
  reset_events
  local plan_dir="$TMP/recover-recovered-plan"
  RECOVERY_TEST_STATE=RECOVERED
  mkdir -p "$plan_dir"
  stub_restore_recover_dependencies

  restore_recover "$plan_dir"
  assert_events 'lock fence identity-check verify-running'
)

test_restore_dispatch_rejects_wrong_arity() {
  if dispatch_command restore-plan one two three four; then return 1; fi
  if dispatch_command restore-plan one two three four five six; then return 1; fi
  if dispatch_command restore-apply; then return 1; fi
  if dispatch_command restore-apply one two; then return 1; fi
  if dispatch_command restore-recover; then return 1; fi
  if dispatch_command restore-recover one two; then return 1; fi
}

test_restore_functions_never_delete_candidate_volumes() {
  local body
  body="$(sed -n '/^restore_plan()/,/^)/p' "$ROOT/deploy/server-docker-compose/deploy.sh")"
  body+="$(sed -n '/^restore_apply()/,/^)/p' "$ROOT/deploy/server-docker-compose/deploy.sh")"
  body+="$(sed -n '/^restore_recover()/,/^)/p' "$ROOT/deploy/server-docker-compose/deploy.sh")"
  ! grep -Eq 'docker volume rm|compose down .*--volumes' <<<"$body"
  grep -q 'write_restore_failure_state' <<<"$body"
}

test_restore_identity_updates_image_and_volumes_together
test_restore_snapshot_manifest_binds_all_recovery_inputs
test_restore_plan_validates_before_sealing_without_switch
test_failed_restore_plan_retains_candidate_volumes
test_restore_apply_fences_stopped_source_before_switch
test_restore_apply_refuses_changed_live_history_and_restores_source
test_restore_apply_refuses_running_source_volume_drift_before_prepared
test_restore_switch_journal_transitions_are_checksummed
test_restore_recover_source_stopped_restores_source
test_restore_recover_source_stopped_accepts_target_config_and_restores_source
test_restore_recover_target_verified_finalizes_committed
test_restore_recover_committed_is_idempotent
test_restore_recover_recovered_is_idempotent
test_restore_dispatch_rejects_wrong_arity
test_restore_functions_never_delete_candidate_volumes
printf 'server Compose guarded restore tests passed\n'
