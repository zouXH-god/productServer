# Product Server 使用文档

本文介绍 Product Server 的安装、配置、服务管理、HTTP API 和 CI Action 使用方式。

## 安装方法

### 使用 Release 二进制（推荐）

Release 中已经包含嵌入前端页面的完整可执行文件，不需要安装 Go、Node.js 或 npm。进入 [GitHub Releases](https://github.com/zouXH-god/productServer/releases/latest)，根据系统和 CPU 架构下载：

- Linux：`productserver-linux-amd64.tar.gz` 或 `productserver-linux-arm64.tar.gz`
- Windows：`productserver-windows-amd64.zip` 或 `productserver-windows-arm64.zip`
- macOS：`productserver-darwin-amd64.tar.gz` 或 `productserver-darwin-arm64.tar.gz`

Linux amd64 示例：

```bash
mkdir -p productserver && cd productserver
curl -LO https://github.com/zouXH-god/productServer/releases/latest/download/productserver-linux-amd64.tar.gz
tar -xzf productserver-linux-amd64.tar.gz
mv productserver-linux-amd64 productserver
chmod +x productserver
```

创建 `.env`，至少设置以下内容：

```dotenv
HTTP_ADDR=:8080
DB_DRIVER=sqlite
DB_DSN=data/productserver.db
STORAGE_DIR=data/artifacts
JWT_SECRET=replace-with-a-long-random-secret
ADMIN_USERNAME=admin
ADMIN_PASSWORD=replace-with-a-strong-password
SECRET_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef
```

分别启动 HTTP 服务和 Worker：

```bash
./productserver server
./productserver worker
```

不传子命令时默认启动 `server`。生产环境必须同时运行至少一个 Server 和一个 Worker；Server 负责 API、前端、调度与事件分发，Worker 负责领取并执行工作流任务。多个实例可共用同一 MySQL 或 PostgreSQL 数据库，生产环境不建议多机共享 SQLite。

### Docker Compose 部署

如果希望使用容器运行，仓库提供三套 Compose 配置，均会启动一个 Server 和一个 Worker。Compose 部署建议先使用 SQLite 版本。

#### SQLite 快速版

不需要额外数据库，应用数据、产物、日志和工作目录保存在 Docker 命名卷中：

```bash
docker compose -f deploy/docker/compose.sqlite.yml up -d --build
```

打开 <http://localhost:8080>，默认账号为 `admin` / `admin123456`。SQLite 已启用 WAL 和 busy timeout，适合单机及轻量使用。

#### 内置 PostgreSQL 版

该版本同时启动 PostgreSQL 16，适合希望直接体验 PostgreSQL 或准备运行多个 Worker 的环境：

```bash
docker compose -f deploy/docker/compose.postgres.yml up -d --build
```

可以在项目根目录创建 `.env` 覆盖默认值：

```dotenv
PRODUCTSERVER_PORT=8080
POSTGRES_USER=productserver
POSTGRES_PASSWORD=replace-with-a-strong-password
POSTGRES_DB=productserver
JWT_SECRET=replace-with-a-long-random-secret
ADMIN_USERNAME=admin
ADMIN_PASSWORD=replace-with-a-strong-password
SECRET_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef
```

#### 外置数据库版

外置版不会创建数据库容器，支持 MySQL、PostgreSQL 或兼容服务。以下变量必须提前配置：

```dotenv
DB_DRIVER=postgres
DB_DSN=host=database.example.com user=productserver password=strong-password dbname=productserver port=5432 sslmode=require TimeZone=Asia/Shanghai
JWT_SECRET=replace-with-a-long-random-secret
ADMIN_PASSWORD=replace-with-a-strong-password
SECRET_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef
```

启动服务：

```bash
docker compose -f deploy/docker/compose.external.yml up -d --build
```

数据库地址必须能从容器内部访问。数据库运行在宿主机时，Docker Desktop 通常可以使用 `host.docker.internal`，Linux 环境应使用宿主机可达地址或自行配置网络。

三种版本都支持以下常用命令：

```bash
# 查看状态和日志
docker compose -f deploy/docker/compose.sqlite.yml ps
docker compose -f deploy/docker/compose.sqlite.yml logs -f server worker

# 停止服务但保留数据
docker compose -f deploy/docker/compose.sqlite.yml down

# 停止并删除命名卷中的全部数据（不可恢复）
docker compose -f deploy/docker/compose.sqlite.yml down -v
```

生产环境务必通过 `.env` 设置强随机 `JWT_SECRET`、管理员密码和恰好 32 字节的 `SECRET_ENCRYPTION_KEY`，并在反向代理层启用 HTTPS。命名卷 `productserver-data` 包含产物、日志和任务工作目录，需要纳入备份。

### 从源码构建

需要 Go 1.22+、Node.js 20+ 和 npm。

```bash
git clone https://gitea.s1f.ren/shiran/productServer.git
cd productServer
cd frontend
npm ci
npm test
npm run build
cd ..
go test ./...
go build -trimpath -o productserver .
```

前端构建结果输出到 `internal/server/frontend/dist`，随后由 Go 编译器嵌入单一二进制。Windows 也可以运行 `build.ps1`，Linux/macOS 可以运行 `build.sh`。

### 首次启动

应用会自动迁移数据库。仅当用户表为空时，使用 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD` 创建初始系统管理员。浏览器打开 `http://服务器地址:端口/` 登录。

## 环境变量说明

程序启动时读取当前工作目录的 `.env`，系统中已经存在的同名环境变量优先。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | HTTP 监听地址，例如 `127.0.0.1:8080`。 |
| `DB_DRIVER` | `sqlite` | 数据库类型：`sqlite`、`mysql`、`postgres` 或 `pgsql`。 |
| `DB_DSN` | `productserver.db` | 数据库连接串。 |
| `STORAGE_DIR` | `data/artifacts` | 发布产物存储目录。 |
| `JWT_SECRET` | `change-me-in-production` | JWT 签名密钥，生产环境必须使用高强度随机值。 |
| `JWT_EXPIRY` | `24h` | 登录 Token 有效期，使用 Go duration 格式。 |
| `MAX_FILE_BYTES` | `1073741824` | 单文件上传上限，单位字节。 |
| `MAX_BATCH_BYTES` | `2147483648` | 单批上传上限，必须大于等于单文件上限。 |
| `ADMIN_USERNAME` | `admin` | 首次启动管理员用户名。 |
| `ADMIN_PASSWORD` | `admin123456` | 首次启动管理员密码，生产环境必须修改。 |
| `SECRET_ENCRYPTION_KEY` | 空 | 敏感数据 AES-GCM 主密钥，必须为 32 字节；缺失时不能创建或执行敏感配置。 |
| `WORKER_GLOBAL_CONCURRENCY` | `4` | 全局工作流节点执行槽数量。 |
| `WORKER_LEASE_DURATION` | `30s` | Worker 任务租约时长。 |
| `WORKER_HEARTBEAT_INTERVAL` | `10s` | Worker 心跳间隔，应小于租约时长。 |
| `WORKFLOW_LOG_DIR` | `data/logs` | 工作流 JSONL 日志目录。 |
| `WORKFLOW_WORK_DIR` | `data/work` | 工作流临时工作目录。 |
| `FAILED_WORKSPACE_RETENTION` | `72h` | 失败或取消任务工作目录保留时间。 |
| `WEBHOOK_ALLOWLIST` | 空 | HTTP 回调允许的域名或 CIDR，逗号分隔；为空时拒绝全部目标。 |
| `AI_MAX_CONTEXT_CHARS` | `200000` | AI 会话发送给模型的最大上下文字符数。 |
| `AI_MAX_TOOL_ROUNDS` | `12` | 单轮 AI 自动编排最大工具调用轮数。 |
| `AI_REQUEST_TIMEOUT` | `120s` | AI 接口请求超时。 |
| `AI_MAX_RESPONSE_BYTES` | `8388608` | AI 单次响应最大字节数。 |

数据库 DSN 示例：

```dotenv
# SQLite
DB_DRIVER=sqlite
DB_DSN=data/productserver.db

# MySQL
DB_DRIVER=mysql
DB_DSN=productserver:password@tcp(127.0.0.1:3306)/productserver?charset=utf8mb4&parseTime=True&loc=Local

# PostgreSQL
DB_DRIVER=postgres
DB_DSN=host=127.0.0.1 user=productserver password=password dbname=productserver port=5432 sslmode=disable TimeZone=Asia/Shanghai
```

## systemd 示例

以下示例假设程序位于 `/opt/productserver`，使用独立用户 `productserver`。先准备目录：

```bash
sudo useradd --system --home /opt/productserver --shell /usr/sbin/nologin productserver
sudo mkdir -p /opt/productserver/data/{artifacts,logs,work}
sudo chown -R productserver:productserver /opt/productserver
sudo chmod 600 /opt/productserver/.env
```

Server 服务 `/etc/systemd/system/productserver.service`：

```ini
[Unit]
Description=Product Server HTTP Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=productserver
Group=productserver
WorkingDirectory=/opt/productserver
EnvironmentFile=/opt/productserver/.env
ExecStart=/opt/productserver/productserver server
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

Worker 服务 `/etc/systemd/system/productserver-worker.service`：

```ini
[Unit]
Description=Product Server Workflow Worker
After=network-online.target productserver.service
Wants=network-online.target

[Service]
Type=simple
User=productserver
Group=productserver
WorkingDirectory=/opt/productserver
EnvironmentFile=/opt/productserver/.env
ExecStart=/opt/productserver/productserver worker
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

加载并启动：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now productserver productserver-worker
sudo systemctl status productserver productserver-worker
```

## API 列表

除特别说明外，接口位于 `/api` 下并使用 `Authorization: Bearer <JWT>`。错误统一返回 `{"error":{"code":"...","message":"..."}}`。

### 公共接口与项目 Token

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/healthz` | 进程存活检查。 |
| `GET` | `/readyz` | 数据库和存储就绪检查。 |
| `POST` | `/api/auth/login` | 用户登录。 |
| `POST` | `/api/auth/register` | 用户注册，受系统注册开关控制。 |
| `POST` | `/api/upload` | 使用项目 Token Bearer 上传产物。 |
| `GET` | `/api/download?token=...&version=...&file=...` | 使用项目 Token 下载；`version` 默认 `latest`，`file` 缺省时选择唯一上传压缩包。 |

### 用户、管理与概览

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/auth/me` | 当前用户。 |
| `PUT` | `/api/auth/profile` | 修改个人资料。 |
| `POST` | `/api/auth/password` | 修改密码并撤销旧 JWT。 |
| `GET/POST` | `/api/admin/users` | 管理员查看或创建用户。 |
| `PUT` | `/api/admin/users/:userId` | 修改用户状态和管理员权限。 |
| `POST` | `/api/admin/users/:userId/reset-password` | 重置用户密码。 |
| `GET/PUT` | `/api/admin/settings` | 获取或修改系统设置。 |
| `GET` | `/api/dashboard` | 仪表盘聚合数据。 |
| `GET` | `/api/runs` | 当前用户可访问的全部运行记录。 |

### 项目、成员、Token 与发布

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET/POST` | `/api/projects` | 项目列表或创建项目。 |
| `GET/PUT/DELETE` | `/api/projects/:id` | 项目详情、更新或删除。 |
| `GET/POST` | `/api/projects/:id/members` | 成员列表或添加成员。 |
| `PUT/DELETE` | `/api/projects/:id/members/:userId` | 修改角色或移除成员。 |
| `POST` | `/api/projects/:id/transfer-owner` | 转移所有权。 |
| `GET` | `/api/projects/:id/token` | 获取唯一 Token 的脱敏信息。 |
| `POST` | `/api/projects/:id/token/rotate` | 轮换 Token，仅本次返回明文。 |
| `GET/POST` | `/api/projects/:id/tokens` | 旧版兼容 Token 列表；创建等价于轮换。 |
| `DELETE` | `/api/projects/:id/tokens/:tokenId` | 旧版兼容 Token 禁用接口。 |
| `GET` | `/api/projects/:id/releases` | 发布版本列表。 |
| `GET` | `/api/projects/:id/releases/:releaseId` | 发布及文件详情。 |
| `GET` | `/api/projects/:id/releases/:releaseId/accesses` | 产物访问记录。 |
| `GET` | `/api/projects/:id/releases/:releaseId/files/:fileId/download` | JWT 下载产物。 |

### 环境变量、SSH 与 AI

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET/POST` | `/api/environment-variables` | 用户全局环境变量列表或创建。 |
| `PUT/DELETE` | `/api/environment-variables/:variableId` | 修改或删除全局变量。 |
| `GET/POST` | `/api/projects/:id/environment-variables` | 项目变量列表或创建。 |
| `GET` | `/api/projects/:id/environment-variables/available` | 工作流可用变量合并视图。 |
| `PUT/DELETE` | `/api/projects/:id/environment-variables/:variableId` | 修改或删除项目变量。 |
| `GET/POST` | `/api/ssh-credentials` | SSH 凭据列表或创建。 |
| `DELETE` | `/api/ssh-credentials/:credentialId` | 删除 SSH 凭据。 |
| `GET/POST` | `/api/ssh-connections` | SSH 连接列表或创建。 |
| `PUT/DELETE` | `/api/ssh-connections/:connectionId` | 修改或删除连接。 |
| `POST` | `/api/ssh-connections/:connectionId/test` | 测试连接。 |
| `GET/POST` | `/api/ai/providers` | AI 模型配置列表或创建。 |
| `PUT/DELETE` | `/api/ai/providers/:providerId` | 修改或删除模型配置。 |
| `POST` | `/api/ai/providers/:providerId/test` | 测试模型接口。 |
| `GET/POST` | `/api/projects/:id/ai/conversations` | AI 会话列表或创建。 |
| `GET/PATCH/DELETE` | `/api/projects/:id/ai/conversations/:conversationId` | 会话详情、重命名或删除。 |
| `POST` | `/api/projects/:id/ai/conversations/:conversationId/messages` | 发送消息并启动自动编排。 |
| `POST` | `/api/projects/:id/ai/conversations/:conversationId/stop` | 停止当前生成任务。 |
| `GET` | `/api/projects/:id/ai/conversations/:conversationId/stream` | AI 文本及画布操作 SSE 流。 |

### 工作流、计划与运行

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET/POST` | `/api/projects/:id/workflows` | 工作流列表或创建。 |
| `GET/PUT/DELETE` | `/api/projects/:id/workflows/:workflowId` | 工作流读取、保存或删除。 |
| `GET` | `/api/projects/:id/workflows/:workflowId/history` | 保存历史列表。 |
| `GET` | `/api/projects/:id/workflows/:workflowId/history/:revisionId` | 历史版本详情。 |
| `POST` | `/api/projects/:id/workflows/:workflowId/copy` | 复制到目标项目。 |
| `POST` | `/api/projects/:id/workflows/:workflowId/runs` | 手动创建运行。 |
| `GET/PUT/DELETE` | `/api/projects/:id/workflows/:workflowId/schedule` | 定时计划读取、保存或删除。 |
| `GET` | `/api/projects/:id/workflows/:workflowId/schedule-events` | 计划触发记录。 |
| `POST` | `/api/projects/:id/workflows/:workflowId/schedule/trigger` | 立即测试计划。 |
| `GET` | `/api/projects/:id/runs` | 项目运行列表。 |
| `GET` | `/api/projects/:id/runs/:runId` | 运行、节点和输出详情。 |
| `GET` | `/api/projects/:id/runs/:runId/logs` | 历史日志。 |
| `GET` | `/api/projects/:id/runs/:runId/logs/stream` | SSE 实时日志。 |
| `POST` | `/api/projects/:id/runs/:runId/cancel` | 请求取消运行。 |

## Action 调用说明

Action 会将匹配的文件确定性打包为一个 ZIP，并以 multipart 流式上传。Tag 构建使用 Tag 名作为版本；其他构建使用完整 commit SHA；也可以显式传入 `version`。

### Gitea Actions

```yaml
- name: Upload artifact
  uses: https://gitea.s1f.ren/shiran/product-server-action@v1.2
  with:
    url: ${{ vars.ARTIFACT_SERVER_URL }}
    token: ${{ vars.ARTIFACT_SERVER_TOKEN }}
    path: |
      dist/**
      checksums.txt
    name: web-build
```

### GitHub Actions

```yaml
- name: Upload artifact
  uses: zouXH-god/product-server-action@v1.2
  with:
    url: ${{ vars.ARTIFACT_SERVER_URL }}
    token: ${{ secrets.ARTIFACT_SERVER_TOKEN }}
    path: |
      dist/**
      checksums.txt
    name: web-build
```

输入参数：

| 参数 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `url` | 是 | 无 | Product Server 根 URL，末尾 `/` 会自动移除。 |
| `token` | 是 | 无 | 目标产物项目的唯一 Token。 |
| `path` | 是 | 无 | 换行分隔的文件、目录或 glob。 |
| `name` | 否 | `artifact.zip` | 上传的 ZIP 文件名，缺少 `.zip` 时自动补充。 |
| `version` | 否 | Tag 或完整 SHA | 手动覆盖发布版本。 |

输出参数包括 `version`、`branch`、`file`、`sha256` 和不含 Token 的 `download-url`。上传成功后，产物项目会保留原始 ZIP，并在独立目录中提供自动解压后的文件树。生产环境应使用 HTTPS；通过查询参数下载时，项目 Token 可能出现在浏览器历史或代理日志中。

下载示例：

```bash
curl -fL "https://product.example.com/api/download?token=ps_xxx&version=latest&file=artifact.zip" -o artifact.zip
```
