package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

func TestTextNavigationUsesProviderWording(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	if !strings.Contains(index, `data-page="text"><span class="nav-icon">T</span><span>模型供应商</span>`) {
		t.Fatal("text navigation must use the model supplier wording")
	}
}

func TestClientConfigureActionsAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, buttonID := range []string{"configureOpenCode", "configureCodex", "configureClaudeCode", "configureOpenClaw"} {
		if !strings.Contains(index, `id="`+buttonID+`"`) {
			t.Fatalf("client configure button %q is missing", buttonID)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, clientID := range []string{`client: "opencode"`, `client: "codex"`, `client: "claude-code"`, `client: "openclaw"`} {
		if !strings.Contains(script, clientID) {
			t.Fatalf("client configure action %q is missing", clientID)
		}
	}
	if !strings.Contains(script, `fetch("/api/client/configure"`) || !strings.Contains(script, `body: JSON.stringify({client})`) {
		t.Fatal("client configure actions are not wired to the shared API request")
	}
	for _, forbidden := range []string{"sk-local", "localPlaceholderKey", "PROXY_MANAGED"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("client configuration preview must not contain placeholder authentication %q", forbidden)
		}
	}
	for _, multiModelConfig := range []string{
		`models: Object.fromEntries(snippetMappings.map((mapping) => {`,
		`inferenceModels: claudeModels`,
		`disableDeploymentModeChooser: true`,
		`models: openclawModels`,
	} {
		if !strings.Contains(script, multiModelConfig) {
			t.Fatalf("multi-model client preview %q is missing", multiModelConfig)
		}
	}
}

func TestLocalAPIHasNoTokenManagement(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, forbidden := range []string{`data-page="tokens"`, `id="generateKey"`, `id="clientKeyList"`} {
		if strings.Contains(index, forbidden) {
			t.Fatalf("obsolete token UI %q remains", forbidden)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, forbidden := range []string{`/api/key`, `client_api_key_entries`, `client_name || "-"`} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("obsolete token behavior %q remains", forbidden)
		}
	}
	for _, expected := range []string{
		`<strong class="log-model">${escapeHTML(log.model || "未知模型")}</strong>`,
		`<span class="log-supplier">供应商 ${escapeHTML(formatUpstream(log))}</span>`,
		`<span class="log-mode ${formatRequestMode(log).className}">${formatRequestMode(log).label}</span>`,
		`<div class="log-token-grid">`,
		`formatRequestMode(log)`,
		`formatFirstTokenDuration(log.first_token_ms)`,
		`<span class="log-duration-label">总耗时</span>`,
		`formatLogDuration(log.duration_ms)`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("request log layout %q is missing", expected)
		}
	}
	if strings.Contains(script, `<strong>${escapeHTML(log.protocol || "-")}</strong>`) {
		t.Fatal("request protocol must not be used as the log card title")
	}
	if strings.Contains(script, "if (name && provider) return `${name} / ${provider}`") {
		t.Fatal("request logs must display the supplier name without the provider type")
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{".log-item::before", ".log-item.failed::before", ".log-mode.stream", ".log-token-grid", ".log-duration-icon"} {
		if !strings.Contains(style, expected) {
			t.Fatalf("request log style %q is missing", expected)
		}
	}
	if !strings.Contains(string(indexRaw), "供应商名称") {
		t.Fatal("model profile modal must label the profile name as the supplier name")
	}
}

func TestClientProviderRoutesAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, inputID := range []string{"routeCodex", "routeOpenCode", "routeClaudeCode", "routeOpenClaw"} {
		if !strings.Contains(index, `id="`+inputID+`"`) {
			t.Fatalf("client provider route switch %q is missing", inputID)
		}
	}
	if got := strings.Count(index, `class="client-route-control"><span>路由</span>`); got != 4 {
		t.Fatalf("client route label count = %d, want 4", got)
	}
	if strings.Contains(index, "供应商路由") {
		t.Fatal("legacy client route label must not be displayed")
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`data.client_route_enabled = normalizeClientRoutes(clientRouteEnabled);`,
		`fetch("/api/client/routes/apply", {method: "POST"})`,
		`供应商切换成功`,
		`请重启客户端程序`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("client provider route behavior %q is missing", expected)
		}
	}
	if strings.Contains(script, "供应商路由") {
		t.Fatal("legacy client route wording must not appear in notifications")
	}
}
func TestProfileSwitchUsesExplicitButtonAndSupportsPersistentDragSorting(t *testing.T) {
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`class="profile-drag-handle"`,
		`data-action="switch"`,
		`data-action="switch"${profile.id === activeId ? " disabled" : ""}>使用</button>`,
		`dragHandle.addEventListener("mousedown"`,
		`document.addEventListener("mousemove"`,
		`document.addEventListener("mouseup"`,
		`function reorderProfiles(kind, draggedId, targetId, insertAfter)`,
		`persistConfig(kind === "text" ? "文本模型顺序已保存" : "视觉模型顺序已保存")`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("profile switch or drag-sort behavior %q is missing", expected)
		}
	}
	if strings.Contains(script, `row.querySelector(".profile-main").addEventListener("click"`) {
		t.Fatal("clicking the profile summary must not switch models")
	}
	if strings.Contains(script, `profile.id === activeId ? "当前" : "切换"`) {
		t.Fatal("profile action must always be named 使用")
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{
		`.profile-drag-handle`,
		`.profile-row.dragging`,
		`.profile-row.drop-before::before`,
		`.profile-row.drop-after::after`,
		`.profile-switch:disabled`,
		`.profile-row:hover .profile-actions`,
		`.profile-row:focus-within .profile-actions`,
		`@media (hover: none), (pointer: coarse)`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("profile drag-sort style %q is missing", expected)
		}
	}
}

func TestClientRouteControlsAlignWithActionButtons(t *testing.T) {
	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{
		`.client-route-control {`,
		`min-height: 40px;`,
		`.client-panel-intro .panel-actions {`,
		`align-items: center;`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("client route alignment style %q is missing", expected)
		}
	}
}

func TestTextProfileProxyFieldUsesFullModalWidth(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexRaw), `class="modal-wide-field" id="modalProfileProxyWrap"`) {
		t.Fatal("text profile proxy field must use the full-width modal field layout")
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{
		`.modal-wide-field {`,
		`grid-column: 1 / -1;`,
		`min-width: 0;`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("full-width proxy field style %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`modalProfileProxyWrap.hidden = !isText;`,
		`modalProfileProxyURL.value = isText ? profile?.proxy_url || "" : "";`,
		`proxy_url: modalProfileProxyURL.value`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("text profile proxy behavior %q is missing", expected)
		}
	}
}

func TestTextProfileHidesLegacyForcedModelField(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	index, script, style := string(indexRaw), string(scriptRaw), string(styleRaw)
	if strings.Contains(index, "强制模型名") || strings.Contains(script, "强制模型名") {
		t.Fatal("legacy forced model name control should not be shown for text profiles")
	}
	if !strings.Contains(script, `modalProfileModelWrap.hidden = isText;`) {
		t.Fatal("the legacy model field must be hidden for text profiles")
	}
	if !strings.Contains(style, `.modal-grid > [hidden]`) || !strings.Contains(style, `display: none !important;`) {
		t.Fatal("modal grid styles must honor the hidden attribute")
	}
	for _, expected := range []string{
		`max-height: calc(100vh - 28px);`,
		`overflow: hidden;`,
		`overflow-y: auto;`,
		`.model-picker-panel`,
		`grid-column: 1 / -1;`,
		`#modelSelect`,
		`width: 100%;`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("modal scrolling style %q is missing", expected)
		}
	}
}

func TestEmptyTextModelMappingsAreNotRendered(t *testing.T) {
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	if !strings.Contains(script, `if (modalModelMappings.length === 0) return;`) {
		t.Fatal("an empty text model mapping list should render no rows")
	}
	if strings.Contains(script, `addModelMappingRow({name: "", model: "", context_window: ""}, false)`) {
		t.Fatal("opening a text model profile must not add a default empty mapping")
	}
	if strings.Contains(script, `String(value?.model || value || "")`) {
		t.Fatal("model mapping objects must not be stringified as [object Object]")
	}
	for _, expected := range []string{
		`const mapping = value && typeof value === "object" && !Array.isArray(value) ? value : {};`,
		`const scalar = typeof value === "string" || typeof value === "number" ? String(value).trim() : "";`,
		`if (!mapping.model || !key || seen.has(key)) return false;`,
		`model_mappings: [],`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("text model mapping safeguard %q is missing", expected)
		}
	}
}
func TestUpdateUIIsEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, id := range []string{"update", "checkUpdate", "installUpdate", "currentVersion", "latestVersion"} {
		if !strings.Contains(index, `id="`+id+`"`) {
			t.Fatalf("update UI element %q is missing", id)
		}
	}
	if !strings.Contains(index, `<h3>更新</h3>`) || strings.Contains(index, `<h3>&#31243;&#24207;&#26356;&#26032;</h3>`) {
		t.Fatal("update page must use the short title")
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	if !strings.Contains(script, `fetch("/api/update"`) || !strings.Contains(script, `method: "POST"`) {
		t.Fatal("update UI is not wired to the update API")
	}
	if strings.Contains(index, "???") || strings.Contains(script, "???") {
		t.Fatal("update UI contains question-mark mojibake")
	}
}

func TestTopbarRemovedAndServiceStatusMovedToSidebar(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	if strings.Contains(index, `class="app-topbar"`) || strings.Contains(index, `id="topServiceState"`) {
		t.Fatal("top bar should be removed")
	}
	if !strings.Contains(index, `id="serviceCard"`) || !strings.Contains(index, `id="serviceState">本地 API 服务检测中`) {
		t.Fatal("sidebar service status is missing")
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`const localAPIEnabled = programSettings.localAPIEnabled !== false;`,
		`function renderTextProfiles() {`,
		`function renderVisionProfiles() {`,
		`"本地 API 服务运行正常"`,
		`"本地 API 服务连接失败"`,
		`"本地 API 服务已关闭"`,
		`serviceCard.classList.toggle("disabled", !localAPIEnabled);`,
		`setServiceOnline(true);`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("local API service status behavior %q is missing", expected)
		}
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	if !strings.Contains(style, `.side-note.disabled strong`) || !strings.Contains(style, `.side-note.disabled .dot`) {
		t.Fatal("disabled local API service status style is missing")
	}
}

func TestUpdateAndSettingsMenusAreAtSidebarBottom(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	navStart := strings.Index(index, `<nav class="nav"`)
	navEnd := strings.Index(index, `</nav>`)
	if navStart < 0 || navEnd <= navStart {
		t.Fatal("sidebar navigation is missing")
	}
	nav := index[navStart:navEnd]
	updateIndex := strings.Index(nav, `data-page="update"`)
	settingsIndex := strings.Index(nav, `data-page="settings"`)
	if updateIndex < 0 || settingsIndex <= updateIndex {
		t.Fatalf("sidebar bottom menu order is invalid: update=%d settings=%d", updateIndex, settingsIndex)
	}
	if strings.Contains(nav, `data-page="playground"`) {
		t.Fatal("playground menu should be removed")
	}
	for _, expected := range []string{`<span>更新</span>`, `<span>设置</span>`} {
		if !strings.Contains(nav, expected) {
			t.Fatalf("short sidebar menu label %q is missing", expected)
		}
	}
	if strings.Contains(nav, "程序更新") || strings.Contains(nav, "程序设置") {
		t.Fatal("sidebar must use the short update and settings labels")
	}
}

func TestBusinessPagesUseCommonLayout(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	expectedOpenings := map[string]string{
		"text":     `<section class="page panel standard-page span-12" id="text"`,
		"vision":   `<section class="page panel standard-page span-12" id="vision"`,
		"access":   `<section class="page panel standard-page access-page span-12" id="access"`,
		"settings": `<section class="page panel standard-page settings-page span-12" id="settings"`,
		"logs":     `<section class="page panel standard-page span-12" id="logs"`,
		"update":   `<section class="page panel standard-page span-12" id="update"`,
	}
	if strings.Contains(index, `id="playground"`) || strings.Contains(index, `data-target-page="playground"`) {
		t.Fatal("playground page and shortcuts should be removed")
	}
	for page, opening := range expectedOpenings {
		if !strings.Contains(index, opening) {
			t.Errorf("%s page does not use the common full-width panel layout", page)
		}
	}
	if strings.Contains(index, `class="page panel span-7"`) || strings.Contains(index, `class="page-heading`) {
		t.Fatal("legacy narrow or standalone page layout remains")
	}
	for _, heading := range []string{`class="panel-head access-page-heading"`, `class="panel-head settings-page-heading"`} {
		if !strings.Contains(index, heading) {
			t.Errorf("common page heading %q is missing", heading)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scriptRaw), `window.scrollTo({top: 0, left: 0, behavior: "auto"});`) {
		t.Fatal("page switching must reset the shared page shell to the top")
	}
}

func TestTextModelReasoningCapabilityIsConfigurable(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexRaw), "推理强度") {
		t.Fatal("text model mapping UI should expose reasoning effort")
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`data-field="reasoning_effort"`,
		`{value: "none", label: "\u4e0d\u652f\u6301"}`,
		`{value: "low", label: "\u4f4e"}`,
		`{value: "medium", label: "\u4e2d"}`,
		`{value: "high", label: "\u9ad8"}`,
		`{value: "xhigh", label: "\u8d85\u9ad8"}`,
		`reasoning: mapping.reasoning_effort !== "none"`,
		`mapping.supports_reasoning ? "high" : "none"`,
		`supported_reasoning_levels: supportsReasoning ? [`,
		`default_reasoning_level: reasoningEffort`,
		`supports_reasoning_summaries: supportsReasoning`,
		`model_reasoning_effort = "${codexDefaultReasoningEffort}"`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("reasoning effort support %q is missing", expected)
		}
	}
}

func TestProgramSettingsAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, expected := range []string{
		`data-page="settings"`,
		`data-page-panel="settings"`,
		`class="page panel standard-page settings-page span-12"`,
		`class="panel-head settings-page-heading"`,
		`<h3>设置</h3>`,
		`id="settingsLocalAPIEnabled"`,
		`id="settingsAPIHost"`,
		`id="settingsAPIPort"`,
		`id="configPathCodex"`,
		`id="programPathCodex"`,
		`id="configPathOpenCode"`,
		`id="programPathOpenCode"`,
		`id="configPathClaudeCode"`,
		`id="programPathClaudeCode"`,
		`id="configPathOpenClaw"`,
		`id="programPathOpenClaw"`,
		`id="autoRestartCodex"`,
		`id="autoStartCodex"`,
		`id="autoRestartCodexCLI"`,
		`id="autoStartCodexCLI"`,
		`id="autoRestartOpenCode"`,
		`id="autoStartOpenCode"`,
		`id="autoRestartClaudeCode"`,
		`id="autoStartClaudeCode"`,
		`id="autoRestartClaudeCLI"`,
		`id="autoStartClaudeCLI"`,
		`id="autoRestartOpenClaw"`,
		`id="autoStartOpenClaw"`,
		`id="detectClientPaths"`,
		`id="saveProgramSettings"`,
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("program settings element %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`data.local_api_enabled = programSettings.localAPIEnabled;`,
		`const previousLocalAPIEnabled = programSettings.localAPIEnabled;`,
		`const updatedClients = localAPIModeChanged ? await applyEnabledClientRoutes() : [];`,
		`async function applyEnabledClientRoutes()`,
		`fetch("/api/client/routes/apply", {method: "POST"})`,
		`data.client_config_paths = normalizeClientConfigPaths(programSettings.clientConfigPaths);`,
		`data.client_program_paths = normalizeClientProgramPaths(programSettings.clientProgramPaths);`,
		`data.client_auto_restart = normalizeClientBehavior(programSettings.clientAutoRestart, true);`,
		`data.client_auto_start = normalizeClientBehavior(programSettings.clientAutoStart, false);`,
		`clientAutoRestart: normalizeClientBehavior(cfg.client_auto_restart, true)`,
		`clientAutoStart: normalizeClientBehavior(cfg.client_auto_start, false)`,
		`const programResults = Array.isArray(payload?.programs) ? payload.programs : [];`,
		`const programRestarted = programResults.some`,
		`const warnings = programResults.map`,
		`if (programResults.length === 0 && payload?.program_warning) warnings.push(payload.program_warning);`,
		`fetch("/api/settings/detect-clients", {method: "POST"})`,
		`cfg.client_paths_detected === true`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("program settings behavior %q is missing", expected)
		}
	}
	if strings.Contains(index, "PROGRAM SETTINGS") || strings.Contains(index, `<h3>程序设置</h3>`) {
		t.Fatal("settings page must use the short title and common panel heading")
	}
	if strings.Contains(index, "启动行为") || strings.Contains(index, `id="settingsOpenWindow"`) || strings.Contains(index, `id="settingsOpenBrowser"`) {
		t.Fatal("program settings must not expose startup behavior controls")
	}
	if strings.Contains(script, `data.open_window = true;`) || strings.Contains(script, `data.open_browser = false;`) {
		t.Fatal("config persistence must not overwrite program settings with hard-coded startup values")
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{`.layout > .settings-page.active`, `row-gap: 18px`, `.access-page .component-card`, `.settings-page > .component-card`, `.client-settings-list`, `.client-settings-list-head`, `.client-path-row`, `.client-path-fields`, `.client-behavior-list`, `.client-behavior-row`, `.client-behavior-options`, `.client-behavior-option`, `grid-column: 2 / -1`, `grid-template-columns: repeat(2, minmax(0, 1fr))`} {
		if !strings.Contains(style, expected) {
			t.Fatalf("program settings style %q is missing", expected)
		}
	}
}

func TestDisabledLocalAPIDirectSupplierUIIsEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, expected := range []string{
		`id="localAPIWarning"`,
		"关闭本地服务后视觉模型将不可用",
		"只有勾选“支持多模态”的文本模型仍可识别图片",
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("disabled local API warning %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`payload?.direct_upstream`,
		`directClientMappings(sourceMappings)`,
		`return mapping?.supports_images === true;`,
		`return mapping?.supports_images === true || visionCapabilityEnabled;`,
		`directUpstream ? clientVersionedBaseURL(profile)`,
		`...(directUpstream ? {apiKey: upstreamKey} : {})`,
		`requires_openai_auth = ${codexRequiresOpenAIAuth}`,
		`const codexBearerToken = preserveOfficialAuth ? (directUpstream ? upstreamKey : "vision-relay-local") : "";`,
		`inferenceProvider: "gateway"`,
		`inferenceModels: claudeModels`,
		`disableDeploymentModeChooser: true`,
		`directUpstream && mappings.length === 0`,
		`directClientCompatibilityMessage("codex", profile)`,
		`directClientCompatibilityMessage("claude-code", profile)`,
		"未勾选多模态的文本模型将无法实现图片识别",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("direct supplier UI behavior %q is missing", expected)
		}
	}
}

func TestClaudeProgramSettingsUseDesktopClient(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(indexRaw)
	for _, want := range []string{
		`<strong>Claude</strong>`,
		`id="programPathClaudeCode"`,
		`id="autoRestartClaudeCode"`,
		`<strong>Claude CLI</strong>`,
		`id="programPathClaudeCLI"`,
		`id="autoRestartClaudeCLI"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("Claude desktop/CLI label %q is missing", want)
		}
	}
	if strings.Contains(html, `<strong>Claude Code</strong><small>`) {
		t.Fatal("Claude desktop row must not be labeled as Claude Code CLI")
	}
}

func TestDashboardAssetsAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, expected := range []string{
		`data-page="dashboard"`,
		`data-page-panel="dashboard"`,
		`id="dashboardSupplier"`,
		`id="dashboardModel"`,
		`id="dashboardTokenChart"`,
		`id="dashboardRequestChart"`,
		`id="dashboardModelRows"`,
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("dashboard markup %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		"fetch(`/api/dashboard?${params.toString()}`, {signal: controller.signal})",
		`dashboardRequestController?.abort();`,
		`if (requestSequence !== dashboardRequestSequence) return;`,
		`if (err?.name === "AbortError") return;`,
		`bucket.models?.[model.series_key]`,
		`data-dashboard-chart-mode`,
		`renderDashboardTokenTrend`,
		`renderDashboardModels`,
		`dashboard-chart-tooltip`,
		`container.onpointermove`,
		`formatNumber(value)} Token`,
		`dashboard-request-bar`,
		`formatNumber(values[index])} 次`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("dashboard behavior %q is missing", expected)
		}
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{
		`.token-trend-panel .dashboard-chart`,
		`height: 360px`,
		`.dashboard-chart-tooltip`,
		`.dashboard-chart-crosshair`,
		`.request-chart`,
		`.dashboard-request-bar`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("dashboard chart style %q is missing", expected)
		}
	}
}
