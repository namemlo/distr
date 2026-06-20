# Use a CI System to Publish a Release

This tutorial shows a neutral CI flow for Release Bundles.

The CI system should:

1. Build and test the application.
2. Push immutable artifacts or images.
3. Capture immutable digests such as `sha256:<64 hex characters>`.
4. Create a draft Release Bundle idempotently.
5. Validate the draft through the public API.
6. Publish only after validation succeeds.

## Release Request

Create a JSON request with organization-scoped application and channel IDs:

```json
{
  "applicationId": "00000000-0000-0000-0000-000000000000",
  "channelId": "00000000-0000-0000-0000-000000000000",
  "releaseNumber": "2026.06.20.1",
  "releaseNotes": "Automated release from CI",
  "sourceRevision": "0123456789abcdef0123456789abcdef01234567",
  "sourceMetadata": {
    "repository": "https://example.invalid/org/project",
    "branch": "main",
    "tag": "",
    "ciProvider": "generic-ci",
    "ciRunId": "12345",
    "ciRunUrl": "https://ci.example.invalid/runs/12345"
  },
  "components": [
    {
      "key": "api-image",
      "name": "API image",
      "type": "oci_image",
      "version": "2026.06.20.1",
      "packageRef": "registry.example.invalid/org/api",
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  ]
}
```

Do not put credentials, authorization headers, environment dumps, or secret values in `sourceMetadata`.

## CLI Flow

```bash
export DISTR_SERVER_URL="https://distr.example.invalid"
export DISTR_API_TOKEN="$CI_DISTR_API_TOKEN"

CREATE_OUTPUT="$(
  distr release create \
    --file release-bundle.json \
    --idempotency-key "$CI_RUN_ID" \
    --output json
)"

RELEASE_BUNDLE_ID="$(
  printf '%s' "$CREATE_OUTPUT" |
    jq -r '.id'
)"

distr release validate "$RELEASE_BUNDLE_ID"
distr release publish "$RELEASE_BUNDLE_ID"
```

Create is retry-safe when the same CI run repeats the same request with the same idempotency key. Publication remains explicit and server-side validation is never bypassed.

## Plain Curl Flow

```bash
CREATE_OUTPUT="$(
  curl --fail-with-body \
    -H "Authorization: AccessToken $DISTR_API_TOKEN" \
    -H "Content-Type: application/json" \
    -H "Idempotency-Key: $CI_RUN_ID" \
    --data @release-bundle.json \
    "$DISTR_SERVER_URL/api/v1/release-bundles"
)"

RELEASE_BUNDLE_ID="$(printf '%s' "$CREATE_OUTPUT" | jq -r '.id')"

VALIDATION_OUTPUT="$(
  curl --fail-with-body \
    -X POST \
    -H "Authorization: AccessToken $DISTR_API_TOKEN" \
    "$DISTR_SERVER_URL/api/v1/release-bundles/$RELEASE_BUNDLE_ID/validate"
)"

if ! printf '%s' "$VALIDATION_OUTPUT" | jq -e '.valid == true' >/dev/null; then
  printf '%s\n' "$VALIDATION_OUTPUT" >&2
  exit 1
fi

curl --fail-with-body \
  -X POST \
  -H "Authorization: AccessToken $DISTR_API_TOKEN" \
  "$DISTR_SERVER_URL/api/v1/release-bundles/$RELEASE_BUNDLE_ID/publish"
```
