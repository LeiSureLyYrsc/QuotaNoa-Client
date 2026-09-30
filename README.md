# QuotaNoa Client

QuotaNoa-Bot `Server_Mode` 的独立 **Go** 客户端。客户端不依赖 NoneBot，也没有聊天或凭证管理功能；它主动通过 WebSocket 连接 Bot 的独立 FastAPI 服务，在收到请求后读取本机 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 与本地渠道（火山方舟 / WorkBuddy / Qoder）的额度并返回结果。

额度刷新（Codex 官方重置券）默认**关闭**；仅在本地配置显式开启后才会执行，且始终以本地配置为准。

## 功能

- 主动连接 Bot 的 Server_Mode（适合客户端位于 NAT / 家庭网络后）
- 查询本机 CLIProxyAPI 的 Claude / Codex / Antigravity / Kimi / xAI / Gemini-CLI 额度
- 查询本机火山方舟 Coding/Agent Plan、WorkBuddy2API、Qoder2OAPI 额度
- 返回绝对时间戳 `reset_at`、订阅到期时间与 Codex 可用重置点数
- 支持按平台 / 账号过滤，本地 TTL 缓存
- 握手上报 agent 版本与刷新能力；自动重连与指数退避
- 默认只读；开启后仅允许 Codex 官方重置券消费

## 安全边界

客户端只接受受控协议动作：

```text
quota.query
codex.refresh
```

本机 CLIProxyAPI 管理接口只允许：

```text
GET  /v0/management/auth-files
POST /v0/management/api-call
```

`/api-call` 只能请求代码内置的额度上游地址，并同时校验固定 HTTP 方法。服务器不能传入任意 URL、Header 模板、请求方法或管理 API 路径。

Codex 刷新受本地配置 `refresh.enabled`（默认 `false`）控制。关闭时客户端在**任何网络 I/O 之前**直接拒绝 `codex.refresh`；即使服务端伪造「能力」声明，客户端也只以本地配置为准。

客户端没有以下实现：`reset-quota`、凭证启用/禁用/删除、OAuth 登录、配置读写、日志读取、通用 HTTP 代理、Shell 执行。

## 环境要求

- Go 1.23+
- 可访问本机 CLIProxyAPI 管理接口
- 可通过 WebSocket 访问已启用 `QUOTANOA_CLIENT_SERVER_ENABLED=true` 的 QuotaNoa-Bot

## 构建与运行

```bash
go build -o quotanoa-client ./cmd/quotanoa-client

# 生成默认配置文件（任选其一）
./quotanoa-client --generate-config                 # 写入 ./config.json
./quotanoa-client --generate-config=home.json       # 写入指定路径
./quotanoa-client config init --out config.json     # 等价子命令
./quotanoa-client --generate-config --force         # 覆盖已存在文件

# 编辑 config.json：client.server_url / client.key 必填
./quotanoa-client run --config config.json
```

> `--generate-config` 的空格分隔写法（`--generate-config path`）不受支持：pflag 对带默认值的标志要求 `--flag=value`。需要空格分隔请用 `config init --out path`。生成不覆盖已有文件，除非加 `--force`。

环境变量覆盖（可选）：`QUOTANOA_CLIENT_NAME`、`QUOTANOA_SERVER_URL`、`QUOTANOA_CLIENT_KEY`、`QUOTANOA_REFRESH_ENABLED`、`QUOTANOA_CPA_BASE_URL`、`QUOTANOA_CPA_MANAGEMENT_KEY`。

校验配置（不联网）：

```bash
./quotanoa-client config check --config config.json
```

升级/修补配置（备份后补齐新增键并写入 `config_version`）：

```bash
./quotanoa-client config patch --config config.json
```

> 生成的配置带顶层 `config_version`（当前为 `1`）。旧版本、缺字段的配置可用 `config patch` 修补：会先把原文件备份为同目录下 `config_<日期>-<时间>_bak.json`，再补齐缺失键、写入当前版本号；已存在的值不会被覆盖。若文件版本高于当前程序支持，则拒绝修补。

## 下载与镜像

GitHub Actions 在 `main` 推送与 `v*` 标签时构建：

- **二进制**（`Build binaries` workflow）：`linux/darwin/windows` × `amd64/arm64`，作为 workflow artifact 上传；`v*` 标签会自动创建 GitHub Release 并附上全部二进制。
- **Docker 镜像**（`Build and publish Docker image` workflow）：`ghcr.io/leisurelyyrsc/quotanoa-client`，多架构 `linux/amd64`、`linux/arm64`。

```bash
docker run --rm -v "$PWD/config.json:/config/config.json:ro" \
  ghcr.io/leisurelyyrsc/quotanoa-client run --config /config/config.json
```

## 配置

```jsonc
{
  "client": { "name": "Home", "server_url": "ws://127.0.0.1:8320/v1/client/ws", "key": "…", "protocol": 2 },
  "refresh": { "enabled": false },
  "cpa": { "base_url": "http://127.0.0.1:8317", "management_key": "…", "timeout": 15, "quota_timeout": 25, "quota_concurrency": 4, "quota_cache_ttl": 0 },
  "volcengine": { "accounts": [ { "name": "火山主号", "access_key_id": "…", "secret_access_key": "…", "region": "cn-beijing" } ] },
  "workbuddy": { "servers": [ { "name": "wb-main", "base_url": "http://127.0.0.1:7863", "username": "admin", "password": "…", "api_key": "", "timeout": 30 } ] },
  "qoder": { "servers": [ { "name": "qoder-main", "base_url": "http://127.0.0.1:8000", "api_key": "…", "timeout": 30 } ] },
  "refreshcache": { "default": 600, "channels": { "claude": 120, "codex": 300 } },
  "reconnect": { "min": 1, "max": 30 }
}
```

| 配置项 | 默认 | 说明 |
| --- | --- | --- |
| `client.name` | `Home` | 客户端名，须与 Bot 端 `client add` 创建的名称一致 |
| `client.server_url` | `ws://127.0.0.1:8320/v1/client/ws` | Server_Mode 地址；公网用 `wss://` |
| `client.key` | 空 | 连接密钥，必填，不上传 |
| `refresh.enabled` | `false` | Codex 刷新开关（本地权威） |
| `cpa.*` | — | 本机 CLIProxyAPI 连接与额度查询参数 |
| `volcengine/workbuddy/qoder` | — | 本地渠道凭据 |
| `refreshcache` | `600` | 各渠道额度缓存秒数（默认 10 分钟）；`channels` 按渠道覆盖；`0` 不缓存。`--fresh` 强制刷新 |

## Bot 服务端配置（回顾）

Bot 侧 `.env`：

```env
QUOTANOA_CLIENT_SERVER_ENABLED=true
QUOTANOA_CLIENT_HOST=127.0.0.1
QUOTANOA_CLIENT_PORT=8320
# QUOTANOA_CLIENT_FILE=data/quotanoa_client.json
```

用 `/quotanoa client add <名称> [--allow-refresh]` 创建客户端实例并获取连接地址与密钥。

## 协议

协议版本 `2`，路径 `/v1/client/ws`。连接头：`Authorization: Bearer <client.key>`、`X-CPA-Client-Name: <client.name>`、`X-CPA-Client-Protocol: 2`。

服务端先下发 `hello`，客户端回 `hello`（含 `agent_version`、`protocol_version`、`os`、`capabilities.refresh/channels`）。随后服务端发送 `quota.query` / `codex.refresh` 请求，客户端回 `response`。未知版本 / 未知 action 返回 `ok:false` 且不断连。

## Docker

```bash
cp config.example.json config.json
docker compose up -d
```

镜像发布到 `ghcr.io/leisurelyyrsc/quotanoa-client`（`linux/amd64`、`linux/arm64`）。

## 本地测试

```bash
go test ./...
go vet ./...
```

## 公网部署建议

不要直接把明文 WebSocket 暴露到公网。推荐 `Client -- wss:// --> Caddy/Nginx -- ws://127.0.0.1:8320 --> Server_Mode`，为每个客户端生成独立高强度随机密钥。
