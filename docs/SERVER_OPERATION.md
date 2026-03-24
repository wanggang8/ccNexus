# ccNexus 服务端操作手册

本文档说明如何在服务器上构建、部署和运维 ccNexus 无头模式（命令行/HTTP 代理）。

---

## 快速开始（Docker）

```bash
git clone <仓库地址>
cd ccNexus
docker compose -f cmd/server/docker-compose.yml up -d --build
```

启动后访问 `http://<服务器IP>:3021/ui/` 配置端点（端口 3021 映射容器 3000）。

---

## 一、目录结构

```
ccNexus/                    # 项目根目录
├── cmd/
│   └── server/            # 服务端入口
│       ├── main.go
│       ├── Dockerfile
│       └── docker-compose.yml
├── go.mod
├── go.sum
└── internal/              # 内部包
```

**构建与运行需在项目根目录 `ccNexus/` 下执行。**

---

## 二、环境要求

- Go 1.22+
- CGO 支持（SQLite 依赖）
- Linux x86_64 / amd64（服务器常见架构）

---

## 三、构建 x86 二进制

### 3.1 本地构建（当前平台）

```bash
cd /path/to/ccNexus
CGO_ENABLED=1 go build -ldflags="-s -w" -o ccnexus-server ./cmd/server
```

### 3.2 交叉编译 Linux x86_64（macOS 用户）

macOS 上交叉编译到 Linux 时，CGO 会因 `setresuid`/`setresgid` 等 Linux 专有符号报错，**推荐用 Docker 在 Linux 环境中构建**：

```bash
cd /path/to/ccNexus
docker run --rm -v "$(pwd)":/app -w /app golang:1.24-alpine sh -c "apk add --no-cache gcc musl-dev sqlite-dev && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags=\"-s -w\" -o ccnexus-server ./cmd/server"
```

生成 `ccnexus-server` 可执行文件，拷贝到 Linux 服务器即可运行。

### 3.3 多架构构建（Docker）

```bash
cd /path/to/ccNexus

# Linux x86_64
docker run --rm -v "$(pwd)":/app -w /app golang:1.24-alpine sh -c "apk add --no-cache gcc musl-dev sqlite-dev && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags=\"-s -w\" -o ccnexus-server-linux-amd64 ./cmd/server"

# Linux ARM64
docker run --rm -v "$(pwd)":/app -w /app golang:1.24-alpine sh -c "apk add --no-cache gcc musl-dev sqlite-dev && CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags=\"-s -w\" -o ccnexus-server-linux-arm64 ./cmd/server"
```

---

## 四、Docker 一键启动（推荐）

在项目根目录执行：

```bash
cd /path/to/ccNexus
docker compose -f cmd/server/docker-compose.yml up -d --build
```

- 首次拉取代码后执行上述命令即可完成构建并启动
- 端口映射 `3021:3000`，数据挂载到 `/data/ccnexus/`
- Web 管理界面：`http://<服务器IP>:3021/ui/`

停止：

```bash
docker compose -f cmd/server/docker-compose.yml down
```

---

## 五、启动与停止

### 5.1 直接运行二进制

```bash
# 前台运行（默认端口 3000，数据目录 ~/.ccNexus）
./ccnexus-server

# 指定环境变量
CCNEXUS_PORT=8080 CCNEXUS_DATA_DIR=/opt/ccnexus ./ccnexus-server
```

### 5.2 后台运行（systemd）

创建 `/etc/systemd/system/ccnexus.service`：

```ini
[Unit]
Description=ccNexus API Proxy
After=network.target

[Service]
Type=simple
User=ccnexus
WorkingDirectory=/opt/ccnexus
ExecStart=/opt/ccnexus/ccnexus-server
Restart=on-failure
RestartSec=5
Environment=CCNEXUS_DATA_DIR=/opt/ccnexus
Environment=CCNEXUS_PORT=3000
Environment=CCNEXUS_DB_PATH=/opt/ccnexus/ccnexus.db
Environment=CCNEXUS_UI_TOKEN=cc7fb9516ff16b8f88fa62961e59b8290b47b5c21172581bb03cfb0ff49d78e2
[Install]
WantedBy=multi-user.target
```

操作命令：

```bash
# 启动
sudo systemctl start ccnexus

# 停止
sudo systemctl stop ccnexus

# 重启
sudo systemctl restart ccnexus

# 开机自启
sudo systemctl enable ccnexus

# 查看状态
sudo systemctl status ccnexus

# 查看日志
journalctl -u ccnexus -f
```

### 5.3 Docker Compose 运行

```bash
cd /path/to/ccNexus
docker compose -f cmd/server/docker-compose.yml up -d --build
```

**启用 UI Token（外网部署建议）：** 编辑 `cmd/server/docker-compose.yml`，在 environment 下添加或修改：
```yaml
- CCNEXUS_UI_TOKEN=your-secret-token
```

或使用 `.env` 文件：`echo "CCNEXUS_UI_TOKEN=$(openssl rand -hex 32)" > .env`，或启动时传入：`CCNEXUS_UI_TOKEN=xxx docker compose -f cmd/server/docker-compose.yml up -d --build`

停止：

```bash
docker compose -f cmd/server/docker-compose.yml down
```

---

## 六、环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `CCNEXUS_DATA_DIR` | 数据目录 | `~/.ccNexus` |
| `CCNEXUS_DB_PATH` | 数据库路径 | `{DATA_DIR}/ccnexus.db` |
| `CCNEXUS_PORT` | 代理监听端口 | 配置中的端口（默认 3000） |
| `CCNEXUS_LOG_LEVEL` | 日志级别 0=DEBUG 1=INFO 2=WARN 3=ERROR | 1 |
| `CCNEXUS_UI_TOKEN` | Web UI 访问 Token（设置后需提供此 Token 才能访问 /ui/ 和 /api/） | 空（不校验） |

### UI Token 使用说明

- **不设置**：任意访问 Web UI 和 API
- **设置后**：访问 `/ui/` 时需先输入 Token；API 请求需在 Header 中携带：
  - `Authorization: Bearer <token>` 或
  - `X-API-Token: <token>`
- 建议使用复杂随机字符串，例如：`openssl rand -hex 32`

---

## 七、页面访问与配置端点

### 7.1 Web 管理界面

启动后访问：

```
http://<服务器IP>:<端口>/ui/
```

例如：`http://192.168.1.100:3000/ui/`

### 7.2 通过 Web 界面添加端点

1. 打开 `http://<服务器IP>:<端口>/ui/`
2. 左侧导航栏点击 **Endpoints**（端点）
3. 点击右上角 **Add Endpoint**（添加端点）
4. 填写表单：
   - **Name**：端点名称，如 `Claude Official`
   - **API URL**：API 地址，如 `https://api.anthropic.com`
   - **API Key**：API 密钥
   - **Transformer**：选择 `claude` / `openai` / `gemini` / `deepseek`
   - **Model**：Claude 可留空，OpenAI 需填写如 `gpt-4`
   - **备注**：可选
   - **Enabled**：勾选启用
5. 点击 **Create** 保存

### 7.3 通过 REST API 添加端点

```bash
curl -X POST http://<服务器IP>:<端口>/api/endpoints \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Claude Official",
    "apiUrl": "https://api.anthropic.com",
    "apiKey": "sk-ant-your-key-here",
    "transformer": "claude",
    "model": "",
    "enabled": true,
    "remark": "官方 Claude API"
  }'
```

### 7.4 常用 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/endpoints` | 列出所有端点 |
| GET | `/api/endpoints/export` | 导出端点（含完整 API Key，用于备份） |
| POST | `/api/endpoints/import` | 导入端点（body: `{ endpoints: [...], mode: "replace"|"merge" }`） |
| POST | `/api/endpoints` | 创建端点 |
| PUT | `/api/endpoints/:name` | 更新端点 |
| DELETE | `/api/endpoints/:name` | 删除端点 |
| PATCH | `/api/endpoints/:name/toggle` | 启用/禁用端点 |
| POST | `/api/endpoints/:name/test` | 测试端点 |
| POST | `/api/endpoints/switch` | 切换到指定端点 |
| GET | `/health` | 健康检查 |

---

## 八、客户端配置

### Claude Code

`~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "任意值",
    "ANTHROPIC_BASE_URL": "http://<服务器IP>:<端口>"
  }
}
```

### Codex CLI

`~/.codex/config.toml`：

```toml
model_provider = "ccNexus"
model = "gpt-5-codex"
preferred_auth_method = "apikey"

[model_providers.ccNexus]
name = "ccNexus"
base_url = "http://<服务器IP>:<端口>/v1"
wire_api = "responses"
```

---

## 九、数据与备份

- 数据库：`{CCNEXUS_DATA_DIR}/ccnexus.db`
- 首次启动会自动创建数据目录和示例端点，需尽快替换为真实配置
- 建议定期备份 `ccnexus.db` 及所在目录

---

## 十、故障排查

| 现象 | 排查步骤 |
|------|----------|
| 无法启动 | 检查端口占用、数据目录权限、CGO 依赖 |
| Web UI 不可访问 | 确认防火墙放行端口、服务已启动 |
| 端点无响应 | 检查 API URL、Key、网络连通性 |
| 健康检查失败 | `curl http://localhost:3000/health` |

查看日志：

```bash
# 直接运行时，日志输出到 stdout
# systemd 时
journalctl -u ccnexus -f
```

### 安全建议

- 生产环境建议通过 Nginx 做反向代理并启用 HTTPS
- 可配置反向代理的 Basic Auth 限制管理界面访问
- 防火墙仅开放必要端口
