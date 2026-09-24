# Product Server

> Turn CI artifact management and automated deployment into one visible pipeline.

[简体中文](README.md) · [English](README_EN.md)

[![Release](https://img.shields.io/github/v/release/zouXH-god/productServer?display_name=tag&sort=semver)](https://github.com/zouXH-god/productServer/releases)
[![GitHub Release](https://github.com/zouXH-god/productServer/actions/workflows/release.yaml/badge.svg)](https://github.com/zouXH-god/productServer/actions/workflows/release.yaml)
[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white)](https://vuejs.org/)
[![License](https://img.shields.io/badge/license-MIT-black.svg)](LICENSE)

Product Server is a self-hosted CI artifact management and visual deployment platform. It receives build outputs from GitHub, Gitea, or any other CI system, keeps every release version, and deploys them through visual workflows containing uploads, extraction, checks, SSH commands, webhooks, and multi-server jobs.

Instead of scattering deployment scripts across repositories, upload an artifact once and configure, run, inspect, and reuse the rest of the release process in one place.

## Screenshots

### Projects and artifacts

![Projects and artifacts](images/projuct.png)

### Visual workflow editor

![Visual workflow editor](images/worker.png)

## Highlights

- **Centralized CI artifacts** — organize build outputs by project, release, branch, and file, with access metrics, storage quotas, and automatic cleanup.
- **Visual deployment workflows** — compose archive, extract, SFTP, SSH, webhook, condition, loop, and multi-server modules on a DAG canvas.
- **Upload-triggered automation** — match any release, tags, commits, tag globs, or branch rules.
- **Multi-server execution** — run the same workflow segment sequentially or in parallel across multiple servers while retaining individual status, logs, and outputs.
- **Recoverable workers** — database-backed leases and heartbeats allow another Worker to recover jobs after a process failure.
- **Action-style run view** — inspect node states, execution order, live logs, structured outputs, and completed runs.
- **Complete access control** — registration, system administrators, and project owner, admin, developer, and viewer roles.
- **Scheduled workflows** — create Cron-based projects in addition to artifact-triggered automation.
- **AI-assisted orchestration** — connect an OpenAI Chat Completions-compatible provider to generate and improve workflow canvases through conversation.
- **Lightweight deployment** — the Vue frontend is embedded in one Go binary, with SQLite, MySQL, and PostgreSQL support.

## Use cases

- Store artifacts produced by GitHub Actions, Gitea Actions, or another CI service.
- Deploy a frontend, Go service, installer, or release bundle to one or many servers.
- Replace duplicated SSH and SFTP deployment scripts across repositories.
- Manage test environments, internal services, and private software releases.
- Build a small self-hosted release center without adopting a large DevOps platform.

## Quick start

### Download a Release binary (recommended)

The frontend is embedded in the executable, so Go, Node.js, and a separate static deployment are not required. Download the archive for your platform and architecture from [GitHub Releases](https://github.com/zouXH-god/productServer/releases/latest).

Linux amd64 example:

```bash
curl -LO https://github.com/zouXH-god/productServer/releases/latest/download/productserver-linux-amd64.tar.gz
tar -xzf productserver-linux-amd64.tar.gz
mv productserver-linux-amd64 productserver
chmod +x productserver
./productserver server
```

Start a Worker in another terminal:

```bash
./productserver worker
```

Open <http://localhost:8080> and sign in with `admin` / `admin123456`. Create an `.env` file and replace the default credentials and secrets before production use.

### Docker Compose

For a containerized setup, start with the SQLite edition, which requires no separate database:

```bash
git clone https://github.com/zouXH-god/productServer.git
cd productServer
docker compose -f deploy/docker/compose.sqlite.yml up -d --build
```

Compose editions with a bundled PostgreSQL database or an external database are also included. See the [Docker Compose deployment guide](document.md#docker-compose-部署) for details.

### Run from source

You need Go 1.22+, Node.js 20+, and npm.

```bash
git clone https://github.com/zouXH-god/productServer.git
cd productServer
cp .env.example .env

cd frontend
npm ci
npm run build
cd ..

go build -o productserver .
```

On Windows, start all development services with:

```bat
start.bat
```

> See the [complete usage guide](document.md) for binary installation, databases, environment variables, systemd services, APIs, and Action configuration. The detailed guide is currently written in Chinese.

## How it works

```text
GitHub / Gitea / CI
          │
          │ upload with a project token
          ▼
   Product Server
   ├─ releases and files
   ├─ durable trigger events
   ├─ visual workflows
   └─ live logs and run history
          │
          │ workers claim jobs
          ▼
  SSH / SFTP / Webhooks / multiple servers
```

The Server provides the API, management UI, triggers, and scheduler. Workers execute workflow jobs. They coordinate through the database and can be deployed or scaled independently. SQLite is suitable for a single-machine setup; MySQL or PostgreSQL is recommended for multi-instance production deployments.

## Upload with the Action

The standalone [Product Server Action](https://github.com/zouXH-god/product-server-action) creates a deterministic ZIP from matching paths, detects the tag, commit SHA, and branch, and uploads it to a target project.

```yaml
- name: Upload artifact
  uses: https://github.com/zouXH-god/product-server-action@v1.2
  with:
    url: ${{ secrets.ARTIFACT_SERVER_URL }}
    token: ${{ secrets.ARTIFACT_SERVER_TOKEN }}
    path: |
      dist/**
      checksums.txt
    name: web-build
```

The Action outputs the resolved version, branch, file name, SHA-256 digest, and a download URL without the token. Retrying the same version with identical contents is idempotent.

## Upload and download directly

Any CI service can upload with multipart form data:

```bash
curl -X POST https://product.example.com/api/upload \
  -H "Authorization: Bearer ps_your_project_token" \
  -F "version=v1.2.3" \
  -F "commit_sha=$CI_COMMIT_SHA" \
  -F "branch=$CI_COMMIT_BRANCH" \
  -F "files=@dist/app.zip"
```

Download the only original archive from the latest release:

```bash
curl -fL "https://product.example.com/api/download?token=ps_your_project_token" -o artifact.zip
```

You can also specify `version` and `file`. Always use HTTPS in production, and remember that a token in the query string may be recorded in browser history or proxy logs.

## Technology

- Backend: Go, Gin, GORM
- Frontend: Vue 3, TypeScript, Vite, Vue Flow
- Databases: SQLite (pure Go), MySQL, PostgreSQL
- Workflow runtime: durable DAGs, leases, heartbeats, execution slots, SSE live logs
- Security: bcrypt, JWT, AES-256-GCM encryption for secrets

## Build and test

```bash
cd frontend && npm ci && npm test && npm run build && cd ..
go test ./...
go build -trimpath -o productserver .
```

You can also use `build.ps1` or `build.sh`. The frontend assets are embedded in the final binary, so the frontend source directory is not required at runtime. Liveness and readiness endpoints are available at `/healthz` and `/readyz`.

## Documentation and contributing

- [Complete usage guide](document.md)
- [Environment example](.env.example)
- [Product Server Action](https://github.com/zouXH-god/product-server-action)
- [GitHub mirror and releases](https://github.com/zouXH-god/productServer)

Issues, feature requests, and pull requests are welcome. If Product Server is useful to you, consider starring the repository so more developers looking for lightweight CI artifact management and deployment automation can find it.

## Security note

SSH host key verification is disabled in the current initial implementation and is vulnerable to man-in-the-middle attacks. Use SSH modules only on trusted networks. Configure `SECRET_ENCRYPTION_KEY`, HTTPS, strong random secrets, and a dedicated database account in production.

## License

Product Server is available under the [MIT License](LICENSE).
