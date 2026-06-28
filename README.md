# Codex Proxy

本地客户端式多接口模型中转工具。它可以让只支持文本的模型“间接”处理图片：客户端把图片请求发到本机服务，服务先调用视觉模型解析图片，再把图片解析结果转成纯文本上下文，转发给文本模型。

前端页面会被 Go `embed` 打进二进制，启动时默认以桌面客户端窗口打开，不再跳转默认浏览器。最终可以发布为单个 Windows exe。

## 支持的入口

| 客户端协议 | 本地路径 | 说明 |
| --- | --- | --- |
| OpenAI Chat Completions | `/v1/chat/completions` 或 `/chat/completions` | 支持 `image_url`、`input_image` |
| Codex / OpenAI Responses | `/v1/responses` 或 `/responses` | 支持 `input_text`、`input_image`，会转成文本上游可理解的纯文本 |
| Anthropic Messages | `/v1/messages` 或 `/messages` | 支持 `content[].type=image` |
| Gemini | `/v1beta/models/{model}:generateContent` | 支持 `inline_data`、`file_data` |
| Ollama Chat | `/api/chat` | 支持 `images` |
| Ollama Generate | `/api/generate` | 支持 `images` |
| 其他路径 | 原路径透传 | 例如 `/v1/models`、`/api/tags` |

## 运行

```powershell
go run .
```

默认地址：

```text
http://127.0.0.1:8787
```

默认会打开 `Codex Proxy` 桌面窗口，并在系统托盘驻留。关闭窗口不会停止中转服务，可从托盘菜单重新打开窗口；需要真正退出时使用托盘菜单里的“退出”。

只作为后台接口服务运行：

```powershell
.\codex-proxy.exe -no-window
```

需要调试时也打开浏览器：

```powershell
.\codex-proxy.exe -browser
```

## 编译单 exe

```powershell
go build -ldflags="-s -w" -o codex-proxy.exe ./backend/cmd/codex-proxy
```

如果不想显示控制台窗口，可以编译 Windows GUI 子系统：

```powershell
go build -ldflags="-s -w -H windowsgui" -o codex-proxy.exe ./backend/cmd/codex-proxy
```

运行：

```powershell
.\codex-proxy.exe
```

## 配置

打开客户端窗口配置文本模型、视觉模型和对外客户端 API Key。文本模型与视觉模型分别有独立的列表，可以新增多套配置、切换当前使用项，也可以删除不再使用的方案。当前选中的文本模型负责最终回答，当前选中的视觉模型负责图片解析。

常用环境变量：

```text
TEXT_PROVIDER=openai|anthropic|gemini|ollama
TEXT_BASE_URL=https://api.openai.com
TEXT_API_KEY=sk-...
TEXT_MODEL_OVERRIDE=
PROXY_URL=http://127.0.0.1:7890

VISION_PROVIDER=openai|anthropic|gemini|ollama
VISION_BASE_URL=https://api.openai.com
VISION_API_KEY=sk-...
VISION_MODEL=gpt-4o-mini

CLIENT_API_KEYS=local-key-1,local-key-2
OPEN_WINDOW=true
OPEN_BROWSER=false
```

图片解析提示词已内置，不需要在页面或环境变量里配置。客户端 API Key 可以在页面里按客户端名称一键生成，可以生成多个，也可以删除不用的令牌。Key 格式为：

```text
sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

如果上游连接报 `198.18.x.x`、超时或 fake-ip 相关错误，在页面的“网络代理 URL”里填你的本地代理地址，例如 `http://127.0.0.1:7890`。

页面里生成的客户端 API Key 是给外部客户端连接本中转服务使用的 Key，不是上游模型 Key。客户端可使用：

```text
Authorization: Bearer local-key-1
```

或：

```text
X-API-Key: local-key-1
```

Gemini 客户端如果只能用 query 参数，也可以用：

```text
?key=local-key-1
```

## 客户端接入示例

OpenAI 兼容客户端：

```text
Base URL: http://127.0.0.1:8787/v1
API Key:  local-key-1
接口:     /v1/chat/completions
```

Codex / Responses 客户端：

```text
Base URL: http://127.0.0.1:8787/v1
Endpoint: /v1/responses
API Key:  local-key-1
```

也可以直接请求完整路径：

```text
POST http://127.0.0.1:8787/v1/chat/completions
POST http://127.0.0.1:8787/v1/responses
```

Anthropic 客户端：

```text
Base URL: http://127.0.0.1:8787
API Key:  local-key-1
```

Gemini 客户端：

```text
Base URL: http://127.0.0.1:8787
API Key:  local-key-1
```

Ollama 客户端：

```text
Base URL: http://127.0.0.1:8787
```

如果设置了客户端 API Key，Ollama 客户端也需要能附加 `Authorization` 或 `X-API-Key`。

## 工作方式

1. 接收客户端请求。
2. 识别请求中的图片字段。
3. 调用配置的视觉上游，把图片解析为文本。
4. 删除原图片字段，把“用户需求 + 图片解析结果”写回请求。
5. 调用配置的文本上游，并把响应原样返回给客户端。

这意味着最终文本上游可以是非多模态模型，只要它能理解文字即可。

## 项目结构

```text
main.go        启动入口、HTTP 服务和窗口/托盘编排
config.go      配置读写、默认值、上游端点配置
routing.go     请求入口路由和协议路径识别
handlers.go    OpenAI、Responses、Anthropic、Gemini、Ollama 请求处理
parsing.go     各协议消息和图片字段解析
vision.go      视觉模型调用和图片转文本
proxy.go       上游转发、URL 拼接、鉴权头处理
http_util.go   HTTP 响应、鉴权、CORS 等公共工具
desktop.go     WebView 客户端窗口和系统托盘
key.go         客户端 API Key 生成
assets.go      前端静态资源嵌入
types.go       核心数据结构
util.go        通用小工具
backend/cmd/codex-proxy/       程序入口与 Windows exe 资源
backend/internal/server/       Go 后端、中转服务、托盘/WebView、数据库
frontend/      桌面客户端前端
```
