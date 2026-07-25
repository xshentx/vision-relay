# Vision Relay

Vision Relay 是一个本地桌面客户端式的多接口 AI 模型中转工具。它把外部客户端发来的图片请求先交给视觉模型解析，再把解析结果转成纯文本上下文转发给文本模型，让只支持文本的上游模型也能间接处理图片。

项目使用 Go 编写后端和桌面外壳，前端静态资源通过 `embed` 打进二进制。Windows 可编译成单个 `vision-relay.exe`，macOS 可编译成原生 `Vision Relay.app`；Windows 默认打开桌面窗口并驻留系统托盘，macOS 默认通过系统浏览器打开管理页面并驻留菜单栏。

## 功能特性

- Go 主程序管理界面默认监听 `http://127.0.0.1:18473`，与中转 API 端口相互独立
- 本地 HTTP 中转 API 默认监听 `http://127.0.0.1:8787`
- Windows 桌面 WebView 与系统托盘菜单；macOS 菜单栏与系统浏览器管理页面
- 支持文本模型与视觉模型分开配置，并保存多套模型方案
- 支持 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini、Ollama 等常见接口形态
- 本地 API 默认无需访问令牌，可直接接入兼容客户端
- 支持为 Codex、OpenCode、Claude、OpenClaw 等客户端生成接入配置
- 文本供应商按 Codex、Claude、OpenCode 独立分组，并提供运行状态与熔断保护
- 支持一键配置 Codex、OpenCode、Claude、OpenClaw
- 提供 Codex、Claude、OpenCode 相互独立的一键破甲、提示词模板、会话清理与备份恢复测试工具；模板页内置 5 个 Codex-X 模板的受信任目录，并可按需从 GitHub 下载缓存正文
- 支持切换 Codex 第三方模型时保留官方登录，并可统一官方与第三方会话历史
- 内置请求日志、Token 统计、首 token 耗时、缓存命中等记录
- 支持网络代理 URL，适配本地代理或 fake-ip 网络环境
- 上游不支持流式响应时，可自动降级为同步请求并重新适配为客户端所需的 SSE、NDJSON 或 JSON 数组流

## 版本更新

### v2.2.2

- 重构概览页信息层级与文案，明确展示管理界面和中转 API 是两套独立地址，并在首页直接显示当前管理地址、中转地址及本地 API 启停状态。
- 修复概览页此前把管理页面地址误当作中转 API 地址的问题；保存程序设置后会立即刷新中转状态，并在地址需要重启后生效时给出明确提示。
- 更新首页中转链路说明和客户端标签，补充 Codex、Claude、OpenCode、OpenClaw 的桌面接入定位，并将协议适配、文本与视觉模型分流关系展示得更清晰。
- 常用路由入口新增 Gemini GenerateContent，并完善地址换行、路由徽标和文本/中转指标配色，提升长地址及多协议信息的可读性。
- 增加管理地址、中转地址、启停状态、重启提示、Gemini 路由和概览样式的前端嵌入资源回归测试。

### v2.2.1

- 重构 Windows 自动更新流程：运行中的程序直接备份自身并从固定的非 EXE 暂存文件 `vision-relay.update` 写入新版本，不再创建或执行点开头、随机名称的临时 helper EXE；新进程启动失败或提前退出时自动回滚并保持旧实例运行。
- 强制 Windows 自动更新同时下载并验证 `vision-relay.exe.sha256`，新增父进程等待、启动存活检查和更新文件白名单清理，只处理程序目录内允许的 `.old`、暂存文件及旧版兼容文件，降低更新误报与误删风险。
- Windows 构建脚本会根据版本号重新生成图标与 `VERSIONINFO` 资源，并保留可选的 Authenticode 签名、时间戳和签名验证能力；v2.2.1 GitHub 标签发布按无签名模式直接编译，并为最终 EXE 生成 SHA-256。
- Codex-X 模板改为只在程序内保留 5 个受信任目录项，不再把安全研究模板正文嵌入 EXE；用户点击“GitHub 更新”后才下载、校验并缓存只读模板，以减少安全软件静态扫描误报。
- 新增项目根目录 MIT License，更新模板来源、自动更新、Windows 构建和发布文档，并补充更新回滚、安全清理、模板同步及前端提示的回归测试。

### v2.2.0

- 将 Go 主程序管理界面与本地中转 API 拆分为两个独立监听器：管理界面默认使用 `127.0.0.1:18473`，中转 API 继续使用 `127.0.0.1:8787`；管理页面和管理接口不会暴露在中转端口，模型路由也不会进入管理端口。
- 新增管理地址配置、`-management-addr` 启动参数和 `VISION_RELAY_MANAGEMENT_ADDR` 环境变量，阻止管理端口与中转端口冲突；桌面窗口、浏览器和托盘激活固定连接管理端口，客户端配置固定使用中转 API 地址。
- 增加管理端与中转端的独立健康标识及中转状态检查接口，前端可单独显示中转 API 在线状态，并完善 IPv4、IPv6 与通配监听地址的本地可访问 URL 处理。
- 扩展一键破甲首页的“其他方案”：Codex、Claude 和 OpenCode 可直接选择按需从 GitHub 缓存的 Codex-X 模板，并保留自定义模板、当前选择摘要、响应式布局与键盘焦点状态。
- 更新设置界面、接入地址说明和 README，并增加双监听器隔离、端口校验、桌面激活、路由来源、中转状态以及破甲模板交互的回归测试。

### v2.1.3

- 修复启用本地 API 时切换文本供应商仍会重写客户端配置并触发客户端重启的问题；现在仅持久化服务端实时配置，避免中断当前会话。
- 本地 API 模式切换保存失败时自动恢复先前供应商与旧版路由状态，避免界面状态和服务配置不一致。
- 保持直连模式原有的客户端配置与重启流程，并增加本地 API / 直连分支、提前返回和切换成功提示的前端资源回归测试。

### v2.1.2

- 重构 Windows 自动更新流程：运行中的主程序直接将自身换名备份并启动规范名称的新版本，不再创建或执行点开头、随机名称的临时 helper EXE，降低端点安全软件把更新行为误判为投放器的问题。
- 更新下载使用固定的非 EXE 暂存名 `vision-relay.update`，强制要求 Release 提供同名 `.sha256` 校验文件；旧版本备份与暂存文件会在新版本启动后清理，无法立即删除时安排在系统重启后清理。
- 强化更新失败恢复：写入、创建新进程失败，或新进程在启动存活检查期间提前退出时，自动移除不完整文件并恢复 `.old`；旧实例保持运行，不再依赖额外 helper 或 `.update-error.txt`。
- 改进更新页面交互：开始安装后自动进入更新页面，任务执行期间保持相关按钮禁用，并完整展示启动失败与后台更新进度。
- 增加固定暂存文件、强制 SHA-256、无 helper 替换重启、失败回滚、旧版本恢复以及前端状态的回归测试。

### v2.1.1

- 新增 5 个 Codex-X Markdown 破甲模板的固定受信任目录；模板正文不再嵌入 Windows EXE，以降低安全软件对安全研究术语的静态误报，并保留原始 MIT 许可与模板来源说明。
- 提示词模板管理新增显式“GitHub 更新”操作：仅在用户点击后检查 Codex-X `examples/`，按当前客户端缓存新版；更新模板保持只读，不覆盖本地自定义模板。
- 模板列表新增来源、GitHub 缓存状态、用途描述与原始文件链接，载入只读模板时会禁用保存和删除操作。
- 强化远程模板同步校验：限定仓库、HTTPS 下载域名、目录和文件白名单，禁止重定向，限制目录项数与单文件大小，并使用 Git Blob SHA-1 验证下载内容。
- 补充 Codex-X 受信任目录、按需同步、缓存替换与去重、只读边界、下载完整性与前端交互回归测试，同时完善 README 使用和授权说明。
- 统一内置模板的换行格式与跨平台哈希校验，修复 macOS Release 构建因 Windows / Unix 换行差异失败的问题。

### v2.1.0

- 新增 Codex、Claude、OpenCode 三类文本供应商分组，每组独立保存当前供应商；Responses、Anthropic Messages 与 Chat Completions / Gemini / Ollama 请求会按协议进入对应分组，切换供应商不再影响其他客户端。
- 新增供应商运行状态与熔断保护：连续失败达到阈值后暂时阻止请求，冷却后通过半开探测自动恢复；管理页面实时展示正常、熔断和探测状态，请求日志记录实际命中的供应商。
- 重构客户端一键配置：Codex 桌面端与 CLI、Claude Desktop 与 CLI 可一次同步，OpenClaw 跟随 OpenCode 分组；补充跨平台配置路径、客户端发现、启动与单实例处理。
- 新增 macOS 11+ 桌面客户端，支持 Intel、Apple Silicon 与 Universal 应用包、菜单栏驻留、浏览器管理页面、架构匹配更新检查及 macOS 构建脚本。
- 修复流式请求断开后的末尾用量采集，以及重复 usage 快照、缓存字段别名、Anthropic 增量、Gemini Thinking Token 等统计问题，并补充协议、路由、配置和桌面平台回归测试。
- 新增 GitHub Actions 双平台发布流程，标签发布时自动构建 Windows EXE 与 macOS Universal ZIP，并上传对应 SHA-256 校验文件。

### v2.0.2

- 修正数据仪表盘 Token 口径：“输入”统一表示包含缓存命中的全部输入 Token，缓存命中与缓存写入作为输入明细展示，不再与输入重复相加。
- 同步调整 Token 趋势、Token 构成和模型用量排行的标签与计算逻辑；总量仅由输入与输出组成，避免缓存明细导致汇总值重复计算。
- 更新前端嵌入资源回归测试，并删除 README 中已过时的界面预览章节。

### v2.0.1

- 修复 OpenAI Responses / Codex 流式请求在客户端提前断开后丢失末尾用量事件的问题；响应头已建立后会在最多 15 秒内继续排空上游流，保留最终 Token 统计，同时仍会及时取消尚未建立响应的请求。
- 修正请求日志中的缓存 Token 统计：Anthropic 顶层缓存读取 / 写入会计入派生总量，OpenAI 兼容接口的嵌套缓存明细不会被重复计算。
- 数据仪表盘新增“非缓存输入”和“缓存写入”指标，并同步到汇总卡片、趋势图、Token 构成和模型用量排行。
- 统一 Anthropic 与 OpenAI 兼容接口的缓存计量语义，并修正全部周期的 SQLite 月度聚合，兼容旧版本历史日志。
- 补充客户端断连排流、超时边界、缓存计量归一化和长期聚合回归测试。

### v2.0.0

- 新增“一键破甲”测试功能，为 Codex、Claude 和 OpenCode 提供相互独立的配置预览、应用、快照与恢复，并支持 v5、v35 和自定义提示词模板。
- 新增会话清理工具，可扫描 Codex / Claude JSONL 与 OpenCode SQLite，检测拒绝回复、按项替换内容、清理 Reasoning / Thinking，并通过独立备份恢复。
- 增强全协议流式兼容：当 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini 或 Ollama 上游不支持流式输出时，自动使用同步请求并转换回客户端需要的流格式。
- 修复 Responses 异常流和请求日志状态统计，清理历史 `"null"` / `"<nil>"` 错误记录，避免未完成且无 Token 用量的流被误记为成功。
- 破甲、客户端一键配置和路由同步改为串行写入，避免并发修改配置文件；补充路径边界、快照回滚、模式隔离及流式降级回归测试。

### v1.9.0

- 新增文本供应商“模型测试”抽屉，可选择已配置模型并输入提示词直接检测上游连通性，不会切换当前路由；支持 OpenAI Chat Completions/Responses、Anthropic Messages、Gemini 和 Ollama。
- 模型测试结果会展示模型回复、HTTP 状态、响应耗时和请求 ID，并提供加载、成功、失败及取消状态；测试接口纳入本地管理访问控制，阻止远程和跨域调用。
- 文本供应商编辑弹窗新增 API Key 显示/隐藏按钮；模型映射简化为单一“请求模型”字段，同时保留上下文窗口、多模态和推理强度设置，并优化桌面与移动端布局。
- 修复 Windows 内置更新在父进程受 Job Object 管理时可能无法重启的问题：更新助手和新版本进程改为分离启动，支持脱离作业对象、准确等待旧进程退出、保留工作目录与启动参数，并在重启失败时回滚旧版本。
- 补充模型测试供应商协议、错误处理、API Key 可见性、模型映射、管理访问控制及 Windows 更新替换与重启相关测试。

### v1.8.3

- Windows 桌面端新增用户级全局单实例机制，在加载配置、数据库和客户端集成前取得原生互斥量；重复启动只向主实例发送激活事件，不再创建多余窗口或残留进程。
- 优化桌面窗口激活与状态管理：重复启动、托盘操作或本地激活接口会恢复并聚焦已有窗口；端口占用回退会校验 Vision Relay 应用身份，避免误识别其他本地服务。
- 修复 Codex 配置层级：供应商、认证信息只写入用户配置，项目配置仅保留模型、目录和沙箱设置；程序启动时会自动重新同步已启用的客户端路由。
- 启动时自动校正已启用的 Codex 统一会话历史配置，并支持继续处理未完成的迁移备份，避免切换官方供应商或迁移中断后需要手动关闭再开启功能。
- 补充单实例边界、窗口激活、管理接口访问控制、Codex 路由与统一历史恢复等测试。

### v1.8.2

- 更新下载改为后台任务，新增 `/api/update/progress` 进度接口，界面可实时显示下载、校验、安装和重启状态，并阻止重复更新任务。
- 新增可关闭的启动自动检测更新设置和新版本提示弹窗，相关偏好会随程序配置持久化保存。
- 数据仪表盘新增“近 30 天”和“全部”周期；全部统计按月聚合，避免加载完整原始日志，同时优化长期数据的趋势展示。
- 统一原生下拉菜单为 Element Plus 选择组件，优化弹窗、多选、动态选项及移动端交互，并重新整理业务页面和请求日志的紧凑布局。
- 更新 Windows 应用、WebView 和网页图标资源，构建脚本会自动生成多尺寸图标并在可用时通过 `windres` 嵌入 EXE。
- 补充后台更新进度、自动检测设置、全部周期聚合、下拉组件、页面布局和图标构建相关测试。

### v1.8.1

- 修复 OpenAI Responses 流式转换在上游连接截断或扫描异常时仍可能输出完成事件的问题，异常流现在会返回 `response.failed`。
- 兼容未发送 `[DONE]`、但已通过 `finish_reason` 正常结束的上游流，避免完整响应被误判为异常。
- 将 Responses 的 `response.incomplete` 视为合法终止状态，避免因输出达到限制等正常情况被错误记录为网关失败。
- 修复数据仪表盘在夏令时切换日期附近的日序号偏移，近 7 日和月度统计改为按自然日稳定分桶。
- 仪表盘紧凑数字新增十亿级 `B` 单位显示，并补充流式异常、终止状态、夏令时分桶和前端格式化相关测试。

### v1.8.0

- 新增数据统计仪表盘，可按今日、近 7 日和本月查看 Token 用量、请求量、模型排行与响应性能，并支持供应商、模型筛选以及按 Token 类型或模型切换趋势图。
- 新增 `/api/dashboard` 管理接口和 SQLite 聚合查询，统计累计与周期用量，按时间桶、供应商和模型生成分析数据。
- 重构请求日志卡片，以模型为主标题，增加供应商和流式/非流式标识，集中展示输入、输出、缓存命中、首 Token 与总耗时；数据库自动迁移并保存请求模式。
- 增强 Anthropic Messages 流式兼容，保持上游 SSE 实时转发，支持文本与工具调用增量、Usage 统计和 JSON 回退转换，并能识别上游流异常中断。
- 完善 Responses/SSE 日志统计，支持命名事件、Usage 尾部保留及异常流状态识别，避免失败或不完整响应被记录为成功。
- 移除旧版请求测试台、视觉调试接口和相关入口，重新整理侧边栏、首页仪表盘及日志界面的桌面端与移动端布局。

### v1.7.1

- 文本模型配置中的“方案名称”调整为“供应商名称”，请求日志优先显示供应商名称，不再重复附加供应商类型。
- 文本与视觉模型方案列表的编辑、删除操作默认收起，在鼠标悬停或键盘聚焦时显示；触摸设备继续保持操作按钮可见。
- 模型配置弹窗新增视口高度限制和内部滚动，内容较多时不会超出屏幕。
- 优化模型选择面板布局，搜索框、模型列表和操作区改为全宽展示，提升模型较多时的可读性与操作体验。
- 新增模型选择界面预览，并补充供应商名称、方案操作和弹窗布局相关的前端资源测试。

### v1.7.0

- 客户端程序管理拆分桌面端与终端端，Codex、Codex CLI、Claude 和 Claude CLI 可分别检测路径、配置自动重启及未运行时自动启动。
- 一键配置 Codex 或 Claude 时会同时处理对应桌面客户端和 CLI，并在界面中分别反馈每个程序的重启、启动或警告状态。
- Claude 桌面端改用第三方配置库的 Gateway 配置格式，自动同步模型、认证方式并激活 Vision Relay 配置；Claude CLI 继续独立写入 `settings.json`。
- 自动迁移旧版 Claude CLI 配置路径和生命周期偏好，避免桌面端与 CLI 共用路径导致配置写错。
- 增强 Windows 客户端检测，支持 Codex CLI、Claude CLI 以及 Claude Desktop Squirrel 版本目录中的运行进程。
- 修正 Codex 保留官方登录时的本地隔离认证配置，避免官方令牌被错误发送到第三方供应商。
- 重构客户端路径与启动行为设置界面，采用桌面端/CLI 分行表格布局并优化窄屏显示。
- 补充客户端配置、路径迁移、进程识别、生命周期管理和前端资源测试。

### v1.6.0

- 本地 API 取消访问令牌验证，移除访问令牌管理页面和令牌生成接口，兼容客户端可直接连接。
- 一键配置不再写入本地 API 的真实访问令牌；Codex 开启“保留官方登录”时会写入仅发往本地 Vision Relay 的隔离标记，避免官方令牌被用于第三方请求。
- 关闭本地 API 后，已配置客户端可直连当前文本供应商，自动写入上游地址、真实令牌和已添加模型。
- 直连模式新增协议兼容性检查：Codex 支持 OpenAI Responses，Claude 支持 Anthropic，配置不兼容时会明确提示。
- 直连模式按模型同步多模态能力；中转模式同时识别文本模型原生图片能力和视觉模型中转能力。
- 新增浏览器跨域访问保护，在取消本地令牌后继续阻止非同源网页调用本地 API。
- 清理 OpenClaw 中已失效的 Vision Relay 模型引用，优化客户端配置、日志字段、响应文本解析和相关测试。
- 更新客户端接入界面、配置说明和测试覆盖。

### v1.5.0

- 新增程序设置页面，支持管理本地 API 监听地址、端口和转发接口启用状态。
- 新增客户端独立路由开关，切换模型供应商时仅同步已开启路由的客户端配置。
- 支持自定义并自动检测 Codex、OpenCode、Claude 和 OpenClaw 的配置文件及程序位置。
- 新增客户端自动重启和自动启动设置，一键配置由程序内置执行，不再弹出终端窗口。
- 增强 Windows 客户端进程识别、停止与启动逻辑，支持桌面程序、CLI 包装程序和进程树处理。
- Codex 桌面客户端支持动态检测 Microsoft Store 安装位置，不依赖固定版本号。
- 新增本地管理界面访问保护，限制管理页面和设置 API 从非本地来源访问。
- 优化客户端接入、程序设置和更新页面，并补充路径检测、路由、进程管理和前端资源测试。

### v1.4.0

- 新增 OpenClaw 一键配置，支持 JSON5 配置、环境变量路径、配置备份和模型能力同步。
- Codex、OpenCode、Claude、OpenClaw 改为分别创建或复用独立访问令牌。
- 模型映射新增推理强度设置，支持 `none`、`low`、`medium`、`high` 和 `xhigh`。
- 支持从旧版 `supports_reasoning` 配置迁移，并自动识别常见推理模型。
- 优化 Codex 模型目录、默认推理强度、客户端配置预览和请求日志归属。
- 补充 OpenClaw、推理能力、客户端令牌和前端资源测试。

### v1.3.0

- 全面升级桌面端界面，优化首页、Token 管理、模型配置和客户端接入页面。
- 引入 Vue 3 和 Element Plus 本地资源，新增统一确认弹窗与消息提示，运行时不依赖 CDN。
- 新增 Codex 官方登录保留开关，切换第三方模型时可保留官方 `auth.json`。
- 新增 Codex 统一会话历史，支持 JSONL 与 `state_5.sqlite` 迁移、备份和精确恢复。
- 文本模型的多模态能力改为按模型映射单独设置，同一供应商下可混合配置文本与多模态模型。
- 优化 Codex 托管认证的请求日志归属，并补充配置迁移、历史恢复和前端资源测试。

### v1.2.0

- 新增 Windows 桌面端自动更新，支持启动后自动检查和手动检查 GitHub Releases。
- 支持下载新版 `vision-relay.exe`、验证 SHA-256、安全替换并自动重启。
- 更新失败时自动恢复旧版本，降低桌面端自更新风险。
- 构建脚本支持嵌入版本号，并自动生成 `vision-relay.exe.sha256` 校验文件。
- 禁用桌面端静态资源缓存，避免更新后继续显示旧界面。

### v1.1.2

- 重构前端静态资源目录，改为 `frontend/public` 分层管理并继续由 Go embed 打包。
- 将 OpenAI Responses 与 Anthropic 协议转换逻辑拆分到 `backend/internal/protocol`，便于维护和测试。
- 优化 Codex 配置写入：支持 `CODEX_HOME`，默认不再把当前启动目录当作项目目录写入。
- 新增 `.gitignore` 忽略本地 `.codex/`、exe 备份文件，避免临时文件误提交。

### v1.1.1

- 新增 `tools/build-windows.ps1` 构建脚本，统一生成 Windows GUI 子系统的 `vision-relay.exe`。
- 修复双击启动时出现终端窗口的问题。
- README 编译和发布步骤改为使用一键构建脚本。

### v1.1.0

- 新增 Codex 一键配置入口，默认写入用户级配置；API 明确传入 `work_dir` 时才写入项目级配置。
- Codex 改用 `model_providers.custom` 和 `vision-relay-model.json` 专用模型目录，避免继续改写账号模型缓存。
- 支持把文本模型映射同步成 Codex 可见模型列表，并保留上下文窗口配置。
- 一键配置支持按客户端控制自动重启和自动启动，优先使用已检测到的桌面客户端或命令行程序位置。
- 增强旧版 Vision Relay、cc-switch 和重复 `[windows]` 配置的清理与接管逻辑。

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

中转 API 继续监听：

```text
http://127.0.0.1:8787
```

常用启动参数：

```powershell
# 指定中转 API 监听地址
.\vision-relay.exe -addr 127.0.0.1:8787

# 指定 Go 主程序管理界面监听地址
.\vision-relay.exe -management-addr 127.0.0.1:18473

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
- 构建脚本支持可选 Authenticode 代码签名：可设置 `WINDOWS_SIGNING_CERTIFICATE_PATH` 与 `WINDOWS_SIGNING_CERTIFICATE_PASSWORD`，或传入 `-SigningCertificatePath`。当前 GitHub 标签工作流按无签名模式直接构建，下载运行时可能出现 Windows SmartScreen 或未知发布者提示。

如果需要调试日志窗口，可以去掉 `-H windowsgui`：

```powershell
go build -ldflags="-s -w" -o vision-relay.exe ./backend/cmd/vision-relay
```

## 编译 macOS 桌面客户端

macOS 原生构建依赖 CGO、Cocoa 和 WebKit，因此必须在 macOS 上执行：

```bash
xcode-select --install  # 尚未安装 Command Line Tools 时执行
bash ./tools/build-macos.sh --version v2.2.2 --arch arm64
```

支持的架构参数：

- `--arch arm64`：Apple Silicon；
- `--arch amd64`：Intel Mac；
- `--arch universal`：分别构建 arm64 / amd64 后通过 `lipo` 合并为通用程序。

默认产物为 `dist/Vision Relay.app`，并同时生成 `dist/vision-relay-darwin-<架构>.zip` 及其 `.sha256`。脚本会创建标准应用目录、`Info.plist`、`.icns` 图标并执行 ad-hoc 签名。公开分发时仍建议使用 Apple Developer ID 重新签名并完成 notarization；否则其他 Mac 上首次打开时可能需要在“系统设置 → 隐私与安全性”中确认。

应用会在 macOS 菜单栏驻留，并通过系统默认浏览器打开管理页面；客户端发现支持 `/Applications` 和 `~/Applications` 中的 Codex / ChatGPT、OpenCode、Claude、OpenClaw 应用，同时继续从 `PATH` 检测 Codex CLI、Claude CLI、OpenCode 和 OpenClaw 命令。重复启动会通知现有实例再次打开管理页面，不会启动第二套数据库与路由服务。

## 打包发布

Windows 无签名发布构建（当前 GitHub 标签工作流使用此模式）：

```powershell
.\tools\build-windows.ps1 -Version v2.2.2
```

### Windows Authenticode 签名

1. 从受 Windows 信任的代码签名 CA 申请以个人或组织身份签发的 Authenticode 证书。自签名证书只适合内部测试，不能建立 SmartScreen 信誉，也通常不能降低第三方安全软件误报。
2. 安装 **Windows SDK**（提供 `signtool.exe`）和 **MinGW-w64**（提供 `windres.exe`）。构建脚本会自动查找 Windows SDK 中的 x64 `signtool.exe`。
3. 如果证书可导出为 PFX，使用上面的环境变量或以下参数构建：

```powershell
.\tools\build-windows.ps1 `
  -Output dist\vision-relay.exe `
  -Version v2.2.2 `
  -SigningCertificatePath C:\secure\vision-relay-code-signing.pfx `
  -SigningCertificatePassword '<PFX 密码>' `
  -RequireSignature
```

脚本先嵌入当前版本资源，再构建、执行 SHA-256/RFC 3161 时间戳签名、验证签名，最后生成已签名文件对应的 `.sha256`。可以再次手动验证：

```powershell
Get-AuthenticodeSignature .\dist\vision-relay.exe | Format-List Status,StatusMessage,SignerCertificate,TimeStamperCertificate
$signtool = Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin' -Filter signtool.exe -Recurse |
  Where-Object FullName -Match '\\x64\\signtool\.exe$' | Sort-Object FullName -Descending | Select-Object -First 1
& $signtool.FullName verify /pa /all /v .\dist\vision-relay.exe
```

`Status` 必须为 `Valid`。如果证书私钥位于 USB 硬件令牌或云签名服务中、无法导出 PFX，应使用证书颁发机构提供的 CSP/KSP 或云签名 GitHub Action；完成签名后必须重新生成 `.sha256`，不要把私钥导出到仓库。

当前 GitHub 标签发布按用户要求执行无签名构建，不读取证书 Secret。以后若要恢复签名发布，应在工作流中安全接入证书或云签名服务，并在生成 `.sha256` 前完成签名和验证；不要把证书私钥提交到仓库。

macOS 发布构建（在对应 Mac 或 macOS CI 上执行）：

```bash
bash ./tools/build-macos.sh --version v2.2.2 --arch universal
```

生成的 Release 附件：

```text
vision-relay.exe
vision-relay.exe.sha256
vision-relay-darwin-universal.zip
vision-relay-darwin-universal.zip.sha256
```

发布到 GitHub Release 时建议使用版本标签：

```powershell
git tag v2.2.2
git push origin v2.2.2
```

Release 标题建议为：

```text
Vision Relay v2.2.2
```

附件上传时应包含对应平台的程序包和同名 `.sha256` 文件。macOS 也可以分别发布 `vision-relay-darwin-arm64.zip` 与 `vision-relay-darwin-amd64.zip`。

## 配置说明

首次启动后，在管理页面中配置文本模型和视觉模型即可使用本地 API。文本供应商列表中的“模型测试”可直接使用该供应商已配置的模型和 API Key 发送测试提示词，并显示响应内容、HTTP 状态及耗时；测试过程不会切换当前供应商或修改客户端路由。供应商编辑弹窗中的眼睛按钮可临时显示或隐藏完整 API Key。

文本供应商按 **Codex**、**Claude**、**OpenCode** 三组管理，每个供应商只能属于一组，每组独立保存当前选择。OpenAI Responses 请求使用 Codex 组，Anthropic Messages 使用 Claude 组，Chat Completions、Gemini 与 Ollama 使用 OpenCode 组；OpenClaw 的一键配置也跟随 OpenCode 组。点击某组供应商的“使用”只同步该组关联的客户端，不会覆盖另外两组。

每组当前供应商都有独立熔断状态。连续 3 次可归因于上游的失败会进入 30 秒熔断，冷却后允许一次半开探测，成功即恢复；页面供应商卡片每 5 秒刷新“正常 / 熔断 / 探测”状态。

常用环境变量：

```text
VISION_RELAY_ADDR=127.0.0.1:8787
VISION_RELAY_MANAGEMENT_ADDR=127.0.0.1:18473

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

本地 API 不需要访问令牌，外部客户端可以直接调用所有兼容入口。普通客户端的一键配置不会写入 API Key 或 Bearer Token；如果第三方客户端的界面强制要求填写 API Key，这是该客户端自身的限制，Vision Relay 本地 API 不会校验该值。Codex 开启“切换第三方时保留官方登录”时是唯一例外：provider 配置会写入仅用于本地路由隔离的无害 Bearer 标记。该标记不是上游 API Key，也不会被本地 API 校验，其作用是防止 Codex 将官方 ChatGPT 登录令牌用于第三方模型请求。关闭本地 API 后，客户端改为直连当前文本供应商，并写入供应商 API 地址、上游令牌和真实模型名；模型列表仍只包含当前供应商配置中已经添加的模型，不会自动导入上游的全部模型。

“客户端接入”中的每个客户端都提供独立的**路由**开关。一键配置会自动开启对应路由；之后切换文本供应商时，Vision Relay 只重写已开启路由的客户端配置，并提示重启受影响的客户端。关闭路由的客户端不会被供应商切换修改；恢复 Codex 官方模式时会同时关闭 Codex 路由。

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

- 可独立修改 Go 主程序管理界面与本地中转 API 的监听地址和端口；两者必须使用不同端口，保存后需重启 Vision Relay 才会重新绑定。
- 桌面 WebView、系统浏览器、托盘激活与管理接口只连接管理端口；Codex、Claude、OpenCode、OpenClaw 及模型请求只连接中转 API 端口，修改其中一个不会改写另一个。
- 可独立关闭或开启本地 API 转发接口。关闭时，中转端口上的 `/v1/*` 等模型接口返回 `503`；管理端口上的管理页面、设置 API 和 `/healthz` 仍可使用。该开关保存后立即生效。
- 关闭本地 API 后，一键配置和文本供应商切换会让已配置客户端直连当前文本供应商，并写入供应商 API 地址、上游令牌和已添加模型的真实模型名（不会自动导入上游全部模型）；视觉模型中转不可用，文本模型的图片能力按每个模型的“支持多模态”设置写入客户端。 直连时 Codex 仅支持使用 Responses 协议的 OpenAI 兼容供应商，Claude 仅支持 Anthropic 协议供应商；协议不兼容或当前供应商未添加模型时会停止写入并给出提示。
- 可查看和修改 Codex、Codex CLI、OpenCode、Claude、Claude CLI、OpenClaw 的配置文件位置与客户端程序位置；Codex 桌面端与 CLI 共用配置，Claude 桌面端与 CLI 使用独立配置。
- Codex 桌面客户端支持自动检测 Microsoft Store 的 `OpenAI.Codex_*\app\ChatGPT.exe` 安装位置，不依赖固定版本号。
- Claude Desktop 与 Claude CLI 的路径检测和程序生命周期管理彼此独立：Windows 桌面端可识别 `%LOCALAPPDATA%\AnthropicClaude\claude.exe` 及 Squirrel 版本目录，macOS 桌面端可识别 `/Applications` 或 `~/Applications` 中的 `Claude.app`；CLI 在两端都可从 `PATH` 识别 `claude` 命令，二者不会互相误判。

首次运行时会自动检测一次客户端路径。从没有该检测字段的旧版本升级时，也会自动执行一次，之后不会反复覆盖手动填写的路径。如需刷新，可在设置页点击“重新检测客户端”。

客户端配置文件位置会实际用于“一键配置”、路由同步和 Codex 官方模式恢复。客户端程序位置用于检测运行状态，并按“设置 → 一键配置行为”中的开关自动重启或启动客户端；这些操作由程序内置完成，不会弹出终端窗口。一键配置 Codex 或 Claude 时会同时处理对应桌面端与 CLI，接口和完成提示会列出实际写入的全部配置路径，并分别返回程序重启、启动或警告结果。默认自动重启配置前已运行的客户端，配置前未运行的客户端保持关闭。

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

用户级配置会使用 `model_providers.custom`、Responses wire API 和本机 `/v1` 地址。一键配置完全由 Vision Relay 内置逻辑直接写入配置文件，不调用终端命令。Vision Relay 每次启动还会重新同步已启用的客户端路由，以修复被其他工具改回的供应商选择。默认情况下，配置前已运行的客户端会自动重启，未运行的客户端不会被启动；可在“设置 → 一键配置行为”中为 Codex、Codex CLI、OpenCode、Claude、Claude CLI、OpenClaw 分别调整。

“Codex 应用增强”提供两个独立开关：

- **切换第三方时保留官方登录**：默认开启。开启后会保留 `%CODEX_HOME%\auth.json` 中的官方 ChatGPT 认证，并让 Codex 继续识别和展示官方账号身份。本地 API 模式会在 provider 配置中写入仅发往本机 Vision Relay 的隔离 Bearer 标记，第三方模型请求不会使用官方登录令牌；关闭本地 API、让 Codex 直连供应商时，则写入真实供应商令牌。如果关闭该选项，本地 API 模式不会激活官方账号身份；直连模式下 Vision Relay 会先把官方认证备份到 `%CODEX_HOME%\vision-relay-auth.json`，再把真实供应商令牌写入托管认证，重新开启或恢复官方模式时会还原备份。
- **统一 Codex 会话历史**：默认关闭。开启时，如果当前正使用官方 `openai` 配置，会安全改为不带第三方 `base_url` 的 `custom` OpenAI provider；当前为第三方配置时不会被覆盖。关闭时只会还原带有 Vision Relay 专用标记的官方 provider，不会误改第三方配置。还可把 `sessions`、`archived_sessions` 中原 `openai` 会话和 `state_5.sqlite` 中原官方线程迁移为共享的 `custom` 标识，使官方与第三方会话显示在同一历史列表。“恢复官方模式”按钮在该开关开启时也会使用同一 `custom` OpenAI provider。

统一历史迁移前会把 JSONL 原文件、SQLite 快照和迁移 ID 账本保存到 `%CODEX_HOME%\vision-relay-history-backups\unified\<时间戳>`。关闭开关时可按账本精确恢复原官方会话；开启期间新建的第三方 `custom` 会话不会被误改回 `openai`。如果 `config.toml` 配置了 `sqlite_home`，或设置了 `CODEX_SQLITE_HOME`，也会查找对应目录下的 `state_5.sqlite`。

> 跨供应商继续旧会话时，对方后端可能无法解密会话中的 `encrypted_content` 推理内容，从而导致继续会话失败。迁移只统一历史归属，不保证加密推理内容能跨供应商复用。

同一页面也提供“一键配置 OpenCode”和“一键配置 Claude”：Windows 下 OpenCode 配置写入 `%USERPROFILE%\.config\opencode\opencode.json`，Claude 桌面配置写入 `%LOCALAPPDATA%\Claude-3p\configLibrary\<active-id>.json`，Claude CLI 配置写入 `%USERPROFILE%\.claude\settings.json`；macOS 下对应路径为 `~/.config/opencode/opencode.json`、`~/Library/Application Support/Claude-3p/configLibrary/<active-id>.json` 和 `~/.claude/settings.json`。现有配置中的其他字段会保留。

[OpenClaw](https://github.com/openclaw/openclaw) 可在同一页面点击“一键配置 OpenClaw”，Windows 和 macOS 默认分别写入 `%USERPROFILE%\.openclaw\openclaw.json` 与 `~/.openclaw/openclaw.json`。配置会新增 `vision-relay` 自定义供应商，通过 `openai-completions` 接入本机 `/v1` 接口，同步当前模型映射、上下文窗口和图片输入能力，并将默认模型切换为 `vision-relay/<模型名>`。现有的其他 OpenClaw 配置会保留，写入前会在同目录生成带时间戳的备份。

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

Windows 与 macOS 桌面版默认都会在启动后访问 GitHub Releases 检查新版本，可在左侧“更新”页面关闭自动检测，也可以随时手动检查。Windows 版支持“下载更新并重启”；macOS 首版会匹配当前架构的 Release 压缩包并引导手动下载，不会直接替换 `.app`（避免破坏 Developer ID 签名与 notarization）。Windows 自动更新流程如下：

1. 从 `xshentx/vision-relay` 的最新 GitHub Release 下载 `vision-relay.exe`；
2. 必须下载 `vision-relay.exe.sha256` 并验证 SHA-256；缺少或校验失败时拒绝安装；
3. 将当前程序直接换名备份为 `vision-relay.exe.old`，从固定的非可执行暂存文件 `vision-relay.update` 写入新版本并启动规范名称的新程序；
4. 旧实例会确认新进程在启动检查窗口内没有提前退出；若安全软件隔离新文件或新程序立即崩溃，则恢复旧文件并保持旧实例运行；
5. 新程序等待旧实例退出后获取单实例锁，并只清理程序目录内经过白名单验证的 `.old`、`vision-relay.update` 或旧版兼容暂存文件。

发布构建时请传入与 Git tag 相同的版本号：

```powershell
.\tools\build-windows.ps1 -Version v2.2.2
```

构建脚本会生成 `vision-relay.exe` 和 `vision-relay.exe.sha256`，发布 Release 时必须同时上传这两个文件。当前 Windows Release 为无签名构建，SHA-256 仅用于验证下载完整性，首次运行可能出现 Windows SmartScreen 或未知发布者提示；代码签名与证书信誉仍是降低此类提示和安全软件误报的关键。自动更新仅支持经构建脚本生成的 Windows EXE；`go run` 开发模式只检查更新，不自动替换。
