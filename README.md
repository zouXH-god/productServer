# Product Server

> 把 CI 产物管理和自动部署，变成一条看得见的流水线。

[简体中文](README.md) · [English](README_EN.md)

[![Release](https://img.shields.io/github/v/release/zouXH-god/productServer?display_name=tag&sort=semver)](https://github.com/zouXH-god/productServer/releases)
[![GitHub Release](https://github.com/zouXH-god/productServer/actions/workflows/release.yaml/badge.svg)](https://github.com/zouXH-god/productServer/actions/workflows/release.yaml)
[![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white)](https://vuejs.org/)
[![License](https://img.shields.io/badge/license-MIT-black.svg)](LICENSE)

Product Server 是一套自托管的 CI 产物管理与可视化部署平台。它可以接收 GitHub、Gitea 等 CI 任务产生的构建文件，保存每个发布版本，并通过可视化工作流完成上传、解压、校验、SSH 命令和多服务器部署。

无需把复杂的部署脚本散落在各个仓库中：上传一次产物，后续发布流程都可以在一个界面里配置、运行、追踪和复用。

## 运行截图

### 项目与产物管理

![项目与产物管理](images/projuct.png)

### 可视化工作流编辑

![可视化工作流编辑](images/worker.png)

## 主要亮点

- **统一管理 CI 产物**：按项目、版本、分支和文件保存构建结果，支持访问统计、容量限制和自动清理。
- **可视化部署工作流**：通过 DAG 画布组合压缩、解压、SFTP、SSH、Webhook、判断、循环和多服务器任务。
- **上传即可触发**：支持任意版本、Tag、Commit、Tag glob 和分支规则，上传完成后自动创建运行。
- **多服务器批量执行**：同一段流程可以串行或并行运行在多台服务器上，并分别保存状态、日志和输出。
- **任务可靠恢复**：Worker 使用数据库租约和心跳领取任务，异常退出后可由其他 Worker 恢复。
- **实时运行视图**：像查看 GitHub Actions 一样查看节点状态、执行顺序、实时日志和结构化输出。
- **完整权限体系**：支持注册、系统管理员及项目所有者、管理员、开发者、只读成员。
- **定时工作流**：除产物上传触发外，也可以创建 Cron 定时项目。
- **AI 辅助编排**：可接入 OpenAI Chat Completions 兼容接口，通过对话生成和优化当前工作流画布。
- **轻量部署**：前端嵌入 Go 二进制，支持 SQLite、MySQL 和 PostgreSQL，无需单独部署静态站点。

## 适用场景

- 保存 GitHub Actions、Gitea Actions 或其他 CI 系统的构建产物。
- 将前端、Go 服务或安装包自动发布到一台或多台服务器。
- 替代散落在仓库中的 SSH/SFTP 部署脚本。
- 管理测试环境、内部服务和私有化项目的发布流程。
- 在不引入大型 DevOps 平台的情况下搭建轻量、自托管的发布中心。

## 三分钟快速开始

### 下载 Release 直接运行（推荐）

前端已经嵌入可执行文件，无需安装 Go、Node.js 或单独部署静态资源。请从 [GitHub Releases](https://github.com/zouXH-god/productServer/releases/latest) 下载对应系统和架构的压缩包。

Linux amd64 示例：

```bash
curl -LO https://github.com/zouXH-god/productServer/releases/latest/download/productserver-linux-amd64.tar.gz
tar -xzf productserver-linux-amd64.tar.gz
mv productserver-linux-amd64 productserver
chmod +x productserver
./productserver server
```

在另一个终端启动 Worker：

```bash
./productserver worker
```

打开 <http://localhost:8080>，默认账号为 `admin` / `admin123456`。生产部署前请创建 `.env` 并修改默认密钥和密码。

### Docker Compose

如果希望使用容器运行，建议从不需要额外数据库的 SQLite 版本开始：

```bash
git clone https://github.com/zouXH-god/productServer.git
cd productServer
docker compose -f deploy/docker/compose.sqlite.yml up -d --build
```

项目同时提供内置 PostgreSQL 和外置数据库版本，具体方式见 [Docker Compose 部署文档](document.md#docker-compose-部署)。

### 从源码运行

需要 Go 1.22+、Node.js 20+ 和 npm。

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

Windows 开发环境可以直接运行：

```bat
start.bat
```

> 完整的二进制安装、数据库配置、环境变量、systemd、API 和 Action 说明请查看 [使用文档](document.md)。

## 工作方式

```text
GitHub / Gitea / CI
          │
          │ Product Token 上传
          ▼
   Product Server
   ├─ 发布版本与文件
   ├─ 持久化触发事件
   ├─ 可视化工作流
   └─ 实时日志与运行历史
          │
          │ Worker 领取任务
          ▼
  SSH / SFTP / Webhook / 多服务器
```

Server 提供 API、管理界面、触发器和调度器；Worker 负责执行工作流。两者通过数据库协作，可以独立部署和水平扩展。SQLite 适合单机体验，多实例生产环境推荐 MySQL 或 PostgreSQL。

## 使用 Action 自动上传

独立的 [Product Server Action](https://github.com/zouXH-god/product-server-action) 会将匹配内容确定性打包为 ZIP，自动读取 Tag、Commit SHA 和分支信息，然后上传到目标项目。

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

Action 输出实际版本、分支、文件名、SHA-256 和不包含 Token 的下载地址。重复上传相同版本和内容时按幂等请求处理。

## 直接上传与下载

任何 CI 都可以通过 multipart 上传：

```bash
curl -X POST https://product.example.com/api/upload \
  -H "Authorization: Bearer ps_your_project_token" \
  -F "version=v1.2.3" \
  -F "commit_sha=$CI_COMMIT_SHA" \
  -F "branch=$CI_COMMIT_BRANCH" \
  -F "files=@dist/app.zip"
```

通过项目 Token 下载最新版本的唯一原始压缩包：

```bash
curl -fL "https://product.example.com/api/download?token=ps_your_project_token" -o artifact.zip
```

也可以显式指定 `version` 和 `file`。生产环境必须使用 HTTPS，且应注意查询参数中的 Token 可能进入浏览器历史或代理日志。

## 技术栈

- 后端：Go、Gin、GORM
- 前端：Vue 3、TypeScript、Vite、Vue Flow
- 数据库：SQLite（纯 Go）、MySQL、PostgreSQL
- 工作流：持久化 DAG、租约、心跳、执行槽、SSE 实时日志
- 安全：bcrypt、JWT、AES-256-GCM 敏感字段加密

## 构建与测试

```bash
cd frontend && npm ci && npm test && npm run build && cd ..
go test ./...
go build -trimpath -o productserver .
```

也可以使用 `build.ps1` 或 `build.sh`。前端产物会嵌入最终二进制，运行时不需要前端源码目录。健康检查为 `/healthz`，就绪检查为 `/readyz`。

## 文档与参与

- [完整使用文档](document.md)
- [环境变量示例](.env.example)
- [Product Server Action](https://github.com/zouXH-god/product-server-action)
- [GitHub 镜像与 Releases](https://github.com/zouXH-god/productServer)

欢迎提交 Issue、功能建议和 Pull Request。如果这个项目对你有帮助，也欢迎点一个 Star，让更多需要轻量 CI 产物管理和自动部署的人看到它。

## 安全说明

当前 SSH 主机密钥校验按首版约定处于关闭状态，存在中间人攻击风险，仅应在可信网络中使用。敏感字段需要通过 `SECRET_ENCRYPTION_KEY` 加密；生产环境请使用 HTTPS、强随机密钥和独立数据库账号。

## 开源许可

本项目基于 [MIT License](LICENSE) 开源。你可以自由使用、修改和分发，但需保留原始版权及许可声明。
