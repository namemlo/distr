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

artifact_sha256() {
  local file="$1" checksum
  checksum="$(sha256sum "$file" | awk '{print $1}')" || return
  [[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || return 1
  printf 'sha256:%s' "$checksum"
}

write_checksum() {
  local file="$1" sidecar
  sidecar="${file}.sha256"
  (
    cd "$(dirname "$file")" || return
    sha256sum "$(basename "$file")" >"$(basename "$sidecar")" || return
    sha256sum -c --status "$(basename "$sidecar")" || return
  ) || return
}

require_real_sbom() {
  local sbom="$1"
  [[ -f "$sbom" && ! -L "$sbom" && -f "${sbom}.sha256" && ! -L "${sbom}.sha256" ]] || {
    die "checksummed image SBOM is missing"
    return 1
  }
  (
    cd "$(dirname "$sbom")" || return
    sha256sum -c --status "$(basename "${sbom}.sha256")" || return
  ) || {
    die "image SBOM checksum is invalid"
    return 1
  }
  node - "$sbom" <<'NODE' || {
const fs = require('node:fs');
const document = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
if (
  document.spdxVersion !== 'SPDX-2.3' ||
  document.SPDXID !== 'SPDXRef-DOCUMENT' ||
  !Array.isArray(document.documentDescribes) ||
  document.documentDescribes.length === 0 ||
  !Array.isArray(document.packages) ||
  document.packages.length === 0
) {
  throw new Error('SBOM must describe at least one image package');
}
NODE
    die "SBOM must describe at least one image package"
    return 1
  }
}

write_provenance_attestation() {
  local digest="$1" source="$2" sbom="$3" provenance temporary sbom_ref sbom_sha
  provenance="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.intoto.json"
  temporary="${STATE_DIR}/provenance.intoto.json"
  sbom_ref="dist/$(basename "$sbom")"
  sbom_sha="$(artifact_sha256 "$sbom")" || return
  [[ ! -e "$provenance" && ! -e "${provenance}.sha256" ]] || {
    die "provenance evidence already exists in the workspace"
    return 1
  }
  node - "$temporary" "$RELEASE_COMMIT" "$source" "$DISTR_IMAGE" "$digest" "$sbom_ref" "$sbom_sha" <<'NODE' || return
const fs = require('node:fs');
const [file, commit, source, image, digest, sbomRef, sbomSha] = process.argv.slice(2);
const statement = {
  _type: 'https://in-toto.io/Statement/v1',
  subject: [{name: image, digest: {sha256: digest.slice('sha256:'.length)}}],
  predicateType: 'https://slsa.dev/provenance/v1',
  predicate: {
    buildDefinition: {
      buildType: 'https://distr.sh/build-types/community-hub-image/v1',
      externalParameters: {platform: 'linux/amd64'},
      internalParameters: {},
      resolvedDependencies: [
        {uri: source, digest: {sha1: commit}},
        {uri: sbomRef, digest: {sha256: sbomSha.slice('sha256:'.length)}},
      ],
    },
    runDetails: {
      builder: {id: 'https://distr.sh/builders/hub-image-publication/v1'},
      metadata: {invocationId: `candidate-${commit}`},
    },
  },
};
fs.writeFileSync(file, `${JSON.stringify(statement, null, 2)}\n`, {mode: 0o600});
NODE
  install -m 0644 "$temporary" "$provenance" || return
  write_checksum "$provenance" || return
  printf '%s' "$provenance"
}

sign_provenance_attestation() {
  local provenance="$1" signature temporary
  signature="${provenance}.sig"
  temporary="${STATE_DIR}/provenance.intoto.json.sig"
  need_cmd cosign || return
  [[ -f "${PROVENANCE_SIGNING_KEY:-}" && ! -L "${PROVENANCE_SIGNING_KEY:-}" ]] || {
    die "PROVENANCE_SIGNING_KEY must be a regular credential file"
    return 1
  }
  [[ -f "${PROVENANCE_SIGNING_PUBLIC_KEY:-}" && ! -L "${PROVENANCE_SIGNING_PUBLIC_KEY:-}" ]] || {
    die "PROVENANCE_SIGNING_PUBLIC_KEY must be a regular credential file"
    return 1
  }
  [[ -n "${COSIGN_PASSWORD:-}" ]] || {
    die "COSIGN_PASSWORD must come from a Jenkins string credential"
    return 1
  }
  [[ ! -e "$signature" && ! -e "${signature}.sha256" ]] || {
    die "provenance signature evidence already exists in the workspace"
    return 1
  }
  cosign sign-blob \
    --yes \
    --key "$PROVENANCE_SIGNING_KEY" \
    --output-signature "$temporary" \
    "$provenance" >/dev/null || return
  [[ -s "$temporary" ]] || {
    die "cosign did not produce a provenance signature"
    return 1
  }
  cosign verify-blob \
    --key "$PROVENANCE_SIGNING_PUBLIC_KEY" \
    --signature "$temporary" \
    "$provenance" >/dev/null || {
    die "cosign could not verify the provenance signature"
    return 1
  }
  install -m 0644 "$temporary" "$signature" || return
  write_checksum "$signature" || return
  printf '%s' "$signature"
}

write_exact_handoff() {
  local source_file="$1" digest="$2" sbom="$3" provenance="$4" signature="$5"
  local expected_ref handoff temporary sbom_sha provenance_sha signature_sha
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
  temporary="${STATE_DIR}/handoff.env"
  sbom_sha="$(artifact_sha256 "$sbom")" || return
  provenance_sha="$(artifact_sha256 "$provenance")" || return
  signature_sha="$(artifact_sha256 "$signature")" || return
  {
    printf 'DISTR_IMAGE_REF=%s\n' "$expected_ref"
    printf 'DISTR_RELEASE_COMMIT=%s\n' "$RELEASE_COMMIT"
    printf 'DISTR_IMAGE_DIGEST=%s\n' "$digest"
    printf 'DISTR_SBOM_REF=dist/%s\n' "$(basename "$sbom")"
    printf 'DISTR_SBOM_SHA256=%s\n' "$sbom_sha"
    printf 'DISTR_PROVENANCE_REF=dist/%s\n' "$(basename "$provenance")"
    printf 'DISTR_PROVENANCE_SHA256=%s\n' "$provenance_sha"
    printf 'DISTR_PROVENANCE_SIGNATURE_REF=dist/%s\n' "$(basename "$signature")"
    printf 'DISTR_PROVENANCE_SIGNATURE_SHA256=%s\n' "$signature_sha"
    printf 'DISTR_ATTESTATION_REF=dist/%s@%s\n' "$(basename "$signature")" "$signature_sha"
  } >"$temporary" || return
  install -m 0644 "$temporary" "$handoff" || return
  write_checksum "$handoff" || return
}

publish() (
  set -Eeuo pipefail
  local expected_source tagged_image digest digest_ref release_file sbom provenance signature
  need_cmd git || return
  need_cmd aws || return
  need_cmd docker || return
  need_cmd node || return
  need_cmd cosign || return
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
  sbom="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.spdx.json"
  require_real_sbom "$sbom" || return

  assert_remote_tag_absent pre-push || return
  "$DEPLOY_SCRIPT" push || return
  digest="$(resolve_remote_digest)" || return
  digest_ref="${DISTR_IMAGE}@${digest}"

  docker pull "$digest_ref" >/dev/null || return
  inspect_image_identity "$digest_ref" "$expected_source" || return
  provenance="$(write_provenance_attestation "$digest" "$expected_source" "$sbom")" || return
  signature="$(sign_provenance_attestation "$provenance")" || return
  write_exact_handoff "$release_file" "$digest" "$sbom" "$provenance" "$signature" || return

  info "published immutable Hub image ${digest_ref}"
  info "wrote checksummed image SBOM, signed provenance attestation, and handoff"
)

require_finalization_inputs() {
  require_commit "${RELEASE_COMMIT:-}" || return
  [[ "${DISTR_IMAGE:-}" == */* && "${DISTR_IMAGE:-}" != *$'\n'* && "${DISTR_IMAGE:-}" != *$'\r'* ]] || {
    die "DISTR_IMAGE is missing or invalid"
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

finalize_evidence() (
  set -Eeuo pipefail
  local migration16_input="$1" migration18_input="$2" postdeploy_input="$3" ui_input="$4"
  local handoff migration16 migration18 postdeploy ui_summary acceptance temporary_dir
  local image_ref digest sbom_ref sbom_sha provenance_ref provenance_sha
  local signature_ref signature_sha attestation_ref sbom provenance signature
  need_cmd node || return
  need_cmd cosign || return
  need_cmd sha256sum || return
  require_finalization_inputs || return
  for input in "$migration16_input" "$migration18_input" "$postdeploy_input" "$ui_input"; do
    [[ -f "$input" && ! -L "$input" ]] || {
      die "release evidence input must be a regular non-symlink file: $input"
      return 1
    }
  done

  handoff="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.env"
  [[ -f "$handoff" && ! -L "$handoff" && -f "${handoff}.sha256" ]] || {
    die "checksummed image handoff must exist before evidence finalization"
    return 1
  }
  (
    cd "$(dirname "$handoff")" || return
    sha256sum -c --status "$(basename "${handoff}.sha256")" || return
  ) || {
    die "image handoff checksum is invalid"
    return 1
  }

  image_ref="$(metadata_value "$handoff" DISTR_IMAGE_REF)" || return
  digest="$(metadata_value "$handoff" DISTR_IMAGE_DIGEST)" || return
  sbom_ref="$(metadata_value "$handoff" DISTR_SBOM_REF)" || return
  sbom_sha="$(metadata_value "$handoff" DISTR_SBOM_SHA256)" || return
  provenance_ref="$(metadata_value "$handoff" DISTR_PROVENANCE_REF)" || return
  provenance_sha="$(metadata_value "$handoff" DISTR_PROVENANCE_SHA256)" || return
  signature_ref="$(metadata_value "$handoff" DISTR_PROVENANCE_SIGNATURE_REF)" || return
  signature_sha="$(metadata_value "$handoff" DISTR_PROVENANCE_SIGNATURE_SHA256)" || return
  attestation_ref="$(metadata_value "$handoff" DISTR_ATTESTATION_REF)" || return
  [[ "$image_ref" == "${DISTR_IMAGE}@${digest}" &&
     "$(metadata_value "$handoff" DISTR_RELEASE_COMMIT)" == "$RELEASE_COMMIT"
  ]] || {
    die "image handoff identity differs from evidence finalization inputs"
    return 1
  }
  [[ "$sbom_ref" == "dist/release-${DISTR_IMAGE_TAG}.spdx.json" &&
      "$provenance_ref" == "dist/release-${DISTR_IMAGE_TAG}.intoto.json" &&
      "$signature_ref" == "dist/release-${DISTR_IMAGE_TAG}.intoto.json.sig" &&
      "$attestation_ref" == "${signature_ref}@${signature_sha}"
  ]] || {
    die "image handoff evidence references are not the exact candidate artifacts"
    return 1
  }
  sbom="${REPO_ROOT}/${sbom_ref}"
  provenance="${REPO_ROOT}/${provenance_ref}"
  signature="${REPO_ROOT}/${signature_ref}"
  require_real_sbom "$sbom" || return
  [[ "$(artifact_sha256 "$sbom")" == "$sbom_sha" ]] || {
    die "image handoff SBOM checksum differs from the retained artifact"
    return 1
  }
  [[ -f "$provenance" && ! -L "$provenance" && -f "${provenance}.sha256" && ! -L "${provenance}.sha256" ]] || {
    die "checksummed provenance attestation is missing"
    return 1
  }
  (
    cd "$(dirname "$provenance")" || return
    sha256sum -c --status "$(basename "${provenance}.sha256")" || return
  ) || {
    die "provenance attestation checksum is invalid"
    return 1
  }
  [[ "$(artifact_sha256 "$provenance")" == "$provenance_sha" ]] || {
    die "image handoff provenance checksum differs from the retained artifact"
    return 1
  }
  [[ -f "$signature" && ! -L "$signature" && -f "${signature}.sha256" && ! -L "${signature}.sha256" ]] || {
    die "checksummed provenance signature is missing"
    return 1
  }
  (
    cd "$(dirname "$signature")" || return
    sha256sum -c --status "$(basename "${signature}.sha256")" || return
  ) || {
    die "provenance signature checksum is invalid"
    return 1
  }
  [[ "$(artifact_sha256 "$signature")" == "$signature_sha" ]] || {
    die "image handoff signature checksum differs from the retained artifact"
    return 1
  }
  [[ -f "${PROVENANCE_SIGNING_PUBLIC_KEY:-}" && ! -L "${PROVENANCE_SIGNING_PUBLIC_KEY:-}" ]] || {
    die "PROVENANCE_SIGNING_PUBLIC_KEY must be a regular credential file"
    return 1
  }
  cosign verify-blob \
    --key "$PROVENANCE_SIGNING_PUBLIC_KEY" \
    --signature "$signature" \
    "$provenance" >/dev/null || {
    die "signed provenance verification failed"
    return 1
  }
  node - "$provenance" "$RELEASE_COMMIT" "$DISTR_IMAGE" "$digest" <<'NODE' || {
const fs = require('node:fs');
const [file, commit, image, digest] = process.argv.slice(2);
const statement = JSON.parse(fs.readFileSync(file, 'utf8'));
if (
  statement._type !== 'https://in-toto.io/Statement/v1' ||
  statement.predicateType !== 'https://slsa.dev/provenance/v1' ||
  statement.subject?.length !== 1 ||
  statement.subject[0].name !== image ||
  statement.subject[0].digest?.sha256 !== digest.slice('sha256:'.length) ||
  statement.predicate?.buildDefinition?.resolvedDependencies?.[0]?.digest?.sha1 !== commit
) {
  throw new Error('provenance attestation does not bind the candidate image and source commit');
}
NODE
    die "provenance attestation does not bind the candidate image and source commit"
    return 1
  }

  migration16="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-migration-postgresql-16.14.json"
  migration18="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-migration-postgresql-18.4.json"
  postdeploy="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-post-deploy.json"
  ui_summary="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-ui.json"
  acceptance="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-acceptance.json"
  for output in "$migration16" "$migration18" "$postdeploy" "$ui_summary" "$acceptance"; do
    [[ ! -e "$output" && ! -e "${output}.sha256" ]] || {
      die "release evidence output already exists: $output"
      return 1
    }
  done
  temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/distr-release-evidence.XXXXXX")" || return
  chmod 0700 "$temporary_dir" || return
  trap 'rm -rf -- "${temporary_dir:-}"' EXIT HUP INT TERM

  node hack/pr050-validate-control-plane-evidence.mjs migration \
    "$migration16_input" "$RELEASE_COMMIT" '16.14' || return
  node hack/pr050-validate-control-plane-evidence.mjs migration \
    "$migration18_input" "$RELEASE_COMMIT" '18.4' || return
  node hack/pr050-validate-control-plane-evidence.mjs postdeploy "$postdeploy_input" || return
  node hack/pr050-validate-control-plane-evidence.mjs ui "$ui_input" || return
  install -m 0644 "$migration16_input" "$migration16" || return
  install -m 0644 "$migration18_input" "$migration18" || return
  install -m 0644 "$postdeploy_input" "$postdeploy" || return
  install -m 0644 "$ui_input" "$ui_summary" || return
  write_checksum "$migration16" || return
  write_checksum "$migration18" || return
  write_checksum "$postdeploy" || return
  write_checksum "$ui_summary" || return

  local migration16_sha migration18_sha postdeploy_sha ui_sha acceptance_sha
  migration16_sha="$(artifact_sha256 "$migration16")" || return
  migration18_sha="$(artifact_sha256 "$migration18")" || return
  postdeploy_sha="$(artifact_sha256 "$postdeploy")" || return
  ui_sha="$(artifact_sha256 "$ui_summary")" || return
  node - "$temporary_dir/acceptance.json" "$RELEASE_COMMIT" "$image_ref" \
    "dist/$(basename "$migration16")" "$migration16_sha" \
    "dist/$(basename "$migration18")" "$migration18_sha" \
    "dist/$(basename "$postdeploy")" "$postdeploy_sha" \
    "dist/$(basename "$ui_summary")" "$ui_sha" \
    "$provenance_ref" "$provenance_sha" "$signature_ref" "$signature_sha" <<'NODE' || return
const fs = require('node:fs');
const [
  file, commit, imageRef,
  migration16Ref, migration16Sha, migration18Ref, migration18Sha,
  postdeployRef, postdeploySha, uiRef, uiSha,
  provenanceRef, provenanceSha, signatureRef, signatureSha,
] = process.argv.slice(2);
const live = (selector) => ({status: 'PASS', reference: postdeployRef, sha256: postdeploySha, selector});
const matrix = (reference, sha256, postgresVersion) => ({
  status: 'PASS',
  reference,
  sha256,
  postgresVersion,
  selectors: ['range=138..167', 'v1-flags-off=PASS', 'mixed-v1-v2=PASS', 'v2-history-flags-off=PASS'],
});
const report = {
  schemaVersion: 'distr.control-plane-release-acceptance/v1',
  status: 'PASS',
  sourceCommit: commit,
  imageRef,
  provenance: {
    status: 'PASS',
    reference: provenanceRef,
    sha256: provenanceSha,
    signatureReference: signatureRef,
    signatureSha256: signatureSha,
    verification: 'cosign verify-blob',
  },
  migrations: [
    matrix(migration16Ref, migration16Sha, '16.14'),
    matrix(migration18Ref, migration18Sha, '18.4'),
  ],
  evidence: {
    operator: live('liveStack.started=true,loopbackOnly=true,required services present,cleanup complete'),
    api: live('proofMode=live-hub-api,targets>=2,v2 migration attempts succeeded'),
    ui: {status: 'PASS', reference: uiRef, sha256: uiSha, selector: 'stats.expected>0,unexpected=0,flaky=0'},
    flag: {
      status: 'PASS',
      references: [migration16Ref, migration18Ref],
      sha256: [migration16Sha, migration18Sha],
      selector: 'v1-flags-off,mixed-v1-v2,v2-history-flags-off all PASS on PostgreSQL 16.14 and 18.4',
    },
    audit: live('trusted current ACCEPTED observer evidence,A-B-A reconciliation,fleet rows,verified flowChecksum'),
  },
};
fs.writeFileSync(file, `${JSON.stringify(report, null, 2)}\n`, {mode: 0o600});
NODE
  install -m 0644 "$temporary_dir/acceptance.json" "$acceptance" || return
  write_checksum "$acceptance" || return
  acceptance_sha="$(artifact_sha256 "$acceptance")" || return

  {
    printf 'DISTR_IMAGE_REF=%s\n' "$image_ref"
    printf 'DISTR_RELEASE_COMMIT=%s\n' "$RELEASE_COMMIT"
    printf 'DISTR_IMAGE_DIGEST=%s\n' "$digest"
    printf 'DISTR_SBOM_REF=%s\n' "$sbom_ref"
    printf 'DISTR_SBOM_SHA256=%s\n' "$sbom_sha"
    printf 'DISTR_PROVENANCE_REF=%s\n' "$provenance_ref"
    printf 'DISTR_PROVENANCE_SHA256=%s\n' "$provenance_sha"
    printf 'DISTR_PROVENANCE_SIGNATURE_REF=%s\n' "$signature_ref"
    printf 'DISTR_PROVENANCE_SIGNATURE_SHA256=%s\n' "$signature_sha"
    printf 'DISTR_ATTESTATION_REF=%s\n' "$attestation_ref"
    printf 'DISTR_MIGRATION_POSTGRESQL_16_REPORT_REF=dist/%s\n' "$(basename "$migration16")"
    printf 'DISTR_MIGRATION_POSTGRESQL_16_REPORT_SHA256=%s\n' "$migration16_sha"
    printf 'DISTR_MIGRATION_POSTGRESQL_18_REPORT_REF=dist/%s\n' "$(basename "$migration18")"
    printf 'DISTR_MIGRATION_POSTGRESQL_18_REPORT_SHA256=%s\n' "$migration18_sha"
    printf 'DISTR_ACCEPTANCE_BUNDLE_REF=dist/%s\n' "$(basename "$acceptance")"
    printf 'DISTR_ACCEPTANCE_BUNDLE_SHA256=%s\n' "$acceptance_sha"
  } >"$temporary_dir/handoff.env" || return
  install -m 0644 "$temporary_dir/handoff.env" "$handoff" || return
  write_checksum "$handoff" || return
  info "finalized signed PostgreSQL 16/18 migration and operator/API/UI/flag/audit acceptance evidence"
)

usage() {
  cat <<'EOF'
Usage:
  publish-hub-image.sh candidate-tag <40-lowercase-hex-commit>
  publish-hub-image.sh publish
  publish-hub-image.sh finalize-evidence <postgres-16-migration-report> <postgres-18-migration-report> <post-deploy-report> <ui-report>

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
  finalize-evidence)
    [[ $# == 5 ]] || {
      usage >&2
      exit 2
    }
    cd "$REPO_ROOT"
    finalize_evidence "$2" "$3" "$4" "$5"
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
