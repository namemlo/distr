#!/usr/bin/env bash
set -euo pipefail

: "${DISTR_SERVER_URL:?set DISTR_SERVER_URL}"
: "${DISTR_API_TOKEN:?set DISTR_API_TOKEN}"
: "${DISTR_APPLICATION_ID:?set DISTR_APPLICATION_ID}"
: "${DISTR_CHANNEL_ID:?set DISTR_CHANNEL_ID}"
: "${RELEASE_NUMBER:?set RELEASE_NUMBER}"
: "${SOURCE_REVISION:?set SOURCE_REVISION}"
: "${IMAGE_DIGEST:?set IMAGE_DIGEST as sha256:<64 hex characters>}"

CI_RUN_ID="${CI_RUN_ID:-manual-$RELEASE_NUMBER}"
CI_RUN_URL="${CI_RUN_URL:-}"

cat > release-bundle.json <<JSON
{
  "applicationId": "$DISTR_APPLICATION_ID",
  "channelId": "$DISTR_CHANNEL_ID",
  "releaseNumber": "$RELEASE_NUMBER",
  "releaseNotes": "Automated release from CI",
  "sourceRevision": "$SOURCE_REVISION",
  "sourceMetadata": {
    "repository": "${SOURCE_REPOSITORY:-}",
    "branch": "${SOURCE_BRANCH:-}",
    "tag": "${SOURCE_TAG:-}",
    "ciProvider": "generic-ci",
    "ciRunId": "$CI_RUN_ID",
    "ciRunUrl": "$CI_RUN_URL"
  },
  "components": [
    {
      "key": "api-image",
      "name": "API image",
      "type": "oci_image",
      "version": "$RELEASE_NUMBER",
      "packageRef": "${IMAGE_REF:-registry.example.invalid/org/api}",
      "digest": "$IMAGE_DIGEST"
    }
  ]
}
JSON

create_output="$(
  curl --fail-with-body \
    -H "Authorization: AccessToken $DISTR_API_TOKEN" \
    -H "Content-Type: application/json" \
    -H "Idempotency-Key: $CI_RUN_ID" \
    --data @release-bundle.json \
    "$DISTR_SERVER_URL/api/v1/release-bundles"
)"

release_bundle_id="$(printf '%s' "$create_output" | jq -r '.id')"

validation_output="$(
  curl --fail-with-body \
    -X POST \
    -H "Authorization: AccessToken $DISTR_API_TOKEN" \
    "$DISTR_SERVER_URL/api/v1/release-bundles/$release_bundle_id/validate"
)"

if ! printf '%s' "$validation_output" | jq -e '.valid == true' >/dev/null; then
  printf '%s\n' "$validation_output" >&2
  exit 1
fi

curl --fail-with-body \
  -X POST \
  -H "Authorization: AccessToken $DISTR_API_TOKEN" \
  "$DISTR_SERVER_URL/api/v1/release-bundles/$release_bundle_id/publish"
