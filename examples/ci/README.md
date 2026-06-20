# CI Release Examples

These examples show how CI systems can create, validate, and publish Release Bundles through the public API.

Required inputs:

- `DISTR_SERVER_URL`
- `DISTR_API_TOKEN`
- `DISTR_APPLICATION_ID`
- `DISTR_CHANNEL_ID`
- an immutable artifact digest such as `sha256:<64 hex characters>`

The examples use placeholders only. Keep registry credentials, API tokens, environment dumps, and authorization headers out of Release Bundle source metadata.

Examples:

- `curl/create-validate-publish.sh`
- `github-actions/release.yml`
- `gitlab-ci/.gitlab-ci.yml`
- `jenkins/Jenkinsfile`
