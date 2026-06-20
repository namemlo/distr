# Distr Release CLI

The `distr release` commands create, validate, and publish Release Bundles through the public API.

## Configuration

```bash
export DISTR_SERVER_URL="https://distr.example.invalid"
export DISTR_API_TOKEN="distr-api-token"
```

Flags override environment variables:

```bash
distr release create \
  --server "https://distr.example.invalid" \
  --token "$DISTR_API_TOKEN" \
  --file release-bundle.json \
  --idempotency-key "$CI_RUN_ID"
```

The CLI accepts tokens as either a raw Distr personal access token or a complete authorization value beginning with `AccessToken ` or `Bearer `. Tokens and authorization headers are not printed by the CLI.

## Commands

Create a draft Release Bundle:

```bash
distr release create --file release-bundle.json --idempotency-key "$CI_RUN_ID"
```

Read the request from standard input:

```bash
cat release-bundle.json | distr release create --file -
```

Validate a Release Bundle:

```bash
distr release validate "00000000-0000-0000-0000-000000000000"
```

Publish a Release Bundle after validation succeeds:

```bash
distr release publish "00000000-0000-0000-0000-000000000000"
```

Use JSON output for automation:

```bash
distr release create --file release-bundle.json --output json
```

## Exit Codes

- `0`: success.
- `2`: local usage, request construction, input, or 4xx request error.
- `3`: validation completed and the server reported the Release Bundle invalid.
- `4`: authentication or authorization failure.
- `5`: network, API, response parsing, or server failure.

## Idempotency

`distr release create` can send an `Idempotency-Key` header:

```bash
distr release create --file release-bundle.json --idempotency-key "$CI_RUN_ID"
```

The server scopes idempotency keys to the authenticated organization. Reusing the same key with the same canonical request returns the original Release Bundle. Reusing the same key with a different canonical request returns a structured `409 Conflict`.
