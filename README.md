# Vision Relay

Vision Relay 是一个本地桌面客户端式的多接口 AI 模型中转工具。它把外部客户端发来的图片请求先交给视觉模型解析，再把解析结果转成纯文本上下文转发给文本模型，让只支持文本的上游模型也能间接处理图片。

项目使用 Go 编写后端和桌面外壳，前端静态资源通过 `embed` 打进二进制。Windows 下可以编译成单个 `vision-relay.exe`，启动后默认打开桌面窗口，并在系统托盘驻留。

## 功能特性

- 本地 HTTP 中转服务，默认监听 `http://127.0.0.1:8787`
- 桌面 WebView 窗口和系统托盘菜单
- 支持文本模型与视觉模型分开配置，并保存多套模型方案
- 支持 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini、Ollama 等常见接口形态
- 支持本地客户端 API Key，用于限制外部客户端访问
- 支持为 Codex、OpenCode、Claude Code 等客户端生成接入配置
- 内置请求日志、Token 统计、首 token 耗时、缓存命中等记录
- 支持网络代理 URL，适配本地代理或 fake-ip 网络环境

## 工作原理

1. 外部客户端把请求发送到 Vision Relay 的本地地址。
2. Vision Relay 识别请求中的图片字段。
3. 如果当前文本模型不直接支持图片，则先调用配置好的视觉模型解析图片内容。
4. Vision Relay 删除原始图片字段，把“用户需求 + 图片解析结果”写回请求上下文。
5. 请求被转发给配置好的文本模型，上游响应会原样返回给客户端。

这样最终回答仍由文本模型完成，视觉模型只负责把图片转成可被文本模型理解的事实描述。

## 支持的接口

| 客户端协议 | 本地路径 | 说明 |
| --- | --- | --- |
| OpenAI Chat Completions | `/v1/chat/completions` 或 `/chat/completions` | 支持 `image_url`、`input_image` |
| OpenAI Responses / Codex | `/v1/responses` 或 `/responses` | 支持 `input_text`、`input_image` |
| Anthropic Messages | `/v1/messages` 或 `/messages` | 支持 `content[].type=image` |
| Gemini | `/v1beta/models/{model}:generateContent` | 支持 `inline_data`、`file_data` |
| Ollama Chat | `/api/chat` | 支持 `images` |
| Ollama Generate | `/api/generate` | 支持 `images` |
| 其他路径 | 原路径透传 | 例如 `/v1/models`、`/api/tags` |

## 运行环境

- Windows 10/11
- Go 1.25 或更新版本
- Microsoft Edge WebView2 Runtime
- 可访问上游模型 API 的网络环境

> 大多数 Windows 10/11 系统已经内置 WebView2 Runtime。如果桌面窗口无法打开，请先安装 Microsoft Edge WebView2 Runtime。

## 源码运行

```powershell
go run ./backend/cmd/vision-relay
```

启动后默认访问：

```text
http://127.0.0.1:8787
```

常用启动参数：

```powershell
# 指定监听地址
.\vision-relay.exe -addr 127.0.0.1:8787

# 只运行后台中转服务，不打开桌面窗口
.\vision-relay.exe -no-window

# 不打开窗口，也不打开浏览器
.\vision-relay.exe -no-open

# 同时打开系统默认浏览器
.\vision-relay.exe -browser

# 指定配置文件和数据库路径
.\vision-relay.exe -config .\config.json -db .\vision-relay.db
```

## 编译 Windows 桌面客户端

在项目根目录执行：

```powershell
go test ./...
go build -ldflags="-s -w -H windowsgui" -o vision-relay.exe ./backend/cmd/vision-relay
```

说明：

- `-s -w` 用于减小二进制体积。
- `-H windowsgui` 会生成 Windows GUI 子系统程序，双击运行时不会弹出控制台窗口。
- 前端页面和图标资源会随 Go 编译一起打进 `vision-relay.exe`。

如果需要调试日志窗口，可以去掉 `-H windowsgui`：

```powershell
go build -ldflags="-s -w" -o vision-relay.exe ./backend/cmd/vision-relay
```

## 打包发布

本项目发布为单文件 Windows 可执行程序：

```powershell
go test ./...
go build -ldflags="-s -w -H windowsgui" -o vision-relay.exe ./backend/cmd/vision-relay
```

生成的文件：

```text
vision-relay.exe
```

发布到 GitHub Release 时建议使用版本标签：

```powershell
git tag v1.0.0
git push origin v1.0.0
```

Release 标题建议为：

```text
Vision Relay v1.0.0
```

附件上传：

```text
vision-relay.exe
```

## 配置说明

首次启动后，在桌面窗口中配置文本模型、视觉模型和本地客户端 API Key。

常用环境变量：

```text
VISION_RELAY_ADDR=127.0.0.1:8787

TEXT_PROVIDER=openai|anthropic|gemini|ollama
TEXT_BASE_URL=https://api.openai.com
TEXT_API_KEY=sk-...
TEXT_MODEL_OVERRIDE=
TEXT_WIRE_API=chat_completions|responses
TEXT_SUPPORTS_IMAGES=false

VISION_PROVIDER=openai|anthropic|gemini|ollama
VISION_BASE_URL=https://api.openai.com
VISION_API_KEY=sk-...
VISION_MODEL=gpt-4o-mini
VISION_ENABLED=true

CLIENT_API_KEYS=local-key-1,local-key-2
PROXY_URL=http://127.0.0.1:7890

OPEN_WINDOW=true
OPEN_BROWSER=false
```

本地客户端 API Key 用于外部客户端连接 Vision Relay，不是上游模型 API Key。客户端可以使用以下任一方式鉴权：

```text
Authorization: Bearer local-key-1
X-API-Key: local-key-1
?key=local-key-1
```

## 客户端接入示例

OpenAI 兼容客户端：

```text
Base URL: http://127.0.0.1:8787/v1
API Key:  local-key-1
Endpoint: /v1/chat/completions
```

Codex / Responses 客户端：

```text
Base URL: http://127.0.0.1:8787/v1
API Key:  local-key-1
Endpoint: /v1/responses
```

Anthropic / Claude Code 客户端：

```text
Base URL: http://127.0.0.1:8787
API Key:  local-key-1
Endpoint: /v1/messages
```

Gemini 客户端：

```text
Base URL: http://127.0.0.1:8787
API Key:  local-key-1
Endpoint: /v1beta/models/{model}:generateContent
```

Ollama 客户端：

```text
Base URL: http://127.0.0.1:8787
Endpoint: /api/chat 或 /api/generate
```

如果设置了本地客户端 API Key，Ollama 客户端也需要能附加 `Authorization`、`X-API-Key` 或 query `key`。

## 项目结构

```text
backend/cmd/vision-relay/      程序入口和 Windows exe 资源
backend/internal/server/       HTTP 服务、中转逻辑、配置、日志、托盘和 WebView
backend/internal/server/assets 图标资源
frontend/                      桌面客户端前端页面
tools/                         辅助工具
go.mod                         Go 模块依赖
```

## 常见问题

### 启动后窗口关闭，服务是否还在？

默认桌面模式下，关闭窗口不会退出服务。Vision Relay 会留在系统托盘中，可以从托盘菜单重新打开窗口或退出程序。

### 上游连接出现 `198.18.x.x`、超时或 fake-ip 问题怎么办？

在页面里的网络代理 URL 填写本地代理地址，例如：

```text
http://127.0.0.1:7890
```

### 文本模型本身支持图片怎么办？

在文本模型配置中启用“文本模型支持多模态”。启用后图片会直接发送给文本模型，不再先调用视觉模型解析。

## License

请在发布前根据项目实际授权方式补充 License。
