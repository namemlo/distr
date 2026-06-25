# Server Docker Compose Deployment With AWS ECR

This guide deploys the community Distr Hub from this source tree to one Linux server with Docker Compose.
The Hub image is built from source, pushed to a private AWS ECR repository, then pulled by Docker Compose on the server.

For production releases, Jenkins builds an immutable image, pushes it to ECR, resolves the pushed image digest, and the
server deploys that exact `repository@sha256:...` image reference. The helper script also supports building directly on
the server for the first controlled deployment.

## What This Package Adds

Files:

- `deploy/server-docker-compose/deploy.sh` - build, ECR login, push, pull, backup, migrate, start, health-check, and rollback helper.
- `deploy/server-docker-compose/docker-compose.yml` - production-oriented Compose stack that runs the configured ECR image.
- `deploy/server-docker-compose/.env.example` - environment template with no real secrets.
- `deploy/server-docker-compose/nginx.example.conf` - reverse-proxy example for Hub and registry hostnames.
- `deploy/server-docker-compose/nginx.single-domain.example.conf` - reverse-proxy example when one hostname serves both Hub and `/v2/` registry traffic.

The package intentionally does not modify `deploy/docker/docker-compose.yaml`, which is the upstream-style sample that
uses the published `ghcr.io/distr-sh/distr-ce:2.24.1` image.

## Server Requirements

Install these on the server:

- Linux server with at least 2 CPU, 4 GB RAM, and enough disk for PostgreSQL, images, logs, and registry artifacts.
- Docker Engine with the `docker compose` plugin.
- AWS CLI v2 authenticated with permission to pull from ECR.
- Git.
- `curl`, `openssl`, `bash`, and `flock`.
- `mise`, only if the server will build from source. If Jenkins builds and pushes to ECR, the server does not need the build toolchain.

Recommended firewall:

- Allow inbound `22`, `80`, and `443`.
- Do not expose PostgreSQL, RustFS, `8080`, `8585`, or metrics ports directly.

Minimum ECR permissions:

- Jenkins build/push identity: `ecr:GetAuthorizationToken`, `ecr:DescribeRepositories`, `ecr:DescribeImages`,
  `ecr:BatchCheckLayerAvailability`, `ecr:InitiateLayerUpload`, `ecr:UploadLayerPart`, `ecr:CompleteLayerUpload`,
  and `ecr:PutImage`.
- Add `ecr:CreateRepository` only if you want `deploy.sh ecr-create-repo` to create the repository.
- Server pull identity: `ecr:GetAuthorizationToken`, `ecr:BatchGetImage`, `ecr:GetDownloadUrlForLayer`, and
  `ecr:DescribeImages` if rollback by tag is allowed.

## Server Setup

Clone the fork on the production server so the server has the Compose file and release helper:

```bash
git clone https://github.com/namemlo/distr.git /opt/distr
cd /opt/distr
git checkout 236a463d
```

Create the production environment file:

```bash
./deploy/server-docker-compose/deploy.sh init
```

Edit `deploy/server-docker-compose/.env`:

```bash
nano deploy/server-docker-compose/.env
```

Set at least:

- `AWS_REGION=ap-southeast-1`
- `ECR_REPOSITORY=distr-community`
- `DISTR_IMAGE=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community`
- `DISTR_IMAGE_TAG=<immutable-release-tag-or-git-sha>` from Jenkins; do not use `latest`.
- `DISTR_IMAGE_REF=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community@sha256:<image-digest>` from Jenkins.
- `DISTR_HOST=https://distr.example.com`
- `REGISTRY_HOST=registry.example.com`
- `REGISTRATION=enabled` for first admin setup, then change to `hidden` or `disabled`.
- SMTP/SES settings before setting `USER_EMAIL_VERIFICATION_REQUIRED=true`.
- Keep the local RustFS settings for the first deploy, or adapt the Compose file before using external S3.

Confirm AWS auth before deploy:

```bash
aws sts get-caller-identity
./deploy/server-docker-compose/deploy.sh ecr-login
```

Do not put production runtime secrets in Jenkins. Keep the full `.env` on the server.

## Jenkins Build And Server Deploy

Use this as the normal deployment path for our infrastructure: Jenkins builds the image and pushes it to ECR, while the
server only pulls and runs it.

The existing `examples/ci/jenkins/Jenkinsfile` demonstrates Distr release API publishing. This section is separate: it
builds and publishes the Distr Hub deployment image itself.

On the Jenkins agent, create a minimal `deploy/server-docker-compose/.env` for the image job only:

```bash
cat > deploy/server-docker-compose/.env <<EOF
AWS_REGION=ap-southeast-1
ECR_REPOSITORY=distr-community
DISTR_IMAGE=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community
DISTR_IMAGE_TAG=$(git rev-parse --short=12 HEAD)
EOF

./deploy/server-docker-compose/deploy.sh image-check
./deploy/server-docker-compose/deploy.sh ecr-create-repo
./deploy/server-docker-compose/deploy.sh build
./deploy/server-docker-compose/deploy.sh push
```

Jenkins does not need the production database password, JWT secret, domain settings, or RustFS secrets for `build` and
`push`. After `push`, archive `dist/release-${DISTR_IMAGE_TAG}.env`; it contains the non-secret ECR digest reference
that the server should deploy.

On the server, copy the `DISTR_IMAGE_REF` value from Jenkins' release env artifact into the full production `.env`, then
run:

```bash
./deploy/server-docker-compose/deploy.sh check
./deploy/server-docker-compose/deploy.sh release
```

`release` locks deployment, validates Compose, pulls the digest-pinned image from ECR, starts dependencies, backs up,
stops Hub, migrates, starts Hub, and runs the health check.

## Optional Server Build

Use this only for a controlled first deployment or emergency case where Jenkins is not available. It builds on the server,
pushes to ECR, resolves the digest, and then releases the same digest-pinned image:

```bash
./deploy/server-docker-compose/deploy.sh deploy
```

That optional command does this:

1. Installs pinned build tools with `mise install`.
2. Builds the community frontend and Hub from source.
3. Copies `dist/distr` to the architecture-specific name required by `Dockerfile.hub`.
4. Builds the Docker image tagged as `DISTR_IMAGE:DISTR_IMAGE_TAG`.
5. Logs in to AWS ECR and pushes the image.
6. Resolves the pushed tag to an ECR digest and writes `dist/release-${DISTR_IMAGE_TAG}.env`.
7. Updates `DISTR_IMAGE_REF` to the digest-pinned reference.
8. Validates the Docker Compose config.
9. Pulls the digest-pinned image from ECR for Compose.
10. Starts PostgreSQL and RustFS.
11. Creates a PostgreSQL backup and a RustFS volume backup when data already exists.
12. Stops Hub before migration.
13. Runs `distr migrate` explicitly.
14. Starts Hub with `serve --migrate=false`.
15. Waits for `http://127.0.0.1:${DISTR_HTTP_PORT}/ready`.

## Reverse Proxy

The Compose file binds Hub and registry to localhost only:

```text
127.0.0.1:8080 -> Hub
127.0.0.1:8585 -> built-in OCI registry
```

For the current `distr.emlotech.com` setup, use one Nginx UI host with:

- `/v2/` -> `http://distr-hub:8585`
- `/` -> `http://distr-hub:8080`

`distr-hub` is a Docker network alias. Set `NGINX_UI_NETWORK=services` so the Hub joins the same Docker network as
the existing `nginx-ui` container.

Use Nginx, Caddy, Traefik, or another TLS proxy in front. For Nginx, start from:

```bash
sudo cp deploy/server-docker-compose/nginx.example.conf /etc/nginx/sites-available/distr.conf
sudo ln -s /etc/nginx/sites-available/distr.conf /etc/nginx/sites-enabled/distr.conf
sudo nginx -t
sudo systemctl reload nginx
```

Update certificates and hostnames before reloading. Enable HSTS only after HTTPS is verified.

## Day-2 Commands

Log in to ECR:

```bash
./deploy/server-docker-compose/deploy.sh ecr-login
```

Pull the configured ECR image:

```bash
./deploy/server-docker-compose/deploy.sh pull
```

Validate Compose config:

```bash
./deploy/server-docker-compose/deploy.sh config
```

Run the server-side release flow after Jenkins has published the digest and `DISTR_IMAGE_REF` is updated:

```bash
./deploy/server-docker-compose/deploy.sh release
```

Check service status:

```bash
./deploy/server-docker-compose/deploy.sh ps
```

Follow logs:

```bash
./deploy/server-docker-compose/deploy.sh logs hub
```

Run a manual health check:

```bash
./deploy/server-docker-compose/deploy.sh health
```

Run only backup:

```bash
sudo BACKUP_DIR=/var/backups/distr ./deploy/server-docker-compose/deploy.sh backup
```

Run only migrations:

```bash
./deploy/server-docker-compose/deploy.sh migrate
```

Run artifact cleanup once:

```bash
./deploy/server-docker-compose/deploy.sh cleanup-artifacts
```

## Upgrade

For a later commit or tag, Jenkins should checkout the reviewed source and run the Jenkins build/push flow again:

```bash
git fetch --all --tags
git checkout <new-tag-or-commit>
./deploy/server-docker-compose/deploy.sh image-check
./deploy/server-docker-compose/deploy.sh build
./deploy/server-docker-compose/deploy.sh push
```

On the server, edit `deploy/server-docker-compose/.env` and set the new immutable tag and digest reference published by
Jenkins:

```text
DISTR_IMAGE_TAG=2026-06-24-rc1
DISTR_IMAGE_REF=123456789012.dkr.ecr.ap-southeast-1.amazonaws.com/distr-community@sha256:<image-digest>
```

```bash
./deploy/server-docker-compose/deploy.sh release
```

The script backs up before migrations. Keep the backup files and the previous ECR image digest until the release is accepted.

If Jenkins is unavailable and you must build directly on the server, use the optional `deploy` command instead.

## Rollback

Application-only rollback to a previous ECR digest reference:

```bash
./deploy/server-docker-compose/deploy.sh rollback <previous-image-ref>
```

This updates `DISTR_IMAGE_REF`, pulls the previous image from ECR, restarts Hub, and runs the health check.
Use it only when the new database schema is backward-compatible with the previous binary.

If a migration is incompatible:

1. Stop Hub.
2. Restore the pre-upgrade PostgreSQL backup.
3. Restore RustFS or external object storage if the release changed artifact data.
4. Start the previous image digest.
5. Run health checks and a smoke test.

Do not automate `distr migrate --down`; the command explicitly warns that it purges the database.

## Backup Notes

The script writes PostgreSQL custom-format dumps and RustFS volume tarballs to `deploy/server-docker-compose/backups` by default.
Set `BACKUP_DIR=/var/backups/distr` when you want root-owned server backups instead.

Database backup alone is not enough if you use the local RustFS registry. Back up both PostgreSQL and object storage,
or use provider snapshots. External S3-compatible storage with versioning is safer than keeping database and artifacts
on the same physical server.

## Security Notes

- `.env` stores plaintext secrets; keep it mode `0600`, owned by the deployment user, and out of Git.
- Prefer an EC2 instance role, ECS task role, or Jenkins/OIDC role over long-lived AWS access keys on the server.
- Keep the ECR repository private and deploy digest-pinned image references. The helper refuses `DISTR_IMAGE_TAG=latest`.
- Replace all generated local secrets before production if your organization requires centrally managed credentials.
- Use external S3 and managed PostgreSQL for stronger durability; this starter Compose stack uses local RustFS by default.
- The included RustFS image is beta; evaluate it before production.
- Building on the server is less controlled than building in Jenkins and deploying by digest.
- `USER_EMAIL_VERIFICATION_REQUIRED=false` is acceptable only until mail is configured.
- Keep `REGISTRATION=enabled` only long enough to create the first admin account.
