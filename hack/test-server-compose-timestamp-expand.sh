#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export DISTR_DEPLOY_LIB_ONLY=1
export ENV_FILE="$TMP/.env"
export BACKUP_DIR="$TMP/backups"
export TIMESTAMP_FENCE_FILE="$TMP/fence"
export DISTR_TIMESTAMP_EVIDENCE_DIR="$TMP/evidence"
export DISTR_IMAGE_REF='registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
export DISTR_IMAGE_DIGEST="${DISTR_IMAGE_REF##*@}"
export DISTR_RELEASE_COMMIT=cccccccccccccccccccccccccccccccccccccccc
mkdir -p "$DISTR_TIMESTAMP_EVIDENCE_DIR"
chmod 0700 "$DISTR_TIMESTAMP_EVIDENCE_DIR"
source "$ROOT/deploy/server-docker-compose/deploy.sh"

events=()
record(){
  events+=("$1")
  printf '%s\n' "$1" >>"$TMP/event-log"
}
assert_events(){
  local want="$1" actual
  actual="$(paste -sd' ' "$TMP/event-log")"
  [[ "$actual" == "$want" ]] || {
    printf 'want: %s\n got: %s\n' "$want" "$actual" >&2
    return 1
  }
}
test_shared_default_evidence_dir_is_restricted(){
  path_mode_is "$DISTR_TIMESTAMP_EVIDENCE_DIR" 700
}
reset_stubs(){
 events=()
 : >"$TMP/event-log"
 active_timestamp_fence(){ return 1; }
 acquire_deploy_lock(){ record lock; }
 check_timestamp_apply_env(){ :; }
 check_env(){ DISTR_RELEASE_COMMIT=cccccccccccccccccccccccccccccccccccccccc; }
 compose_config(){ record config; }
 pull_image(){ record pull; }
 start_dependencies(){ record deps; }
 prepare_timestamp_evidence_dir(){ :; }
 write_fence_id_evidence(){ :; }
 ensure_fence_id_evidence(){ :; }
 capture_evidence_complete(){ return 1; }
 reset_incomplete_capture_evidence(){ :; }
 write_timestamp_evidence_bundle(){ :; }
 reviewed_manifest_checksum(){ printf '%s' 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; }
 validate_timestamp_apply_report(){ :; }
 write_sha256_sidecar_create_new(){ :; }
 copy_file_create_new_0600(){ :; }
 persist_timestamp_fence(){ record "fence:$1"; }
 stop_hub(){ record stop; }
 assert_hub_writers_stopped(){ record writers-stopped; }
 backup_and_restore_timestamp_evidence(){
   record backup-db
   record backup-object
   record restore-db
   record restore-object
   record restore-inspect
   record object-restore-inspect
 }
 compare_timestamp_inspections(){ record compare; }
 require_timestamp_fence(){ record require-fence; }
 timestamp_expand_apply_phase(){ printf 'SCHEMA_137'; }
 timestamp_manifest_transition_counts(){ printf '5:5:30'; }
 stop_fenced_hub_if_running(){ :; }
 cleanup_fenced_acceptance_hubs(){ :; }
 verify_timestamp_evidence(){ record evidence; }
 stage_approved_manifest(){ record stage-approved; }
 run_timestamp_operator(){ case "$*" in *validate-manifest*) record validate;; *verify*) record verify;; *inspect*) record source-inspect;; esac; }
 run_timestamp_apply_report(){
   if [[ "$3" == true ]]; then
     record dry-run
     printf '{"wouldPopulateCount":1}\n' >"$4"
   else
     record apply
   fi
 }
 timestamp_apply_expected_population(){ printf 1; }
 verify_applied_population_count(){ :; }
 run_timestamp_migration_138(){ record migrate-138; }
 start_hub(){ record start; }
 health(){ record health; }
 verify_running_digest(){ record digest; }
 require_clean_schema_138(){ record schema-138; }
 require_verified_manifest(){ record manifest-verified; }
 verify_post_start_counts(){ record counts; }
 verify_audit_history_visibility(){ record audit; }
 verify_task_lock_integrity(){ record task-locks; }
 verify_no_duplicate_event_sequence(){ record sequences; }
 persist_timestamp_compatibility(){ record compatibility; }
 clear_timestamp_fence(){ record clear-fence; }
 start_isolated_acceptance_hub(){ record acceptance-start; TIMESTAMP_ACCEPTANCE_NAME=test; TIMESTAMP_ACCEPTANCE_URL=http://127.0.0.1:39001; }
 health_at_url(){ record acceptance-health; }
 verify_isolated_acceptance_digest(){ record acceptance-digest; }
 stop_isolated_acceptance_hub(){ if [[ -n "${TIMESTAMP_ACCEPTANCE_NAME:-}" ]]; then record acceptance-stop; fi; TIMESTAMP_ACCEPTANCE_NAME=; TIMESTAMP_ACCEPTANCE_URL=; }
 run_migration_preflight(){ record check; }
 backup_and_restore_release_evidence(){
   record ordinary-backup-db
   record ordinary-backup-object
   record ordinary-restore-db
   record ordinary-restore-object
   record ordinary-verify-restore
 }
 run_migrations(){ record ordinary-migrate; }
 require_clean_schema_137(){ record schema-137; }
 validate_source_inspection(){ record source-validate; }
 set_env_var(){ record set-env; }
 set_image_identity(){ record "set-image:$1"; }
 pull_immutable_image_ref(){ record pull; }
 image_release_commit(){ printf cccccccccccccccccccccccccccccccccccccccc; }
 load_env(){ :; }
 fence_value(){
   case "$1" in
     TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
     SOURCE_IMAGE_DIGEST)
       printf '%s' 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
       ;;
     FENCE_ID) printf fence42 ;;
     *) printf '%s' 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' ;;
   esac
 }
 running_hub_digest(){ printf '%s' 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; }
 latest_database_backup(){ printf '%s' "$BACKUP_DIR/postgres-fence42.dump"; }
 checksum_value(){ printf '%s' 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; }
 manifest_id(){ printf '%s' '11111111-1111-4111-8111-111111111111'; }
}
test_capture_order(){ reset_stubs; timestamp_expand_capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"; assert_events 'lock config pull deps fence:PREPARING stop writers-stopped fence:CAPTURED_WRITERS_STOPPED backup-db backup-object restore-db restore-object restore-inspect object-restore-inspect source-inspect compare'; }
test_apply_order(){ reset_stubs; timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; assert_events 'lock require-fence writers-stopped evidence stage-approved validate dry-run migrate-138 apply verify acceptance-start acceptance-health acceptance-digest schema-138 manifest-verified counts audit task-locks sequences acceptance-stop start health digest schema-138 manifest-verified counts audit task-locks sequences compatibility clear-fence'; }
test_ordinary_release_order(){ reset_stubs; release_from_ecr; assert_events 'lock config pull deps check stop writers-stopped ordinary-backup-db ordinary-backup-object ordinary-restore-db ordinary-restore-object ordinary-verify-restore ordinary-migrate start health'; }
test_backup_command_fences_and_restores_hub_order(){
  reset_stubs
  need_cmd(){ :; }
  running_hub_digest(){ printf '%s' "$DISTR_IMAGE_REF"; }
  backup_postgres
  assert_events \
    'stop writers-stopped ordinary-backup-db ordinary-backup-object ordinary-restore-db ordinary-restore-object ordinary-verify-restore start health'
}
test_public_migrate_fences_and_backs_up_before_migration(){
  local boundary expected
  reset_stubs
  dispatch_ordinary_command migrate
  assert_events \
    'lock config pull deps check stop writers-stopped ordinary-backup-db ordinary-backup-object ordinary-restore-db ordinary-restore-object ordinary-verify-restore ordinary-migrate start health'

  reset_stubs
  run_migration_preflight(){ record check; return 42; }
  if dispatch_ordinary_command migrate; then
    printf 'public migrate unexpectedly continued after failed preflight\n' >&2
    return 1
  fi
  assert_events 'lock config pull deps check'
  ! grep -Eq '^(stop|ordinary-backup-|ordinary-migrate|start)$' "$TMP/event-log"

  for boundary in config pull deps; do
    reset_stubs
    case "$boundary" in
      config) compose_config(){ record config; return 42; }; expected='lock config' ;;
      pull) pull_image(){ record pull; return 42; }; expected='lock config pull' ;;
      deps) start_dependencies(){ record deps; return 42; }; expected='lock config pull deps' ;;
    esac
    if dispatch_ordinary_command migrate; then
      printf 'public migrate unexpectedly continued after %s failure\n' "$boundary" >&2
      return 1
    fi
    assert_events "$expected"
    ! grep -Eq '^(check|stop|ordinary-backup-|ordinary-migrate|start)$' \
      "$TMP/event-log"
  done
}
test_backup_prepares_parent_and_refuses_image_drift_before_outage() (
  local clean_backup="$TMP/clean-standalone-backups"
  BACKUP_DIR="$clean_backup"
  export BACKUP_DIR
  rm -rf "$clean_backup"
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  check_env(){ :; }
  need_cmd(){ :; }
  stop_hub(){ record stop; }
  assert_hub_writers_stopped(){ record writers-stopped; }
  backup_and_restore_release_evidence(){ record backup; }
  start_hub(){ record start; }
  health(){ record health; }
  DISTR_IMAGE_REF='registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  export DISTR_IMAGE_REF
  running_hub_digest(){ printf '%s' "$DISTR_IMAGE_REF"; }
  : >"$TMP/event-log"
  backup_postgres
  [[ -d "$clean_backup" ]]
  path_mode_is "$clean_backup" 700
  assert_events 'stop writers-stopped backup start health'

  rm -rf "$clean_backup"
  running_hub_digest(){
    printf '%s' 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  }
  : >"$TMP/event-log"
  if backup_postgres; then
    printf 'standalone backup unexpectedly accepted running/configured image drift\n' >&2
    return 1
  fi
  [[ -d "$clean_backup" ]]
  [[ ! -s "$TMP/event-log" ]]
)
test_failure_keeps_fence(){
  reset_stubs
  run_timestamp_migration_138(){ record migrate-138; return 42; }
  if timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then return 1; fi
  assert_events 'lock require-fence writers-stopped evidence stage-approved validate dry-run migrate-138'
  ! grep -Eq '^(apply|verify|acceptance-start|start|clear-fence)$' "$TMP/event-log"
}
test_post_start_failure_stops_hub_and_keeps_fence(){
  reset_stubs
  verify_task_lock_integrity(){ record task-locks; return 42; }
  if timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    return 1
  fi
  assert_events 'lock require-fence writers-stopped evidence stage-approved validate dry-run migrate-138 apply verify acceptance-start acceptance-health acceptance-digest schema-138 manifest-verified counts audit task-locks acceptance-stop stop writers-stopped'
  ! grep -Eq '^(compatibility|clear-fence)$' "$TMP/event-log"
}
test_public_post_start_failure_stops_hub_and_keeps_fence(){
  reset_stubs
  local task_lock_calls=0
  verify_task_lock_integrity(){
    task_lock_calls=$((task_lock_calls + 1))
    record task-locks
    if ((task_lock_calls == 2)); then return 42; fi
  }
  if timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'public Hub integrity failure unexpectedly cleared the fence\n' >&2
    return 1
  fi
  assert_events 'lock require-fence writers-stopped evidence stage-approved validate dry-run migrate-138 apply verify acceptance-start acceptance-health acceptance-digest schema-138 manifest-verified counts audit task-locks sequences acceptance-stop start health digest schema-138 manifest-verified counts audit task-locks stop writers-stopped'
  ! grep -Eq '^(compatibility|clear-fence)$' "$TMP/event-log"
}
test_ordinary_restore_failure_prevents_migration(){
  reset_stubs
  backup_and_restore_release_evidence(){
    record ordinary-backup-db
    record ordinary-backup-object
    record ordinary-restore-db
    return 42
  }
  if release_from_ecr; then return 1; fi
  assert_events 'lock config pull deps check stop writers-stopped ordinary-backup-db ordinary-backup-object ordinary-restore-db'
  [[ " ${events[*]} " != *' ordinary-migrate '* && " ${events[*]} " != *' start '* ]]
}
test_nonempty_137_preflight_keeps_old_hub_running(){
  reset_stubs
  run_migration_preflight(){ record check; return 42; }
  if release_from_ecr; then return 1; fi
  assert_events 'lock config pull deps check'
  ! grep -Eq '^(stop|writers-stopped|ordinary-backup-|ordinary-migrate|start)$' \
    "$TMP/event-log"
}

test_approved_manifest_is_create_new_0600() {
  reset_stubs
  local source="$TMP/reviewed-approved.json"
  printf '{"id":"11111111-1111-4111-8111-111111111111"}\n' >"$source"
  # Exercise the real staging functions rather than the reset stub.
  unset -f stage_approved_manifest
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  stage_approved_manifest "$source" "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  path_mode_is "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json" 600
  stage_approved_manifest "$source" "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  verify_sha256_sidecar "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json"
  printf '%064d  approved-manifest.json\n' 0 \
    >"$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json.sha256"
  if stage_approved_manifest "$source" "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'corrupt approved-manifest sidecar unexpectedly accepted\n' >&2
    return 1
  fi
  rm "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json.sha256"
  write_sha256_sidecar_create_new \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json"
  printf '{"id":"22222222-2222-4222-8222-222222222222"}\n' >"$source"
  if (stage_approved_manifest "$source" "$DISTR_TIMESTAMP_EVIDENCE_DIR"); then
    printf 'changed approved manifest unexpectedly replaced staged file\n' >&2
    return 1
  fi
  grep -q '11111111-1111-4111-8111-111111111111' \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json"
  rm "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json.sha256"
  if stage_approved_manifest \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json" \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'missing approved-manifest sidecar unexpectedly accepted\n' >&2
    return 1
  fi

  local dangling_dir="$TMP/dangling-approved"
  mkdir "$dangling_dir"
  chmod 0700 "$dangling_dir"
  if ln -s "$TMP/missing-approved-target" \
      "$dangling_dir/approved-manifest.json" 2>/dev/null; then
    if stage_approved_manifest "$source" "$dangling_dir"; then
      printf 'dangling approved-manifest destination unexpectedly accepted\n' >&2
      return 1
    fi
  fi
}

test_reviewed_manifest_checksum_requires_valid_review_sidecar() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local reviewed="$TMP/independently-reviewed-approved.json"
  printf '{"id":"11111111-1111-4111-8111-111111111111"}\n' >"$reviewed"
  chmod 0600 "$reviewed"

  if reviewed_manifest_checksum "$reviewed" >/dev/null; then
    printf 'reviewed manifest without its review checksum was accepted\n' >&2
    return 1
  fi

  write_sha256_sidecar_create_new "$reviewed"
  reviewed_manifest_checksum "$reviewed" >/dev/null

  printf 'tampered\n' >>"$reviewed"
  if reviewed_manifest_checksum "$reviewed" >/dev/null; then
    printf 'reviewed manifest with a stale review checksum was accepted\n' >&2
    return 1
  fi
)

test_apply_rejects_invalid_review_sidecar_before_parsing_or_staging() {
  reset_stubs
  reviewed_manifest_checksum(){ record review-sidecar; return 42; }
  if timestamp_expand_apply "$TMP/approved.json" \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'apply unexpectedly crossed an invalid review-sidecar boundary\n' >&2
    return 1
  fi
  assert_events 'lock require-fence writers-stopped review-sidecar'
  ! grep -Eq '^(evidence|stage-approved|validate|migrate-138|apply)$' \
    "$TMP/event-log"
}

test_active_fence_refuses_every_mutating_command() {
  local command
  for command in up release deploy rollback cleanup-artifacts; do
    reset_stubs
    active_timestamp_fence(){ return 0; }
    if (
      case "$command" in
        rollback)
          dispatch_command rollback \
            'registry.example.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
          ;;
        *) dispatch_command "$command" ;;
      esac
    ); then
      printf '%s unexpectedly accepted an active timestamp fence\n' \
        "$command" >&2
      return 1
    fi
    if grep -Eq '^(start|ordinary-migrate|ordinary-backup-|clear-fence)' \
      "$TMP/event-log"; then
      printf '%s mutated state after active-fence refusal\n' \
        "$command" >&2
      return 1
    fi
  done
}

test_cancel_clean_137_order() {
  reset_stubs
  timestamp_expand_cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  assert_events \
    'lock require-fence config pull writers-stopped schema-137 source-validate pull set-image:registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa start health digest clear-fence'
}

test_cancel_refuses_schema_138() {
  reset_stubs
  require_clean_schema_137(){
    record schema-138
    return 42
  }
  if timestamp_expand_cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'cancel unexpectedly accepted schema 138\n' >&2
    return 1
  fi
  assert_events 'lock require-fence config pull writers-stopped schema-138'
  ! grep -Eq '^(start|clear-fence)$' "$TMP/event-log"
}

test_fence_file_is_atomic_restricted_and_directory_bound() {
  reset_stubs
  unset -f active_timestamp_fence persist_timestamp_fence fence_value \
    require_timestamp_fence clear_timestamp_fence
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence="$TMP/fence-evidence" other="$TMP/other-evidence"
  local valid="$TMP/valid-fence"
  mkdir -p "$evidence" "$other"
  chmod 0700 "$evidence" "$other"
  rm -f "$TIMESTAMP_FENCE_FILE"
  persist_timestamp_fence PREPARING fence42 "$evidence" \
    'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
    'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  if require_timestamp_fence "$evidence"; then
    printf 'PREPARING fence unexpectedly accepted for apply/cancel\n' >&2
    return 1
  fi
  cp "$TIMESTAMP_FENCE_FILE" "$valid.preparing"
  if persist_timestamp_fence PREPARING fence42 "$evidence" \
      'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
      'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; then
    printf 'invalid PREPARING-to-PREPARING transition accepted\n' >&2
    return 1
  fi
  sync(){ return 42; }
  if persist_timestamp_fence CAPTURED_WRITERS_STOPPED fence42 "$evidence" \
      'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
      'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; then
    return 1
  fi
  unset -f sync
  cmp -s "$valid.preparing" "$TIMESTAMP_FENCE_FILE"
  mv(){ return 42; }
  if persist_timestamp_fence CAPTURED_WRITERS_STOPPED fence42 "$evidence" \
      'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
      'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; then
    return 1
  fi
  unset -f mv
  cmp -s "$valid.preparing" "$TIMESTAMP_FENCE_FILE"
  persist_timestamp_fence CAPTURED_WRITERS_STOPPED fence42 "$evidence" \
    'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
    'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  path_mode_is "$TIMESTAMP_FENCE_FILE" 600
  [[ "$(wc -l <"$TIMESTAMP_FENCE_FILE")" == 6 ]]
  require_timestamp_fence "$evidence"
  if (require_timestamp_fence "$other"); then return 1; fi
  cp "$TIMESTAMP_FENCE_FILE" "$valid"

  printf 'UNKNOWN=value\n' >>"$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi
  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  printf 'STATE=CAPTURED_WRITERS_STOPPED\n' >>"$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi
  grep -v '^STATE=' "$valid" >"$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi
  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  sed -i 's/^STATE=.*/STATE=CAPTURED=WRITERS_STOPPED/' "$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi
  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  sed -i 's/^FENCE_ID=.*/FENCE_ID=bad value/' "$TIMESTAMP_FENCE_FILE"
  if (require_timestamp_fence "$evidence"); then return 1; fi

  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  local symlink_fence="$TMP/symlink-fence"
  if ln -s "$TIMESTAMP_FENCE_FILE" "$symlink_fence" 2>/dev/null && \
      [[ -L "$symlink_fence" ]]; then
    local original_fence="$TIMESTAMP_FENCE_FILE"
    TIMESTAMP_FENCE_FILE="$symlink_fence"
    if (require_timestamp_fence "$evidence"); then return 1; fi
    TIMESTAMP_FENCE_FILE="$original_fence"
  fi
  if [[ "$(uname -s)" != MINGW* && "$(uname -s)" != MSYS* && "$(uname -s)" != CYGWIN* ]]; then
    cp "$valid" "$TIMESTAMP_FENCE_FILE"
    chmod 0666 "$TIMESTAMP_FENCE_FILE"
    if (require_timestamp_fence "$evidence"); then return 1; fi
  fi
  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  stat(){
    if [[ "${2:-}" == %u ]]; then printf 999999; else command stat "$@"; fi
  }
  if (require_timestamp_fence "$evidence"); then return 1; fi
  unset -f stat

  cp "$valid" "$TIMESTAMP_FENCE_FILE"
  chmod 0600 "$TIMESTAMP_FENCE_FILE"
  clear_timestamp_fence "$evidence"
  [[ ! -e "$TIMESTAMP_FENCE_FILE" ]]
}

test_operator_uses_deployment_identity_and_env_override() (
  reset_stubs
  local fakebin="$TMP/operator-fakebin" uid gid owner mode operator_lines
  mkdir -p "$fakebin"
  cat >"$fakebin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s|%s\n' "${DISTR_COMPOSE_ENV_FILE:-}" "$*" >>"$TMP/operator-docker-log"
case "$*" in
  *'inspect --format'*'distr-timestamp-operator-'*)
    name="${*: -1}"
    printf '%s\n' "${name##*-}"
    ;;
esac
exit 0
SH
  chmod +x "$fakebin/docker"
  export PATH="$fakebin:$PATH" TMP
  unset DISTR_COMPOSE_ENV_FILE
  unset -f compose run_timestamp_operator run_timestamp_operator_with_database \
    run_migration_preflight run_timestamp_migration_138 \
    prepare_timestamp_evidence_dir
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  : >"$TMP/operator-docker-log"
  uid="$(id -u)"
  gid="$(id -g)"

  prepare_timestamp_evidence_dir "$TMP/operator-evidence"
  mode="$(stat -c '%a' "$TMP/operator-evidence")"
  owner="$(stat -c '%u:%g' "$TMP/operator-evidence")"
  path_mode_is "$TMP/operator-evidence" 700
  [[ "$owner" == "$uid:$gid" ]]

  compose config
  run_timestamp_operator "$TMP/operator-evidence" \
    external-execution-timestamps validate-manifest --manifest /evidence/approved-manifest.json
  run_timestamp_operator_with_database "$TMP/operator-evidence" \
    'postgres://restore.invalid/distr' \
    external-execution-timestamps inspect --output /evidence/restore-inspection.json
  run_migration_preflight
  run_timestamp_migration_138 "$TMP/operator-evidence"

  while IFS='|' read -r propagated_env arguments; do
    [[ "$arguments" == compose\ * ]] || continue
    [[ "$propagated_env" == "$ENV_FILE" ]] || return 1
  done <"$TMP/operator-docker-log"
  operator_lines="$(grep 'timestamp-operator' "$TMP/operator-docker-log")"
  [[ "$(grep -c -- "--user $uid:$gid" <<<"$operator_lines")" == 4 ]]
)

test_restore_failure_runs_cleanup_trap() (
  reset_stubs
  local fakebin="$TMP/fakebin"
  local fence_id='0123456789abcdef0123456789abcdef'
  local restore_container
  export RESTORE_TEST_FENCE="$fence_id"
  mkdir -p "$fakebin"
  cat >"$fakebin/docker" <<'SH'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"$TMP/docker-log"
case "$*" in
  *'pg_restore'*) exit 42 ;;
  *'tar -C /data -czf'*)
    archive="${*#*tar -C /data -czf /backup/}"
    archive="${archive%% *}"
    : >"$BACKUP_DIR/$archive"
    ;;
  *'inspect --format'*'distr-timestamp-pg-'*)
    name="${*: -1}"
    nonce="${name##*-}"
    printf '%s_%s\n' "$RESTORE_TEST_FENCE" "$nonce"
    ;;
  *'volume inspect --format'*"_${RESTORE_TEST_FENCE}_"*)
    name="${*: -1}"
    nonce="${name##*_}"
    printf '%s_%s\n' "$RESTORE_TEST_FENCE" "$nonce"
    ;;
esac
exit 0
SH
  chmod +x "$fakebin/docker"
  export PATH="$fakebin:$PATH"
  export TMP BACKUP_DIR
  : >"$TMP/docker-log"
  unset -f backup_and_restore_timestamp_evidence
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  fence_value(){ printf '%s' "$RESTORE_TEST_FENCE"; }
  compose(){ printf 'database-backup'; }
  prepare_timestamp_evidence_dir(){ mkdir -p "$1"; chmod 0700 "$1"; }
  aggregate_volume_checksum(){
    printf '%064d\n' 0
  }
  if backup_and_restore_timestamp_evidence \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'restore unexpectedly succeeded\n' >&2
    return 1
  fi
  restore_container="$(sed -n \
    's/^docker run -d --name \([^ ]*\).*/\1/p' "$TMP/docker-log")"
  [[ "$restore_container" =~ ^distr-timestamp-pg-[0-9a-f]{16}$ ]]
  ((${#restore_container} <= 63))
  grep -q 'docker rm -f distr-timestamp-pg-' "$TMP/docker-log"
  grep -q "docker volume rm -f .*timestamp_pg_$fence_id" \
    "$TMP/docker-log"
  grep -q "docker volume rm -f .*timestamp_object_$fence_id" \
    "$TMP/docker-log"
  grep -q 'docker ps -aq --filter name=^/distr-timestamp-pg-' \
    "$TMP/docker-log"
  grep -q "docker volume ls -q --filter name=^.*timestamp_pg_$fence_id" \
    "$TMP/docker-log"
  grep -q "docker volume ls -q --filter name=^.*timestamp_object_$fence_id" \
    "$TMP/docker-log"
)

test_restore_postgres_readiness_requires_final_server() (
  local attempts=0
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  docker(){
    [[ "$*" == *'/proc/1/comm'* && "$*" == *'pg_isready'* ]] || return 1
    attempts=$((attempts + 1))
    ((attempts >= 3))
  }
  sleep(){ :; }

  wait_for_restored_postgres distr-timestamp-pg-test
  [[ "$attempts" == 3 ]]
)

test_pre_expand_rollback_refused_after_fence_clear() {
  reset_stubs
  rm -f "$TIMESTAMP_FENCE_FILE"
  export TIMESTAMP_COMPATIBILITY_FILE="$TMP/compatibility"
  cat >"$TIMESTAMP_COMPATIBILITY_FILE" <<'EOF'
SCHEMA_VERSION=138
EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
PRE_EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
MANIFEST_ID=11111111-1111-4111-8111-111111111111
CREATED_AT=2026-07-15T00:00:00Z
EOF
  chmod 0600 "$TIMESTAMP_COMPATIBILITY_FILE"
  current_schema_status(){ printf 138:false; }
  if (require_rollback_schema_compatibility \
      'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'); then
    printf 'pre-expand rollback unexpectedly accepted after fence clear\n' >&2
    return 1
  fi
}

test_real_compatibility_record_is_restricted_idempotent_and_positive() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  TIMESTAMP_COMPATIBILITY_FILE="$TMP/real-compatibility"
  local expand='registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  local source_ref='registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  local manifest='11111111-1111-4111-8111-111111111111' owner
  fence_value(){
    case "$1" in
      TARGET_IMAGE_DIGEST) printf '%s' "$expand" ;;
      SOURCE_IMAGE_DIGEST) printf '%s' "$source_ref" ;;
      *) return 1 ;;
    esac
  }
  persist_timestamp_compatibility "$manifest"
  persist_timestamp_compatibility "$manifest"
  path_mode_is "$TIMESTAMP_COMPATIBILITY_FILE" 600
  owner="$(stat -c '%u' "$TIMESTAMP_COMPATIBILITY_FILE")"
  [[ "$owner" == "$(id -u)" ]]
  [[ "$(wc -l <"$TIMESTAMP_COMPATIBILITY_FILE")" == 5 ]]
  grep -qx 'SCHEMA_VERSION=138' "$TIMESTAMP_COMPATIBILITY_FILE"
  grep -qx "EXPAND_IMAGE_DIGEST=$expand" "$TIMESTAMP_COMPATIBILITY_FILE"
  grep -qx "PRE_EXPAND_IMAGE_DIGEST=$source_ref" "$TIMESTAMP_COMPATIBILITY_FILE"
  grep -qx "MANIFEST_ID=$manifest" "$TIMESTAMP_COMPATIBILITY_FILE"
  [[ "$(grep -c '^CREATED_AT=' "$TIMESTAMP_COMPATIBILITY_FILE")" == 1 ]]
  current_schema_status(){ printf 138:false; }
  require_rollback_schema_compatibility "$expand"
  if require_rollback_schema_compatibility "$source_ref"; then return 1; fi
)

test_schema_139_rollback_fails_closed_before_mutation() {
  reset_stubs
  export TIMESTAMP_COMPATIBILITY_FILE="$TMP/compatibility-139"
  cat >"$TIMESTAMP_COMPATIBILITY_FILE" <<'EOF'
SCHEMA_VERSION=138
EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
PRE_EXPAND_IMAGE_DIGEST=registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
MANIFEST_ID=11111111-1111-4111-8111-111111111111
CREATED_AT=2026-07-15T00:00:00Z
EOF
  chmod 0600 "$TIMESTAMP_COMPATIBILITY_FILE"
  check_env(){ :; }
  current_schema_status(){ printf 139:false; }
  if rollback_app \
      'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; then
    printf 'schema-139 rollback unexpectedly accepted schema-138 metadata\n' >&2
    return 1
  fi
  assert_events 'lock'
  ! grep -Eq '^(set-source-image|pull|start)$' "$TMP/event-log"
}

test_dirty_schema_rollback_fails_closed_before_mutation() {
  local expected_status
  for expected_status in 137:true 138:true; do
    reset_stubs
    check_env(){ :; }
    current_schema_status(){ printf '%s' "$expected_status"; }
    if rollback_app \
        'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; then
      printf 'rollback unexpectedly accepted dirty schema %s\n' "$expected_status" >&2
      return 1
    fi
    assert_events 'lock'
    ! grep -Eq '^(set-env|set-image:|pull|start)$' "$TMP/event-log"
  done
}

test_rollback_calls_compatibility_gate_before_mutation() {
  reset_stubs
  check_env(){ :; }
  require_rollback_schema_compatibility(){ record rollback-gate; return 42; }
  if rollback_app \
      'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; then
    return 1
  fi
  assert_events 'lock rollback-gate'
  [[ " ${events[*]} " != *' set-source-image '* &&
     " ${events[*]} " != *' pull '* && " ${events[*]} " != *' start '* ]]
}

test_compose_uses_one_absolute_env_file_for_every_distr_service() {
  local compose_file="$ROOT/deploy/server-docker-compose/docker-compose.yml"
  [[ "$(grep -Fc '${DISTR_COMPOSE_ENV_FILE:?deploy.sh supplies the absolute env file}' \
    "$compose_file")" == 4 ]]
  grep -Fq "command: ['serve', '--migrate=false']" "$compose_file"
  grep -Fq 'PGAPPNAME: distr-hub' "$compose_file"
  grep -Fq 'PGAPPNAME: distr-timestamp-operator' "$compose_file"
  grep -Fq '${DISTR_TIMESTAMP_EVIDENCE_DIR:-./timestamp-evidence}:/evidence' \
    "$compose_file"
  grep -A20 '^  timestamp-operator:' "$compose_file" |
    grep -Fq -- '- timestamp-operator'
}

test_env_and_evidence_paths_fail_closed() (
  local real_env="$TMP/real.env" linked_env="$TMP/linked.env"
  printf 'SAFE=value\n' >"$real_env"
  ln -s "$real_env" "$linked_env"

  if ENV_FILE=relative.env DISTR_DEPLOY_LIB_ONLY=1 \
      source "$ROOT/deploy/server-docker-compose/deploy.sh"; then
    printf 'relative ENV_FILE unexpectedly accepted\n' >&2
    return 1
  fi

  if [[ -L "$linked_env" ]]; then
    ENV_FILE="$linked_env"
    export ENV_FILE DISTR_DEPLOY_LIB_ONLY=1
    source "$ROOT/deploy/server-docker-compose/deploy.sh"
    if load_env; then
      printf 'symlink ENV_FILE unexpectedly sourced\n' >&2
      return 1
    fi
  fi

  ENV_FILE="$real_env"
  DISTR_COMPOSE_ENV_FILE="$TMP/different.env"
  export ENV_FILE DISTR_COMPOSE_ENV_FILE
  local fakebin="$TMP/env-fakebin"
  mkdir -p "$fakebin"
  printf '#!/usr/bin/env bash\nprintf invoked >>"$TMP/env-docker-log"\n' \
    >"$fakebin/docker"
  chmod +x "$fakebin/docker"
  PATH="$fakebin:$PATH"
  export PATH TMP
  : >"$TMP/env-docker-log"
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  if compose config; then
    printf 'mismatched DISTR_COMPOSE_ENV_FILE unexpectedly accepted\n' >&2
    return 1
  fi
  [[ ! -s "$TMP/env-docker-log" ]]

  if prepare_timestamp_evidence_dir relative-evidence; then
    printf 'relative evidence directory unexpectedly accepted\n' >&2
    return 1
  fi
)

test_dispatch_rechecks_fence_after_lock_for_every_mutator() {
  local command first_argument calls
  for command in init ecr-login ecr-create-repo build push pull deps backup \
      migrate up release deploy cleanup-artifacts rollback; do
    reset_stubs
    calls=0
    active_timestamp_fence(){
      calls=$((calls + 1))
      ((calls >= 2))
    }
    init_env(){ record mutate; }
    ecr_login(){ record mutate; }
    ensure_ecr_repository(){ record mutate; }
    build_image(){ record mutate; }
    push_image(){ record mutate; }
    backup_postgres(){ record mutate; }
    cleanup_artifacts(){ record mutate; }
    first_argument=()
    if [[ "$command" == rollback ]]; then
      first_argument=(
        'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
      )
    fi
    if dispatch_command "$command" "${first_argument[@]}"; then
      printf '%s unexpectedly crossed a fence created before lock\n' \
        "$command" >&2
      return 1
    fi
    assert_events lock
    ! grep -q '^mutate$' "$TMP/event-log"
  done
}

test_dispatch_rejects_wrong_arity_without_exiting_caller() {
  local -a invocation
  while IFS= read -r invocation_text; do
    read -r -a invocation <<<"$invocation_text"
    if (dispatch_command "${invocation[@]}"); then
      printf 'wrong arity unexpectedly accepted: %s\n' "$invocation_text" >&2
      return 1
    fi
  done <<'EOF'
timestamp-expand-capture
timestamp-expand-capture one two
timestamp-expand-apply one
timestamp-expand-apply one two three
timestamp-expand-cancel
timestamp-expand-cancel one two
timestamp-expand-recover-dirty
timestamp-expand-recover-dirty one two three
timestamp-expand-recover-dirty one two three four five
rollback
rollback one two
EOF
  if rg -q '\$\{[0-9]+:\?' \
      "$ROOT/deploy/server-docker-compose/deploy.sh"; then
    printf 'sourceable helper still uses fatal positional expansion\n' >&2
    return 1
  fi
}

test_lock_refuses_symlink_before_opening() (
  local lock_target="$TMP/lock-target" linked_lock="$TMP/linked-lock"
  printf 'do-not-truncate\n' >"$lock_target"
  ln -s "$lock_target" "$linked_lock"
  [[ -L "$linked_lock" ]] || return 0
  LOCK_FILE="$linked_lock"
  export LOCK_FILE
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  if acquire_deploy_lock; then
    printf 'symlink lock unexpectedly acquired\n' >&2
    return 1
  fi
  grep -qx 'do-not-truncate' "$lock_target"
)

test_capture_resumes_preparing_fence() {
  reset_stubs
  active_timestamp_fence(){ return 0; }
  fence_value(){
    case "$1" in
      STATE) printf PREPARING ;;
      FENCE_ID) printf fence42 ;;
      EVIDENCE_DIR_CHECKSUM) evidence_dir_checksum "$DISTR_TIMESTAMP_EVIDENCE_DIR" ;;
      SOURCE_IMAGE_DIGEST)
        printf 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        ;;
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) return 1 ;;
    esac
  }
  ensure_fence_id_evidence(){ :; }
  load_env(){ :; }
  reset_incomplete_capture_evidence(){ record reset-partial; }
  timestamp_expand_capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  assert_events \
    'lock config pull deps stop writers-stopped fence:CAPTURED_WRITERS_STOPPED reset-partial backup-db backup-object restore-db restore-object restore-inspect object-restore-inspect source-inspect compare'
}

test_capture_resumes_captured_fence_without_restarting_writers() {
  reset_stubs
  active_timestamp_fence(){ return 0; }
  fence_value(){
    case "$1" in
      STATE) printf CAPTURED_WRITERS_STOPPED ;;
      FENCE_ID) printf fence42 ;;
      EVIDENCE_DIR_CHECKSUM) evidence_dir_checksum "$DISTR_TIMESTAMP_EVIDENCE_DIR" ;;
      SOURCE_IMAGE_DIGEST)
        printf 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        ;;
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) return 1 ;;
    esac
  }
  ensure_fence_id_evidence(){ :; }
  load_env(){ :; }
  reset_incomplete_capture_evidence(){ record reset-partial; }
  timestamp_expand_capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  assert_events \
    'lock config pull deps writers-stopped reset-partial backup-db backup-object restore-db restore-object restore-inspect object-restore-inspect source-inspect compare'
  ! grep -Eq '^(stop|fence:PREPARING|fence:CAPTURED_WRITERS_STOPPED)$' \
    "$TMP/event-log"
}

test_capture_resume_requires_coherent_fenced_image_identity() (
  local env_file="$TMP/resume-image.env" before="$TMP/resume-image.before"
  local target_ref='registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  local target_digest="${target_ref##*@}"
  local expected_commit=cccccccccccccccccccccccccccccccccccccccc
  ENV_FILE="$env_file"
  export ENV_FILE
  write_identity(){
    printf 'DISTR_IMAGE_REF=%s\nDISTR_RELEASE_COMMIT=%s\nDISTR_IMAGE_DIGEST=%s\n' \
      "$1" "$2" "$3" >"$env_file"
    chmod 0600 "$env_file"
  }
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  compose_config(){ record config; }
  pull_image(){ record pull; }
  image_release_commit(){ printf '%s' "$expected_commit"; }

  : >"$TMP/event-log"
  write_identity "$target_ref" "$expected_commit" "$target_digest"
  resume_fenced_target_image "$target_ref"
  assert_events 'config pull'

  local current_ref='registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  write_identity "$current_ref" "$expected_commit" "${current_ref##*@}"
  cp "$env_file" "$before"
  : >"$TMP/event-log"
  if resume_fenced_target_image "$target_ref"; then
    printf 'capture resume unexpectedly accepted a different image ref\n' >&2
    return 1
  fi
  cmp -s "$before" "$env_file"
  [[ ! -s "$TMP/event-log" ]]

  write_identity "$target_ref" "$expected_commit" 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  cp "$env_file" "$before"
  if resume_fenced_target_image "$target_ref"; then
    printf 'capture resume unexpectedly accepted a mismatched digest\n' >&2
    return 1
  fi
  cmp -s "$before" "$env_file"

  write_identity "$target_ref" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$target_digest"
  cp "$env_file" "$before"
  : >"$TMP/event-log"
  if resume_fenced_target_image "$target_ref"; then
    printf 'capture resume unexpectedly accepted a mismatched OCI commit\n' >&2
    return 1
  fi
  cmp -s "$before" "$env_file"
  assert_events 'config pull'
)

test_dangling_fence_is_active_and_clear_is_durable() (
  reset_stubs
  unset -f active_timestamp_fence persist_timestamp_fence fence_value \
    require_timestamp_fence clear_timestamp_fence
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence="$TMP/durable-fence-evidence"
  mkdir -p "$evidence"
  chmod 0700 "$evidence"
  rm -f "$TIMESTAMP_FENCE_FILE"
  persist_timestamp_fence PREPARING fence99 "$evidence" \
    'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
    'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  persist_timestamp_fence CAPTURED_WRITERS_STOPPED fence99 "$evidence" \
    'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
    'registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  clear_timestamp_fence "$evidence"
  [[ ! -e "$TIMESTAMP_FENCE_FILE" && ! -L "$TIMESTAMP_FENCE_FILE" ]]

  local dangling="$TMP/missing-fence-target"
  if ! ln -s "$dangling" "$TIMESTAMP_FENCE_FILE" 2>/dev/null; then
    return 0
  fi
  if [[ -L "$TIMESTAMP_FENCE_FILE" ]]; then
    active_timestamp_fence
  fi
)

test_capture_writes_canonical_evidence_bundle_after_compare() {
  reset_stubs
  write_timestamp_evidence_bundle(){ record bundle; }
  timestamp_expand_capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  assert_events \
    'lock config pull deps fence:PREPARING stop writers-stopped fence:CAPTURED_WRITERS_STOPPED backup-db backup-object restore-db restore-object restore-inspect object-restore-inspect source-inspect compare bundle'
}

test_evidence_bundle_checksum_binds_every_member() (
  unset -f timestamp_evidence_bundle_checksum write_sha256_sidecar_create_new \
    verify_sha256_sidecar checksum_value
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence="$TMP/bundle-evidence" database_backup object_backup
  mkdir -p "$evidence" "$BACKUP_DIR"
  chmod 0700 "$evidence" "$BACKUP_DIR"
  database_backup="$BACKUP_DIR/postgres-fencebundle.dump"
  object_backup="$BACKUP_DIR/rustfs-fencebundle.tar.gz"
  fence_value(){ printf fencebundle; }
  local file
  for file in "$database_backup" "$object_backup" \
      "$evidence/fence-id" "$evidence/restore-inspection.json" \
      "$evidence/object-restore-inspection.json" \
      "$evidence/source-inspection.json" "$evidence/draft-manifest.json"; do
    if [[ "$file" == "$evidence/fence-id" ]]; then
      printf 'fencebundle\n' >"$file"
    else
      printf 'member:%s\n' "$(basename "$file")" >"$file"
    fi
    chmod 0600 "$file"
    write_sha256_sidecar_create_new "$file"
  done
  local before after original_file="$TMP/member-original" original_sidecar="$TMP/member-original.sha256"
  before="$(timestamp_evidence_bundle_checksum "$evidence")"
  [[ "$before" =~ ^sha256:[0-9a-f]{64}$ ]]
  for file in "$database_backup" "$object_backup" \
      "$evidence/fence-id" "$evidence/restore-inspection.json" \
      "$evidence/object-restore-inspection.json" \
      "$evidence/source-inspection.json" "$evidence/draft-manifest.json"; do
    cp "$file" "$original_file"
    cp "$file.sha256" "$original_sidecar"
    printf 'changed\n' >>"$file"
    rm "$file.sha256"
    write_sha256_sidecar_create_new "$file"
    after="$(timestamp_evidence_bundle_checksum "$evidence")"
    [[ "$after" =~ ^sha256:[0-9a-f]{64}$ && "$before" != "$after" ]]
    cp "$original_file" "$file"
    cp "$original_sidecar" "$file.sha256"
    rm "$file.sha256"
    if timestamp_evidence_bundle_checksum "$evidence" >/dev/null; then
      printf 'bundle accepted missing sidecar for %s\n' "$file" >&2
      return 1
    fi
    cp "$original_sidecar" "$file.sha256"
    printf '%064d  %s\n' 0 "$(basename "$file")" >"$file.sha256"
    if timestamp_evidence_bundle_checksum "$evidence" >/dev/null; then
      printf 'bundle accepted corrupt sidecar for %s\n' "$file" >&2
      return 1
    fi
    cp "$original_sidecar" "$file.sha256"
    [[ "$(timestamp_evidence_bundle_checksum "$evidence")" == "$before" ]]
  done
)

test_apply_revalidates_cross_evidence_and_bundle_before_staging() {
  reset_stubs
  verify_timestamp_evidence(){
    compare_timestamp_inspections a b c || return
    timestamp_evidence_bundle_checksum "$2" >/dev/null || return
    record evidence
  }
  compare_timestamp_inspections(){ record compare-evidence; }
  timestamp_evidence_bundle_checksum(){ record bundle-evidence; printf 'sha256:%064d' 0; }
  timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"
  assert_events \
    'lock require-fence writers-stopped compare-evidence bundle-evidence evidence stage-approved validate dry-run migrate-138 apply verify acceptance-start acceptance-health acceptance-digest schema-138 manifest-verified counts audit task-locks sequences acceptance-stop start health digest schema-138 manifest-verified counts audit task-locks sequences compatibility clear-fence'
}

test_invalid_dry_run_report_prevents_migration() {
  reset_stubs
  validate_timestamp_apply_report(){ record validate-report; return 42; }
  run_timestamp_apply_report(){ record dry-run; validate_timestamp_apply_report; }
  if timestamp_expand_apply "$TMP/approved.json" \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'invalid dry-run report unexpectedly crossed migration boundary\n' >&2
    return 1
  fi
  assert_events \
    'lock require-fence writers-stopped evidence stage-approved validate dry-run validate-report'
  ! grep -Eq '^(migrate-138|apply|start|clear-fence)$' "$TMP/event-log"
}

test_real_apply_report_semantic_gate_matrix() (
  command -v jq >/dev/null 2>&1 || return 0
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  eval "$(declare -f validate_timestamp_apply_report | \
    sed '1s/validate_timestamp_apply_report/real_validate_timestamp_apply_report/')"
  local manifest="$TMP/semantic-manifest.json" source="$TMP/semantic-source.json"
  local report="$TMP/semantic-report.json" invalid="$TMP/semantic-invalid.json"
  cat >"$manifest" <<'JSON'
{"id":"11111111-1111-4111-8111-111111111111","rawCellCount":2,"rawCellChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","databaseIdentityChecksum":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","cells":[{"decision":"PROVEN"},{"decision":"NULL_VALUE"}]}
JSON
  cat >"$source" <<'JSON'
{"rawCellChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","databaseIdentityChecksum":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
JSON
  cat >"$report" <<'JSON'
{"manifestId":"11111111-1111-4111-8111-111111111111","dryRun":true,"idempotent":false,"provenCount":1,"attestedCount":0,"unresolvedCount":0,"nullCount":1,"provenanceRows":0,"wouldPopulateCount":1,"populatedShadowCount":0,"rawSetChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","databaseIdentityChecksum":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
JSON
  real_validate_timestamp_apply_report "$report" "$manifest" "$source" true ''

  expect_semantic_rejection_before_migration() {
    local candidate_report="$1" candidate_manifest="$2" candidate_source="$3" label="$4"
    reset_stubs
    run_timestamp_apply_report(){
      record dry-run
      real_validate_timestamp_apply_report "$candidate_report" \
        "$candidate_manifest" "$candidate_source" true ''
    }
    if timestamp_expand_apply "$candidate_manifest" \
        "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
      printf 'semantic mutation unexpectedly accepted: %s\n' "$label" >&2
      return 1
    fi
    ! grep -Eq '^(migrate-138|apply|verify|acceptance-start|start)$' \
      "$TMP/event-log"
  }

  local filter label
  while IFS='|' read -r label filter; do
    jq "$filter" "$report" >"$invalid"
    expect_semantic_rejection_before_migration "$invalid" "$manifest" "$source" "$label"
  done <<'CASES'
manifest-id|.manifestId="22222222-2222-4222-8222-222222222222"
dry-run|.dryRun=false
idempotent-type|.idempotent="false"
numeric-type|.provenCount="1"
decision-count|.provenCount=0
root-population|.wouldPopulateCount=0
raw-checksum|.rawSetChecksum="sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
database-checksum|.databaseIdentityChecksum="sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
provenance-count|.provenanceRows=1
populated-count|.populatedShadowCount=1
CASES

  jq '.rawCellCount=3' "$manifest" >"$invalid"
  expect_semantic_rejection_before_migration "$report" "$invalid" "$source" manifest-count
  jq '.rawCellChecksum="sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"' \
    "$manifest" >"$invalid"
  expect_semantic_rejection_before_migration "$report" "$invalid" "$source" manifest-checksum
  jq '.databaseIdentityChecksum="sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"' \
    "$source" >"$invalid"
  expect_semantic_rejection_before_migration "$report" "$manifest" "$invalid" source-identity
)

test_post_start_audit_helper_checks_readiness_and_authenticated_history() {
  local deploy="$ROOT/deploy/server-docker-compose/deploy.sh"
  local body
  body="$(sed -n '/^verify_audit_history_visibility()/,/^}/p' "$deploy")"
  grep -Fq 'external-execution-timestamps readiness' <<<"$body"
  grep -Fq 'DISTR_AUDIT_HISTORY_PROBE_URL' <<<"$body"
  grep -Fq 'DISTR_AUDIT_HISTORY_PROBE_TOKEN' <<<"$body"
  grep -Fq 'curl ' <<<"$body"
}

test_audit_history_probe_is_bound_authenticated_and_rejects_empty_history() (
  local fakebin="$TMP/audit-fakebin" evidence="$TMP/audit-evidence"
  local execution_id=11111111-1111-4111-8111-111111111111
  mkdir -p "$fakebin" "$evidence"
  chmod 0700 "$evidence"
  cat >"$fakebin/curl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$TMP/audit-curl-argv"
cat >>"$TMP/audit-curl-config"
output=''
while (($#)); do
  if [[ "$1" == --output ]]; then output="$2"; shift 2; else shift; fi
done
printf '%s\n' "$AUDIT_RESPONSE" >"$output"
SH
  chmod +x "$fakebin/curl"
  PATH="$fakebin:$PATH"
  export PATH TMP AUDIT_RESPONSE
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  run_timestamp_operator(){ :; }
  audit_probe_execution_id(){ printf '%s' "$execution_id"; }
  jq(){
    local expected='' file="${!#}" previous='' argument
    for argument in "$@"; do
      if [[ "$previous" == executionID ]]; then expected="$argument"; break; fi
      [[ "$argument" == executionID ]] && previous=executionID
    done
    node -e '
      const fs=require("fs");
      const value=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));
      const ok=value && value.id===process.argv[2] && Array.isArray(value.events) &&
        value.events.length>0 && value.events.every(e => typeof e.id==="string" &&
          Number.isInteger(e.sequence) && e.sequence>=0);
      process.exit(ok?0:1);
    ' "$file" "$expected"
  }
  DISTR_AUDIT_HISTORY_PROBE_URL="http://127.0.0.1:8080/api/v1/external-executions/$execution_id/"
  DISTR_AUDIT_HISTORY_PROBE_TOKEN=superSecretReadOnlyToken
  export DISTR_AUDIT_HISTORY_PROBE_URL DISTR_AUDIT_HISTORY_PROBE_TOKEN
  : >"$TMP/audit-curl-argv"
  : >"$TMP/audit-curl-config"
  AUDIT_RESPONSE="{\"id\":\"$execution_id\",\"events\":[{\"id\":\"22222222-2222-4222-8222-222222222222\",\"sequence\":1}]}"
  verify_audit_history_visibility "$execution_id" "$evidence"
  grep -Fq -- '--config -' "$TMP/audit-curl-argv"
  ! grep -Fq superSecretReadOnlyToken "$TMP/audit-curl-argv"
  grep -Fq 'Authorization: Bearer superSecretReadOnlyToken' \
    "$TMP/audit-curl-config"
  AUDIT_RESPONSE="{\"id\":\"$execution_id\",\"events\":[]}"
  if verify_audit_history_visibility "$execution_id" "$evidence"; then
    printf 'empty audit history unexpectedly accepted\n' >&2
    return 1
  fi
)

test_audit_probe_uses_canonical_table_and_requires_bound_history() (
  local evidence="$TMP/audit-preflight-evidence"
  local execution_id=11111111-1111-4111-8111-111111111111
  local source="$evidence/source-inspection.json"
  mkdir -p "$evidence"
  chmod 0700 "$evidence"
  cat >"$source" <<JSON
{"eventCount":1,"cells":[{"sourceTable":"externalexecution","sourceRowId":"$execution_id"}]}
JSON
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  jq(){
    local expected='' filter='' file="${!#}" previous='' argument
    for argument in "$@"; do
      if [[ "$previous" == executionID ]]; then
        expected="$argument"
        previous=''
        continue
      fi
      [[ "$argument" == executionID ]] && previous=executionID
      [[ "$argument" == *'.sourceTable =='* ]] && filter="$argument"
    done
    [[ "$filter" == *'.sourceTable == "externalexecution"'* ]] || return 1
    node -e '
      const fs=require("fs");
      const value=JSON.parse(fs.readFileSync(process.argv[1],"utf8"));
      const id=process.argv[2];
      const ok=Number.isFinite(value.eventCount) && value.eventCount>0 &&
        Array.isArray(value.cells) && value.cells.some(cell =>
          cell.sourceTable==="externalexecution" && cell.sourceRowId===id);
      process.exit(ok?0:1);
    ' "$file" "$expected"
  }
  DISTR_AUDIT_HISTORY_PROBE_URL="http://127.0.0.1:8080/api/v1/external-executions/$execution_id/"
  DISTR_AUDIT_HISTORY_PROBE_TOKEN=readOnlyToken
  DISTR_TIMESTAMP_EVIDENCE_CHECKSUM="sha256:$(printf 'a%.0s' {1..64})"
  export DISTR_AUDIT_HISTORY_PROBE_URL DISTR_AUDIT_HISTORY_PROBE_TOKEN
  export DISTR_TIMESTAMP_EVIDENCE_CHECKSUM

  [[ "$(audit_probe_execution_id "$source")" == "$execution_id" ]]
  sed 's/externalexecution/ExternalExecution/' "$source" >"$evidence/wrong-case.json"
  if audit_probe_execution_id "$evidence/wrong-case.json" >/dev/null; then
    printf 'non-canonical source table unexpectedly accepted\n' >&2
    return 1
  fi

  check_env(){ :; }
  verify_sha256_sidecar(){ :; }
  AUDIT_EVENT_COUNT=1
  postgres_scalar(){
    printf '%s\n' "$1" >"$TMP/audit-preflight-sql"
    printf '%s' "$AUDIT_EVENT_COUNT"
  }
  check_timestamp_apply_env "$evidence"
  grep -Fq "external_execution_id = '$execution_id'::uuid" \
    "$TMP/audit-preflight-sql"

  AUDIT_EVENT_COUNT=0
  if check_timestamp_apply_env "$evidence"; then
    printf 'execution without bound audit history unexpectedly accepted\n' >&2
    return 1
  fi
)

test_timed_out_operator_is_stopped_and_removed() (
  local fakebin="$TMP/timeout-fakebin" evidence="$TMP/timeout-evidence"
  mkdir -p "$fakebin" "$evidence"
  chmod 0700 "$evidence"
  cat >"$fakebin/docker" <<'SH'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"$TMP/timeout-docker-log"
case "$*" in
  *'compose '*' run '*'timestamp-operator'*) sleep 5 ;;
  *'inspect --format'*'distr-timestamp-operator-'*)
    name="${*: -1}"
    printf '%s\n' "${name##*-}"
    ;;
esac
exit 0
SH
  chmod +x "$fakebin/docker"
  PATH="$fakebin:$PATH"
  export PATH TMP
  export DISTR_TIMESTAMP_OPERATOR_TIMEOUT=1s
  unset DISTR_COMPOSE_ENV_FILE
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  : >"$TMP/timeout-docker-log"
  if run_timestamp_operator "$evidence" \
      external-execution-timestamps readiness; then
    printf 'timed-out timestamp operator unexpectedly succeeded\n' >&2
    return 1
  fi
  grep -q '^docker stop --time 15 distr-timestamp-operator-' \
    "$TMP/timeout-docker-log"
  grep -q '^docker rm -f distr-timestamp-operator-' \
    "$TMP/timeout-docker-log"
)

test_restore_uses_pg18_layout_labels_and_complete_object_digest() {
  local deploy="$ROOT/deploy/server-docker-compose/deploy.sh" body
  body="$(sed -n '/^backup_and_restore_timestamp_evidence()/,/^)/p' "$deploy")"
  grep -Fq ':/var/lib/postgresql"' <<<"$body"
  ! grep -Fq ':/var/lib/postgresql/data"' <<<"$body"
  grep -Fq "restore_label='distr.sh/timestamp-restore'" <<<"$body"
  grep -Fq -- '--label "$restore_label=$restore_owner"' <<<"$body"
  grep -Fq 'database_restore_container="distr-timestamp-pg-$nonce"' <<<"$body"
  ! grep -Fq 'database_restore_container="${project}-timestamp-pg-$evidence_id-$nonce"' \
    <<<"$body"
  body="$(sed -n '/^aggregate_volume_checksum()/,/^}/p' "$deploy")"
  grep -Fq 'reject' <<<"$body"
  grep -Fq 'type' <<<"$body"
  grep -Fq 'mode' <<<"$body"
  grep -Fq 'set -o pipefail' <<<"$body"
  [[ "$(grep -Fc 'cut -d " " -f 1' <<<"$body")" == 2 ]]
  ! grep -Fq 'awk "{print \\$1}"' <<<"$body"
}

test_guard_review_safety_contracts_are_present() {
  local deploy="$ROOT/deploy/server-docker-compose/deploy.sh" body

  body="$(sed -n '/^path_mode_is()/,/^}/p' "$deploy")"
  grep -Fq 'uname -s' <<<"$body"
  ! grep -Fq 'MSYSTEM' <<<"$body"

  body="$(sed -n '/^load_env()/,/^}/p' "$deploy")"
  grep -Fq 'require_secure_env_file' <<<"$body"

  grep -Fq 'set_image_identity' "$deploy"
  body="$(sed -n '/^write_release_metadata()/,/^}/p' "$deploy")"
  grep -Fq 'set_image_identity' <<<"$body"

  body="$(sed -n '/^timestamp_evidence_bundle_checksum()/,/^}/p' "$deploy")"
  grep -Fq 'latest_database_backup "$evidence_dir"' <<<"$body"
  body="$(sed -n '/^latest_database_backup()/,/^}/p' "$deploy")"
  grep -Fq 'evidence_fence_id' <<<"$body"

  body="$(sed -n '/^validate_source_inspection()/,/^}/p' "$deploy")"
  grep -Fq 'verify_timestamp_evidence_bundle' <<<"$body"
  grep -Fq 'aggregate_volume_checksum' <<<"$body"

  body="$(sed -n '/^run_timestamp_operator_container()/,/^)/p' "$deploy")"
  grep -Fq 'pg_stat_activity' <<<"$body"

  body="$(sed -n '/^start_verify_and_finalize_timestamp_expand()/,/^)/p' "$deploy")"
  grep -Fq 'start_isolated_acceptance_hub' <<<"$body"
  grep -Fq 'stop_isolated_acceptance_hub' <<<"$body"

  body="$(sed -n '/^verify_audit_history_visibility()/,/^}/p' "$deploy")"
  grep -Fq 'expected_execution_id' <<<"$body"
  grep -Fq -- '--config -' <<<"$body"
  ! grep -Fq -- '--header "Authorization:' <<<"$body"

  body="$(sed -n '/^check_timestamp_apply_env()/,/^}/p' "$deploy")"
  grep -Fq 'DISTR_AUDIT_HISTORY_PROBE_TOKEN' <<<"$body"
  grep -Fq 'audit_probe_execution_id' <<<"$body"
  body="$(sed -n '/^audit_probe_execution_id()/,/^}/p' "$deploy")"
  grep -Fq 'DISTR_AUDIT_HISTORY_PROBE_URL' <<<"$body"

  body="$(sed -n '/^run_timestamp_apply_report()/,/^)/p' "$deploy")"
  grep -Fq 'expected_population_count' <<<"$body"
}

test_preflight_creates_secure_backup_parent_on_clean_host() (
  local clean_backup="$TMP/clean-host-backups"
  rm -rf "$clean_backup"
  BACKUP_DIR="$clean_backup"
  unset DISTR_TIMESTAMP_EVIDENCE_DIR
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  run_timestamp_operator(){ :; }
  run_migration_preflight
  [[ -d "$clean_backup/operator-evidence" ]]
  path_mode_is "$clean_backup" 700
  path_mode_is "$clean_backup/operator-evidence" 700
)

test_invalid_complete_capture_is_not_deleted_or_rebuilt() {
  reset_stubs
  active_timestamp_fence(){ return 0; }
  fence_value(){
    case "$1" in
      STATE) printf CAPTURED_WRITERS_STOPPED ;;
      FENCE_ID) printf fence42 ;;
      EVIDENCE_DIR_CHECKSUM) evidence_dir_checksum "$DISTR_TIMESTAMP_EVIDENCE_DIR" ;;
      SOURCE_IMAGE_DIGEST) printf source ;;
      TARGET_IMAGE_DIGEST) printf target ;;
    esac
  }
  capture_evidence_status(){ return 1; }
  reset_incomplete_capture_evidence(){ record destructive-reset; }
  backup_and_restore_timestamp_evidence(){ record rebuilt; }
  if timestamp_expand_capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'invalid complete capture unexpectedly resumed\n' >&2
    return 1
  fi
  ! grep -Eq '^(destructive-reset|rebuilt)$' "$TMP/event-log"
}

test_failed_database_backup_leaves_no_partial_publication() (
  local evidence="$TMP/partial-evidence"
  mkdir -p "$evidence" "$BACKUP_DIR"
  chmod 0700 "$evidence" "$BACKUP_DIR"
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  fence_value(){ printf partial42; }
  prepare_timestamp_evidence_dir(){ :; }
  docker(){ return 0; }
  compose(){ printf partial-database-bytes; return 42; }
  if backup_and_restore_timestamp_evidence "$evidence"; then
    printf 'failed database dump unexpectedly succeeded\n' >&2
    return 1
  fi
  [[ ! -e "$BACKUP_DIR/postgres-partial42.dump" &&
     ! -e "$BACKUP_DIR/postgres-partial42.dump.sha256" ]]
)

test_failed_cancel_restores_target_image_configuration() {
  reset_stubs
  fence_value(){
    case "$1" in
      SOURCE_IMAGE_DIGEST)
        printf 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        ;;
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) printf fence42 ;;
    esac
  }
  set_image_identity(){ record "set-image:$1"; }
  start_verify_cancel_and_clear(){ return 42; }
  if timestamp_expand_cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'failed cancel unexpectedly succeeded\n' >&2
    return 1
  fi
  assert_events \
    "lock require-fence config pull writers-stopped schema-137 source-validate pull set-image:registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa set-image:$DISTR_IMAGE_REF"
  ! grep -Eq '^(start|clear-fence)$' "$TMP/event-log"
}

test_failed_cancel_image_switch_still_restores_fenced_identity() {
  reset_stubs
  local setter_calls=0
  set_image_identity(){
    record "set-image:$1"
    ((setter_calls += 1))
    ((setter_calls > 1))
  }
  if timestamp_expand_cancel "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'cancel image-switch failure unexpectedly succeeded\n' >&2
    return 1
  fi
  assert_events \
    "lock require-fence config pull writers-stopped schema-137 source-validate pull set-image:registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa set-image:$DISTR_IMAGE_REF"
  ! grep -Eq '^(start|clear-fence)$' "$TMP/event-log"
}

test_release_metadata_derives_commit_and_digest_together() (
  cd "$TMP"
  export DISTR_IMAGE_TAG=release-test
  export DISTR_IMAGE='123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr'
  export AWS_REGION=ap-southeast-1
  export ECR_REPOSITORY=distr
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  check_image_env(){ :; }
  need_cmd(){ :; }
  git(){ printf 'cccccccccccccccccccccccccccccccccccccccc\n'; }
  resolve_digest_ref_for_tag(){
    printf '%s' "$DISTR_IMAGE@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
  }
  : >"$TMP/release-metadata-events"
  set_image_identity(){
    printf 'DISTR_IMAGE_REF=%s\n' "$1" >>"$TMP/release-metadata-events"
    printf 'DISTR_RELEASE_COMMIT=%s\n' "$2" >>"$TMP/release-metadata-events"
    printf 'DISTR_IMAGE_DIGEST=%s\n' "${1##*@}" >>"$TMP/release-metadata-events"
  }
  write_release_metadata
  grep -qx "DISTR_IMAGE_REF=$DISTR_IMAGE@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" \
    "$TMP/release-metadata-events"
  grep -qx 'DISTR_RELEASE_COMMIT=cccccccccccccccccccccccccccccccccccccccc' \
    "$TMP/release-metadata-events"
  grep -qx 'DISTR_IMAGE_DIGEST=sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd' \
    "$TMP/release-metadata-events"
  local metadata="$TMP/dist/release-release-test.env"
  grep -qx 'DISTR_RELEASE_COMMIT=cccccccccccccccccccccccccccccccccccccccc' \
    "$metadata"
  grep -qx 'DISTR_IMAGE_DIGEST=sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd' \
    "$metadata"
)

test_pull_image_rejects_mixed_release_identity_before_fence() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  check_env(){ :; }
  need_cmd(){ :; }
  ecr_login(){ :; }
  compose(){ printf '%s\n' "$*" >>"$TMP/mixed-image-pulls"; }
  image_release_commit(){ printf dddddddddddddddddddddddddddddddddddddddd; }
  DISTR_RELEASE_COMMIT=cccccccccccccccccccccccccccccccccccccccc
  DISTR_IMAGE_REF='registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
  export DISTR_RELEASE_COMMIT DISTR_IMAGE_REF
  : >"$TMP/mixed-image-pulls"
  if pull_image; then
    printf 'pull unexpectedly accepted digest/commit from different images\n' >&2
    return 1
  fi
  [[ "$(wc -l <"$TMP/mixed-image-pulls")" == 4 ]]
)

test_capture_pull_identity_failure_precedes_fence_and_outage() {
  reset_stubs
  pull_image(){ record pull; return 42; }
  if timestamp_expand_capture "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'capture unexpectedly continued after image identity refusal\n' >&2
    return 1
  fi
  assert_events 'lock config pull'
  ! grep -Eq '^(fence:|stop|writers-stopped|backup-db)$' "$TMP/event-log"
}

test_nested_command_failure_matrix() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local sample="$TMP/failure-sample" evidence="$TMP/failure-evidence"
  printf sample >"$sample"
  chmod 0600 "$sample"
  write_sha256_sidecar_create_new "$sample"
  mkdir -p "$evidence" "$BACKUP_DIR"
  chmod 0700 "$evidence" "$BACKUP_DIR"

  : >"$TMP/failure-events"
  record_failure(){ printf '%s\n' "$1" >>"$TMP/failure-events"; }
  check_image_env(){ :; }
  need_cmd(){ :; }
  ecr_registry(){ printf registry.invalid; }
  AWS_REGION=ap-southeast-1
  export AWS_REGION
  aws(){ record_failure aws; return 42; }
  docker(){ record_failure docker-login; return 0; }
  if ecr_login; then return 1; fi
  grep -qx aws "$TMP/failure-events"
  ! grep -q docker-login "$TMP/failure-events"

  : >"$TMP/failure-events"
  flock(){ record_failure flock; return 42; }
  LOCK_FILE="$TMP/failure.lock"
  if acquire_deploy_lock; then return 1; fi
  grep -qx flock "$TMP/failure-events"

  : >"$TMP/failure-events"
  stat(){ record_failure stat; return 42; }
  local -A parsed=()
  if parse_restricted_key_file "$sample" A parsed; then return 1; fi
  grep -qx stat "$TMP/failure-events"
  unset -f stat

  : >"$TMP/failure-events"
  jq(){ record_failure jq; return 42; }
  if validate_timestamp_apply_report "$sample" "$sample" "$sample" true; then
    return 1
  fi
  grep -qx jq "$TMP/failure-events"
  unset -f jq

  : >"$TMP/failure-events"
  sha256sum(){ record_failure sha256sum; return 42; }
  if reviewed_manifest_checksum "$sample"; then return 1; fi
  grep -qx sha256sum "$TMP/failure-events"
  unset -f sha256sum

  : >"$TMP/failure-events"
  fence_value(){ printf failure42; }
  prepare_timestamp_evidence_dir(){ :; }
  prepare_backup_directory(){ :; }
  docker(){ record_failure "docker:$*"; return 0; }
  compose(){ record_failure pg_dump; printf partial; return 42; }
  if backup_and_restore_timestamp_evidence "$evidence"; then return 1; fi
  grep -qx pg_dump "$TMP/failure-events"
  ! grep -q 'tar -C /data' "$TMP/failure-events"

  : >"$TMP/failure-events"
  compose(){ printf database; }
  docker(){
    record_failure "docker:$*"
    if [[ "$*" == *'tar -C /data -czf'* ]]; then return 42; fi
    return 0
  }
  if backup_and_restore_timestamp_evidence "$evidence"; then return 1; fi
  grep -q 'tar -C /data -czf' "$TMP/failure-events"
  ! grep -q 'volume create' "$TMP/failure-events"

  : >"$TMP/failure-events"
  run_timestamp_operator(){ :; }
  audit_probe_execution_id(){ printf 11111111-1111-4111-8111-111111111111; }
  need_cmd(){ :; }
  curl(){ record_failure curl; return 42; }
  jq(){ record_failure jq-after-curl; return 0; }
  DISTR_AUDIT_HISTORY_PROBE_URL='http://127.0.0.1:8080/api/v1/external-executions/11111111-1111-4111-8111-111111111111/'
  DISTR_AUDIT_HISTORY_PROBE_TOKEN=token
  export DISTR_AUDIT_HISTORY_PROBE_URL DISTR_AUDIT_HISTORY_PROBE_TOKEN
  if verify_audit_history_visibility \
      11111111-1111-4111-8111-111111111111 "$evidence"; then
    return 1
  fi
  grep -qx curl "$TMP/failure-events"
  ! grep -q jq-after-curl "$TMP/failure-events"
)

test_filesystem_failure_matrix_stops_before_publication() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local source_file="$TMP/fs-source" destination="$TMP/fs-destination"
  printf source >"$source_file"

  readlink(){ return 42; }
  if evidence_dir_checksum "$TMP"; then return 1; fi
  unset -f readlink

  mktemp(){ return 42; }
  if copy_file_create_new_0600 "$source_file" "$destination"; then return 1; fi
  [[ ! -e "$destination" ]]
  unset -f mktemp

  install(){ return 42; }
  if copy_file_create_new_0600 "$source_file" "$destination"; then return 1; fi
  [[ ! -e "$destination" ]]
  unset -f install

  sync(){ return 42; }
  if copy_file_create_new_0600 "$source_file" "$destination"; then return 1; fi
  [[ ! -e "$destination" ]]
  unset -f sync

  ln(){ return 42; }
  if copy_file_create_new_0600 "$source_file" "$destination"; then return 1; fi
  [[ ! -e "$destination" ]]
  unset -f ln

  dirname(){ return 42; }
  if copy_file_create_new_0600 "$source_file" "$destination"; then return 1; fi
  [[ ! -e "$destination" ]]
  unset -f dirname

  basename(){ return 42; }
  if copy_file_create_new_0600 "$source_file" "$destination"; then return 1; fi
  [[ ! -e "$destination" ]]
  unset -f basename

  id(){ return 42; }
  if prepare_timestamp_evidence_dir "$TMP/fs-evidence"; then return 1; fi
  unset -f id

  date(){ return 42; }
  TIMESTAMP_FENCE_FILE="$TMP/fs-fence"
  if persist_timestamp_fence PREPARING fencefs "$TMP" source target; then
    return 1
  fi
  [[ ! -e "$TIMESTAMP_FENCE_FILE" ]]
  unset -f date

  timeout(){ return 42; }
  docker(){ printf 'docker-called\n' >>"$TMP/fs-events"; }
  : >"$TMP/fs-events"
  if run_timestamp_operator "$TMP" external-execution-timestamps readiness; then
    return 1
  fi
  ! grep -q 'compose ' "$TMP/fs-events"
)

test_apply_resumes_after_migration_without_rerunning_migration() (
  reset_stubs
  timestamp_expand_apply_phase(){ printf 'SCHEMA_138_EMPTY'; }
  stop_fenced_hub_if_running(){ record stop-restarted-hub; }
  cleanup_fenced_acceptance_hubs(){ record acceptance-orphans; }
  run_timestamp_apply_report(){
    if [[ "$3" == true ]]; then
      record dry-report-reuse
      printf '{"wouldPopulateCount":1}\n' >"$4"
    else
      record apply
    fi
  }

  timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"

  assert_events \
    'lock require-fence stop-restarted-hub acceptance-orphans writers-stopped evidence stage-approved validate dry-report-reuse apply verify acceptance-start acceptance-health acceptance-digest schema-138 manifest-verified counts audit task-locks sequences acceptance-stop start health digest schema-138 manifest-verified counts audit task-locks sequences compatibility clear-fence'
  ! grep -q '^migrate-138$' "$TMP/event-log"
)

test_apply_resumes_exact_verified_manifest_idempotently() (
  reset_stubs
  printf '{"wouldPopulateCount":1}\n' > \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-apply-dry-run.json"
  timestamp_expand_apply_phase(){ printf 'SCHEMA_138_VERIFIED'; }
  stop_fenced_hub_if_running(){ record stop-restarted-hub; }
  cleanup_fenced_acceptance_hubs(){ record acceptance-orphans; }
  run_timestamp_apply_report(){
    [[ "$3" == true ]] || return 1
    record dry-report-reuse
    printf '{"wouldPopulateCount":1}\n' >"$4"
  }
  run_timestamp_idempotent_apply_report(){ record exact-reapply; }

  timestamp_expand_apply "$TMP/approved.json" "$DISTR_TIMESTAMP_EVIDENCE_DIR"

  assert_events \
    'lock require-fence stop-restarted-hub acceptance-orphans writers-stopped evidence stage-approved dry-report-reuse exact-reapply verify acceptance-start acceptance-health acceptance-digest schema-138 manifest-verified counts audit task-locks sequences acceptance-stop start health digest schema-138 manifest-verified counts audit task-locks sequences compatibility clear-fence'
  ! grep -Eq '^(dry-run|migrate-138|apply)$' "$TMP/event-log"
)

test_verified_resume_refuses_missing_retained_dry_report() (
  reset_stubs
  rm -f \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-apply-dry-run.json" \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-apply-dry-run.json.sha256" \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-apply-result.json" \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-apply-result.json.sha256"
  timestamp_expand_apply_phase(){ printf 'SCHEMA_138_VERIFIED'; }
  timestamp_manifest_resolved_count(){ record inferred-dry-count; printf 1; }
  run_timestamp_idempotent_apply_report(){ record exact-reapply; }

  if timestamp_expand_apply "$TMP/approved.json" \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'verified recovery accepted a missing retained dry-run report\n' >&2
    return 1
  fi
  ! grep -Eq '^(inferred-dry-count|exact-reapply|acceptance-start|start)$' \
    "$TMP/event-log"
)

test_apply_phase_rejects_conflicting_manifest_state() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  declare -F timestamp_expand_apply_phase >/dev/null || {
    printf 'timestamp apply phase helper is missing\n' >&2
    return 1
  }
  current_schema_status(){ printf '137:false'; }
  [[ "$(timestamp_expand_apply_phase \
    11111111-1111-4111-8111-111111111111 5 5 30)" == SCHEMA_137 ]]

  current_schema_status(){ printf '138:false'; }
  stub_phase_row='0:0:0:ABSENT:1:MANIFEST_REQUIRED:137:5:5:30'
  postgres_scalar(){ printf '%s' "$stub_phase_row"; }
  [[ "$(timestamp_expand_apply_phase \
    11111111-1111-4111-8111-111111111111 5 5 30)" == SCHEMA_138_EMPTY ]]

  stub_phase_row='1:30:1:VERIFIED:1:MANIFEST_REQUIRED:137:5:5:30'
  [[ "$(timestamp_expand_apply_phase \
    11111111-1111-4111-8111-111111111111 5 5 30)" == SCHEMA_138_VERIFIED ]]

  local conflict
  for conflict in \
      '1:30:0:ABSENT:1:MANIFEST_REQUIRED:137:5:5:30' \
      '1:30:1:APPLIED:1:MANIFEST_REQUIRED:137:5:5:30' \
      '1:29:1:VERIFIED:1:MANIFEST_REQUIRED:137:5:5:30' \
      '0:0:0:ABSENT:1:ZERO_HISTORY:137:5:5:30' \
      '0:0:0:ABSENT:1:MANIFEST_REQUIRED:136:5:5:30' \
      '0:0:0:ABSENT:1:MANIFEST_REQUIRED:137:4:5:25' \
      '0:0:0:ABSENT:0:ABSENT:0:0:0:0' \
      '2:60:1:VERIFIED:1:MANIFEST_REQUIRED:137:5:5:30'; do
    stub_phase_row="$conflict"
    if timestamp_expand_apply_phase \
        11111111-1111-4111-8111-111111111111 5 5 30; then
      printf 'conflicting timestamp phase was accepted: %s\n' "$conflict" >&2
      return 1
    fi
  done

  current_schema_status(){ printf '138:true'; }
  if timestamp_expand_apply_phase \
      11111111-1111-4111-8111-111111111111 5 5 30; then
    printf 'dirty timestamp schema was accepted\n' >&2
    return 1
  fi
  current_schema_status(){ printf '139:false'; }
  if timestamp_expand_apply_phase \
      11111111-1111-4111-8111-111111111111 5 5 30; then
    printf 'unsupported timestamp schema was accepted\n' >&2
    return 1
  fi
)

test_apply_refuses_transition_marker_mismatch_before_mutation() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local phase_impl
  phase_impl="$(declare -f timestamp_expand_apply_phase)"
  phase_impl="${phase_impl/timestamp_expand_apply_phase ()/timestamp_expand_apply_phase_real ()}"
  eval "$phase_impl"
  reset_stubs
  timestamp_expand_apply_phase(){ timestamp_expand_apply_phase_real "$@"; }
  timestamp_manifest_transition_counts(){ printf '5:5:30'; }
  current_schema_status(){ printf '138:false'; }
  postgres_scalar(){
    if [[ "$1" == *transition_execution_count* ]]; then
      printf '0:0:0:ABSENT:1:MANIFEST_REQUIRED:137:4:5:25'
    else
      printf '0:0:0:ABSENT:MANIFEST_REQUIRED'
    fi
  }

  if timestamp_expand_apply "$TMP/approved.json" \
      "$DISTR_TIMESTAMP_EVIDENCE_DIR"; then
    printf 'transition marker mismatch reached timestamp mutation\n' >&2
    return 1
  fi
  ! grep -Eq '^(dry-run|migrate-138|apply|acceptance-start|start)$' \
    "$TMP/event-log"
)

test_stop_fenced_hub_stops_restarting_service_container() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local stopped=0
  compose(){
    case "$*" in
      "ps --status running -q hub") : ;;
      "ps -q hub") printf 'restarting-hub-container\n' ;;
      *) return 1 ;;
    esac
  }
  stop_hub(){ stopped=$((stopped + 1)); }

  stop_fenced_hub_if_running

  [[ "$stopped" == 1 ]] || {
    printf 'restarting fenced Hub was not stopped before recovery\n' >&2
    return 1
  }
)

test_verified_resume_allows_count_growth_but_not_count_loss() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local stub_counts='5:10'
  jq(){ printf '5:10'; }
  postgres_scalar(){ printf '%s' "$stub_counts"; }

  verify_post_start_counts "$TMP/source-inspection.json"

  stub_counts='6:11'
  verify_post_start_counts "$TMP/source-inspection.json"

  stub_counts='4:11'
  if verify_post_start_counts "$TMP/source-inspection.json"; then
    printf 'post-manifest execution-count loss was accepted\n' >&2
    return 1
  fi
  stub_counts='5:9'
  if verify_post_start_counts "$TMP/source-inspection.json"; then
    printf 'post-manifest event-count loss was accepted\n' >&2
    return 1
  fi
)

test_apply_report_recovers_only_a_valid_file_missing_its_sidecar() (
  unset -f run_timestamp_apply_report validate_timestamp_apply_report \
    verify_sha256_sidecar write_sha256_sidecar_create_new \
    run_timestamp_operator
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence_dir="$TMP/report-crash" output="$TMP/report-crash/result.json"
  mkdir -p "$evidence_dir"
  chmod 0700 "$evidence_dir"
  printf '{"valid":true}\n' >"$output"
  chmod 0600 "$output"
  : >"$TMP/report-crash-events"
  validate_timestamp_apply_report(){
    grep -q '"valid":true' "$1" || return 1
    printf 'validate\n' >>"$TMP/report-crash-events"
  }
  verify_sha256_sidecar(){
    printf 'verify-sidecar\n' >>"$TMP/report-crash-events"
    return 1
  }
  write_sha256_sidecar_create_new(){
    printf 'recover-sidecar\n' >>"$TMP/report-crash-events"
  }
  run_timestamp_operator(){
    printf 'operator\n' >>"$TMP/report-crash-events"
    return 1
  }

  run_timestamp_apply_report "$evidence_dir" "$TMP/approved.json" true \
    "$output" '' external-execution-timestamps apply
  [[ "$(paste -sd' ' "$TMP/report-crash-events")" == \
    'validate recover-sidecar' ]]
)

test_apply_report_refuses_orphan_sidecar_without_running_operator() (
  unset -f run_timestamp_apply_report validate_timestamp_apply_report \
    verify_sha256_sidecar write_sha256_sidecar_create_new \
    run_timestamp_operator
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence_dir="$TMP/report-orphan" output="$TMP/report-orphan/result.json"
  mkdir -p "$evidence_dir"
  chmod 0700 "$evidence_dir"
  printf 'orphan\n' >"$output.sha256"
  chmod 0600 "$output.sha256"
  : >"$TMP/report-orphan-events"
  run_timestamp_operator(){
    printf 'operator\n' >>"$TMP/report-orphan-events"
    return 1
  }

  if run_timestamp_apply_report "$evidence_dir" "$TMP/approved.json" true \
      "$output" '' external-execution-timestamps apply; then
    printf 'orphan report sidecar was accepted\n' >&2
    return 1
  fi
  [[ ! -s "$TMP/report-orphan-events" ]]
)

write_idempotent_helper_fixture() {
  local evidence_dir="${1:-}"
  mkdir -p "$evidence_dir"
  chmod 0700 "$evidence_dir"
  cat >"$evidence_dir/approved-manifest.json" <<'JSON'
{"id":"11111111-1111-4111-8111-111111111111","rawCellCount":2,"rawCellChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","databaseIdentityChecksum":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","cells":[{"decision":"PROVEN"},{"decision":"NULL_VALUE"}]}
JSON
  cat >"$evidence_dir/source-inspection.json" <<'JSON'
{"rawCellChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","databaseIdentityChecksum":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
JSON
  chmod 0600 "$evidence_dir/approved-manifest.json" \
    "$evidence_dir/source-inspection.json"
}

write_idempotent_helper_report() {
  local output="${1:-}" idempotent="${2:-}" manifest_id="${3:-11111111-1111-4111-8111-111111111111}"
  case "$idempotent" in
    true)
      printf '%s\n' \
        "{\"manifestId\":\"$manifest_id\",\"dryRun\":false,\"idempotent\":true,\"provenCount\":1,\"attestedCount\":0,\"unresolvedCount\":0,\"nullCount\":1,\"provenanceRows\":2,\"wouldPopulateCount\":0,\"populatedShadowCount\":0,\"rawSetChecksum\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"databaseIdentityChecksum\":\"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"}" \
        >"$output"
      ;;
    false)
      printf '%s\n' \
        "{\"manifestId\":\"$manifest_id\",\"dryRun\":false,\"idempotent\":false,\"provenCount\":1,\"attestedCount\":0,\"unresolvedCount\":0,\"nullCount\":1,\"provenanceRows\":2,\"wouldPopulateCount\":1,\"populatedShadowCount\":1,\"rawSetChecksum\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"databaseIdentityChecksum\":\"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"}" \
        >"$output"
      ;;
    *) return 1 ;;
  esac
  chmod 0600 "$output"
}

test_real_idempotent_helper_publishes_only_idempotent_result() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence_dir="$TMP/idempotent-helper-new"
  local manifest="$evidence_dir/approved-manifest.json"
  local output="$evidence_dir/timestamp-apply-result.json"
  local operator_report="$evidence_dir/operator-report.json"
  write_idempotent_helper_fixture "$evidence_dir"
  : >"$TMP/idempotent-helper-new-events"
  validate_timestamp_apply_report(){
    [[ "${4:-}" == false ]] &&
      grep -q '"manifestId":"11111111-1111-4111-8111-111111111111"' "$1" &&
      grep -Eq '"idempotent":(true|false)' "$1"
  }
  jq(){ grep -q '"idempotent":true' "${!#}"; }
  run_timestamp_operator(){
    printf 'operator\n' >>"$TMP/idempotent-helper-new-events"
    command cat "$operator_report"
  }

  write_idempotent_helper_report "$operator_report" false
  if run_timestamp_idempotent_apply_report "$evidence_dir" "$manifest" \
      "$output" 1 external-execution-timestamps apply; then
    printf 'non-idempotent first proof was accepted\n' >&2
    return 1
  fi
  [[ ! -e "$output" && ! -L "$output" &&
     ! -e "$output.sha256" && ! -L "$output.sha256" ]] || {
    printf 'non-idempotent first proof was published\n' >&2
    return 1
  }

  write_idempotent_helper_report "$operator_report" true
  run_timestamp_idempotent_apply_report "$evidence_dir" "$manifest" \
    "$output" 1 external-execution-timestamps apply
  verify_sha256_sidecar "$output"
  jq -e '.idempotent == true' "$output" >/dev/null
  [[ "$(grep -c '^operator$' "$TMP/idempotent-helper-new-events")" == 2 ]]
)

test_real_idempotent_helper_reproves_without_overwriting_retained_report() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence_dir="$TMP/idempotent-helper-retained"
  local manifest="$evidence_dir/approved-manifest.json"
  local output="$evidence_dir/timestamp-apply-result.json"
  local operator_report="$evidence_dir/operator-report.json" before
  write_idempotent_helper_fixture "$evidence_dir"
  write_idempotent_helper_report "$output" true
  write_sha256_sidecar_create_new "$output"
  before="$(checksum_value "$output")"
  : >"$TMP/idempotent-helper-retained-events"
  validate_timestamp_apply_report(){
    [[ "${4:-}" == false ]] &&
      grep -q '"manifestId":"11111111-1111-4111-8111-111111111111"' "$1" &&
      grep -Eq '"idempotent":(true|false)' "$1"
  }
  jq(){ grep -q '"idempotent":true' "${!#}"; }
  run_timestamp_operator(){
    printf 'operator\n' >>"$TMP/idempotent-helper-retained-events"
    command cat "$operator_report"
  }

  write_idempotent_helper_report "$operator_report" true
  run_timestamp_idempotent_apply_report "$evidence_dir" "$manifest" \
    "$output" 1 external-execution-timestamps apply
  [[ "$(checksum_value "$output")" == "$before" ]]

  write_idempotent_helper_report "$operator_report" false
  if run_timestamp_idempotent_apply_report "$evidence_dir" "$manifest" \
      "$output" 1 external-execution-timestamps apply; then
    printf 'non-idempotent retained-report proof was accepted\n' >&2
    return 1
  fi
  [[ "$(checksum_value "$output")" == "$before" ]]

  write_idempotent_helper_report "$operator_report" true \
    22222222-2222-4222-8222-222222222222
  if run_timestamp_idempotent_apply_report "$evidence_dir" "$manifest" \
      "$output" 1 external-execution-timestamps apply; then
    printf 'mismatched-manifest retained-report proof was accepted\n' >&2
    return 1
  fi
  [[ "$(checksum_value "$output")" == "$before" ]]
  verify_sha256_sidecar "$output"
  [[ "$(grep -c '^operator$' "$TMP/idempotent-helper-retained-events")" == 3 ]]
)

test_real_idempotent_helper_preserves_report_sidecar_crash_rules() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local evidence_dir="$TMP/idempotent-helper-sidecar"
  local manifest="$evidence_dir/approved-manifest.json"
  local output="$evidence_dir/timestamp-apply-result.json"
  local orphan="$evidence_dir/orphan-result.json"
  local operator_report="$evidence_dir/operator-report.json"
  write_idempotent_helper_fixture "$evidence_dir"
  write_idempotent_helper_report "$output" true
  write_idempotent_helper_report "$operator_report" true
  : >"$TMP/idempotent-helper-sidecar-events"
  validate_timestamp_apply_report(){
    [[ "${4:-}" == false ]] &&
      grep -q '"manifestId":"11111111-1111-4111-8111-111111111111"' "$1" &&
      grep -Eq '"idempotent":(true|false)' "$1"
  }
  jq(){ grep -q '"idempotent":true' "${!#}"; }
  run_timestamp_operator(){
    printf 'operator\n' >>"$TMP/idempotent-helper-sidecar-events"
    command cat "$operator_report"
  }

  run_timestamp_idempotent_apply_report "$evidence_dir" "$manifest" \
    "$output" 1 external-execution-timestamps apply
  verify_sha256_sidecar "$output"
  [[ "$(grep -c '^operator$' "$TMP/idempotent-helper-sidecar-events")" == 1 ]]

  printf 'orphan\n' >"$orphan.sha256"
  chmod 0600 "$orphan.sha256"
  : >"$TMP/idempotent-helper-sidecar-events"
  if run_timestamp_idempotent_apply_report "$evidence_dir" "$manifest" \
      "$orphan" 1 external-execution-timestamps apply; then
    printf 'orphan retained-report sidecar was accepted\n' >&2
    return 1
  fi
  [[ ! -s "$TMP/idempotent-helper-sidecar-events" ]]
)

test_acceptance_hub_uses_durable_fence_ownership_label() (
  reset_stubs
  unset -f start_isolated_acceptance_hub
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  fence_value(){
    case "$1" in
      FENCE_ID) printf 'fence42' ;;
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) return 1 ;;
    esac
  }
  : >"$TMP/acceptance-label-log"
  docker(){
    printf '%s\n' "$*" >>"$TMP/acceptance-label-log"
    case "$*" in
      *"inspect --format {{.Config.Image}}"*) printf '%s\n' "$DISTR_IMAGE_REF" ;;
      *"inspect --format"*"NetworkSettings.Networks"*) printf 'acceptance-only\n' ;;
      *"port"*"8080/tcp"*) printf '127.0.0.1:39001\n' ;;
    esac
  }
  TIMESTAMP_ACCEPTANCE_NAME=''
  TIMESTAMP_ACCEPTANCE_URL=''
  TIMESTAMP_ACCEPTANCE_OWNER=''

  start_isolated_acceptance_hub

  grep -q -- '--label distr.sh/timestamp-acceptance=fence42' \
    "$TMP/acceptance-label-log"
  [[ "$TIMESTAMP_ACCEPTANCE_OWNER" == fence42 ]]
)

test_cleanup_removes_only_fence_owned_acceptance_hubs() (
  declare -F cleanup_fenced_acceptance_hubs >/dev/null || {
    printf 'fence-owned acceptance cleanup helper is missing\n' >&2
    return 1
  }
  reset_stubs
  unset -f cleanup_fenced_acceptance_hubs
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  fence_value(){
    case "$1" in
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) return 1 ;;
    esac
  }
  local marker="$TMP/acceptance-cleaned"
  rm -f "$marker"
  : >"$TMP/acceptance-cleanup-log"
  need_cmd(){ :; }
  docker(){
    printf '%s\n' "$*" >>"$TMP/acceptance-cleanup-log"
    case "$*" in
      "ps -aq --filter label=distr.sh/timestamp-acceptance=fence42")
        [[ -e "$marker" ]] || printf 'container-42\n'
        ;;
      "inspect --format {{.Name}} container-42")
        printf '/distr-timestamp-acceptance-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
        ;;
      "inspect --format {{.Config.Image}} container-42")
        printf '%s\n' "$DISTR_IMAGE_REF"
        ;;
      "inspect --format {{ index .Config.Labels \"distr.sh/timestamp-acceptance\" }} container-42")
        printf 'fence42\n'
        ;;
      "rm -f container-42")
        : >"$marker"
        ;;
    esac
  }

  cleanup_fenced_acceptance_hubs fence42

  grep -q '^stop --time 45 container-42$' "$TMP/acceptance-cleanup-log"
  grep -q '^rm -f container-42$' "$TMP/acceptance-cleanup-log"
)

test_cleanup_refuses_conflicting_acceptance_ownership_without_mutation() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local fixture
  need_cmd(){ :; }
  fence_value(){
    case "$1" in
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) return 1 ;;
    esac
  }

  for fixture in bad-name bad-image bad-label; do
    : >"$TMP/acceptance-conflict-log"
    docker(){
      printf '%s\n' "$*" >>"$TMP/acceptance-conflict-log"
      case "$*" in
        "ps -aq --filter label=distr.sh/timestamp-acceptance=fence42")
          printf 'container-conflict\n'
          ;;
        "inspect --format {{.Name}} container-conflict")
          if [[ "$fixture" == bad-name ]]; then
            printf '/not-a-distr-acceptance-container\n'
          else
            printf '/distr-timestamp-acceptance-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
          fi
          ;;
        "inspect --format {{.Config.Image}} container-conflict")
          if [[ "$fixture" == bad-image ]]; then
            printf 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
          else
            printf '%s\n' "$DISTR_IMAGE_REF"
          fi
          ;;
        "inspect --format {{ index .Config.Labels \"distr.sh/timestamp-acceptance\" }} container-conflict")
          if [[ "$fixture" == bad-label ]]; then
            printf 'another-fence\n'
          else
            printf 'fence42\n'
          fi
          ;;
      esac
    }

    if cleanup_fenced_acceptance_hubs fence42; then
      printf 'conflicting acceptance ownership was accepted: %s\n' \
        "$fixture" >&2
      return 1
    fi
    ! grep -Eq '^(stop|rm -f) ' "$TMP/acceptance-conflict-log" || {
      printf 'conflicting acceptance container was mutated: %s\n' \
        "$fixture" >&2
      return 1
    }
  done
)

test_cleanup_validates_all_acceptance_candidates_before_mutation() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  need_cmd(){ :; }
  fence_value(){
    case "$1" in
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) return 1 ;;
    esac
  }
  : >"$TMP/acceptance-mixed-log"
  docker(){
    printf '%s\n' "$*" >>"$TMP/acceptance-mixed-log"
    case "$*" in
      "ps -aq --filter label=distr.sh/timestamp-acceptance=fence42")
        printf 'container-owned\ncontainer-conflict\n'
        ;;
      "inspect --format {{.Name}} container-owned")
        printf '/distr-timestamp-acceptance-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
        ;;
      "inspect --format {{.Name}} container-conflict")
        printf '/distr-timestamp-acceptance-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n'
        ;;
      "inspect --format {{.Config.Image}} container-owned")
        printf '%s\n' "$DISTR_IMAGE_REF"
        ;;
      "inspect --format {{.Config.Image}} container-conflict")
        printf 'registry.invalid/distr@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
        ;;
      "inspect --format {{ index .Config.Labels \"distr.sh/timestamp-acceptance\" }} container-owned"|\
      "inspect --format {{ index .Config.Labels \"distr.sh/timestamp-acceptance\" }} container-conflict")
        printf 'fence42\n'
        ;;
    esac
  }

  if cleanup_fenced_acceptance_hubs fence42; then
    printf 'mixed owned/conflicting acceptance set was accepted\n' >&2
    return 1
  fi
  ! grep -Eq '^(stop|rm -f) ' "$TMP/acceptance-mixed-log" || {
    printf 'owned acceptance container was mutated before full validation\n' >&2
    return 1
  }
)

test_cleanup_refuses_partial_failed_enumeration_without_mutation() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  need_cmd(){ :; }
  fence_value(){
    case "$1" in
      TARGET_IMAGE_DIGEST) printf '%s' "$DISTR_IMAGE_REF" ;;
      *) return 1 ;;
    esac
  }
  : >"$TMP/acceptance-enumeration-log"
  docker(){
    printf '%s\n' "$*" >>"$TMP/acceptance-enumeration-log"
    case "$*" in
      "ps -aq --filter label=distr.sh/timestamp-acceptance=fence42")
        printf 'container-partial\n'
        return 42
        ;;
      "inspect --format {{.Name}} container-partial")
        printf '/distr-timestamp-acceptance-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
        ;;
      "inspect --format {{.Config.Image}} container-partial")
        printf '%s\n' "$DISTR_IMAGE_REF"
        ;;
      "inspect --format {{ index .Config.Labels \"distr.sh/timestamp-acceptance\" }} container-partial")
        printf 'fence42\n'
        ;;
    esac
  }

  if cleanup_fenced_acceptance_hubs fence42; then
    printf 'failed partial acceptance enumeration was accepted\n' >&2
    return 1
  fi
  ! grep -Eq '^(inspect|stop|rm -f) ' "$TMP/acceptance-enumeration-log" || {
    printf 'partial enumeration caused acceptance mutation\n' >&2
    return 1
  }
)

reset_dirty_recovery_stubs() {
  reset_stubs
  DISTR_TIMESTAMP_EVIDENCE_CHECKSUM="sha256:$(printf '%064d' 9)"
  export DISTR_TIMESTAMP_EVIDENCE_CHECKSUM
  DIRTY_RECOVERY_PLAN_STATE=ABSENT
  DIRTY_RECOVERY_RESULT_STATE=ABSENT
  prepare_timestamp_evidence_dir(){ record prepare-evidence; }
  require_timestamp_fence(){ record require-fence; }
  resume_fenced_target_image(){ record "resume-image:$1"; }
  start_dependencies(){ record deps; }
  stop_fenced_hub_if_running(){ record stop; }
  cleanup_fenced_acceptance_hubs(){ record cleanup-acceptance; }
  assert_hub_writers_stopped(){ record writers-stopped; }
  capture_evidence_complete(){ record capture-evidence; }
  verify_timestamp_evidence_bundle(){
    record bundle
    printf '%s' "$DISTR_TIMESTAMP_EVIDENCE_CHECKSUM"
  }
  evidence_fence_id(){ record evidence-fence; printf fence42; }
  reviewed_manifest_checksum(){
    record review-manifest
    printf 'sha256:%064d' 7
  }
  verify_timestamp_evidence(){ record verify-manifest; }
  stage_approved_manifest(){ record stage-manifest; }
  checksum_value(){
    case "$1" in
      */approved-manifest.json)
        record staged-checksum
        printf 'sha256:%064d' 7
        ;;
      *) return 1 ;;
    esac
  }
  current_schema_status(){ record schema-status; printf '137:true'; }
  timestamp_dirty_recovery_artifact_state(){
    case "$(basename "$1")" in
      timestamp-dirty-recovery-plan.json)
        [[ "$DIRTY_RECOVERY_PLAN_STATE" != ORPHAN ]] || {
          record orphan-plan
          return 42
        }
        printf '%s' "$DIRTY_RECOVERY_PLAN_STATE"
        ;;
      timestamp-dirty-recovery-result.json)
        [[ "$DIRTY_RECOVERY_RESULT_STATE" != ORPHAN ]] || {
          record orphan-result
          return 42
        }
        printf '%s' "$DIRTY_RECOVERY_RESULT_STATE"
        ;;
      *) return 1 ;;
    esac
  }
  timestamp_dirty_recovery_existing_checksum(){
    case "$(basename "$1")" in
      timestamp-dirty-recovery-plan.json)
        record existing-plan-checksum
        printf 'sha256:%064d' 1
        ;;
      timestamp-dirty-recovery-result.json)
        record existing-result-checksum
        printf 'sha256:%064d' 2
        ;;
      *) return 1 ;;
    esac
  }
  timestamp_dirty_recovery_raw_checksum(){
    case "$(basename "$1")" in
      timestamp-dirty-recovery-plan.json)
        record raw-plan-checksum
        printf 'sha256:%064d' 1
        ;;
      timestamp-dirty-recovery-result.json)
        record raw-result-checksum
        printf 'sha256:%064d' 2
        ;;
      *) return 1 ;;
    esac
  }
  repair_timestamp_dirty_recovery_sidecar(){
    record "repair:$(basename "$1")"
    printf '%s' "$2"
  }
  archive_interrupted_timestamp_dirty_recovery_result(){ :; }
  retain_timestamp_dirty_recovery_checksum(){
    record "retain:$(basename "$1"):$2"
    printf '%s' "$2"
  }
  validate_timestamp_dirty_recovery_plan(){
    record validate-plan
  }
  timestamp_dirty_recovery_plan_value(){
    case "$2" in
      recoveryId) printf '11111111-2222-4333-8444-555555555555' ;;
      expectedDirtyVersion) printf 137 ;;
      forceVersion) printf 137 ;;
      catalogChecksum) printf 'sha256:%064d' 8 ;;
      *) return 1 ;;
    esac
  }
  validate_timestamp_dirty_recovery_result(){
    record validate-result
  }
  run_timestamp_operator(){
    local evidence_dir="$1"
    shift
    [[ "$evidence_dir" == "$DISTR_TIMESTAMP_EVIDENCE_DIR" ]] || return 1
    record "operator:$*"
    case " $* " in
      *" migrate recover-dirty plan "*)
        printf 'sha256:%064d' 1
        ;;
      *" migrate recover-dirty apply "*)
        printf 'sha256:%064d' 2
        ;;
      *) return 1 ;;
    esac
  }
}

test_dirty_recovery_happy_order_with_manifest() {
  reset_dirty_recovery_stubs
  timestamp_expand_recover_dirty \
    "$TMP/reviewed-approved.json" \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle evidence-fence review-manifest verify-manifest stage-manifest staged-checksum schema-status operator:migrate recover-dirty plan --expected-dirty-version 137 --operator-identity release.operator@example.test --reason Recover verified interrupted migration --writer-fence-id fence42 --external-execution-timestamp-manifest /evidence/approved-manifest.json --output /evidence/timestamp-dirty-recovery-plan.json --lock-timeout 2m retain:timestamp-dirty-recovery-plan.json:sha256:0000000000000000000000000000000000000000000000000000000000000001 validate-plan operator:migrate recover-dirty apply --plan /evidence/timestamp-dirty-recovery-plan.json --plan-checksum sha256:0000000000000000000000000000000000000000000000000000000000000001 --writer-fence-id fence42 --external-execution-timestamp-manifest /evidence/approved-manifest.json --output /evidence/timestamp-dirty-recovery-result.json --lock-timeout 2m retain:timestamp-dirty-recovery-result.json:sha256:0000000000000000000000000000000000000000000000000000000000000002 validate-result writers-stopped'
  ! grep -Eq '^(start|compatibility|clear-fence)$' "$TMP/event-log"
}

test_dirty_recovery_happy_order_without_manifest() {
  reset_dirty_recovery_stubs
  timestamp_expand_recover_dirty \
    - \
    "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle evidence-fence schema-status operator:migrate recover-dirty plan --expected-dirty-version 137 --operator-identity release.operator@example.test --reason Recover verified interrupted migration --writer-fence-id fence42 --output /evidence/timestamp-dirty-recovery-plan.json --lock-timeout 2m retain:timestamp-dirty-recovery-plan.json:sha256:0000000000000000000000000000000000000000000000000000000000000001 validate-plan operator:migrate recover-dirty apply --plan /evidence/timestamp-dirty-recovery-plan.json --plan-checksum sha256:0000000000000000000000000000000000000000000000000000000000000001 --writer-fence-id fence42 --output /evidence/timestamp-dirty-recovery-result.json --lock-timeout 2m retain:timestamp-dirty-recovery-result.json:sha256:0000000000000000000000000000000000000000000000000000000000000002 validate-result writers-stopped'
  ! grep -q -- '--external-execution-timestamp-manifest' "$TMP/event-log"
  ! grep -Eq '^(start|compatibility|clear-fence)$' "$TMP/event-log"
}

test_dirty_recovery_retained_plan_clean_retry_applies_once() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=COMPLETE
  current_schema_status(){ record schema-status; printf '137:false'; }
  archive_interrupted_timestamp_dirty_recovery_result(){
    record archive-interrupted
  }
  timestamp_expand_recover_dirty \
    - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle evidence-fence existing-plan-checksum validate-plan schema-status archive-interrupted operator:migrate recover-dirty apply --plan /evidence/timestamp-dirty-recovery-plan.json --plan-checksum sha256:0000000000000000000000000000000000000000000000000000000000000001 --writer-fence-id fence42 --output /evidence/timestamp-dirty-recovery-result.json --lock-timeout 2m retain:timestamp-dirty-recovery-result.json:sha256:0000000000000000000000000000000000000000000000000000000000000002 validate-result writers-stopped'
  [[ "$(grep -c 'operator:migrate recover-dirty apply' "$TMP/event-log")" == 1 ]]
  ! grep -q 'operator:migrate recover-dirty plan' "$TMP/event-log"
}

test_dirty_recovery_reuses_valid_result_without_operator() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=COMPLETE
  DIRTY_RECOVERY_RESULT_STATE=COMPLETE
  current_schema_status(){ record schema-status; printf '137:false'; }
  timestamp_expand_recover_dirty \
    - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle evidence-fence existing-plan-checksum validate-plan schema-status existing-result-checksum validate-result writers-stopped'
  ! grep -q '^operator:' "$TMP/event-log"
}

test_dirty_recovery_reuses_result_after_archiving_exact_temp() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=COMPLETE
  DIRTY_RECOVERY_RESULT_STATE=COMPLETE
  current_schema_status(){ record schema-status; printf '137:false'; }
  archive_interrupted_timestamp_dirty_recovery_result(){
    [[ "$1" == "$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-dirty-recovery-result.json" ]]
    [[ "$2" == 11111111-2222-4333-8444-555555555555 ]]
    record archive-interrupted
  }
  timestamp_expand_recover_dirty \
    - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle evidence-fence existing-plan-checksum validate-plan schema-status archive-interrupted existing-result-checksum validate-result writers-stopped'
  ! grep -q '^operator:' "$TMP/event-log"
}

test_dirty_recovery_valid_result_refuses_unsafe_leftover_temp() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=COMPLETE
  DIRTY_RECOVERY_RESULT_STATE=COMPLETE
  current_schema_status(){ record schema-status; printf '137:false'; }
  archive_interrupted_timestamp_dirty_recovery_result(){
    record reject-interrupted
    return 42
  }
  if timestamp_expand_recover_dirty \
      - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
      release.operator@example.test \
      'Recover verified interrupted migration'; then
    printf 'unsafe leftover result reservation unexpectedly accepted\n' >&2
    return 1
  fi
  grep -qx reject-interrupted "$TMP/event-log"
  ! grep -Eq '^(operator:|existing-result-checksum|validate-result|start|compatibility|clear-fence)' \
    "$TMP/event-log"
}

test_dirty_recovery_repairs_valid_finals_missing_sidecars() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=FINAL_ONLY
  DIRTY_RECOVERY_RESULT_STATE=FINAL_ONLY
  current_schema_status(){ record schema-status; printf '137:false'; }
  timestamp_expand_recover_dirty \
    - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle evidence-fence raw-plan-checksum validate-plan repair:timestamp-dirty-recovery-plan.json schema-status raw-result-checksum validate-result repair:timestamp-dirty-recovery-result.json writers-stopped'
  ! grep -q '^operator:' "$TMP/event-log"
}

test_dirty_recovery_rejects_orphan_sidecar_before_operator() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=ORPHAN
  if timestamp_expand_recover_dirty \
      - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
      release.operator@example.test \
      'Recover verified interrupted migration'; then
    printf 'orphan recovery plan sidecar unexpectedly accepted\n' >&2
    return 1
  fi
  ! grep -q '^operator:' "$TMP/event-log"
  ! grep -Eq '^(start|compatibility|clear-fence)$' "$TMP/event-log"
}

test_dirty_recovery_archives_exact_temp_before_single_retry() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=COMPLETE
  current_schema_status(){ record schema-status; printf '137:true'; }
  archive_interrupted_timestamp_dirty_recovery_result(){
    [[ "$1" == "$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-dirty-recovery-result.json" ]]
    [[ "$2" == 11111111-2222-4333-8444-555555555555 ]]
    record archive-interrupted
  }
  timestamp_expand_recover_dirty \
    - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  [[ "$(grep -c '^archive-interrupted$' "$TMP/event-log")" == 1 ]]
  [[ "$(grep -c 'operator:migrate recover-dirty apply' "$TMP/event-log")" == 1 ]]
  awk '
    /^archive-interrupted$/ { archived=NR }
    /operator:migrate recover-dirty apply/ { applied=NR }
    END { exit !(archived > 0 && applied == archived + 1) }
  ' "$TMP/event-log"
}

test_dirty_recovery_apply_failure_leaves_temp_and_no_result_sidecar() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=COMPLETE
  current_schema_status(){ record schema-status; printf '137:true'; }
  local result="$DISTR_TIMESTAMP_EVIDENCE_DIR/timestamp-dirty-recovery-result.json"
  local reservation="$DISTR_TIMESTAMP_EVIDENCE_DIR/.timestamp-dirty-recovery-result.json.11111111-2222-4333-8444-555555555555.tmp"
  rm -f -- "$result" "$result.sha256" "$reservation"
  run_timestamp_operator(){
    local evidence_dir="$1"
    shift
    record "operator:$*"
    case " $* " in
      *" migrate recover-dirty apply "*)
        : >"$reservation"
        chmod 0600 "$reservation"
        return 42
        ;;
      *) return 1 ;;
    esac
  }
  if timestamp_expand_recover_dirty \
      - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
      release.operator@example.test \
      'Recover verified interrupted migration'; then
    printf 'failed dirty recovery Apply unexpectedly succeeded\n' >&2
    return 1
  fi
  [[ -f "$reservation" && ! -L "$reservation" ]]
  [[ ! -e "$result" && ! -L "$result" ]]
  [[ ! -e "$result.sha256" && ! -L "$result.sha256" ]]
  [[ "$(grep -c 'operator:migrate recover-dirty apply' "$TMP/event-log")" == 1 ]]
  ! grep -Eq '^(validate-result|writers-stopped|start|compatibility|clear-fence)$' \
    <(tail -n +10 "$TMP/event-log")
}

test_dirty_recovery_bundle_mismatch_stops_before_operator() {
  reset_dirty_recovery_stubs
  verify_timestamp_evidence_bundle(){
    record bundle
    printf 'sha256:%064d' 6
  }
  if timestamp_expand_recover_dirty \
      - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
      release.operator@example.test \
      'Recover verified interrupted migration'; then
    printf 'mismatched recovery evidence bundle unexpectedly accepted\n' >&2
    return 1
  fi
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle'
  ! grep -q '^operator:' "$TMP/event-log"
}

test_dirty_recovery_fence_id_mismatch_stops_before_operator() {
  reset_dirty_recovery_stubs
  evidence_fence_id(){ record evidence-fence; printf different-fence; }
  if timestamp_expand_recover_dirty \
      - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
      release.operator@example.test \
      'Recover verified interrupted migration'; then
    printf 'mismatched captured recovery fence unexpectedly accepted\n' >&2
    return 1
  fi
  assert_events \
    'lock prepare-evidence require-fence resume-image:registry.invalid/distr@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb deps stop cleanup-acceptance writers-stopped capture-evidence bundle evidence-fence'
  ! grep -q '^operator:' "$TMP/event-log"
}

test_dirty_recovery_dirty_138_without_manifest_uses_exact_version() {
  reset_dirty_recovery_stubs
  current_schema_status(){ record schema-status; printf '138:true'; }
  timestamp_expand_recover_dirty \
    - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
    release.operator@example.test \
    'Recover verified interrupted migration'
  grep -qx \
    'operator:migrate recover-dirty plan --expected-dirty-version 138 --operator-identity release.operator@example.test --reason Recover verified interrupted migration --writer-fence-id fence42 --output /evidence/timestamp-dirty-recovery-plan.json --lock-timeout 2m' \
    "$TMP/event-log"
  ! grep -q -- '--external-execution-timestamp-manifest' "$TMP/event-log"
}

test_dirty_recovery_invalid_lock_timeout_stops_before_lock() {
  reset_dirty_recovery_stubs
  DISTR_TIMESTAMP_DIRTY_RECOVERY_LOCK_TIMEOUT=0s
  export DISTR_TIMESTAMP_DIRTY_RECOVERY_LOCK_TIMEOUT
  if timestamp_expand_recover_dirty \
      - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
      release.operator@example.test \
      'Recover verified interrupted migration'; then
    printf 'invalid recovery lock timeout unexpectedly accepted\n' >&2
    return 1
  fi
  [[ ! -s "$TMP/event-log" ]]
  unset DISTR_TIMESTAMP_DIRTY_RECOVERY_LOCK_TIMEOUT
}

test_dirty_recovery_rejects_result_binding_drift_without_operator() {
  reset_dirty_recovery_stubs
  DIRTY_RECOVERY_PLAN_STATE=COMPLETE
  DIRTY_RECOVERY_RESULT_STATE=COMPLETE
  current_schema_status(){ record schema-status; printf '137:false'; }
  validate_timestamp_dirty_recovery_result(){
    record validate-result
    return 42
  }
  if timestamp_expand_recover_dirty \
      - "$DISTR_TIMESTAMP_EVIDENCE_DIR" \
      release.operator@example.test \
      'Recover verified interrupted migration'; then
    printf 'drifted retained recovery result unexpectedly accepted\n' >&2
    return 1
  fi
  ! grep -q '^operator:' "$TMP/event-log"
  grep -qx validate-result "$TMP/event-log"
}

test_dirty_recovery_real_artifact_state_and_sidecar_repair() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  sync(){ :; }
  local directory="$TMP/dirty-recovery-real-artifacts"
  local artifact="$directory/timestamp-dirty-recovery-plan.json"
  local checksum
  rm -rf -- "$directory"
  mkdir "$directory"
  chmod 0700 "$directory"
  [[ "$(timestamp_dirty_recovery_artifact_state "$artifact")" == ABSENT ]]

  printf 'orphan\n' >"$artifact.sha256"
  chmod 0600 "$artifact.sha256"
  if timestamp_dirty_recovery_artifact_state "$artifact" >/dev/null; then
    printf 'orphan recovery sidecar unexpectedly accepted\n' >&2
    return 1
  fi
  rm "$artifact.sha256"

  printf '{"recordType":"PLAN"}\n' >"$artifact"
  chmod 0600 "$artifact"
  [[ "$(timestamp_dirty_recovery_artifact_state "$artifact")" == FINAL_ONLY ]]
  checksum="$(timestamp_dirty_recovery_raw_checksum "$artifact")"
  [[ "$checksum" =~ ^sha256:[0-9a-f]{64}$ ]]
  [[ "$(repair_timestamp_dirty_recovery_sidecar \
    "$artifact" "$checksum")" == "$checksum" ]]
  [[ "$(timestamp_dirty_recovery_artifact_state "$artifact")" == COMPLETE ]]
  [[ "$(timestamp_dirty_recovery_existing_checksum "$artifact")" == "$checksum" ]]

  printf 'tampered\n' >>"$artifact"
  if timestamp_dirty_recovery_existing_checksum "$artifact" >/dev/null; then
    printf 'tampered recovery artifact unexpectedly accepted\n' >&2
    return 1
  fi
)

test_dirty_recovery_real_no_manifest_plan_and_result_validation() (
  command -v jq >/dev/null 2>&1 || return 0
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local directory="$TMP/dirty-recovery-real-json"
  local plan="$directory/timestamp-dirty-recovery-plan.json"
  local result="$directory/timestamp-dirty-recovery-result.json"
  local plan_checksum
  rm -rf -- "$directory"
  mkdir "$directory"
  chmod 0700 "$directory"
  cat >"$plan" <<'JSON'
{
  "formatVersion": "distr.timestamp-dirty-recovery/v1",
  "recordType": "PLAN",
  "recoveryId": "11111111-2222-4333-8444-555555555555",
  "createdAt": "2026-07-17T01:02:03.000004Z",
  "operatorIdentity": "release.operator@example.test",
  "reason": "Recover verified interrupted migration",
  "writerFenceIdentifier": "fence42",
  "expectedDirtyVersion": 137,
  "catalogShape": "PREDECESSOR_137",
  "forceVersion": 137,
  "catalogChecksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
JSON
  chmod 0600 "$plan"
  plan_checksum="$(timestamp_dirty_recovery_raw_checksum "$plan")"
  validate_timestamp_dirty_recovery_plan \
    "$plan" 137 release.operator@example.test \
    'Recover verified interrupted migration' fence42 -
  cat >"$result" <<JSON
{
  "formatVersion": "distr.timestamp-dirty-recovery/v1",
  "recordType": "RESULT",
  "recoveryId": "11111111-2222-4333-8444-555555555555",
  "planChecksum": "$plan_checksum",
  "completedAt": "2026-07-17T01:03:04.000005Z",
  "plannedStatus": {
    "version": 137,
    "dirty": true
  },
  "observedPreApplyStatus": {
    "version": 137,
    "dirty": false
  },
  "action": "OBSERVED_ALREADY_CLEAN",
  "forcedVersion": 137,
  "catalogChecksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "result": "SUCCEEDED",
  "postStatus": {
    "version": 137,
    "dirty": false
  }
}
JSON
  chmod 0600 "$result"
  current_schema_status(){ printf '137:false'; }
  validate_timestamp_dirty_recovery_result "$result" "$plan" "$plan_checksum"
  sed -i 's/"result": "SUCCEEDED"/"result": "FAILED"/' "$result"
  if validate_timestamp_dirty_recovery_result \
      "$result" "$plan" "$plan_checksum"; then
    printf 'tampered recovery result binding unexpectedly accepted\n' >&2
    return 1
  fi
)

test_dirty_recovery_real_manifest_bound_plan_and_result_validation() (
  command -v jq >/dev/null 2>&1 || return 0
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  sync(){ :; }
  local directory="$TMP/dirty-recovery-real-manifest-json"
  local manifest="$directory/approved-manifest.json"
  local plan="$directory/timestamp-dirty-recovery-plan.json"
  local result="$directory/timestamp-dirty-recovery-result.json"
  local manifest_checksum plan_checksum
  rm -rf -- "$directory"
  mkdir "$directory"
  chmod 0700 "$directory"
  cat >"$manifest" <<'JSON'
{
  "id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
  "decisionContentChecksum": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  "rawCellChecksum": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
  "databaseIdentityChecksum": "sha256:3333333333333333333333333333333333333333333333333333333333333333",
  "executionCount": 1,
  "eventCount": 1,
  "rawCellCount": 6
}
JSON
  chmod 0600 "$manifest"
  write_sha256_sidecar_create_new "$manifest"
  manifest_checksum="$(checksum_value "$manifest")"
  cat >"$plan" <<JSON
{
  "formatVersion": "distr.timestamp-dirty-recovery/v1",
  "recordType": "PLAN",
  "recoveryId": "11111111-2222-4333-8444-555555555555",
  "createdAt": "2026-07-17T01:02:03.000004Z",
  "operatorIdentity": "release.operator@example.test",
  "reason": "Recover verified interrupted migration",
  "writerFenceIdentifier": "fence42",
  "expectedDirtyVersion": 138,
  "catalogShape": "EXPAND_138",
  "forceVersion": 138,
  "catalogChecksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "manifest": {
    "id": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
    "documentChecksum": "$manifest_checksum",
    "decisionContentChecksum": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
    "rawSetChecksum": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
    "databaseIdentityChecksum": "sha256:3333333333333333333333333333333333333333333333333333333333333333",
    "executionCount": 1,
    "eventCount": 1,
    "rawCellCount": 6
  }
}
JSON
  chmod 0600 "$plan"
  validate_timestamp_dirty_recovery_plan \
    "$plan" 138 release.operator@example.test \
    'Recover verified interrupted migration' fence42 reviewed-manifest.json
  plan_checksum="$(timestamp_dirty_recovery_raw_checksum "$plan")"
  cat >"$result" <<JSON
{
  "formatVersion": "distr.timestamp-dirty-recovery/v1",
  "recordType": "RESULT",
  "recoveryId": "11111111-2222-4333-8444-555555555555",
  "planChecksum": "$plan_checksum",
  "completedAt": "2026-07-17T01:03:04.000005Z",
  "plannedStatus": {
    "version": 138,
    "dirty": true
  },
  "observedPreApplyStatus": {
    "version": 138,
    "dirty": true
  },
  "action": "FORCED",
  "forcedVersion": 138,
  "catalogChecksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "result": "SUCCEEDED",
  "postStatus": {
    "version": 138,
    "dirty": false
  }
}
JSON
  chmod 0600 "$result"
  current_schema_status(){ printf '138:false'; }
  validate_timestamp_dirty_recovery_result "$result" "$plan" "$plan_checksum"
  sed -i \
    's/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/ffffffff-bbbb-4ccc-8ddd-eeeeeeeeeeee/' \
    "$plan"
  if validate_timestamp_dirty_recovery_plan \
      "$plan" 138 release.operator@example.test \
      'Recover verified interrupted migration' fence42 reviewed-manifest.json; then
    printf 'drifted manifest-bound recovery plan unexpectedly accepted\n' >&2
    return 1
  fi
)

test_dirty_recovery_real_interrupted_temp_archives_durably() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local directory="$TMP/dirty-recovery-real-archive"
  local result="$directory/timestamp-dirty-recovery-result.json"
  local recovery_id=11111111-2222-4333-8444-555555555555
  local reservation="$directory/.timestamp-dirty-recovery-result.json.$recovery_id.tmp"
  local first="$directory/timestamp-dirty-recovery-result.interrupted-001.partial"
  local second="$directory/timestamp-dirty-recovery-result.interrupted-002.partial"
  local operations="$directory/operations.log" remove_line final_sync_line
  rm -rf -- "$directory"
  mkdir "$directory"
  chmod 0700 "$directory"
  : >"$operations"
  sync(){
    printf 'sync:%s\n' "$*" >>"$operations"
  }
  rm(){
    printf 'rm:%s\n' "$*" >>"$operations"
    command rm "$@"
  }
  printf 'prior partial\n' >"$first"
  chmod 0600 "$first"
  write_sha256_sidecar_create_new "$first"
  printf 'interrupted apply bytes\n' >"$reservation"
  chmod 0600 "$reservation"

  archive_interrupted_timestamp_dirty_recovery_result \
    "$result" "$recovery_id"

  [[ ! -e "$reservation" && ! -L "$reservation" ]]
  cmp -s -- "$second" <(printf 'interrupted apply bytes\n')
  verify_sha256_sidecar "$second"
  remove_line="$(grep -nF "rm:-- $reservation" "$operations" |
    tail -n1 | cut -d: -f1)"
  final_sync_line="$(grep -nF "sync:-f $directory" "$operations" |
    tail -n1 | cut -d: -f1)"
  [[ "$remove_line" =~ ^[0-9]+$ && "$final_sync_line" =~ ^[0-9]+$ ]]
  ((final_sync_line > remove_line))
  awk -v removeLine="$remove_line" -v archive="$second" '
    NR < removeLine && $0 == "sync:-f " archive { archiveSync=1 }
    NR < removeLine && $0 == "sync:-f " archive ".sha256" { sidecarSync=1 }
    END { exit !(archiveSync && sidecarSync) }
  ' "$operations"
)

test_dirty_recovery_real_post_link_temp_matches_and_archives_final() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local directory="$TMP/dirty-recovery-real-post-link"
  local result="$directory/timestamp-dirty-recovery-result.json"
  local recovery_id=11111111-2222-4333-8444-555555555555
  local reservation="$directory/.timestamp-dirty-recovery-result.json.$recovery_id.tmp"
  local archive="$directory/timestamp-dirty-recovery-result.interrupted-001.partial"
  rm -rf -- "$directory"
  mkdir "$directory"
  chmod 0700 "$directory"
  sync(){ :; }
  printf 'published result bytes\n' >"$result"
  chmod 0600 "$result"
  ln "$result" "$reservation"
  archive_interrupted_timestamp_dirty_recovery_result \
    "$result" "$recovery_id"
  [[ -f "$result" && ! -L "$result" ]]
  [[ ! -e "$reservation" && ! -L "$reservation" ]]
  cmp -s -- "$result" "$archive"
  verify_sha256_sidecar "$archive"

  printf 'different interrupted bytes\n' >"$reservation"
  chmod 0600 "$reservation"
  if archive_interrupted_timestamp_dirty_recovery_result \
      "$result" "$recovery_id"; then
    printf 'mismatched post-link recovery reservation unexpectedly archived\n' >&2
    return 1
  fi
  [[ -f "$reservation" ]]
  [[ ! -e "$directory/timestamp-dirty-recovery-result.interrupted-002.partial" ]]
)

test_dirty_recovery_real_interrupted_temp_rejects_unsafe_matrix() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  eval "$(declare -f path_mode_is |
    sed '1s/path_mode_is/real_dirty_recovery_path_mode_is/')"
  local root="$TMP/dirty-recovery-unsafe-temp"
  local recovery_id=11111111-2222-4333-8444-555555555555
  local kind directory result reservation target
  rm -rf -- "$root"
  mkdir "$root"
  chmod 0700 "$root"
  for kind in directory mode owner symlink; do
    (
      directory="$root/$kind"
      mkdir "$directory"
      chmod 0700 "$directory"
      result="$directory/timestamp-dirty-recovery-result.json"
      reservation="$directory/.timestamp-dirty-recovery-result.json.$recovery_id.tmp"
      case "$kind" in
        directory)
          mkdir "$reservation"
          ;;
        mode)
          printf 'partial\n' >"$reservation"
          chmod 0600 "$reservation"
          path_mode_is(){
            [[ "$1" != "$reservation" ]] &&
              real_dirty_recovery_path_mode_is "$@"
          }
          ;;
        owner)
          printf 'partial\n' >"$reservation"
          chmod 0600 "$reservation"
          stat(){
            if [[ "$1" == -c && "$2" == %u &&
                  "$3" == -- && "$4" == "$reservation" ]]; then
              printf '999999\n'
            else
              command stat "$@"
            fi
          }
          ;;
        symlink)
          target="$directory/target"
          printf 'partial\n' >"$target"
          chmod 0600 "$target"
          if ! ln -s "$target" "$reservation" 2>/dev/null; then
            exit 0
          fi
          [[ -L "$reservation" ]] || exit 0
          ;;
      esac
      if archive_interrupted_timestamp_dirty_recovery_result \
          "$result" "$recovery_id"; then
        printf 'unsafe interrupted temp unexpectedly accepted: %s\n' \
          "$kind" >&2
        exit 1
      fi
      [[ -e "$reservation" || -L "$reservation" ]]
      ! compgen -G \
        "$directory/timestamp-dirty-recovery-result.interrupted-*.partial*" \
        >/dev/null
    )
  done
)

test_dirty_recovery_archive_refuses_numbering_collision_and_exhaustion() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local directory="$TMP/dirty-recovery-archive-numbering"
  local result="$directory/timestamp-dirty-recovery-result.json"
  local recovery_id=11111111-2222-4333-8444-555555555555
  local reservation="$directory/.timestamp-dirty-recovery-result.json.$recovery_id.tmp"
  rm -rf -- "$directory"
  mkdir "$directory"
  chmod 0700 "$directory"
  printf 'partial\n' >"$reservation"
  chmod 0600 "$reservation"
  timestamp_dirty_recovery_partial_members(){
    printf '%s\0' \
      "$directory/timestamp-dirty-recovery-result.interrupted-002.partial" \
      "$directory/timestamp-dirty-recovery-result.interrupted-002.partial.sha256"
  }
  timestamp_dirty_recovery_existing_checksum(){ printf 'sha256:%064d' 1; }
  if archive_interrupted_timestamp_dirty_recovery_result \
      "$result" "$recovery_id"; then
    printf 'non-contiguous interrupted archive numbering accepted\n' >&2
    return 1
  fi
  [[ -f "$reservation" ]]

  timestamp_dirty_recovery_partial_members(){
    local index suffix
    for ((index=1; index<=999; index++)); do
      printf -v suffix '%03d' "$index"
      printf '%s\0' \
        "$directory/timestamp-dirty-recovery-result.interrupted-$suffix.partial" \
        "$directory/timestamp-dirty-recovery-result.interrupted-$suffix.partial.sha256"
    done
  }
  if archive_interrupted_timestamp_dirty_recovery_result \
      "$result" "$recovery_id"; then
    printf 'exhausted interrupted archive numbering accepted\n' >&2
    return 1
  fi
  [[ -f "$reservation" ]]
)

test_dirty_recovery_function_never_starts_or_clears_fence() (
  source "$ROOT/deploy/server-docker-compose/deploy.sh"
  local body
  body="$(declare -f timestamp_expand_recover_dirty)"
  ! grep -Eq \
    '\b(start_hub|start_verify_and_finalize_timestamp_expand|persist_timestamp_compatibility|clear_timestamp_fence)\b' \
    <<<"$body"
)

if [[ "${DISTR_TIMESTAMP_TEST_GROUP:-}" == dirty-recovery ]]; then
  test_dirty_recovery_happy_order_with_manifest
  test_dirty_recovery_happy_order_without_manifest
  test_dirty_recovery_retained_plan_clean_retry_applies_once
  test_dirty_recovery_reuses_valid_result_without_operator
  test_dirty_recovery_reuses_result_after_archiving_exact_temp
  test_dirty_recovery_valid_result_refuses_unsafe_leftover_temp
  test_dirty_recovery_repairs_valid_finals_missing_sidecars
  test_dirty_recovery_rejects_orphan_sidecar_before_operator
  test_dirty_recovery_archives_exact_temp_before_single_retry
  test_dirty_recovery_apply_failure_leaves_temp_and_no_result_sidecar
  test_dirty_recovery_bundle_mismatch_stops_before_operator
  test_dirty_recovery_fence_id_mismatch_stops_before_operator
  test_dirty_recovery_dirty_138_without_manifest_uses_exact_version
  test_dirty_recovery_invalid_lock_timeout_stops_before_lock
  test_dirty_recovery_rejects_result_binding_drift_without_operator
  test_dirty_recovery_real_artifact_state_and_sidecar_repair
  test_dirty_recovery_real_no_manifest_plan_and_result_validation
  test_dirty_recovery_real_manifest_bound_plan_and_result_validation
  test_dirty_recovery_real_interrupted_temp_archives_durably
  test_dirty_recovery_real_post_link_temp_matches_and_archives_final
  test_dirty_recovery_real_interrupted_temp_rejects_unsafe_matrix
  test_dirty_recovery_archive_refuses_numbering_collision_and_exhaustion
  test_dirty_recovery_function_never_starts_or_clears_fence
  test_dispatch_rejects_wrong_arity_without_exiting_caller
  printf 'timestamp dirty recovery compose orchestration tests passed\n'
  exit 0
fi

if [[ "${DISTR_TIMESTAMP_TEST_GROUP:-}" == resume ]]; then
  case "${DISTR_TIMESTAMP_TEST_CASE:-all}" in
    post-migrate) test_apply_resumes_after_migration_without_rerunning_migration ;;
    verified) test_apply_resumes_exact_verified_manifest_idempotently ;;
    missing-dry) test_verified_resume_refuses_missing_retained_dry_report ;;
    phase) test_apply_phase_rejects_conflicting_manifest_state ;;
    marker-mismatch) test_apply_refuses_transition_marker_mismatch_before_mutation ;;
    restarting-hub) test_stop_fenced_hub_stops_restarting_service_container ;;
    event-growth) test_verified_resume_allows_count_growth_but_not_count_loss ;;
    report-crash) test_apply_report_recovers_only_a_valid_file_missing_its_sidecar ;;
    orphan-sidecar) test_apply_report_refuses_orphan_sidecar_without_running_operator ;;
    idempotent-new) test_real_idempotent_helper_publishes_only_idempotent_result ;;
    idempotent-retained) test_real_idempotent_helper_reproves_without_overwriting_retained_report ;;
    idempotent-sidecar) test_real_idempotent_helper_preserves_report_sidecar_crash_rules ;;
    acceptance-label) test_acceptance_hub_uses_durable_fence_ownership_label ;;
    acceptance-cleanup) test_cleanup_removes_only_fence_owned_acceptance_hubs ;;
    acceptance-conflicts) test_cleanup_refuses_conflicting_acceptance_ownership_without_mutation ;;
    acceptance-mixed) test_cleanup_validates_all_acceptance_candidates_before_mutation ;;
    acceptance-enumeration) test_cleanup_refuses_partial_failed_enumeration_without_mutation ;;
    legacy-timeout) test_timed_out_operator_is_stopped_and_removed ;;
    legacy-nested) test_nested_command_failure_matrix ;;
    legacy-filesystem) test_filesystem_failure_matrix_stops_before_publication ;;
    legacy-then-resume)
      test_nested_command_failure_matrix
      test_filesystem_failure_matrix_stops_before_publication
      test_apply_resumes_after_migration_without_rerunning_migration
      test_apply_resumes_exact_verified_manifest_idempotently
      test_verified_resume_refuses_missing_retained_dry_report
      test_apply_phase_rejects_conflicting_manifest_state
      test_apply_refuses_transition_marker_mismatch_before_mutation
      test_stop_fenced_hub_stops_restarting_service_container
      test_verified_resume_allows_count_growth_but_not_count_loss
      test_apply_report_recovers_only_a_valid_file_missing_its_sidecar
      test_apply_report_refuses_orphan_sidecar_without_running_operator
      test_real_idempotent_helper_publishes_only_idempotent_result
      test_real_idempotent_helper_reproves_without_overwriting_retained_report
      test_real_idempotent_helper_preserves_report_sidecar_crash_rules
      test_acceptance_hub_uses_durable_fence_ownership_label
      test_cleanup_removes_only_fence_owned_acceptance_hubs
      test_cleanup_refuses_conflicting_acceptance_ownership_without_mutation
      test_cleanup_validates_all_acceptance_candidates_before_mutation
      test_cleanup_refuses_partial_failed_enumeration_without_mutation
      ;;
    all)
      test_apply_resumes_after_migration_without_rerunning_migration
      test_apply_resumes_exact_verified_manifest_idempotently
      test_verified_resume_refuses_missing_retained_dry_report
      test_apply_phase_rejects_conflicting_manifest_state
      test_apply_refuses_transition_marker_mismatch_before_mutation
      test_stop_fenced_hub_stops_restarting_service_container
      test_verified_resume_allows_count_growth_but_not_count_loss
      test_apply_report_recovers_only_a_valid_file_missing_its_sidecar
      test_apply_report_refuses_orphan_sidecar_without_running_operator
      test_real_idempotent_helper_publishes_only_idempotent_result
      test_real_idempotent_helper_reproves_without_overwriting_retained_report
      test_real_idempotent_helper_preserves_report_sidecar_crash_rules
      test_acceptance_hub_uses_durable_fence_ownership_label
      test_cleanup_removes_only_fence_owned_acceptance_hubs
      test_cleanup_refuses_conflicting_acceptance_ownership_without_mutation
      test_cleanup_validates_all_acceptance_candidates_before_mutation
      test_cleanup_refuses_partial_failed_enumeration_without_mutation
      ;;
    *) printf 'unknown resume test case\n' >&2; exit 2 ;;
  esac
  printf 'timestamp expand resume tests passed\n'
  exit 0
fi

test_shared_default_evidence_dir_is_restricted
test_capture_order
test_apply_order
test_ordinary_release_order
test_backup_command_fences_and_restores_hub_order
test_public_migrate_fences_and_backs_up_before_migration
test_backup_prepares_parent_and_refuses_image_drift_before_outage
test_failure_keeps_fence
test_post_start_failure_stops_hub_and_keeps_fence
test_public_post_start_failure_stops_hub_and_keeps_fence
test_ordinary_restore_failure_prevents_migration
test_nonempty_137_preflight_keeps_old_hub_running
test_approved_manifest_is_create_new_0600
test_reviewed_manifest_checksum_requires_valid_review_sidecar
test_apply_rejects_invalid_review_sidecar_before_parsing_or_staging
test_active_fence_refuses_every_mutating_command
test_cancel_clean_137_order
test_cancel_refuses_schema_138
test_fence_file_is_atomic_restricted_and_directory_bound
test_operator_uses_deployment_identity_and_env_override
test_restore_failure_runs_cleanup_trap
test_restore_postgres_readiness_requires_final_server
test_pre_expand_rollback_refused_after_fence_clear
test_real_compatibility_record_is_restricted_idempotent_and_positive
test_schema_139_rollback_fails_closed_before_mutation
test_dirty_schema_rollback_fails_closed_before_mutation
test_rollback_calls_compatibility_gate_before_mutation
test_compose_uses_one_absolute_env_file_for_every_distr_service
test_env_and_evidence_paths_fail_closed
test_dispatch_rechecks_fence_after_lock_for_every_mutator
test_dispatch_rejects_wrong_arity_without_exiting_caller
test_lock_refuses_symlink_before_opening
test_capture_resumes_preparing_fence
test_capture_resumes_captured_fence_without_restarting_writers
test_capture_resume_requires_coherent_fenced_image_identity
test_dangling_fence_is_active_and_clear_is_durable
test_capture_writes_canonical_evidence_bundle_after_compare
test_evidence_bundle_checksum_binds_every_member
test_apply_revalidates_cross_evidence_and_bundle_before_staging
test_invalid_dry_run_report_prevents_migration
test_real_apply_report_semantic_gate_matrix
test_post_start_audit_helper_checks_readiness_and_authenticated_history
test_audit_history_probe_is_bound_authenticated_and_rejects_empty_history
test_audit_probe_uses_canonical_table_and_requires_bound_history
test_timed_out_operator_is_stopped_and_removed
test_restore_uses_pg18_layout_labels_and_complete_object_digest
test_guard_review_safety_contracts_are_present
test_preflight_creates_secure_backup_parent_on_clean_host
test_invalid_complete_capture_is_not_deleted_or_rebuilt
test_failed_database_backup_leaves_no_partial_publication
test_failed_cancel_restores_target_image_configuration
test_failed_cancel_image_switch_still_restores_fenced_identity
test_release_metadata_derives_commit_and_digest_together
test_pull_image_rejects_mixed_release_identity_before_fence
test_capture_pull_identity_failure_precedes_fence_and_outage
test_nested_command_failure_matrix
test_filesystem_failure_matrix_stops_before_publication
test_apply_resumes_after_migration_without_rerunning_migration
test_apply_resumes_exact_verified_manifest_idempotently
test_verified_resume_refuses_missing_retained_dry_report
test_apply_phase_rejects_conflicting_manifest_state
test_apply_refuses_transition_marker_mismatch_before_mutation
test_stop_fenced_hub_stops_restarting_service_container
test_verified_resume_allows_count_growth_but_not_count_loss
test_apply_report_recovers_only_a_valid_file_missing_its_sidecar
test_apply_report_refuses_orphan_sidecar_without_running_operator
test_real_idempotent_helper_publishes_only_idempotent_result
test_real_idempotent_helper_reproves_without_overwriting_retained_report
test_real_idempotent_helper_preserves_report_sidecar_crash_rules
test_acceptance_hub_uses_durable_fence_ownership_label
test_cleanup_removes_only_fence_owned_acceptance_hubs
test_cleanup_refuses_conflicting_acceptance_ownership_without_mutation
test_cleanup_validates_all_acceptance_candidates_before_mutation
test_cleanup_refuses_partial_failed_enumeration_without_mutation
printf 'timestamp expand compose orchestration tests passed\n'
