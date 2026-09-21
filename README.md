# Product Server

一个用于保存 Git CI 发布产物的最小应用。后端使用 Gin + GORM，前端使用 Vue 3；支持 SQLite、MySQL 和 PostgreSQL，生产构建为内嵌前端资源的单一二进制文件。

## 开发

需要 Go 1.22+、Node.js 20+。默认使用当前目录的 SQLite 数据库，初始账号为 `admin` / `admin123456`，仅供本地开发使用。

```bat
start.bat
```

浏览器访问 <http://localhost:5173>。生产环境务必设置安全的 `JWT_SECRET` 和 `ADMIN_PASSWORD`。完整配置见 `.env.example`；程序直接读取环境变量，不自动加载 `.env` 文件。

## 项目 Token

创建项目时服务会自动生成唯一 Token，明文只显示一次。Token 可用于上传和下载；在项目页面轮换后，旧 Token 会立即失效。服务端只保存 Token 的 SHA-256 哈希。

## GitHub Action 自动上传

Action 会把匹配的内容确定性打包为一个 ZIP。版本默认使用 Git tag，没有 tag 时使用完整 commit SHA，也可通过 `version` 覆盖。

```yaml
- uses: shiran/product-server-action@v1
  with:
    url: ${{ secrets.ARTIFACT_SERVER_URL }}
    token: ${{ secrets.ARTIFACT_SERVER_TOKEN }}
    path: |
      dist/**
      checksums.txt
    name: web-build
```

Action 输出 `version`、`file`、`sha256` 和不包含 Token 的 `download-url`。Action 源码位于独立仓库 `shiran/product-server-action`。

## 通用 CI 上传

在项目页面创建 Token 后，通过 multipart 上传一个版本的多个文件：

```bash
curl -X POST http://localhost:8080/api/upload \
  -H "Authorization: Bearer ps_your_project_token" \
  -F "version=v1.2.3" \
  -F "commit_sha=$CI_COMMIT_SHA" \
  -F "branch=$CI_COMMIT_BRANCH" \
  -F "pipeline_id=$CI_PIPELINE_ID" \
  -F "job_url=$CI_JOB_URL" \
  -F "files=@dist/app.zip" \
  -F "files=@dist/checksums.txt"
```

版本号在项目内唯一。任一文件失败时整个上传批次都会回滚；相同版本、文件名、大小和 SHA-256 的重试视为幂等成功，内容不同则返回 `409 Conflict`。

## Token 直链下载

必须精确指定版本和文件名：

```text
GET /api/download?token=PROJECT_TOKEN&version=v1.2.3&file=web-build.zip
```

例如：

```bash
curl --get 'https://artifacts.example.com/api/download' \
  --data-urlencode 'token=ps_your_project_token' \
  --data-urlencode 'version=v1.2.3' \
  --data-urlencode 'file=web-build.zip' \
  --output web-build.zip
```

生产环境必须使用 HTTPS。查询参数中的 Token 可能进入浏览器历史、反向代理或访问日志；若环境不适合通过 URL 携带密钥，应继续使用管理界面的 JWT 下载接口。

## 数据库

- SQLite：`DB_DRIVER=sqlite`，`DB_DSN=productserver.db`
- MySQL：`DB_DRIVER=mysql`，DSN 示例 `user:pass@tcp(localhost:3306)/productserver?charset=utf8mb4&parseTime=True&loc=Local`
- PostgreSQL：`DB_DRIVER=postgres`，DSN 示例 `host=localhost user=postgres password=pass dbname=productserver port=5432 sslmode=disable TimeZone=Asia/Shanghai`

应用启动时自动执行迁移，并且只在用户表为空时创建环境变量指定的初始管理员。

## 测试与生产构建

Windows：

```powershell
.\build.ps1
```

Linux/macOS：

```sh
./build.sh
```

构建产物位于 `bin/`，运行时不需要单独部署前端目录。健康检查为 `/healthz`，就绪检查为 `/readyz`。
