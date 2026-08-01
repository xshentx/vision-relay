# Vision Relay

Vision Relay 是一个本地桌面客户端式的多接口 AI 模型中转工具。它把外部客户端发来的图片请求先交给视觉模型解析，再把解析结果转成纯文本上下文转发给文本模型，让只支持文本的上游模型也能间接处理图片。

项目使用 Go 编写后端和桌面外壳，前端静态资源通过 `embed` 打进二进制。Windows 可编译成单个 `vision-relay.exe`，macOS 可编译成原生 `Vision Relay.app`；Windows 默认打开桌面窗口并驻留系统托盘，macOS 默认通过系统浏览器打开管理页面并驻留菜单栏。

## 功能特性

- Go 主程序管理界面优先监听 `http://127.0.0.1:18473`，端口被占用时自动选择其他本地可用端口，与中转 API 端口相互独立
- 本地 HTTP 中转 API 默认监听 `http://127.0.0.1:8787`
- Windows 桌面 WebView 与系统托盘菜单；macOS 菜单栏与系统浏览器管理页面
- 支持文本模型与视觉模型分开配置，并保存多套模型方案
- 支持 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini、Ollama 等常见接口形态
- 中转 API 默认仅监听 loopback 且无需访问令牌；改为非 loopback 地址时会自动生成并强制校验 Relay Token
- 支持为 Codex、OpenCode、Claude、OpenClaw 等客户端生成接入配置
- 模型供应商按 Codex、Claude、OpenCode、OpenClaw 独立分组，并提供运行状态与熔断保护
- 支持一键配置 Codex、OpenCode、Claude、OpenClaw
- 提供 Codex、Claude、OpenCode 相互独立的一键破甲、一键去除、未破甲基线恢复、提示词模板与会话清理测试工具；模板页内置 5 个 Codex-X 模板的受信任目录，并可按需从 GitHub 下载缓存正文
- 支持切换 Codex 第三方模型时保留官方登录，并可统一官方与第三方会话历史
- 内置请求日志、Token 统计、首 token 耗时、缓存命中等记录
- 支持网络代理 URL，适配本地代理或 fake-ip 网络环境
- 上游不支持流式响应时，可自动降级为同步请求并重新适配为客户端所需的 SSE、NDJSON 或 JSON 数组流

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

- Windows 10/11，或 macOS 11 Big Sur 及更新版本（Intel / Apple Silicon）
- Go 1.25 或更新版本
- Node.js 20 或更新版本（仅用于同步前端组件库资源）
- Windows：Microsoft Edge WebView2 Runtime
- macOS 源码构建：Xcode Command Line Tools（提供 Clang、Cocoa/WebKit Framework、`iconutil` 和 `codesign`）
- 可访问上游模型 API 的网络环境

> 大多数 Windows 10/11 系统已经内置 WebView2 Runtime。如果 Windows 桌面窗口无法打开，请先安装 Microsoft Edge WebView2 Runtime。macOS 菜单栏客户端使用系统浏览器打开管理页面，不需要额外安装 WebView Runtime。

## 源码运行

Windows PowerShell 或 macOS Terminal 均可直接运行：

```text
go run ./backend/cmd/vision-relay
```

启动后管理界面默认访问：

```text
http://127.0.0.1:18473
```

如果 `18473` 已被占用，程序会在日志中输出实际选择的管理地址，桌面窗口、托盘和浏览器会自动使用该地址；中转 API 端口不会因此改变。

中转 API 继续监听：

```text
http://127.0.0.1:8787
```

常用启动参数：

```powershell
# 指定中转 API 监听地址
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

默认数据库固定保存在程序可执行文件同一目录的 `vision-relay.db`，不会创建 `data` 子目录，也不会默认写入 `%APPDATA%` 或其他用户配置目录。升级时程序会从旧的程序目录数据库和历史用户配置目录数据库复制现有配置与日志；原数据库不会自动删除。可通过 `-db` 显式覆盖数据库路径。

## 编译 Windows 桌面客户端

首次构建或更新 Vue / Element Plus 依赖后，先同步本地前端资源：

```powershell
cd frontend
npm install
npm run build
cd ..
.\tools\build-windows.ps1
```

Vue 3 和 Element Plus 会复制到 `frontend/public/assets/vendor`，程序运行时不依赖 CDN。仅修改业务 HTML、CSS 或 JS 时，可直接执行 `.\tools\build-windows.ps1`。

说明：

- `-s -w` 用于减小二进制体积。
- `-H windowsgui` 会生成 Windows GUI 子系统程序，双击运行时不会弹出控制台窗口。
- 前端页面、图标和 Windows 版本信息会随 Go 编译一起打进 `vision-relay.exe`。
- 构建依赖 MinGW-w64 的 `windres.exe`；找不到资源编译器时脚本会直接失败，避免沿用旧版本的 VERSIONINFO。
- 构建脚本支持可选 Authenticode 代码签名：可设置 `WINDOWS_SIGNING_CERTIFICATE_PATH` 与 `WINDOWS_SIGNING_CERTIFICATE_PASSWORD`，或传入 `-SigningCertificatePath`。当前 GitHub 标签工作流未配置 Authenticode，下载运行时仍可能出现 Windows SmartScreen 或未知发布者提示；独立的 Ed25519 更新签名仅在启用 Windows 应用内自动替换更新时需要。

如果需要调试日志窗口，可以去掉 `-H windowsgui`：

```powershell
go build -ldflags="-s -w" -o vision-relay.exe ./backend/cmd/vision-relay
```

## 编译 macOS 桌面客户端

macOS 原生构建依赖 CGO、Cocoa 和 WebKit，因此必须在 macOS 上执行：

```bash
xcode-select --install  # 尚未安装 Command Line Tools 时执行
bash ./tools/build-macos.sh --version v2.3.2 --arch arm64
```

支持的架构参数：

- `--arch arm64`：Apple Silicon；
- `--arch amd64`：Intel Mac；
- `--arch universal`：分别构建 arm64 / amd64 后通过 `lipo` 合并为通用程序。

默认产物为 `dist/Vision Relay.app`，并同时生成 `dist/vision-relay-darwin-<架构>.zip` 及其 `.sha256`。脚本会创建标准应用目录、`Info.plist`、`.icns` 图标并执行 ad-hoc 签名。公开分发时仍建议使用 Apple Developer ID 重新签名并完成 notarization；否则其他 Mac 上首次打开时可能需要在“系统设置 → 隐私与安全性”中确认。

应用会在 macOS 菜单栏驻留，并通过系统默认浏览器打开管理页面；客户端发现支持 `/Applications` 和 `~/Applications` 中的 Codex / ChatGPT、OpenCode、Claude、OpenClaw 应用，同时继续从 `PATH` 检测 Codex CLI、Claude CLI、OpenCode 和 OpenClaw 命令。重复启动会通知现有实例再次打开管理页面，不会启动第二套数据库与路由服务。

## 打包发布

Windows 本地开发构建（未提供更新签名密钥时，构建产物会禁用自动替换更新）：

```powershell
.\tools\build-windows.ps1 -Version v2.3.2
```

如需启用 Windows 应用内自动替换更新，可选提供 Ed25519 更新签名私钥，使构建脚本把对应公钥嵌入 EXE，并生成独立的 `.sig` 附件：

```powershell
.\tools\build-windows.ps1 `
  -Version v2.3.2 `
  -UpdateSigningPrivateKeyPath C:\secure\vision-relay-update-signing.key `
  -RequireUpdateSignature
```

密钥文件内容必须是 base64 编码的 32 字节 Ed25519 seed 或 64 字节 Ed25519 private key。私钥应离线生成并存放于受控密钥存储中，绝不能提交到仓库。GitHub 标签工作流在配置了 `UPDATE_SIGNING_PRIVATE_KEY_B64` Secret 时会写入临时文件并在构建后立即删除；未配置时直接生成无更新签名的 Windows GUI 程序。

### Windows Authenticode 签名

1. 从受 Windows 信任的代码签名 CA 申请以个人或组织身份签发的 Authenticode 证书。自签名证书只适合内部测试，不能建立 SmartScreen 信誉，也通常不能降低第三方安全软件误报。
2. 安装 **Windows SDK**（提供 `signtool.exe`）和 **MinGW-w64**（提供 `windres.exe`）。构建脚本会自动查找 Windows SDK 中的 x64 `signtool.exe`。
3. 如果证书可导出为 PFX，使用上面的环境变量或以下参数构建：

```powershell
.\tools\build-windows.ps1 `
  -Output dist\vision-relay.exe `
  -Version v2.3.2 `
  -SigningCertificatePath C:\secure\vision-relay-code-signing.pfx `
  -SigningCertificatePassword '<PFX 密码>' `
  -RequireSignature
```

脚本先嵌入当前版本资源，再构建、执行 SHA-256/RFC 3161 时间戳签名并验证签名，随后生成已签名文件对应的 `.sha256`；提供更新签名密钥时还会对该摘要生成 Ed25519 `.sig`。可以再次手动验证 Authenticode：

```powershell
Get-AuthenticodeSignature .\dist\vision-relay.exe | Format-List Status,StatusMessage,SignerCertificate,TimeStamperCertificate
$signtool = Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin' -Filter signtool.exe -Recurse |
  Where-Object FullName -Match '\\x64\\signtool\.exe$' | Sort-Object FullName -Descending | Select-Object -First 1
& $signtool.FullName verify /pa /all /v .\dist\vision-relay.exe
```

`Status` 必须为 `Valid`。如果证书私钥位于 USB 硬件令牌或云签名服务中、无法导出 PFX，应使用证书颁发机构提供的 CSP/KSP 或云签名 GitHub Action；完成 Authenticode 签名后必须重新生成 `.sha256` 和 Ed25519 `.sig`，不要把任何私钥导出到仓库。

当前 GitHub 标签发布不读取 Authenticode 证书 Secret。若配置了 `UPDATE_SIGNING_PRIVATE_KEY_B64`，工作流会生成 Ed25519 更新签名；未配置时按无签名 GUI 模式直接编译，程序仍可检查和手动下载更新，但不会自动替换 EXE。以后若要接入 Authenticode，应使用证书或云签名服务，并在生成 `.sha256` 与可选 `.sig` 前完成签名和验证；不要把证书或更新签名私钥提交到仓库。

macOS 发布构建（在对应 Mac 或 macOS CI 上执行）：

```bash
bash ./tools/build-macos.sh --version v2.3.2 --arch universal
```

生成的 Release 附件：

```text
vision-relay.exe
vision-relay.exe.sha256
vision-relay.exe.sig（配置更新签名时生成）
vision-relay-darwin-universal.zip
vision-relay-darwin-universal.zip.sha256
```

发布到 GitHub Release 时建议使用版本标签：

```powershell
git tag v2.3.2
git push origin v2.3.2
```

Release 标题建议为：

```text
Vision Relay v2.3.2
```

附件上传时应包含对应平台的程序包和同名 `.sha256` 文件；启用 Ed25519 更新签名时，Windows 还应包含 `vision-relay.exe.sig`。macOS 也可以分别发布 `vision-relay-darwin-arm64.zip` 与 `vision-relay-darwin-amd64.zip`。

## 配置说明

首次启动后，在管理页面中配置文本模型和视觉模型即可使用本地 API。文本供应商列表中的“模型测试”可直接使用该供应商已配置的模型和 API Key 发送测试提示词，并显示响应内容、HTTP 状态及耗时；测试过程不会切换当前供应商或修改客户端路由；测试结果会写入请求日志，响应中的 Token 用量也会计入数据看板。供应商编辑弹窗中的眼睛按钮可临时显示或隐藏完整 API Key。

文本供应商按 **Codex**、**Claude**、**OpenCode**、**OpenClaw** 四组管理，每个供应商只能属于一组，每组独立保存当前选择。OpenAI Responses 请求使用 Codex 组，Anthropic Messages 使用 Claude 组，普通 Chat Completions、Gemini 与 Ollama 使用 OpenCode 组；OpenClaw 通过专属的本机 `/openclaw/v1` 路径使用 OpenClaw 组。点击某组供应商的“使用”只写入该组关联的客户端配置，不会覆盖其他客户端，并按“设置 → 客户端行为”自动重启已运行客户端或启动未运行客户端。

每组供应商都有独立熔断状态。熔断仅由真实转发请求驱动，不会在后台定期请求正常供应商，也不会用手动模型测试改变熔断状态。网络错误、限流、认证/模型不可用及大多数上游错误会记为可重试失败并触发故障转移；明确属于请求格式或语义错误的 400/405/406/413/414/415/422/501 不切换供应商，也不影响熔断状态。连续 5 次可重试失败会进入 30 秒熔断；冷却期内自动跳过，冷却结束后的下一次真实请求作为唯一的半开探测，成功即恢复，失败则重新熔断 30 秒。页面供应商卡片每 5 秒只刷新本地状态，不会向供应商发起健康检查。

常用环境变量：

```text
VISION_RELAY_ADDR=127.0.0.1:8787
VISION_RELAY_TOKEN=

TEXT_PROVIDER=openai|anthropic|gemini|ollama
TEXT_BASE_URL=https://api.openai.com
TEXT_API_KEY=sk-...
TEXT_MODEL_OVERRIDE=
TEXT_WIRE_API=chat_completions|responses

VISION_PROVIDER=openai|anthropic|gemini|ollama
VISION_BASE_URL=https://api.openai.com
VISION_API_KEY=sk-...
VISION_MODEL=gpt-4o-mini
VISION_ENABLED=true

PROXY_URL=http://127.0.0.1:7890

OPEN_WINDOW=true
OPEN_BROWSER=false
```

中转 API 默认监听 `127.0.0.1`，此 loopback 模式不校验访问令牌。将 `VISION_RELAY_ADDR` 或页面监听地址改为 `0.0.0.0`、`::` 或其他非 loopback 地址时，服务会自动生成并持久化 `VISION_RELAY_TOKEN`，所有兼容入口都必须通过 `Authorization: Bearer`、`X-API-Key` 或 `X-Local-Token` 提交该令牌；也可在首次启动前显式设置环境变量。一键配置会在认证实际启用时把 Relay Token 写入对应客户端，而不会把它转发给上游。Codex 开启“切换第三方时保留官方登录”时，loopback 模式仍可能写入仅用于本地路由隔离的无害 Bearer 标记；它不是上游 API Key。关闭本地 API 后，客户端改为直连当前文本供应商，并写入供应商 API 地址、上游令牌和真实模型名；模型列表仍只包含当前供应商配置中已经添加的模型，不会自动导入上游的全部模型。

“客户端接入”中的每个客户端都提供独立的**路由**开关。一键配置或点击该客户端分组供应商的“使用”都会写入对应客户端配置并自动开启路由，同时按“客户端行为”设置启动或重启客户端；关闭路由后，启动同步和其他分组的供应商切换不会重写该客户端配置。恢复 Codex 官方模式时会同时关闭 Codex 路由。

## 一键破甲（测试功能）

左侧“一键破甲”页面为本地测试工具，包含“提示词破甲”“会话清理”和“模板管理”三个区域：

- **提示词破甲**：可分别为 Codex、Claude、OpenCode 选择 v5、v35 或自定义模板，执行前先预览改动，并自动创建时间戳快照。
- **Codex 模式**：Profile 模式只写入 `CODEX_HOME/ctf.config.toml`，全局模式只管理 `config.toml` 顶层破甲字段，工作区模式使用独立工作区文件；不会覆盖客户端一键配置维护的供应商、模型和路由。
- **会话清理**：可扫描 Codex / Claude JSONL 与 OpenCode SQLite 会话，逐项预览拒绝回复替换，并可选择清理 Reasoning / Thinking 内容。
- **备份恢复**：三个客户端的破甲状态、配置快照和会话备份完全独立；配置应用和会话清理前都会自动备份，可从页面恢复指定历史版本。
- **安全边界**：文件操作会检查允许目录、路径越界和符号链接逃逸；破甲写入与一键配置、路由同步共用串行锁，避免同时修改同一配置文件。

> 该功能标记为测试功能。建议先查看“破甲预览”和会话修改前后对比，确认目标路径与影响范围后再执行；恢复操作只恢复当前选择的客户端或会话，不会修改另外两个客户端。

## 程序设置

左侧“设置”菜单可以管理 Vision Relay 的运行参数：

- Go 主程序管理界面优先监听 `127.0.0.1:18473`，不在设置页提供修改入口；若首选端口被占用，程序会自动改用其他本地可用端口。可修改本地中转 API 的监听地址和端口，保存后需重启 Vision Relay 才会重新绑定。
- 桌面 WebView、系统浏览器、托盘激活与管理接口会连接本次运行实际选定的管理端口；重复启动时由系统级单实例机制唤醒已运行窗口，不依赖固定端口。Codex、Claude、OpenCode、OpenClaw 及模型请求只连接可配置的中转 API 端口。
- 可独立关闭或开启本地 API 转发接口。关闭时，中转端口上的 `/v1/*` 等模型接口返回 `503`；管理端口上的管理页面、设置 API 和 `/healthz` 仍可使用。该开关保存后立即生效。
- 关闭本地 API 后，一键配置和文本供应商切换会让已配置客户端直连当前文本供应商，并写入供应商 API 地址、上游令牌和已添加模型的真实模型名（不会自动导入上游全部模型）；供应商故障转移和视觉模型中转都会停用，但已配置的 P1/P2 队列会保留，重新开启本地 API 后可再次启用故障转移。文本模型的图片能力按每个模型的“支持多模态”设置写入客户端。直连时 Codex 仅支持使用 Responses 协议的 OpenAI 兼容供应商，Claude 仅支持 Anthropic 协议供应商；协议不兼容或当前供应商未添加模型时会停止写入并给出提示。
- 可查看和修改 Codex、Codex CLI、OpenCode、Claude、Claude CLI、OpenClaw 的配置文件位置与客户端程序位置；Codex 桌面端与 CLI 共用配置，Claude 桌面端与 CLI 使用独立配置。
- Codex 桌面客户端支持自动检测 Microsoft Store 的 `OpenAI.Codex_*\app\ChatGPT.exe` 安装位置，不依赖固定版本号。
- Claude Desktop 与 Claude CLI 的路径检测和程序生命周期管理彼此独立：Windows 桌面端可识别 `%LOCALAPPDATA%\AnthropicClaude\claude.exe` 及 Squirrel 版本目录，macOS 桌面端可识别 `/Applications` 或 `~/Applications` 中的 `Claude.app`；CLI 在两端都可从 `PATH` 识别 `claude` 命令，二者不会互相误判。

首次运行时会自动检测一次客户端路径。从没有该检测字段的旧版本升级时，也会自动执行一次，之后不会反复覆盖手动填写的路径。如需刷新，可在设置页点击“重新检测客户端”。

客户端配置文件位置会实际用于“一键配置”、路由同步和 Codex 官方模式恢复。客户端程序位置用于检测运行状态，并按“设置 → 客户端行为”中的开关自动重启或启动客户端；这些操作由程序内置完成，不会弹出终端窗口。一键配置 Codex 或 Claude 时会同时处理对应桌面端与 CLI，接口和完成提示会列出实际写入的全部配置路径，并分别返回程序重启、启动或警告结果。默认自动重启配置前已运行的客户端，配置前未运行的客户端保持关闭。

## 客户端接入示例

OpenAI 兼容客户端：

```text
Base URL: http://127.0.0.1:8787/v1
API Key:  留空（本地 API 无需认证）
Endpoint: /v1/chat/completions
```

Codex / Responses 客户端：

```text
Base URL: http://127.0.0.1:8787/v1
API Key:  留空（本地 API 无需认证）
Endpoint: /v1/responses
```

Codex 桌面客户端推荐在“客户端接入”页面点击“一键配置 Codex”。Vision Relay 默认只写入用户级配置：

```text
CODEX_HOME/config.toml
CODEX_HOME/vision-relay-model.json
```

如果没有设置 `CODEX_HOME`，Windows 和 macOS 分别使用 `%USERPROFILE%\.codex` 与 `~/.codex`。只有调用客户端配置 API 时明确传入 `work_dir`，才会额外写入该项目的 `.codex/config.toml` 和 `.codex/vision-relay-model.json`，避免把 Vision Relay 自身的启动目录误当成项目目录。项目配置只包含 Codex 允许的模型和模型目录设置；Windows 还会写入 `sandbox = "unelevated"`，macOS 不会创建或改写 `[windows]` 段。`model_provider`、`model_providers.*` 及认证设置始终保留在用户级配置中，避免 Codex 忽略项目配置并显示警告。

用户级配置会使用 `model_providers.custom`、Responses wire API 和本机 `/v1` 地址。一键配置完全由 Vision Relay 内置逻辑直接写入配置文件，不调用终端命令。Vision Relay 每次启动还会重新同步已启用的客户端路由，以修复被其他工具改回的供应商选择。默认情况下，配置前已运行的客户端会自动重启，未运行的客户端不会被启动；可在“设置 → 客户端行为”中为 Codex、Codex CLI、OpenCode、Claude、Claude CLI、OpenClaw 分别调整。

“Codex 应用增强”提供两个独立开关：

- **切换第三方时保留官方登录**：默认开启。开启后会保留 `%CODEX_HOME%\auth.json` 中的官方 ChatGPT 认证，并让 Codex 继续识别和展示官方账号身份。本地 API 模式会在 provider 配置中写入仅发往本机 Vision Relay 的隔离 Bearer 标记，第三方模型请求不会使用官方登录令牌；关闭本地 API、让 Codex 直连供应商时，则写入真实供应商令牌。如果关闭该选项，本地 API 模式不会激活官方账号身份；直连模式下 Vision Relay 会先把官方认证备份到 `%CODEX_HOME%\vision-relay-auth.json`，再把真实供应商令牌写入托管认证，重新开启或恢复官方模式时会还原备份。
- **统一 Codex 会话历史**：默认关闭。开启时，如果当前正使用官方 `openai` 配置，会安全改为不带第三方 `base_url` 的 `custom` OpenAI provider；当前为第三方配置时不会被覆盖。关闭时只会还原带有 Vision Relay 专用标记的官方 provider，不会误改第三方配置。还可把 `sessions`、`archived_sessions` 中原 `openai` 会话和 `state_5.sqlite` 中原官方线程迁移为共享的 `custom` 标识，使官方与第三方会话显示在同一历史列表。“恢复官方模式”按钮在该开关开启时也会使用同一 `custom` OpenAI provider。

统一历史迁移前会把 JSONL 原文件、SQLite 快照和迁移 ID 账本保存到 `%CODEX_HOME%\vision-relay-history-backups\unified\<时间戳>`。关闭开关时可按账本精确恢复原官方会话；开启期间新建的第三方 `custom` 会话不会被误改回 `openai`。如果 `config.toml` 配置了 `sqlite_home`，或设置了 `CODEX_SQLITE_HOME`，也会查找对应目录下的 `state_5.sqlite`。

> 跨供应商继续旧会话时，对方后端可能无法解密会话中的 `encrypted_content` 推理内容，从而导致继续会话失败。迁移只统一历史归属，不保证加密推理内容能跨供应商复用。

同一页面也提供“一键配置 OpenCode”和“一键配置 Claude”：Windows 下 OpenCode 配置写入 `%USERPROFILE%\.config\opencode\opencode.json`，Claude 桌面配置写入 `%LOCALAPPDATA%\Claude-3p\configLibrary\<active-id>.json`，Claude CLI 配置写入 `%USERPROFILE%\.claude\settings.json`；macOS 下对应路径为 `~/.config/opencode/opencode.json`、`~/Library/Application Support/Claude-3p/configLibrary/<active-id>.json` 和 `~/.claude/settings.json`。现有配置中的其他字段会保留。

[OpenClaw](https://github.com/openclaw/openclaw) 可在同一页面点击“一键配置 OpenClaw”，Windows 和 macOS 默认分别写入 `%USERPROFILE%\.openclaw\openclaw.json` 与 `~/.openclaw/openclaw.json`。配置会新增 `vision-relay` 自定义供应商，通过 `openai-completions` 接入本机 `/openclaw/v1` 接口，同步当前模型映射、上下文窗口和图片输入能力，并将默认模型切换为 `vision-relay/<模型名>`。现有的其他 OpenClaw 配置会保留，写入前会在同目录生成带时间戳的备份。

如果设置了 `OPENCLAW_CONFIG_PATH`、`OPENCLAW_STATE_DIR` 或 `OPENCLAW_HOME`，Vision Relay 会按 OpenClaw 的路径规则写入对应配置。OpenClaw 配置文件支持 JSON5；一键配置可读取带注释、单引号和尾随逗号的现有文件，写回时会标准化为 JSON。详见 [OpenClaw 配置文档](https://docs.openclaw.ai/gateway/configuration)。

Anthropic / Claude 客户端：

```text
Base URL: http://127.0.0.1:8787
API Key:  留空（本地 API 无需认证）
Endpoint: /v1/messages
```

Gemini 客户端：

```text
Base URL: http://127.0.0.1:8787
API Key:  留空（本地 API 无需认证）
Endpoint: /v1beta/models/{model}:generateContent
```

Ollama 客户端：

```text
Base URL: http://127.0.0.1:8787
Endpoint: /api/chat 或 /api/generate
```

Ollama 客户端可直接调用本地接口，不需要附加 `Authorization`、`X-API-Key` 或 query `key`。

## 项目结构

```text
backend/cmd/vision-relay/              程序入口和 Windows 专用 exe 资源
backend/internal/protocol/             OpenAI Responses 与 Anthropic 协议转换
backend/internal/server/               HTTP 服务、中转、配置、日志和跨平台桌面集成
backend/internal/server/assets/        桌面程序图标资源
frontend/assets.go                     前端静态资源嵌入入口
frontend/public/index.html             桌面客户端页面结构
frontend/public/assets/css/            页面样式
frontend/public/assets/js/             页面交互逻辑
frontend/public/assets/images/         页面图标资源
tools/                                 构建和辅助工具
go.mod                                 Go 模块依赖
```

## 常见问题

### 启动后窗口关闭，服务是否还在？

Windows 默认桌面模式下，关闭窗口不会退出服务，可从系统托盘重新打开窗口或退出程序。macOS 会驻留菜单栏并使用系统浏览器显示管理页面，可从菜单栏再次打开页面或退出程序。

### 上游连接出现 `198.18.x.x`、超时或 fake-ip 问题怎么办？

在页面里的网络代理 URL 填写本地代理地址，例如：

```text
http://127.0.0.1:7890
```

### 文本模型本身支持图片怎么办？

在文本模型配置的“模型映射”列表中，为具体模型勾选“支持多模态”。勾选后图片会直接发送给该模型；同一供应商下未勾选的模型仍会在视觉能力开启时先调用视觉模型解析。

## 参考项目与授权

- 以下破甲模板来自 [yynxxxxx/Codex-X](https://github.com/yynxxxxx/Codex-X)（MIT License）。Vision Relay 的 EXE 仅内置文件名与用途组成的受信任目录，不嵌入模板正文，以降低安全软件对安全研究术语的静态误报。程序不会在后台静默拉取；只有用户点击“GitHub 更新”时才检查、校验并缓存上游 `examples/` 正文。

| 受信任目录项 | 用途 |
| --- | --- |
| `gpt5.5-unrestricted.md` | 短小通用，适合日常编码与常规技术任务 |
| `gpt5.4-unrestricted.md` | 面向 GPT-5.4 / Codex CLI，偏 CTF 与安全研究工作流 |
| `gpt5.5-jeli.md` | 大白话通用版本，提供更完整的工程与逆向执行流程 |
| `gpt-5.6-sol-unrestricted.md` | GPT-5.6-sol 破甲提示词，偏直接执行与中英文任务 |
| `海鸥3.0破甲.md` | 中文技术操作员人格，覆盖编码、CTF、逆向、内存与协议任务路由 |

Codex-X 的原始 MIT 许可文本保存在 `backend/internal/server/break_armor_codex_x_templates/LICENSE.codex-x`。

## License

Vision Relay 采用 [MIT License](LICENSE) 开源。

项目包含的第三方依赖、资源和模板仍遵循其各自许可证。Codex-X 模板的原始 MIT 许可见 [`backend/internal/server/break_armor_codex_x_templates/LICENSE.codex-x`](backend/internal/server/break_armor_codex_x_templates/LICENSE.codex-x)。

## 自动更新

Windows 与 macOS 桌面版默认都会在启动后访问 GitHub Releases 检查新版本，可在左侧“更新”页面关闭自动检测，也可以随时手动检查。默认通过公开的 Release feed 读取版本信息，不消耗 GitHub 匿名 REST API 的每 IP 配额；如需使用 REST API 完整元数据，可选设置 `VISION_RELAY_GITHUB_TOKEN`，API 限流或故障时仍会自动回退到 Release feed。Windows 版支持“下载更新并重启”；macOS 首版会匹配当前架构的 Release 压缩包并引导手动下载，不会直接替换 `.app`（避免破坏 Developer ID 签名与 notarization）。Windows 自动更新流程如下：

1. 从 `xshentx/vision-relay` 的最新 GitHub Release 下载 `vision-relay.exe`；
2. 必须下载 `vision-relay.exe.sha256` 并验证 SHA-256；缺少或校验失败时拒绝安装；
3. 必须下载 `vision-relay.exe.sig`，并使用当前 EXE 内嵌的 Ed25519 公钥验证 SHA-256 摘要的发布者签名；未嵌入可信公钥、缺少签名或验签失败时拒绝安装；
4. 将当前程序直接换名备份为 `vision-relay.exe.old`，从固定的非可执行暂存文件 `vision-relay.update` 写入新版本并启动规范名称的新程序；
5. 旧实例会确认新进程在启动检查窗口内没有提前退出；若安全软件隔离新文件或新程序立即崩溃，则恢复旧文件并保持旧实例运行；
6. 新程序等待旧实例退出后获取单实例锁，并只清理程序目录内经过白名单验证的 `.old`、`vision-relay.update` 或旧版兼容暂存文件。

发布构建时请传入与 Git tag 相同的版本号：

```powershell
.\tools\build-windows.ps1 `
  -Version v2.3.2 `
  -UpdateSigningPrivateKeyPath C:\secure\vision-relay-update-signing.key `
  -RequireUpdateSignature
```

提供更新签名密钥时，构建脚本会生成 `vision-relay.exe`、`vision-relay.exe.sha256` 和 `vision-relay.exe.sig`，发布 Release 时应同时上传三个文件。未提供密钥时只生成 EXE 与 `.sha256`，程序可检查并手动下载更新，但不会自动替换。SHA-256 用于验证下载完整性，Ed25519 签名在此基础上验证更新发布者；两者不能互相替代。Authenticode 和证书信誉是 Windows/SmartScreen 的另一层信任机制，当前标签工作流未配置该证书，因此首次运行仍可能出现 SmartScreen 或未知发布者提示。
