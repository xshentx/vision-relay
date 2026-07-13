package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

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
	for _, clientName := range []string{`clientNamedKey("Codex")`, `clientNamedKey("OpenCode")`, `clientNamedKey("Claude Code")`, `clientNamedKey("OpenClaw")`} {
		if !strings.Contains(script, clientName) {
			t.Fatalf("client-specific key preview %q is missing", clientName)
		}
	}
	if strings.Contains(script, `const key = normalizeClientKeys(clientKeys)[0]`) {
		t.Fatal("client configuration preview must not silently use the first token")
	}
	for _, multiModelConfig := range []string{
		`models: Object.fromEntries(snippetMappings.map((mapping) => {`,
		`availableModels: claudeModelIDs`,
		`models: openclawModels`,
	} {
		if !strings.Contains(script, multiModelConfig) {
			t.Fatalf("multi-model client preview %q is missing", multiModelConfig)
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
	if !strings.Contains(index, `id="serviceCard"`) || !strings.Contains(index, `id="serviceState">服务检测中`) {
		t.Fatal("sidebar service status is missing")
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	if !strings.Contains(script, `serviceState.textContent = online ? "服务运行正常" : "服务连接失败"`) {
		t.Fatal("service status text was not moved to the sidebar card")
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
