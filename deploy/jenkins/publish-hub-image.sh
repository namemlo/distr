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

require_checksummed_artifact() {
  local file="$1" expected_sha="${2:-}" actual_sha sidecar expected_line
  sidecar="${file}.sha256"
  [[ -f "$file" && ! -L "$file" && -f "$sidecar" && ! -L "$sidecar" ]] || {
    die "checksummed release artifact is missing: $file"
    return 1
  }
  actual_sha="$(artifact_sha256 "$file")" || return
  [[ -z "$expected_sha" || "$actual_sha" == "$expected_sha" ]] || {
    die "release artifact checksum differs from expected metadata: $file"
    return 1
  }
  expected_line="${actual_sha#sha256:}  $(basename "$file")"
  [[ "$(wc -l <"$sidecar")" -eq 1 && "$(<"$sidecar")" == "$expected_line" ]] || {
    die "release artifact checksum sidecar is not exact: $sidecar"
    return 1
  }
  (
    cd "$(dirname "$file")" || return
    sha256sum -c --status "$(basename "$sidecar")" || return
  ) || {
    die "release artifact checksum is invalid: $file"
    return 1
  }
}

write_checksum() {
  local file="$1" sidecar checksum
  sidecar="${file}.sha256"
  [[ -f "$file" && ! -L "$file" && ! -L "$sidecar" ]] || {
    die "refusing to checksum a missing, symlinked, or unsafe release artifact: $file"
    return 1
  }
  checksum="$(sha256sum "$file" | awk '{print $1}')" || return
  [[ "$checksum" =~ ^[0-9a-f]{64}$ ]] || return 1
  (
    cd "$(dirname "$file")" || return
    printf '%s  %s\n' "$checksum" "$(basename "$file")" >"$(basename "$sidecar")" || return
    sha256sum -c --status "$(basename "$sidecar")" || return
  ) || return
}

inspect_image_id_platform() {
  local image_ref="$1" raw image_id platform extra
  raw="$(docker image inspect --format '{{.Id}}|{{.Os}}/{{.Architecture}}' "$image_ref")" || {
    die "could not inspect image identity for ${image_ref}"
    return 1
  }
  IFS='|' read -r image_id platform extra <<<"$raw"
  [[ -z "$extra" && "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    die "image identity is invalid for ${image_ref}"
    return 1
  }
  case "$platform" in
    linux/amd64|linux/x86_64) platform=linux/amd64 ;;
    *)
      die "Hub image platform must be linux/amd64"
      return 1
      ;;
  esac
  printf '%s|%s' "$image_id" "$platform"
}

require_image_sbom_binding() {
  local sbom="$1" binding="$2" commit="$3" tagged_ref="$4" image_id="$5" platform="$6"
  local sbom_file sbom_sha binding_sha commit_epoch
  sbom_file="dist/$(basename "$sbom")"
  require_checksummed_artifact "$sbom" || return
  require_checksummed_artifact "$binding" || return
  sbom_sha="$(artifact_sha256 "$sbom")" || return
  binding_sha="$(artifact_sha256 "$binding")" || return
  [[ "$sbom_file" == "dist/release-${DISTR_IMAGE_TAG}.spdx.json" &&
     "dist/$(basename "$binding")" == "dist/release-${DISTR_IMAGE_TAG}.image-binding.json" &&
     "$commit" =~ ^[0-9a-f]{40}$ &&
     "$tagged_ref" == "${DISTR_IMAGE}:${DISTR_IMAGE_TAG}" &&
     "$image_id" =~ ^sha256:[0-9a-f]{64}$ &&
     "$platform" == linux/amd64
  ]] || {
    die "image SBOM binding inputs do not match the candidate image"
    return 1
  }
  commit_epoch="$(git show -s --format=%ct "$commit")" || return
  [[ "$commit_epoch" =~ ^[0-9]+$ ]] || return 1
  node - "$sbom" "$binding" "$DISTR_IMAGE" "$DISTR_IMAGE_TAG" "$commit" \
    "$commit_epoch" "$tagged_ref" "$image_id" "$platform" "$sbom_file" "$sbom_sha" <<'NODE' || {
const fs = require('node:fs');
const [
  sbomPath, bindingPath, repository, tag, commit, commitEpoch,
  taggedRef, imageId, platform, sbomFile, sbomSha,
] = process.argv.slice(2);
const sbomRaw = fs.readFileSync(sbomPath, 'utf8');
const document = JSON.parse(sbomRaw);
const relationships = Array.isArray(document.relationships) ? document.relationships : [];
const describes = relationships.filter(
  (item) => item?.spdxElementId === 'SPDXRef-DOCUMENT' && item?.relationshipType === 'DESCRIBES'
);
if (describes.length !== 1 || typeof describes[0].relatedSpdxElement !== 'string') {
  throw new Error('SBOM must contain exactly one DOCUMENT DESCRIBES relationship');
}
const rootId = describes[0].relatedSpdxElement;
const roots = Array.isArray(document.packages)
  ? document.packages.filter((item) => item?.SPDXID === rootId)
  : [];
const timestamp = new Date(Number(commitEpoch) * 1000);
if (
  document.spdxVersion !== 'SPDX-2.3' ||
  document.SPDXID !== 'SPDXRef-DOCUMENT' ||
  !Array.isArray(document.documentDescribes) ||
  document.documentDescribes.length !== 1 ||
  document.documentDescribes[0] !== rootId ||
  roots.length !== 1 ||
  document.documentNamespace !==
    `https://distr.sh/spdx/hub-image/${commit}/${tag}/${imageId.slice('sha256:'.length)}` ||
  document.creationInfo?.created !== timestamp.toISOString().replace('.000Z', 'Z')
) {
  throw new Error('SBOM document identity does not match the image binding');
}
const root = roots[0];
const checksums = Array.isArray(root.checksums)
  ? root.checksums.filter((item) => item?.algorithm === 'SHA256')
  : [];
const purls = Array.isArray(root.externalRefs)
  ? root.externalRefs.filter(
      (item) => item?.referenceCategory === 'PACKAGE-MANAGER' && item?.referenceType === 'purl'
    )
  : [];
if (
  root.name !== repository ||
  root.versionInfo !== tag ||
  root.primaryPackagePurpose !== 'CONTAINER' ||
  checksums.length !== 1 ||
  !/^[0-9a-f]{64}$/.test(checksums[0].checksumValue ?? '') ||
  purls.length !== 1
) {
  throw new Error('SBOM root package does not match the candidate image');
}
const expectedPurl =
  `pkg:oci/${encodeURIComponent(repository)}@${encodeURIComponent(`sha256:${checksums[0].checksumValue}`)}` +
  `?arch=amd64&tag=${encodeURIComponent(tag)}`;
const parsedPurl = new URL(purls[0].referenceLocator);
if (
  parsedPurl.searchParams.getAll('arch').length !== 1 ||
  parsedPurl.searchParams.getAll('tag').length !== 1 ||
  [...parsedPurl.searchParams.keys()].some((key) => key !== 'arch' && key !== 'tag') ||
  purls[0].referenceLocator !== expectedPurl
) {
  throw new Error('SBOM root OCI purl does not match the candidate image');
}
const unorderedArrays = new Set([
  'annotations', 'checksums', 'creators', 'documentDescribes', 'externalDocumentRefs',
  'externalRefs', 'files', 'hasExtractedLicensingInfos', 'licenseInfoFromFiles',
  'packages', 'relationships', 'snippets',
]);
function canonicalize(value, key = '') {
  if (Array.isArray(value)) {
    const items = value.map((item) => canonicalize(item));
    return unorderedArrays.has(key)
      ? items.sort((left, right) => JSON.stringify(left).localeCompare(JSON.stringify(right)))
      : items;
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value).sort().map((childKey) => [childKey, canonicalize(value[childKey], childKey)])
    );
  }
  return value;
}
if (sbomRaw !== `${JSON.stringify(canonicalize(document))}\n`) {
  throw new Error('SBOM is not the deterministic canonical document');
}

const bindingRaw = fs.readFileSync(bindingPath, 'utf8');
const binding = JSON.parse(bindingRaw);
const expectedBinding = {
  schemaVersion: 'distr.image-sbom-binding/v1',
  sourceCommit: commit,
  taggedImageRef: taggedRef,
  localImageId: imageId,
  platform,
  sbomFile,
  sbomSha256: sbomSha,
};
if (bindingRaw !== `${JSON.stringify(expectedBinding)}\n`) {
  throw new Error('image binding is not the exact canonical candidate binding');
}
NODE
    die "image SBOM or binding does not match the deterministic candidate contract"
    return 1
  }
}

write_provenance_attestation() {
  local digest="$1" source="$2" sbom="$3" binding="$4" image_id="$5" platform="$6"
  local provenance temporary sbom_ref sbom_sha binding_ref binding_sha
  provenance="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.intoto.json"
  temporary="${STATE_DIR}/provenance.intoto.json"
  sbom_ref="dist/$(basename "$sbom")"
  sbom_sha="$(artifact_sha256 "$sbom")" || return
  binding_ref="dist/$(basename "$binding")"
  binding_sha="$(artifact_sha256 "$binding")" || return
  [[ ! -e "$provenance" && ! -L "$provenance" &&
     ! -e "${provenance}.sha256" && ! -L "${provenance}.sha256" ]] || {
    die "provenance evidence already exists in the workspace"
    return 1
  }
  node - "$temporary" "$RELEASE_COMMIT" "$source" "$DISTR_IMAGE" "$DISTR_IMAGE_TAG" "$digest" \
    "$image_id" "$platform" "$sbom_ref" "$sbom_sha" "$binding_ref" "$binding_sha" <<'NODE' || return
const fs = require('node:fs');
const [
  file, commit, source, image, tag, digest, localImageId, platform,
  sbomRef, sbomSha, bindingRef, bindingSha,
] = process.argv.slice(2);
const statement = {
  _type: 'https://in-toto.io/Statement/v1',
  subject: [{name: image, digest: {sha256: digest.slice('sha256:'.length)}}],
  predicateType: 'https://slsa.dev/provenance/v1',
  predicate: {
    buildDefinition: {
      buildType: 'https://distr.sh/build-types/community-hub-image/v1',
      externalParameters: {localImageId, platform, taggedImageRef: `${image}:${tag}`},
      internalParameters: {},
      resolvedDependencies: [
        {uri: source, digest: {sha1: commit}},
        {uri: sbomRef, digest: {sha256: sbomSha.slice('sha256:'.length)}},
        {uri: bindingRef, digest: {sha256: bindingSha.slice('sha256:'.length)}},
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

require_provenance_binding() {
  local provenance="$1" source="$2" digest="$3" local_image_id="$4" platform="$5"
  local sbom_file="$6" sbom_sha="$7" binding_file="$8" binding_sha="$9"
  node - "$provenance" "$RELEASE_COMMIT" "$source" "$DISTR_IMAGE" "$DISTR_IMAGE_TAG" \
    "$digest" "$local_image_id" "$platform" "$sbom_file" "$sbom_sha" \
    "$binding_file" "$binding_sha" <<'NODE' || {
const fs = require('node:fs');
const [
  file, commit, source, image, tag, digest, localImageId, platform,
  sbomFile, sbomSha, bindingFile, bindingSha,
] = process.argv.slice(2);
const statement = JSON.parse(fs.readFileSync(file, 'utf8'));
const dependencies = statement.predicate?.buildDefinition?.resolvedDependencies;
const dependencyMap = new Map(
  Array.isArray(dependencies) ? dependencies.map((item) => [item?.uri, item?.digest]) : []
);
const parameters = statement.predicate?.buildDefinition?.externalParameters;
if (
  statement._type !== 'https://in-toto.io/Statement/v1' ||
  statement.predicateType !== 'https://slsa.dev/provenance/v1' ||
  statement.subject?.length !== 1 ||
  statement.subject[0].name !== image ||
  statement.subject[0].digest?.sha256 !== digest.slice('sha256:'.length) ||
  statement.predicate?.buildDefinition?.buildType !==
    'https://distr.sh/build-types/community-hub-image/v1' ||
  JSON.stringify(parameters) !==
    JSON.stringify({localImageId, platform, taggedImageRef: `${image}:${tag}`}) ||
  dependencies?.length !== 3 ||
  dependencyMap.size !== 3 ||
  dependencyMap.get(source)?.sha1 !== commit ||
  dependencyMap.get(sbomFile)?.sha256 !== sbomSha.slice('sha256:'.length) ||
  dependencyMap.get(bindingFile)?.sha256 !== bindingSha.slice('sha256:'.length)
) {
  throw new Error('provenance attestation does not co-bind the candidate image and release evidence');
}
NODE
    die "provenance attestation does not co-bind the candidate image and release evidence"
    return 1
  }
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
  [[ ! -e "$signature" && ! -L "$signature" &&
     ! -e "${signature}.sha256" && ! -L "${signature}.sha256" ]] || {
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
  local source_file="$1" digest="$2" sbom="$3" binding="$4" provenance="$5" signature="$6" source="$7"
  local expected_ref tagged_ref handoff temporary sbom_sha binding_sha provenance_sha signature_sha
  local local_image_id image_platform sbom_file binding_file
  expected_ref="${DISTR_IMAGE}@${digest}"
  tagged_ref="${DISTR_IMAGE}:${DISTR_IMAGE_TAG}"
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
  local_image_id="$(metadata_value "$source_file" DISTR_LOCAL_IMAGE_ID)" || return
  image_platform="$(metadata_value "$source_file" DISTR_IMAGE_PLATFORM)" || return
  sbom_file="$(metadata_value "$source_file" DISTR_SBOM_FILE)" || return
  sbom_sha="$(metadata_value "$source_file" DISTR_SBOM_SHA256)" || return
  binding_file="$(metadata_value "$source_file" DISTR_SBOM_BINDING_FILE)" || return
  binding_sha="$(metadata_value "$source_file" DISTR_SBOM_BINDING_SHA256)" || return
  [[ "$sbom_file" == "dist/$(basename "$sbom")" &&
     "$binding_file" == "dist/$(basename "$binding")" &&
     "$sbom_sha" == "$(artifact_sha256 "$sbom")" &&
     "$binding_sha" == "$(artifact_sha256 "$binding")"
  ]] || {
    die "release metadata SBOM binding differs from the retained candidate artifacts"
    return 1
  }
  require_image_sbom_binding \
    "$sbom" "$binding" "$RELEASE_COMMIT" "$tagged_ref" "$local_image_id" "$image_platform" || return
  require_checksummed_artifact "$provenance" || return
  require_checksummed_artifact "$signature" || return
  require_provenance_binding "$provenance" "$source" "$digest" "$local_image_id" "$image_platform" \
    "$sbom_file" "$sbom_sha" "$binding_file" "$binding_sha" || return

  handoff="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.env"
  temporary="${STATE_DIR}/handoff.env"
  provenance_sha="$(artifact_sha256 "$provenance")" || return
  signature_sha="$(artifact_sha256 "$signature")" || return
  {
    printf 'DISTR_IMAGE_REF=%s\n' "$expected_ref"
    printf 'DISTR_RELEASE_COMMIT=%s\n' "$RELEASE_COMMIT"
    printf 'DISTR_IMAGE_DIGEST=%s\n' "$digest"
    printf 'DISTR_LOCAL_IMAGE_ID=%s\n' "$local_image_id"
    printf 'DISTR_IMAGE_PLATFORM=%s\n' "$image_platform"
    printf 'DISTR_SBOM_FILE=%s\n' "$sbom_file"
    printf 'DISTR_SBOM_SHA256=%s\n' "$sbom_sha"
    printf 'DISTR_SBOM_BINDING_FILE=%s\n' "$binding_file"
    printf 'DISTR_SBOM_BINDING_SHA256=%s\n' "$binding_sha"
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
  local expected_source tagged_image digest digest_ref release_file sbom binding provenance signature
  local identity local_image_id image_platform published_identity
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
  [[ ! -e "$release_file" && ! -L "$release_file" &&
     ! -e "${release_file}.sha256" && ! -L "${release_file}.sha256" ]] || {
    die "release handoff already exists in the workspace"
    return 1
  }

  require_immutable_repository || return
  assert_remote_tag_absent pre-build || return
  "$DEPLOY_SCRIPT" image-check || return
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 "$DEPLOY_SCRIPT" build || return

  tagged_image="${DISTR_IMAGE}:${DISTR_IMAGE_TAG}"
  inspect_image_identity "$tagged_image" "$expected_source" || return
  identity="$(inspect_image_id_platform "$tagged_image")" || return
  IFS='|' read -r local_image_id image_platform <<<"$identity"
  sbom="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.spdx.json"
  binding="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}.image-binding.json"
  require_image_sbom_binding \
    "$sbom" "$binding" "$RELEASE_COMMIT" "$tagged_image" "$local_image_id" "$image_platform" || return
  [[ "$(inspect_image_id_platform "$tagged_image")" == "$identity" ]] || {
    die "local image tag changed while validating the SBOM binding"
    return 1
  }

  assert_remote_tag_absent pre-push || return
  "$DEPLOY_SCRIPT" push || return
  digest="$(resolve_remote_digest)" || return
  digest_ref="${DISTR_IMAGE}@${digest}"

  docker pull "$digest_ref" >/dev/null || return
  inspect_image_identity "$digest_ref" "$expected_source" || return
  published_identity="$(inspect_image_id_platform "$digest_ref")" || return
  [[ "$published_identity" == "$identity" ]] || {
    die "published ECR digest does not resolve to the SBOM-bound local image"
    return 1
  }
  require_image_sbom_binding \
    "$sbom" "$binding" "$RELEASE_COMMIT" "$tagged_image" "$local_image_id" "$image_platform" || return
  [[ "$(inspect_image_id_platform "$tagged_image")" == "$identity" ]] || {
    die "local image tag changed before provenance generation"
    return 1
  }
  provenance="$(write_provenance_attestation \
    "$digest" "$expected_source" "$sbom" "$binding" "$local_image_id" "$image_platform")" || return
  signature="$(sign_provenance_attestation "$provenance")" || return
  write_exact_handoff \
    "$release_file" "$digest" "$sbom" "$binding" "$provenance" "$signature" "$expected_source" || return

  info "published immutable Hub image ${digest_ref}"
  info "wrote checksummed image SBOM binding, signed provenance attestation, and handoff"
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
  local image_ref digest local_image_id image_platform sbom_file sbom_sha binding_file binding_sha
  local provenance_ref provenance_sha signature_ref signature_sha attestation_ref
  local tagged_image sbom binding provenance signature expected_source remote_identity
  need_cmd git || return
  need_cmd docker || return
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
  require_checksummed_artifact "$handoff" || {
    die "checksummed image handoff must exist before evidence finalization"
    return 1
  }

  image_ref="$(metadata_value "$handoff" DISTR_IMAGE_REF)" || return
  digest="$(metadata_value "$handoff" DISTR_IMAGE_DIGEST)" || return
  local_image_id="$(metadata_value "$handoff" DISTR_LOCAL_IMAGE_ID)" || return
  image_platform="$(metadata_value "$handoff" DISTR_IMAGE_PLATFORM)" || return
  sbom_file="$(metadata_value "$handoff" DISTR_SBOM_FILE)" || return
  sbom_sha="$(metadata_value "$handoff" DISTR_SBOM_SHA256)" || return
  binding_file="$(metadata_value "$handoff" DISTR_SBOM_BINDING_FILE)" || return
  binding_sha="$(metadata_value "$handoff" DISTR_SBOM_BINDING_SHA256)" || return
  provenance_ref="$(metadata_value "$handoff" DISTR_PROVENANCE_REF)" || return
  provenance_sha="$(metadata_value "$handoff" DISTR_PROVENANCE_SHA256)" || return
  signature_ref="$(metadata_value "$handoff" DISTR_PROVENANCE_SIGNATURE_REF)" || return
  signature_sha="$(metadata_value "$handoff" DISTR_PROVENANCE_SIGNATURE_SHA256)" || return
  attestation_ref="$(metadata_value "$handoff" DISTR_ATTESTATION_REF)" || return
  [[ "$image_ref" == "${DISTR_IMAGE}@${digest}" &&
     "$digest" =~ ^sha256:[0-9a-f]{64}$ &&
     "$local_image_id" =~ ^sha256:[0-9a-f]{64}$ &&
     "$image_platform" == linux/amd64 &&
     "$(metadata_value "$handoff" DISTR_RELEASE_COMMIT)" == "$RELEASE_COMMIT"
  ]] || {
    die "image handoff identity differs from evidence finalization inputs"
    return 1
  }
  [[ "$sbom_file" == "dist/release-${DISTR_IMAGE_TAG}.spdx.json" &&
      "$binding_file" == "dist/release-${DISTR_IMAGE_TAG}.image-binding.json" &&
      "$provenance_ref" == "dist/release-${DISTR_IMAGE_TAG}.intoto.json" &&
      "$signature_ref" == "dist/release-${DISTR_IMAGE_TAG}.intoto.json.sig" &&
      "$attestation_ref" == "${signature_ref}@${signature_sha}"
  ]] || {
    die "image handoff evidence references are not the exact candidate artifacts"
    return 1
  }
  tagged_image="${DISTR_IMAGE}:${DISTR_IMAGE_TAG}"
  sbom="${REPO_ROOT}/${sbom_file}"
  binding="${REPO_ROOT}/${binding_file}"
  provenance="${REPO_ROOT}/${provenance_ref}"
  signature="${REPO_ROOT}/${signature_ref}"
  require_image_sbom_binding \
    "$sbom" "$binding" "$RELEASE_COMMIT" "$tagged_image" "$local_image_id" "$image_platform" || return
  [[ "$(artifact_sha256 "$sbom")" == "$sbom_sha" &&
     "$(artifact_sha256 "$binding")" == "$binding_sha"
  ]] || {
    die "image handoff SBOM binding checksums differ from the retained artifacts"
    return 1
  }
  require_checksummed_artifact "$provenance" "$provenance_sha" || return
  require_checksummed_artifact "$signature" "$signature_sha" || return
  expected_source="$(source_repository)" || return
  docker pull "$image_ref" >/dev/null || {
    die "could not pull the immutable image for evidence finalization"
    return 1
  }
  inspect_image_identity "$image_ref" "$expected_source" || return
  remote_identity="$(inspect_image_id_platform "$image_ref")" || return
  [[ "$remote_identity" == "${local_image_id}|${image_platform}" ]] || {
    die "immutable ECR image differs from the SBOM-bound local image identity"
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
  require_provenance_binding "$provenance" "$expected_source" "$digest" "$local_image_id" \
    "$image_platform" "$sbom_file" "$sbom_sha" "$binding_file" "$binding_sha" || return

  migration16="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-migration-postgresql-16.14.json"
  migration18="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-migration-postgresql-18.4.json"
  postdeploy="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-post-deploy.json"
  ui_summary="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-ui.json"
  acceptance="${REPO_ROOT}/dist/release-${DISTR_IMAGE_TAG}-acceptance.json"
  for output in "$migration16" "$migration18" "$postdeploy" "$ui_summary" "$acceptance"; do
    [[ ! -e "$output" && ! -L "$output" &&
       ! -e "${output}.sha256" && ! -L "${output}.sha256" ]] || {
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
    "$local_image_id" "$image_platform" "$sbom_file" "$sbom_sha" "$binding_file" "$binding_sha" \
    "dist/$(basename "$migration16")" "$migration16_sha" \
    "dist/$(basename "$migration18")" "$migration18_sha" \
    "dist/$(basename "$postdeploy")" "$postdeploy_sha" \
    "dist/$(basename "$ui_summary")" "$ui_sha" \
    "$provenance_ref" "$provenance_sha" "$signature_ref" "$signature_sha" <<'NODE' || return
const fs = require('node:fs');
const [
  file, commit, imageRef, localImageId, imagePlatform, sbomFile, sbomSha, bindingFile, bindingSha,
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
  selectors: ['range=138..170', 'v1-flags-off=PASS', 'mixed-v1-v2=PASS', 'v2-history-flags-off=PASS'],
});
const report = {
  schemaVersion: 'distr.control-plane-release-acceptance/v1',
  status: 'PASS',
  sourceCommit: commit,
  imageRef,
  imageBinding: {
    localImageId,
    platform: imagePlatform,
    sbomFile,
    sbomSha256: sbomSha,
    bindingFile,
    bindingSha256: bindingSha,
  },
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
    printf 'DISTR_LOCAL_IMAGE_ID=%s\n' "$local_image_id"
    printf 'DISTR_IMAGE_PLATFORM=%s\n' "$image_platform"
    printf 'DISTR_SBOM_FILE=%s\n' "$sbom_file"
    printf 'DISTR_SBOM_SHA256=%s\n' "$sbom_sha"
    printf 'DISTR_SBOM_BINDING_FILE=%s\n' "$binding_file"
    printf 'DISTR_SBOM_BINDING_SHA256=%s\n' "$binding_sha"
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
