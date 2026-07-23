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
	for _, expected := range []string{
		`fetch("/api/client/configure"`,
		`clientConfigureActions.forEach(({button, client, profileGroup, name}) => {`,
		`configureClient({button, client, profileGroup, name})`,
		`body: JSON.stringify({client, ...(profile ? {profile_id: profile.id} : {})})`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("client configure action %q is not wired to the selected supplier", expected)
		}
	}
	for _, groupedAction := range []string{
		`client: "codex", profileGroup: "codex"`,
		`client: "claude-code", profileGroup: "claude"`,
		`client: "opencode", profileGroup: "opencode"`,
		`client: "openclaw", profileGroup: "opencode"`,
	} {
		if !strings.Contains(script, groupedAction) {
			t.Fatalf("client configure supplier mapping %q is missing", groupedAction)
		}
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

func TestClientPreviewsUseTheirSelectedSupplierGroup(t *testing.T) {
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`function textProfileForClient(groupId)`,
		`const selectedId = activeTextProfileByClient[normalizedGroup] || "";`,
		`return textProfiles.find((profile) => profile.client === normalizedGroup && profile.id === selectedId) || null;`,
		`renderForSupplier("codex", [codexConfig]`,
		`renderForSupplier("claude", [claudeCodeConfig]`,
		`renderForSupplier("opencode", [opencodeConfig, openclawConfig]`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("grouped client preview behavior %q is missing", expected)
		}
	}
	start := strings.Index(script, "function renderOpenCodeSnippet()")
	if start < 0 {
		t.Fatal("client preview renderer is missing")
	}
	previewBody := script[start:]
	if strings.Contains(previewBody, "activeTextProfile()") {
		t.Fatal("client previews must not fall back to the globally active supplier")
	}
	if strings.Contains(script, `textProfiles.find((profile) => profile.client === normalizedGroup)`) {
		t.Fatal("client previews must not silently replace a missing group selection")
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
		`<span class="log-mode ${requestMode.className}">${requestMode.label}</span>`,
		`<time class="log-time"`,
		`<div class="log-details">`,
		`<code class="log-endpoint">`,
		`<div class="log-metrics">`,
		`formatRequestMode(log)`,
		`return {className: "unknown", label: "未知"};`,
		`formatLogTimestamp(log.at)`,
		`formatFirstTokenDuration(log.first_token_ms)`,
		`renderLogMetric("总耗时", formatLogDuration(log.duration_ms))`,
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
	for _, expected := range []string{".log-item::before", ".log-item.failed::before", ".log-mode.stream", ".log-mode.sync", ".log-details", ".log-metrics", ".layout > .standard-page.active"} {
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
	for _, expected := range []string{
		`id="modalProfileClient"`,
		`<option value="codex">Codex</option>`,
		`<option value="claude">Claude</option>`,
		`<option value="opencode">OpenCode</option>`,
		`data-provider-client-tab="codex"`,
		`data-provider-client-tab="claude"`,
		`data-provider-client-tab="opencode"`,
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("client supplier grouping field %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`data.client_route_enabled = normalizeClientRoutes(clientRouteEnabled);`,
		`fetch("/api/client/routes/apply", {method: "POST"})`,
		`const textProfileClientGroups = [`,
		`active_text_profile_by_client`,
		`fetch("/api/client/configure", {`,
		`body: JSON.stringify({client: group.routeClient, profile_id: profile.id})`,
		`activeTextProfileByClient[clientGroup] = profile.id;`,
		`button.dataset.providerClientTab === group.id`,
		`affectedSelectedTextProfileGroups(previousSelections, changedProfileId)`,
		`await persistTextProfileChanges("\u5df2\u5220\u9664\u5e76\u4fdd\u5b58\u6587\u672c\u6a21\u578b", affectedGroups);`,
		`const updatedClients = await applyEnabledClientRoutes();`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("client provider route behavior %q is missing", expected)
		}
	}
	switchStart := strings.Index(script, "async function switchTextProvider(profile)")
	switchEnd := strings.Index(script[switchStart:], "async function restoreCodexOfficialMode")
	if switchStart < 0 || switchEnd < 0 {
		t.Fatal("supplier switch function is missing")
	}
	switchBody := script[switchStart : switchStart+switchEnd]
	if strings.Contains(switchBody, "applyEnabledClientRoutes") || strings.Contains(switchBody, "/api/client/routes/apply") {
		t.Fatal("supplier switch must only configure its own client, not all enabled client routes")
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

func TestModelProfileProxyFieldUsesFullModalWidth(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexRaw), `class="modal-wide-field" id="modalProfileProxyWrap"`) {
		t.Fatal("model profile proxy field must use the full-width modal field layout")
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
		`modalProfileProxyWrap.hidden = false;`,
		`modalProfileProxyURL.value = profile?.proxy_url || "";`,
		`model: modalProfileModel.value,
      proxy_url: modalProfileProxyURL.value`,
		`proxy_url: String(profile.proxy_url || "").trim()`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("model profile proxy behavior %q is missing", expected)
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
	for _, id := range []string{
		"update",
		"checkUpdate",
		"installUpdate",
		"currentVersion",
		"latestVersion",
		"autoCheckUpdates",
		"updateProgressPanel",
		"updateProgressBar",
		"updateProgressPercent",
	} {
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
	for _, expected := range []string{
		`fetch("/api/update/progress", {cache: "no-store"})`,
		`updatePromptedVersion !== info.latest_version`,
		`title: ` + "`发现新版本 ${info.latest_version}`",
		`renderUpdateProgress`,
		`scheduleUpdateProgressPoll`,
		`if (res.status === 409 && result?.progress)`,
		`renderUpdateProgress(result.progress);`,
		`let updateInstallAvailable = false;`,
		`installUpdateButton.disabled = active || !updateInstallAvailable;`,
		`scheduleUpdateProgressPoll(1000);`,
		`data.auto_check_updates = programSettings.autoCheckUpdates;`,
		`programSettings.autoCheckUpdates`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("update progress behavior %q is missing", expected)
		}
	}
	if strings.Contains(index, "???") || strings.Contains(script, "???") {
		t.Fatal("update UI contains question-mark mojibake")
	}
}

func TestSelectsUseComponentEnhancement(t *testing.T) {
	componentRaw, err := fs.ReadFile(FS, "assets/js/ui-components.js")
	if err != nil {
		t.Fatal(err)
	}
	componentScript := string(componentRaw)
	for _, expected := range []string{
		`function enhanceSelect(select)`,
		`root.querySelectorAll?.("select").forEach(enhanceSelect);`,
		`<el-select`,
		`popper-class="vr-component-select-popper"`,
		`:append-to="state.appendTo"`,
		`appendTo: select.closest("dialog") || document.body`,
		`select.dispatchEvent(new Event("change", {bubbles: true}));`,
		`function destroySelect(select)`,
		`binding.selectApp.unmount();`,
		`record.removedNodes.forEach`,
		`selectObserver.observe(document.body, {childList: true, subtree: true});`,
	} {
		if !strings.Contains(componentScript, expected) {
			t.Fatalf("component select behavior %q is missing", expected)
		}
	}
	if strings.Contains(componentScript, "vr-select-prefix") {
		t.Fatal("component selects must not render letter prefix icons")
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{
		`.vr-native-select {`,
		`.vr-component-select .el-select__wrapper {`,
		`.vr-component-select-popper .el-select-dropdown__item.is-selected::after {`,
		`content: "✓";`,
		`.update-auto-check {`,
		`margin: 0;`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("component select style %q is missing", expected)
		}
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
		"text":     `<section class="page standard-page span-12" id="text"`,
		"vision":   `<section class="page standard-page span-12" id="vision"`,
		"access":   `<section class="page standard-page access-page span-12" id="access"`,
		"settings": `<section class="page standard-page settings-page span-12" id="settings"`,
		"logs":     `<section class="page standard-page span-12" id="logs"`,
		"update":   `<section class="page standard-page span-12" id="update"`,
	}
	if strings.Contains(index, `id="playground"`) || strings.Contains(index, `data-target-page="playground"`) {
		t.Fatal("playground page and shortcuts should be removed")
	}
	for page, opening := range expectedOpenings {
		if !strings.Contains(index, opening) {
			t.Errorf("%s page does not use the common dashboard-style layout", page)
		}
	}
	if strings.Contains(index, `class="page panel standard-page`) || strings.Contains(index, `class="page panel span-7"`) || strings.Contains(index, `class="page-heading`) {
		t.Fatal("legacy outer page card or narrow layout remains")
	}
	if strings.Contains(index, `id="visionModelHint"`) || strings.Contains(index, "不要使用 llama-guard") {
		t.Fatal("obsolete vision model warning remains")
	}
	for _, heading := range []string{`class="dashboard-heading access-page-heading"`, `class="dashboard-heading settings-page-heading"`} {
		if !strings.Contains(index, heading) {
			t.Errorf("dashboard-style page heading %q is missing", heading)
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
		`class="page standard-page settings-page span-12"`,
		`class="dashboard-heading settings-page-heading"`,
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
		`data-dashboard-period="day">今日</button>`,
		`data-dashboard-period="7d">近7天</button>`,
		`data-dashboard-period="30d">近30天</button>`,
		`data-dashboard-period="all">全部</button>`,
		`id="dashboardSupplier"`,
		`id="dashboardModel"`,
		`id="dashboardTokenChart"`,
		`id="dashboardRequestChart"`,
		`id="dashboardModelRows"`,
		`所选周期，包含缓存命中`,
		`<th>输入</th>`,
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
		`"30d": "近30天统计"`,
		`all: "全部统计"`,
		`bucket.models?.[model.series_key]`,
		`data-dashboard-chart-mode`,
		`renderDashboardTokenTrend`,
		`renderDashboardModels`,
		`function dashboardInputTokens(usage)`,
		`const total = Math.max(0, input + output);`,
		`缓存命中（输入中）`,
		`缓存写入（输入中）`,
		`dashboard-chart-tooltip`,
		`container.onpointermove`,
		`formatNumber(value)} Token`,
		`dashboard-request-bar`,
		`formatNumber(values[index])} 次`,
		`number / 1000000000)}B`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("dashboard behavior %q is missing", expected)
		}
	}

	for _, obsolete := range []string{`dashboardUncachedInputTokens`, `非缓存输入`} {
		if strings.Contains(index+script, obsolete) {
			t.Fatalf("obsolete dashboard token wording or calculation %q is still embedded", obsolete)
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

func TestModelTestDrawerIsEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, expected := range []string{
		`id="modelTestLayer"`,
		`id="modelTestModel"`,
		`id="modelTestPrompt"`,
		`id="runModelTest"`,
		`id="modelTestResult"`,
		`>hi</textarea>`,
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("model test drawer element %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`data-action="test"`,
		`openModelTestDrawer(profile)`,
		`fetch("/api/model-test"`,
		`JSON.stringify({profile_id: modelTestProfileId, model, prompt})`,
		`modelTestPrompt.value = "hi"`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("model test behavior %q is missing", expected)
		}
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{".model-test-drawer", ".model-test-layer.open .model-test-drawer", ".model-test-result.is-success"} {
		if !strings.Contains(style, expected) {
			t.Fatalf("model test style %q is missing", expected)
		}
	}
}

func TestProviderAPIKeyVisibilityToggleIsEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, expected := range []string{
		`id="modalProfileAPIKey" type="password"`,
		`id="toggleModalProfileAPIKey"`,
		`aria-controls="modalProfileAPIKey"`,
		`id="modalProfileAPIKeyEye"`,
		`id="modalProfileAPIKeyEyeOff"`,
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("API key visibility markup %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`toggleModalProfileAPIKey.addEventListener("click"`,
		`setModalProfileAPIKeyVisible(modalProfileAPIKey.type === "password")`,
		`modalProfileAPIKey.type = visible ? "text" : "password"`,
		`setModalProfileAPIKeyVisible(false);`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("API key visibility behavior %q is missing", expected)
		}
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{
		`.secret-input`,
		`.secret-visibility-toggle`,
		`.secret-visibility-toggle:focus-visible`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("API key visibility style %q is missing", expected)
		}
	}
}

func TestModelMappingCapabilityControlsAreAligned(t *testing.T) {
	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := strings.ReplaceAll(string(styleRaw), "\r\n", "\n")
	for _, expected := range []string{
		`.model-mapping-supports-images,
.model-mapping-supports-reasoning {`,
		`  min-height: 44px;
  margin: 0;`,
		`.model-mapping-supports-reasoning .vr-component-select.compact .el-select__wrapper {
  min-height: 44px;
}`,
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("model mapping alignment style %q is missing", expected)
		}
	}
}

func TestModelMappingUsesRequestModelOnly(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	if !strings.Contains(index, "<span>\u8bf7\u6c42\u6a21\u578b</span>") {
		t.Fatal("model mapping header should be request model")
	}
	for _, obsolete := range []string{"\u83dc\u5355\u663e\u793a\u540d", "\u5b9e\u9645\u8bf7\u6c42\u6a21\u578b"} {
		if strings.Contains(index, obsolete) {
			t.Fatalf("obsolete model mapping header %q remains", obsolete)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	if strings.Contains(script, `data-field="name"`) {
		t.Fatal("model mapping should not render a separate display-name field")
	}
	for _, expected := range []string{
		`const model = String(mapping.model || mapping.name || mapping.display_name || scalar).trim();`,
		`const name = model;`,
		`rows[rows.length - 1]?.querySelector('[data-field="model"]')?.focus();`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("request-model-only behavior %q is missing", expected)
		}
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	if !strings.Contains(style, `grid-template-columns: minmax(0, 1fr) 140px 100px 110px 34px;`) {
		t.Fatal("model mapping should use the five-column layout")
	}
}

func TestBreakArmorWorkbenchIsEmbeddedAndIndependent(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	start := strings.Index(index, `<section class="page standard-page span-12 break-armor-page"`)
	if start < 0 {
		t.Fatal("break armor page is missing")
	}
	endOffset := strings.Index(index[start+1:], `<section class="page`)
	if endOffset < 0 {
		t.Fatal("break armor page boundary is missing")
	}
	feature := index[start : start+1+endOffset]
	for _, expected := range []string{
		`data-page="break-armor"`,
		`<span>一键破甲</span><small class="nav-test-badge">测试</small>`,
		`<h3>一键破甲 <span class="break-armor-test-badge">测试功能</span></h3>`,
		`data-break-armor-client="codex"`,
		`data-break-armor-client="claude"`,
		`data-break-armor-client="opencode"`,
		`data-break-armor-panel="codex"`,
		`data-break-armor-panel="claude"`,
		`data-break-armor-panel="opencode"`,
		`class="nav-test-badge">测试</small>`,
		`class="break-armor-test-badge">测试功能</span>`,
		"破甲状态与备份独立管理；Codex 全局模式也只管理破甲字段，不覆盖或恢复供应商、模型与路由。",
		`data-break-armor-view-tab="sessions"`,
		`data-break-armor-view-tab="templates"`,
		`data-break-armor-mode="codex"`,
		`id="breakArmorSessionList"`,
		`id="breakArmorTemplateList"`,
		">\u4e00\u952e\u7834\u7532 Codex</button>",
		">\u4e00\u952e\u7834\u7532 Claude</button>",
		">\u4e00\u952e\u7834\u7532 OpenCode</button>",
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("break armor markup %q is missing", expected)
		}
	}
	for _, forbidden := range []string{"\u90e8\u7f72", "\u91cd\u65b0\u90e8\u7f72", "\u5df2\u542f\u7528", "\u672a\u542f\u7528", "AI 改写", "AI 智能改写", `data-break-armor-view-tab="ai"`} {
		if strings.Contains(feature, forbidden) {
			t.Fatalf("break armor page contains forbidden wording %q", forbidden)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`fetch("/api/break-armor/status"`,
		`fetch("/api/break-armor/preview"`,
		`fetch("/api/break-armor/apply"`,
		`fetch("/api/break-armor/restore"`,
		`/api/break-armor/sessions`,
		`/api/break-armor/session/preview`,
		`/api/break-armor/session/patch`,
		`/api/break-armor/session/backups`,
		`/api/break-armor/session/restore`,
		`/api/break-armor/templates`,
		`injection_mode`,
		`[data-break-armor-client]`,
		`[data-break-armor-panel]`,
		`title: "与一键配置完全隔离"`,
		`detail: "不读取、不写入、不恢复供应商、模型与路由配置"`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("break armor behavior %q is missing", expected)
		}
	}

	for _, forbidden := range []string{`/api/break-armor/ai/settings`, `/api/break-armor/ai/rewrite`, `/api/break-armor/prompt/rewrite`, `breakArmorAI`, `rewriteBreakArmorSessionAI`} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("removed AI rewrite behavior %q is still embedded", forbidden)
		}
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(styleRaw)
	for _, expected := range []string{".break-armor-page", ".break-armor-test-badge", ".nav-test-badge", ".break-armor-tabs", ".break-armor-flow", ".break-armor-code", ".break-armor-function-tabs", ".break-armor-session-grid", ".break-armor-template-grid", ".break-armor-backup-row { display:grid; grid-template-columns:minmax(0,1fr) auto; align-items:end;", ".break-armor-backup-row > label { min-width:0; margin:0; }", ".break-armor-backup-row > label > .vr-component-select { margin-bottom:0; }", ".break-armor-backup-row > button { min-height:44px; margin:0; }", ".break-armor-mode-note { border-color:#bae6fd; color:#36556f; background:#f0f9ff; font-size:13px; font-weight:600; }", ".break-armor-mode-note code { padding:1px 4px; border-radius:4px; color:#075985; background:#e0f2fe; font-weight:800; }"} {
		if !strings.Contains(style, expected) {
			t.Fatalf("break armor style %q is missing", expected)
		}
	}
	if strings.Contains(style, ".break-armor-ai-grid") {
		t.Fatal("removed AI rewrite style is still embedded")
	}
}

func TestOpenClawDirectRouteRefreshesWithOpenCodeSupplier(t *testing.T) {
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	expected := `if (groupId === "opencode") return clientRouteEnabled.opencode === true || clientRouteEnabled.openclaw === true;`
	if !strings.Contains(script, expected) {
		t.Fatal("OpenClaw-only direct routing is not refreshed after an OpenCode supplier change")
	}
}

func TestLegacyTextRoutingMarkerIsPreservedByFrontendSaves(t *testing.T) {
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`legacyTextRouting = cfg.legacy_text_routing === true;`,
		`data.legacy_text_routing = legacyTextRouting;`,
		`async function persistTextProfileChanges(successMessage, affectedGroups) {
  legacyTextRouting = false;`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("legacy text routing behavior %q is missing", expected)
		}
	}
}

func TestClientOneClickPreviewUsesDetectedCrossPlatformPaths(t *testing.T) {
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		`Object.values(payload?.config_paths || {})`,
		`function configuredClientPreviewPath(client, fallback)`,
		`const windowsSandbox = isWindowsClientConfigPath(codexConfigPath)`,
		`configuredClientPreviewPath("claude-code"`,
		`configuredClientPreviewPath("claude-cli"`,
		`# Claude Desktop: ${desktopPath}`,
		`# Claude CLI: ${cliPath}`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("cross-platform one-click preview behavior %q is missing", expected)
		}
	}
	if strings.Contains(script, `# %USERPROFILE%\\.codex\\config.toml`) {
		t.Fatal("Codex preview still hard-codes the Windows user profile path")
	}
	if strings.Contains(script, `# 当前项目 .codex\\config.toml`) {
		t.Fatal("one-click preview must not claim that an unrequested project config is written")
	}
}
