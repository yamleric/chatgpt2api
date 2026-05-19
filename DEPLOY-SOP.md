# HiMo 部署 SOP

> 项目：chatgpt2api（品牌名 HiMo）
> 目标服务器：`ubuntu@123.207.79.201`
> SSH 密钥：`C:\Users\74022\.ssh\sshkey1.pem`
> 站点域名：`https://himo.zzuli.site`
> 仓库：`yamleric/chatgpt2api`（origin）

---

## 变量约定

以下命令中使用的变量，实际执行前请确认：

```bash
SSH_KEY="C:\Users\74022\.ssh\sshkey1.pem"
SERVER="ubuntu@123.207.79.201"
```

---

## 目录结构（服务器端）

```
~/chatgpt2api-deploy/      # 运行目录
  ├── .env                 # 环境变量（含密钥，勿泄露）
  ├── data/                # 持久化数据（SQLite、图片、日志）
  └── deploy/
      └── docker-compose.yml

~/chatgpt2api-build/       # 源码目录（仅完整构建时使用）
```

---

## 快速部署（推荐）

本地交叉编译 + 上传二进制，耗时约 30 秒。

### Step 1：本地构建

```bash
# 构建前端（输出到 internal/web/dist/，会被 embed 进二进制）
cd web && npm run build && cd ..

# 交叉编译 linux/amd64
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -tags=embed -ldflags="-s -w" -o chatgpt2api ./internal
```

Windows CMD 环境：
```cmd
cd web && npm run build && cd ..
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -tags=embed -ldflags="-s -w" -o chatgpt2api ./internal
```

**构建成功标志：** 项目根目录生成 `chatgpt2api` 文件（约 23MB ELF 二进制）。

### Step 2：上传二进制

```bash
scp -i "$SSH_KEY" chatgpt2api $SERVER:~/chatgpt2api-deploy/chatgpt2api.new
```

### Step 3：构建镜像 + 重启

```bash
ssh -i "$SSH_KEY" $SERVER '
cat > /tmp/Dockerfile.quick << "EOF"
FROM chatgpt2api:relay
COPY chatgpt2api.new /app/chatgpt2api
RUN chmod +x /app/chatgpt2api
EOF
cd ~/chatgpt2api-deploy && docker build -f /tmp/Dockerfile.quick -t chatgpt2api:relay .
docker compose -f deploy/docker-compose.yml down
docker compose -f deploy/docker-compose.yml up -d
'
```

### Step 4：验证

```bash
# 健康检查
ssh -i "$SSH_KEY" $SERVER "curl -s http://127.0.0.1:3000/health"
# 期望: {"status":"ok","version":"..."}

# 静态资源（确认前端正常嵌入）
ssh -i "$SSH_KEY" $SERVER "curl -sI http://127.0.0.1:3000/logo-mark.svg | head -1"
# 期望: HTTP/1.1 200 OK

# 容器日志（无报错）
ssh -i "$SSH_KEY" $SERVER "docker logs chatgpt2api --tail 5"
```

外部访问：浏览器打开 `https://himo.zzuli.site`，确认：
- [ ] 页面正常加载，导航栏 logo 和 "HiMo" 标题显示
- [ ] 登录功能正常
- [ ] 图片生成功能正常
- [ ] 管理员设置页面可访问

### Step 5：清理

```bash
rm chatgpt2api   # 删除本地编译产物
```

---

## 完整 Docker 构建（备用）

耗时 10-15 分钟。仅在无法本地编译时使用（如 Go 版本不兼容、依赖 CGO 等）。

### 前置条件

- 本地已安装 Node.js（用于上传 web 源码）
- 服务器上已配置 BuildKit 镜像加速（见下方）

### Step 1：上传源码

从本地项目根目录执行，**必须包含以下所有目录/文件**：

```bash
SSH_KEY="C:\Users\74022\.ssh\sshkey1.pem"
SERVER="ubuntu@123.207.79.201"
BUILD_DIR="~/chatgpt2api-build"

# 清空旧源码
ssh -i "$SSH_KEY" $SERVER "rm -rf $BUILD_DIR && mkdir -p $BUILD_DIR/web"

# 上传 Go 源码
scp -i "$SSH_KEY" -r go.mod go.sum internal/ deploy/ $SERVER:$BUILD_DIR/

# 上传 Web 源码（注意：public/ 不可遗漏）
scp -i "$SSH_KEY" -r \
  web/src \
  web/public \
  web/index.html \
  web/vite.config.ts \
  web/tsconfig.json \
  web/package.json \
  web/bun.lock \
  web/components.json \
  web/postcss.config.mjs \
  $SERVER:$BUILD_DIR/web/
```

**检查清单（上传后验证）：**

```bash
ssh -i "$SSH_KEY" $SERVER "ls $BUILD_DIR/web/public/logo-mark.svg && echo '✓ public assets OK'"
ssh -i "$SSH_KEY" $SERVER "ls $BUILD_DIR/internal/httpapi/app.go && echo '✓ Go source OK'"
ssh -i "$SSH_KEY" $SERVER "ls $BUILD_DIR/deploy/Dockerfile && echo '✓ Dockerfile OK'"
```

---

### Step 2：修补 Dockerfile（国内网络适配）

服务器无法直连 Docker Hub 和 proxy.golang.org，需要：

```bash
ssh -i "$SSH_KEY" $SERVER "
  cd $BUILD_DIR/deploy

  # 1. 移除 syntax 指令（避免拉取 docker/dockerfile 镜像）
  sed -i '1s|^# syntax=docker/dockerfile:1.7|# syntax removed for offline build|' Dockerfile

  # 2. 添加 GOPROXY（Go 模块代理）
  python3 -c \"
import re
with open('Dockerfile', 'r') as f:
    content = f.read()
if 'GOPROXY' not in content:
    content = content.replace('WORKDIR /src\n', 'WORKDIR /src\nENV GOPROXY=https://goproxy.cn,direct\n')
    with open('Dockerfile', 'w') as f:
        f.write(content)
\"
"
```

BuildKit 镜像加速（首次部署时配置，后续复用）：

```bash
ssh -i "$SSH_KEY" $SERVER "
  mkdir -p ~/.cache/chatgpt2api-buildkit
  cat > ~/.cache/chatgpt2api-buildkit/buildkitd.toml << 'EOF'
[worker.oci]
  max-parallelism = 1

[registry.\"docker.io\"]
  mirrors = [\"mirror.ccs.tencentyun.com\"]
EOF
"
```

---

### Step 3：构建 Docker 镜像

```bash
ssh -i "$SSH_KEY" $SERVER "
  cd ~/chatgpt2api-build
  CHATGPT2API_LOCAL_IMAGE=chatgpt2api:relay sh deploy/docker-build-limited.sh build
"
```

预计耗时：
- 首次构建（无缓存）：10-15 分钟
- 增量构建（有缓存）：1-3 分钟

**构建成功标志：** 输出包含 `exporting to oci image format` 和 `importing to docker`，无 ERROR。

---

### Step 4：重启服务

```bash
ssh -i "$SSH_KEY" $SERVER "
  cd ~/chatgpt2api-deploy
  docker compose -f deploy/docker-compose.yml down
  docker compose -f deploy/docker-compose.yml up -d
"
```

---

### Step 5：验证

```bash
# 健康检查
ssh -i "$SSH_KEY" $SERVER "curl -s http://127.0.0.1:3000/health"
# 期望: {"status":"ok","version":"..."}

# 静态资源
ssh -i "$SSH_KEY" $SERVER "curl -sI http://127.0.0.1:3000/logo-mark.svg | head -1"
# 期望: HTTP/1.1 200 OK

# 容器日志（无报错）
ssh -i "$SSH_KEY" $SERVER "docker logs chatgpt2api --tail 5"
```

外部访问验证：浏览器打开 `https://himo.zzuli.site`，确认：
- [ ] 页面正常加载，导航栏 logo 显示
- [ ] 登录功能正常
- [ ] 图片生成功能正常
- [ ] 管理员设置页面可访问

---

## 常见问题

| 症状 | 原因 | 解决 |
|------|------|------|
| 图片/logo 不显示 | `web/public/` 未上传 | 重新上传 public 目录并重新构建 |
| `dial tcp ... i/o timeout`（Docker Hub） | 国内网络无法访问 Docker Hub | 确认 buildkitd.toml 配置了腾讯云镜像 |
| `go mod download` 超时 | proxy.golang.org 被墙 | 确认 Dockerfile 中有 `ENV GOPROXY=https://goproxy.cn,direct` |
| `set: Illegal option -` | shell 脚本有 Windows CRLF 换行 | `find deploy -name '*.sh' -exec sed -i 's/\r$//' {} \;` |
| 容器启动后立即退出 | .env 配置错误或端口冲突 | `docker logs chatgpt2api` 查看错误 |

---

## 回滚

如果新版本有问题，用旧镜像快速回滚：

```bash
# 查看历史镜像
ssh -i "$SSH_KEY" $SERVER "docker images chatgpt2api"

# 如果有 tag 为之前版本的镜像，修改 docker-compose.yml 中的 image 字段
# 否则需要从旧源码重新构建
```

---

## 安全提醒

- `.env` 文件包含 API 密钥和管理员密码，**绝不**提交到 Git
- SSH 密钥文件权限应为 600
- 部署完成后不要在服务器上留存 `.env` 的副本到 build 目录
