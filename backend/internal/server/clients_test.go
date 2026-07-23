package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestClientKeyName(t *testing.T) {
	tests := map[string]string{
		clientCodex:      "Codex",
		clientOpenCode:   "OpenCode",
		clientClaudeCode: "Claude",
		clientOpenClaw:   "OpenClaw",
	}
	for client, want := range tests {
		if got := clientKeyName(client); got != want {
			t.Fatalf("clientKeyName(%q) = %q, want %q", client, got, want)
		}
	}
	if got := clientKeyName("unknown"); got != "" {
		t.Fatalf("unknown client key name = %q, want empty", got)
	}
}

func TestNormalizeClientRouteEnabled(t *testing.T) {
	routes := normalizeClientRouteEnabled(map[string]bool{
		"codex":     true,
		"open-code": true,
		"unknown":   true,
	})
	if !routes[clientCodex] || !routes[clientOpenCode] {
		t.Fatalf("known client routes were not normalized: %#v", routes)
	}
	if routes[clientClaudeCode] || routes[clientOpenClaw] || routes["unknown"] {
		t.Fatalf("disabled or unknown routes were enabled: %#v", routes)
	}
}

func TestConfigureEnabledClientRoutesRepairsCodexProvider(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	t.Setenv("CODEX_HOME", codexDir)
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`model_provider = "openai"

[model_providers.custom]
base_url = "http://old.invalid/v1"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	cfg.ClientRouteEnabled = map[string]bool{clientCodex: true}
	cfg.ClientConfigPaths = map[string]string{clientCodex: configPath}
	cfg.TextModelMappings = []textModelMapping{{Name: "gpt-test", Model: "upstream-test"}}
	a := &app{cfg: cfg}

	results, applyErrors := a.configureEnabledClientRoutes("http://127.0.0.1:8787/", home)
	if len(applyErrors) != 0 {
		t.Fatalf("route synchronization errors = %#v", applyErrors)
	}
	if len(results) != 1 || results[0].Client != clientCodex {
		t.Fatalf("route synchronization results = %#v", results)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	if !strings.Contains(config, `model_provider = "custom"`) ||
		!strings.Contains(config, `base_url = "http://127.0.0.1:8787/v1"`) ||
		strings.Contains(config, `model_provider = "openai"`) ||
		strings.Count(config, "[model_providers.custom]") != 1 {
		t.Fatalf("enabled Codex route was not repaired:\n%s", config)
	}
}

func TestConfigureEnabledClientRoutesRestoresSelectedProfilePerClient(t *testing.T) {
	home := t.TempDir()
	localAPIEnabled := false
	codexPath := filepath.Join(home, "clients", "codex.toml")
	openCodePath := filepath.Join(home, "clients", "opencode.json")
	claudeDesktopPath := filepath.Join(home, "clients", "claude-desktop.json")
	claudeCLIPath := filepath.Join(home, "clients", "claude-cli.json")
	cfg := normalizeSeparateModelProfiles(config{
		LocalAPIEnabled:     &localAPIEnabled,
		ActiveTextProfileID: "legacy-global",
		ActiveTextProfileByClient: map[string]string{
			textProfileClientCodex:    "codex-selected",
			textProfileClientClaude:   "claude-selected",
			textProfileClientOpenCode: "opencode-selected",
		},
		TextModelProfiles: []textModelProfile{
			{ID: "legacy-global", Name: "Legacy global", Client: textProfileClientOpenCode, Provider: "openai", BaseURL: "https://wrong.example/v1", APIKey: "sk-wrong", ModelMappings: []textModelMapping{{Name: "wrong", Model: "wrong"}}},
			{ID: "codex-selected", Name: "Codex selected", Client: textProfileClientCodex, Provider: "openai", WireAPI: "responses", BaseURL: "https://codex.example/v1", APIKey: "sk-codex", ModelMappings: []textModelMapping{{Name: "codex-alias", Model: "codex-model"}}},
			{ID: "claude-selected", Name: "Claude selected", Client: textProfileClientClaude, Provider: "anthropic", BaseURL: "https://claude.example/v1", APIKey: "sk-claude", ModelMappings: []textModelMapping{{Name: "claude-alias", Model: "claude-model"}}},
			{ID: "opencode-selected", Name: "OpenCode selected", Client: textProfileClientOpenCode, Provider: "openai", BaseURL: "https://opencode.example/v1", APIKey: "sk-opencode", ModelMappings: []textModelMapping{{Name: "opencode-alias", Model: "opencode-model"}}},
		},
		ClientRouteEnabled: map[string]bool{
			clientCodex: true, clientClaudeCode: true, clientOpenCode: true,
		},
		ClientConfigPaths: map[string]string{
			clientCodex: codexPath, clientOpenCode: openCodePath,
			clientClaudeCode: claudeDesktopPath, clientClaudeCLI: claudeCLIPath,
		},
	})
	a := &app{cfg: cfg}

	results, applyErrors := a.configureEnabledClientRoutes("http://127.0.0.1:8787/", home)
	if len(applyErrors) != 0 || len(results) != 3 {
		t.Fatalf("route synchronization results = %#v, errors = %#v", results, applyErrors)
	}
	for _, result := range results {
		if !result.DirectUpstream {
			t.Fatalf("route %q was not restored in direct mode: %#v", result.Client, result)
		}
	}

	codexRaw, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	codexConfig := string(codexRaw)
	if !strings.Contains(codexConfig, `base_url = "https://codex.example/v1"`) ||
		!strings.Contains(codexConfig, `experimental_bearer_token = "sk-codex"`) ||
		!strings.Contains(codexConfig, `model = "codex-model"`) || strings.Contains(codexConfig, "wrong.example") {
		t.Fatalf("Codex did not use its selected profile:\n%s", codexConfig)
	}

	var openCode map[string]any
	if err := readJSON(openCodePath, &openCode); err != nil {
		t.Fatal(err)
	}
	openCodeProvider := openCode["provider"].(map[string]any)[relayProviderID].(map[string]any)
	openCodeOptions := openCodeProvider["options"].(map[string]any)
	if openCodeOptions["baseURL"] != "https://opencode.example/v1" || openCodeOptions["apiKey"] != "sk-opencode" || openCode["model"] != "vision-relay/opencode-model" {
		t.Fatalf("OpenCode did not use its selected profile: %#v", openCode)
	}

	var claudeDesktop map[string]any
	if err := readJSON(claudeDesktopPath, &claudeDesktop); err != nil {
		t.Fatal(err)
	}
	if claudeDesktop["inferenceGatewayBaseUrl"] != "https://claude.example" || claudeDesktop["inferenceGatewayApiKey"] != "sk-claude" {
		t.Fatalf("Claude Desktop did not use its selected profile: %#v", claudeDesktop)
	}
	var claudeCLI map[string]any
	if err := readJSON(claudeCLIPath, &claudeCLI); err != nil {
		t.Fatal(err)
	}
	claudeEnv := claudeCLI["env"].(map[string]any)
	if claudeEnv["ANTHROPIC_BASE_URL"] != "https://claude.example/v1" || claudeEnv["ANTHROPIC_AUTH_TOKEN"] != "sk-claude" || claudeEnv["ANTHROPIC_CUSTOM_MODEL_OPTION"] != "claude-model" {
		t.Fatalf("Claude Code did not use its selected profile: %#v", claudeCLI)
	}
}

func TestStartupRouteSyncPreservesCodexOfficialAuthAndSessionHistory(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	sessionDir := filepath.Join(codexDir, "sessions")
	t.Setenv("CODEX_HOME", codexDir)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("model_provider = \"openai\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(codexDir, "auth.json")
	historyPath := filepath.Join(sessionDir, "session.jsonl")
	authBefore := []byte(`{"tokens":{"access_token":"official-login"}}`)
	historyBefore := []byte("{\"session\":\"unified-history\"}\n")
	if err := os.WriteFile(authPath, authBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, historyBefore, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	cfg.PreserveCodexOfficialAuthOnSwitch = boolPtr(true)
	cfg.UnifyCodexSessionHistory = true
	cfg.ClientRouteEnabled = map[string]bool{clientCodex: true}
	cfg.ClientConfigPaths = map[string]string{clientCodex: configPath}
	cfg.TextModelMappings = []textModelMapping{{Name: "gpt-test", Model: "upstream-test"}}
	a := &app{cfg: cfg}

	results, applyErrors := a.configureEnabledClientRoutes("http://127.0.0.1:8787/", home)
	if len(applyErrors) != 0 {
		t.Fatalf("route synchronization errors = %#v", applyErrors)
	}
	if len(results) != 1 || results[0].Client != clientCodex {
		t.Fatalf("route synchronization results = %#v", results)
	}
	assertFileBytes(t, authPath, authBefore)
	assertFileBytes(t, historyPath, historyBefore)
	got := a.currentConfig()
	if !preserveCodexOfficialAuth(got) || !got.UnifyCodexSessionHistory {
		t.Fatalf("Codex enhancement settings changed during route synchronization: %#v", got)
	}
}

func TestHandleClientConfigureAppliesDesktopAndCLIPrograms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	controller := &recordingClientProgramController{}
	a := &app{
		cfg:                     defaultConfig(),
		configPath:              filepath.Join(home, "vision-relay.json"),
		clientProgramController: controller,
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/client/configure", bytes.NewBufferString(`{"client":"codex"}`))
	a.handleClientConfigure(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("configure status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Programs []clientProgramActionResult `json:"programs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Programs) != 2 {
		t.Fatalf("Codex configure programs = %#v, want desktop and CLI results", payload.Programs)
	}
	if payload.Programs[0].Client != clientCodex || payload.Programs[1].Client != clientCodexCLI {
		t.Fatalf("Codex configure program order = %#v", payload.Programs)
	}
}

func TestCodexRelayConfigBlockAuthModes(t *testing.T) {
	tests := []struct {
		name             string
		directUpstream   bool
		preserveOfficial bool
		requiresAuth     bool
		bearerToken      string
	}{
		{name: "local preserved", preserveOfficial: true, requiresAuth: true, bearerToken: codexLocalBearerToken},
		{name: "local not preserved", preserveOfficial: false, requiresAuth: false},
		{name: "direct preserved", directUpstream: true, preserveOfficial: true, requiresAuth: true, bearerToken: "sk-upstream"},
		{name: "direct managed", directUpstream: true, preserveOfficial: false, requiresAuth: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preserveOfficial := test.preserveOfficial
			ctx := clientConfigContext{
				Origin:               "http://127.0.0.1:8787",
				Key:                  "sk-upstream",
				DirectUpstream:       test.directUpstream,
				PreserveOfficialAuth: &preserveOfficial,
			}
			config := strings.Join(codexRelayConfigBlock(ctx, "gpt-test"), "\n")
			requiresAuth := fmt.Sprintf("requires_openai_auth = %t", test.requiresAuth)
			if !strings.Contains(config, requiresAuth) {
				t.Fatalf("Codex auth requirement = unexpected:\n%s", config)
			}
			if test.bearerToken == "" {
				if strings.Contains(config, "experimental_bearer_token") {
					t.Fatalf("Codex config should use managed or disabled auth without a provider token:\n%s", config)
				}
				return
			}
			bearerToken := fmt.Sprintf("experimental_bearer_token = %q", test.bearerToken)
			if !strings.Contains(config, bearerToken) {
				t.Fatalf("Codex config should use bearer token %q:\n%s", test.bearerToken, config)
			}
		})
	}
}

func TestWriteClientConfigs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	projectDir := filepath.Join(home, "project")
	ctx := clientConfigContext{
		HomeDir:       home,
		ProjectDir:    projectDir,
		Origin:        "http://127.0.0.1:8787",
		Model:         "z-ai/glm-5.2",
		VisionEnabled: true,
		ModelMappings: []textModelMapping{
			{Name: "z-ai/glm-5.2", Model: "z-ai/glm-5.2", ContextWindow: flexInt(196000)},
			{Name: "deepseek-ai/deepseek-v4-pro", Model: "deepseek-ai/deepseek-v4-pro"},
		},
	}

	codexPath, err := writeClientConfig(clientCodex, ctx)
	if err != nil {
		t.Fatal(err)
	}
	codexUserRaw, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	codexUser := string(codexUserRaw)
	if !strings.Contains(codexUser, `model_provider = "custom"`) ||
		!strings.Contains(codexUser, `model = "z-ai/glm-5.2"`) ||
		!strings.Contains(codexUser, `model_catalog_json = "vision-relay-model.json"`) ||
		!strings.Contains(codexUser, `disable_response_storage = true`) ||
		!strings.Contains(codexUser, `web_search = "disabled"`) ||
		!strings.Contains(codexUser, `[model_providers.custom]`) ||
		!strings.Contains(codexUser, `requires_openai_auth = true`) ||
		!strings.Contains(codexUser, `[windows]`) ||
		!strings.Contains(codexUser, `sandbox = "unelevated"`) ||
		!strings.Contains(codexUser, `base_url = "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("bad codex user config:\n%s", codexUser)
	}
	if !strings.Contains(codexUser, `experimental_bearer_token = "`+codexLocalBearerToken+`"`) {
		t.Fatalf("local Codex config should isolate official auth with the relay bearer marker:\n%s", codexUser)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("codex account auth should not be replaced, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "vision-relay-model.json")); err != nil {
		t.Fatalf("global codex model catalog should be written, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "models_cache.json")); !os.IsNotExist(err) {
		t.Fatalf("dedicated catalog should not create models_cache.json, stat err: %v", err)
	}
	codexProjectRaw, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	codexProject := string(codexProjectRaw)
	if !strings.Contains(codexProject, `model = "z-ai/glm-5.2"`) ||
		!strings.Contains(codexProject, `model_catalog_json = "vision-relay-model.json"`) ||
		!strings.Contains(codexProject, `sandbox = "unelevated"`) {
		t.Fatalf("bad codex project config:\n%s", codexProject)
	}
	for _, forbidden := range []string{"model_provider =", "[model_providers.", "requires_openai_auth =", "experimental_bearer_token =", "base_url ="} {
		if strings.Contains(codexProject, forbidden) {
			t.Fatalf("project config must not contain user-only Codex setting %q:\n%s", forbidden, codexProject)
		}
	}
	catalogRaw, err := os.ReadFile(filepath.Join(projectDir, ".codex", "vision-relay-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(catalogRaw), `"slug": "z-ai/glm-5.2"`) ||
		!strings.Contains(string(catalogRaw), `"base_instructions"`) ||
		!strings.Contains(string(catalogRaw), `"shell_type": "shell_command"`) ||
		strings.Contains(string(catalogRaw), `"apply_patch_tool_type"`) {
		t.Fatalf("bad codex project catalog:\n%s", string(catalogRaw))
	}

	openCodePath, err := writeClientConfig(clientOpenCode, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var openCode map[string]any
	if err := readJSON(openCodePath, &openCode); err != nil {
		t.Fatal(err)
	}
	if openCode["model"] != "vision-relay/z-ai/glm-5.2" {
		t.Fatalf("bad opencode model: %#v", openCode["model"])
	}
	provider := openCode["provider"].(map[string]any)["vision-relay"].(map[string]any)
	options := provider["options"].(map[string]any)
	if options["baseURL"] != "http://127.0.0.1:8787/v1" {
		t.Fatalf("bad opencode options: %#v", options)
	}
	if _, exists := options["apiKey"]; exists {
		t.Fatalf("local OpenCode config should not contain an API key: %#v", options)
	}
	models := provider["models"].(map[string]any)
	if len(models) != 2 {
		t.Fatalf("OpenCode should expose every configured model: %#v", models)
	}
	model := models["z-ai/glm-5.2"].(map[string]any)
	if model["attachment"] != true || model["vision"] != true {
		t.Fatalf("opencode model does not advertise image support: %#v", model)
	}
	if model["limit"].(map[string]any)["context"] != float64(196000) {
		t.Fatalf("OpenCode model context limit was not synchronized: %#v", model)
	}
	if _, ok := models["deepseek-ai/deepseek-v4-pro"]; !ok {
		t.Fatalf("OpenCode secondary model is missing: %#v", models)
	}

	claudePath, err := writeClientConfig(clientClaudeCode, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var claude map[string]any
	if err := readJSON(claudePath, &claude); err != nil {
		t.Fatal(err)
	}
	if claude["inferenceProvider"] != "gateway" ||
		claude["inferenceGatewayBaseUrl"] != "http://127.0.0.1:8787" ||
		claude["inferenceGatewayAuthScheme"] != "bearer" ||
		claude["inferenceGatewayApiKey"] != "vision-relay" ||
		claude["disableDeploymentModeChooser"] != true {
		t.Fatalf("bad Claude Desktop gateway config: %#v", claude)
	}
	for _, key := range []string{"$schema", "availableModels", "env", "model", "inferenceAnthropicApiKey", "modelDiscoveryEnabled"} {
		if _, exists := claude[key]; exists {
			t.Fatalf("Claude Desktop config contains CLI-only key %q: %#v", key, claude)
		}
	}
	desktopModels := claude["inferenceModels"].([]any)
	if len(desktopModels) != 2 || desktopModels[0].(map[string]any)["name"] != "z-ai/glm-5.2" || desktopModels[1].(map[string]any)["name"] != "deepseek-ai/deepseek-v4-pro" {
		t.Fatalf("Claude Desktop should expose every configured model: %#v", models)
	}
	metaPath := filepath.Join(filepath.Dir(claudePath), "_meta.json")
	var meta map[string]any
	if err := readJSON(metaPath, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["appliedId"] != strings.TrimSuffix(filepath.Base(claudePath), filepath.Ext(claudePath)) {
		t.Fatalf("Claude Desktop active profile was not updated: %#v", meta)
	}

	cliCtx := ctx
	cliCtx.ConfigPath = filepath.Join(home, ".claude", "settings.json")
	claudeCLIPath, err := writeClaudeCodeConfig(cliCtx)
	if err != nil {
		t.Fatal(err)
	}
	var claudeCLI map[string]any
	if err := readJSON(claudeCLIPath, &claudeCLI); err != nil {
		t.Fatal(err)
	}
	env := claudeCLI["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787" {
		t.Fatalf("bad Claude Code CLI env: %#v", env)
	}
	if _, exists := env["ANTHROPIC_AUTH_TOKEN"]; exists {
		t.Fatalf("local Claude Code CLI config should not contain an auth token: %#v", env)
	}
	availableModels := claudeCLI["availableModels"].([]any)
	if len(availableModels) != 2 || availableModels[0] != "z-ai/glm-5.2" || availableModels[1] != "deepseek-ai/deepseek-v4-pro" {
		t.Fatalf("Claude Code CLI should expose every configured model: %#v", availableModels)
	}
	if env["ANTHROPIC_CUSTOM_MODEL_OPTION"] != "z-ai/glm-5.2" ||
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "deepseek-ai/deepseek-v4-pro" ||
		env["ANTHROPIC_DEFAULT_SONNET_MODEL_NAME"] != "Vision Relay deepseek-ai/deepseek-v4-pro" {
		t.Fatalf("Claude Code CLI picker slots were not synchronized: %#v", env)
	}

}

func TestWriteClaudeDesktopConfigUsesGatewaySchemaForDirectAnthropic(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "AppData", "Local", "Claude-3p", "configLibrary", "profile.json")
	ctx := clientConfigContext{
		HomeDir: home, ConfigPath: path, Origin: "https://api.anthropic.com/v1",
		Key: "anthropic-key", Provider: "anthropic", DirectUpstream: true,
		ModelMappings: []textModelMapping{{Name: "claude-sonnet-4-6", Model: "claude-sonnet-4-6"}},
	}
	if _, err := writeClaudeDesktopConfig(ctx); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := readJSON(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["inferenceProvider"] != "gateway" || cfg["inferenceGatewayBaseUrl"] != "https://api.anthropic.com" || cfg["inferenceGatewayAuthScheme"] != "x-api-key" || cfg["inferenceGatewayApiKey"] != "anthropic-key" {
		t.Fatalf("bad direct Anthropic Claude Desktop config: %#v", cfg)
	}
	if _, exists := cfg["inferenceAnthropicApiKey"]; exists {
		t.Fatalf("unsupported Anthropic-specific key was written: %#v", cfg)
	}
}

func TestWriteOpenClawConfigPreservesExistingJSON5AndAddsModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCLAW_HOME", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	path := filepath.Join(home, ".openclaw", "openclaw.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{
		// Existing settings must survive a one-click configuration.
		gateway: { mode: 'local', },
		agents: { defaults: { models: { 'anthropic/claude-sonnet-4-6': { alias: 'sonnet' }, 'vision-relay/stale-model': {}, }, }, },
		models: { providers: { existing: { baseUrl: 'https://example.com/v1', apiKey: 'keep-me', }, }, },
	}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := clientConfigContext{
		HomeDir:       home,
		Origin:        "http://127.0.0.1:8787",
		Model:         "upstream-model",
		VisionEnabled: true,
		ModelMappings: []textModelMapping{
			{Name: "relay-default", Model: "upstream-model", ContextWindow: flexInt(196000)},
			{Name: "relay-fast", Model: "upstream-fast"},
		},
	}
	gotPath, err := writeClientConfig(clientOpenClaw, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path {
		t.Fatalf("wrong OpenClaw config path: got %q want %q", gotPath, path)
	}

	var cfg map[string]any
	if err := readJSON(path, &cfg); err != nil {
		t.Fatal(err)
	}
	gateway := cfg["gateway"].(map[string]any)
	if gateway["mode"] != "local" {
		t.Fatalf("existing OpenClaw settings were not preserved: %#v", gateway)
	}
	defaults := cfg["agents"].(map[string]any)["defaults"].(map[string]any)
	if defaults["model"].(map[string]any)["primary"] != "vision-relay/relay-default" {
		t.Fatalf("wrong OpenClaw primary model: %#v", defaults["model"])
	}
	allowed := defaults["models"].(map[string]any)
	if _, ok := allowed["anthropic/claude-sonnet-4-6"]; !ok {
		t.Fatalf("existing model allowlist entry was removed: %#v", allowed)
	}
	if _, ok := allowed["vision-relay/stale-model"]; ok {
		t.Fatalf("stale Vision Relay model was not removed from the allowlist: %#v", allowed)
	}
	if _, ok := allowed["vision-relay/relay-default"]; !ok {
		t.Fatalf("Vision Relay model was not added to existing allowlist: %#v", allowed)
	}
	if _, ok := allowed["vision-relay/relay-fast"]; !ok {
		t.Fatalf("Vision Relay secondary model was not added to existing allowlist: %#v", allowed)
	}

	modelConfig := cfg["models"].(map[string]any)
	if modelConfig["mode"] != "merge" {
		t.Fatalf("OpenClaw model catalog should merge configured providers: %#v", modelConfig)
	}
	providers := modelConfig["providers"].(map[string]any)
	if _, ok := providers["existing"]; !ok {
		t.Fatalf("existing provider was removed: %#v", providers)
	}
	provider := providers["vision-relay"].(map[string]any)
	if provider["baseUrl"] != "http://127.0.0.1:8787/v1" || provider["api"] != "openai-completions" {
		t.Fatalf("bad OpenClaw provider: %#v", provider)
	}
	if _, exists := provider["apiKey"]; exists {
		t.Fatalf("local OpenClaw config should not contain an API key: %#v", provider)
	}
	models := provider["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("wrong OpenClaw model count: %#v", models)
	}
	first := models[0].(map[string]any)
	if first["id"] != "relay-default" || first["contextWindow"] != float64(196000) {
		t.Fatalf("bad OpenClaw model: %#v", first)
	}
	inputs := first["input"].([]any)
	if len(inputs) != 2 || inputs[1] != "image" {
		t.Fatalf("OpenClaw model does not advertise image support: %#v", inputs)
	}
	if backups, err := filepath.Glob(path + ".bak.*"); err != nil || len(backups) != 1 {
		t.Fatalf("OpenClaw config backup was not created: paths=%#v err=%v", backups, err)
	}
}

func TestOpenClawConfigPathHonorsEnvironmentOverrides(t *testing.T) {
	home := t.TempDir()
	customHome := filepath.Join(home, "custom-home")
	t.Setenv("OPENCLAW_HOME", customHome)
	t.Setenv("OPENCLAW_STATE_DIR", "~/state")
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	if got, want := openClawConfigPath(home), filepath.Join(customHome, "state", "openclaw.json"); got != want {
		t.Fatalf("OPENCLAW_STATE_DIR was not honored: got %q want %q", got, want)
	}

	override := filepath.Join(home, "config", "custom.json5")
	t.Setenv("OPENCLAW_CONFIG_PATH", override)
	if got := openClawConfigPath(home); got != override {
		t.Fatalf("OPENCLAW_CONFIG_PATH was not honored: got %q want %q", got, override)
	}
}

func TestWriteOpenCodeConfigSynchronizesModelCatalog(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := map[string]any{
		"provider": map[string]any{
			"keep-provider": map[string]any{"name": "Keep Me"},
			relayProviderID: map[string]any{
				"obsoleteProviderOption": true,
				"models": map[string]any{
					"stale-model": map[string]any{"name": "stale-model"},
				},
			},
		},
	}
	if err := writeJSONFile(path, existing); err != nil {
		t.Fatal(err)
	}
	ctx := clientConfigContext{
		HomeDir:       home,
		Origin:        "http://127.0.0.1:8787",
		Model:         "z-ai/glm-5.2",
		VisionEnabled: true,
		ModelMappings: []textModelMapping{{Name: "z-ai/glm-5.2", Model: "z-ai/glm-5.2"}},
	}
	if _, err := writeOpenCodeConfig(ctx); err != nil {
		t.Fatal(err)
	}

	ctx.Model = "grok-4.5"
	ctx.ModelMappings = []textModelMapping{{Name: "grok-4.5", Model: "grok-4.5", ContextWindow: 256000}}
	if _, err := writeOpenCodeConfig(ctx); err != nil {
		t.Fatal(err)
	}

	var cfg map[string]any
	if err := readJSON(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["model"] != "vision-relay/grok-4.5" {
		t.Fatalf("OpenCode default should be replaced with the current model: %#v", cfg["model"])
	}
	providers := cfg["provider"].(map[string]any)
	if _, exists := providers["keep-provider"]; !exists {
		t.Fatalf("unrelated OpenCode providers should be preserved: %#v", providers)
	}
	provider := providers[relayProviderID].(map[string]any)
	if _, exists := provider["obsoleteProviderOption"]; exists {
		t.Fatalf("Vision Relay provider should be fully regenerated: %#v", provider)
	}
	models := provider["models"].(map[string]any)
	if len(models) != 1 {
		t.Fatalf("OpenCode catalog should exactly match the current profile: %#v", models)
	}
	if _, exists := models["z-ai/glm-5.2"]; exists {
		t.Fatalf("previous profile model should be removed: %#v", models)
	}
	grok, exists := models["grok-4.5"].(map[string]any)
	if !exists {
		t.Fatalf("current profile model should be configured: %#v", models)
	}
	if grok["limit"].(map[string]any)["context"] != float64(256000) {
		t.Fatalf("current model metadata should be regenerated: %#v", grok)
	}
}
func TestWriteClaudeCodeConfigExposesAllModelsAndLegacyPickerSlots(t *testing.T) {
	home := t.TempDir()
	mappings := make([]textModelMapping, 0, 5)
	for i := 1; i <= 5; i++ {
		id := "relay-model-" + strconv.Itoa(i)
		mappings = append(mappings, textModelMapping{Name: id, Model: "upstream-" + strconv.Itoa(i)})
	}
	ctx := clientConfigContext{
		HomeDir:       home,
		Origin:        "http://127.0.0.1:8787",
		Model:         "upstream-1",
		ModelMappings: mappings,
	}
	path, err := writeClaudeCodeConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := readJSON(path, &cfg); err != nil {
		t.Fatal(err)
	}
	available := cfg["availableModels"].([]any)
	if len(available) != 5 || available[4] != "relay-model-5" {
		t.Fatalf("Claude Code available model catalog is incomplete: %#v", available)
	}
	env := cfg["env"].(map[string]any)
	wantSlots := map[string]string{
		"ANTHROPIC_CUSTOM_MODEL_OPTION":  "relay-model-1",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "relay-model-2",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "relay-model-3",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "relay-model-4",
	}
	for key, want := range wantSlots {
		if env[key] != want {
			t.Fatalf("Claude Code picker slot %s = %#v, want %q; env=%#v", key, env[key], want, env)
		}
	}
}

func TestWriteOpenCodeConfigCanDisableImageSupport(t *testing.T) {
	home := t.TempDir()
	ctx := clientConfigContext{
		HomeDir:       home,
		Origin:        "http://127.0.0.1:8787",
		Model:         "z-ai/glm-5.2",
		VisionEnabled: false,
	}
	openCodePath, err := writeClientConfig(clientOpenCode, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var openCode map[string]any
	if err := readJSON(openCodePath, &openCode); err != nil {
		t.Fatal(err)
	}
	provider := openCode["provider"].(map[string]any)["vision-relay"].(map[string]any)
	model := provider["models"].(map[string]any)["z-ai/glm-5.2"].(map[string]any)
	if model["attachment"] != false || model["vision"] != false {
		t.Fatalf("opencode model should not advertise image support: %#v", model)
	}
	modalities := model["modalities"].(map[string]any)
	input := modalities["input"].([]any)
	if len(input) != 1 || input[0] != "text" {
		t.Fatalf("opencode input modalities should be text only: %#v", model)
	}
}

func TestClientCatalogUsesVisionCapabilitySetting(t *testing.T) {
	ctx := clientConfigContext{
		Model: "vision-model",
		ModelMappings: []textModelMapping{
			{Name: "vision-model", Model: "upstream-vision", SupportsImages: true},
			{Name: "text-model", Model: "upstream-text"},
		},
		VisionEnabled: false,
	}
	entries := codexModelCatalogEntries(ctx, nil)
	if len(entries) != 2 {
		t.Fatalf("expected two catalog entries, got %#v", entries)
	}
	if got := entries[0]["input_modalities"].([]string); len(got) != 2 || got[0] != "text" || got[1] != "image" {
		t.Fatalf("native multimodal model should retain image input when vision relay is disabled: %#v", entries[0])
	}
	if entries[0]["supports_image_detail_original"] != true {
		t.Fatalf("native multimodal model should retain original image detail: %#v", entries[0])
	}
	if got := entries[1]["input_modalities"].([]string); len(got) != 1 || got[0] != "text" {
		t.Fatalf("text-only model should advertise text input only when vision relay is disabled: %#v", entries[1])
	}
	if entries[1]["supports_image_detail_original"] != false {
		t.Fatalf("text-only model should not support original image detail when vision relay is disabled: %#v", entries[1])
	}

	ctx.VisionEnabled = true
	entries = codexModelCatalogEntries(ctx, nil)
	for _, entry := range entries {
		if got := entry["input_modalities"].([]string); len(got) != 2 || got[0] != "text" || got[1] != "image" {
			t.Fatalf("vision-enabled model should advertise image input: %#v", entry)
		}
		if entry["supports_image_detail_original"] != true {
			t.Fatalf("vision-enabled model should support original image detail: %#v", entry)
		}
	}

	ctx.DirectUpstream = true
	ctx.VisionEnabled = false
	entries = codexModelCatalogEntries(ctx, nil)
	if got := entries[0]["input_modalities"].([]string); len(got) != 2 || got[0] != "text" || got[1] != "image" {
		t.Fatalf("direct multimodal model should retain image input: %#v", entries[0])
	}
	if entries[0]["supports_image_detail_original"] != true {
		t.Fatalf("direct multimodal model should support original image detail: %#v", entries[0])
	}
	if got := entries[1]["input_modalities"].([]string); len(got) != 1 || got[0] != "text" {
		t.Fatalf("direct text-only model should advertise text input only: %#v", entries[1])
	}
	if entries[1]["supports_image_detail_original"] != false {
		t.Fatalf("direct text-only model should not support original image detail: %#v", entries[1])
	}
}

func TestCodexCatalogUsesReasoningEffortPerModel(t *testing.T) {
	ctx := clientConfigContext{
		Model: "chat-only",
		ModelMappings: []textModelMapping{
			{Name: "chat-only", Model: "gpt-5-chat-only", ReasoningEffort: "none"},
			{Name: "reasoning-low", Model: "upstream-low", ReasoningEffort: "low"},
			{Name: "reasoning-medium", Model: "upstream-medium", ReasoningEffort: "medium"},
			{Name: "reasoning-high", Model: "upstream-high", ReasoningEffort: "high"},
			{Name: "reasoning-xhigh", Model: "upstream-xhigh", ReasoningEffort: "xhigh"},
		},
	}
	entries := codexModelCatalogEntries(ctx, nil)
	if len(entries) != 5 {
		t.Fatalf("expected five catalog entries, got %#v", entries)
	}

	unsupported := entries[0]
	if _, exists := unsupported["default_reasoning_level"]; exists {
		t.Fatalf("non-reasoning model should not define a default reasoning level: %#v", unsupported)
	}
	levels, ok := unsupported["supported_reasoning_levels"].([]any)
	if !ok || len(levels) != 0 {
		t.Fatalf("non-reasoning model should not expose reasoning levels: %#v", unsupported)
	}
	if unsupported["supports_reasoning_summaries"] != false || unsupported["supports_reasoning_summary_parameter"] != false {
		t.Fatalf("non-reasoning model should disable reasoning summaries: %#v", unsupported)
	}

	for index, effort := range []string{"low", "medium", "high", "xhigh"} {
		entry := entries[index+1]
		if entry["default_reasoning_level"] != effort || entry["supports_reasoning_summaries"] != true || entry["supports_reasoning_summary_parameter"] != true {
			t.Fatalf("%s model should advertise its configured reasoning effort: %#v", effort, entry)
		}
		levels, ok := entry["supported_reasoning_levels"].([]any)
		if !ok || len(levels) != 2 {
			t.Fatalf("%s model should expose none/%s levels: %#v", effort, effort, entry)
		}
		configured, ok := levels[1].(map[string]any)
		if !ok || configured["effort"] != effort {
			t.Fatalf("%s model has the wrong reasoning level: %#v", effort, entry)
		}
	}
}

func TestCodexConfigUsesDefaultModelReasoningEffort(t *testing.T) {
	ctx := clientConfigContext{
		Origin: "http://127.0.0.1:8787",
		ModelMappings: []textModelMapping{
			{Name: "chat-only", Model: "gpt-5-chat-only", ReasoningEffort: "none"},
			{Name: "reasoning-model", Model: "upstream-reasoning", ReasoningEffort: "xhigh"},
		},
	}
	block := strings.Join(codexRelayConfigBlock(ctx, codexConfigModel(ctx)), "\n")
	if strings.Contains(block, "model_reasoning_effort") {
		t.Fatalf("non-reasoning default model should not force a reasoning effort:\n%s", block)
	}

	ctx.ModelMappings[0], ctx.ModelMappings[1] = ctx.ModelMappings[1], ctx.ModelMappings[0]
	block = strings.Join(codexRelayConfigBlock(ctx, codexConfigModel(ctx)), "\n")
	if !strings.Contains(block, `model_reasoning_effort = "xhigh"`) {
		t.Fatalf("reasoning default model should use its configured effort:\n%s", block)
	}
}

func TestTextModelReasoningEffortMigratesLegacyBoolean(t *testing.T) {
	mappings := normalizeTextModelMappings([]textModelMapping{
		{Name: "legacy-reasoning", Model: "legacy-reasoning", SupportsReasoning: boolPtr(true)},
		{Name: "legacy-chat", Model: "legacy-chat", SupportsReasoning: boolPtr(false)},
	}, nil, "")
	if mappings[0].ReasoningEffort != "high" || mappings[1].ReasoningEffort != "none" {
		t.Fatalf("legacy reasoning booleans were not migrated: %#v", mappings)
	}
	if mappings[0].SupportsReasoning != nil || mappings[1].SupportsReasoning != nil {
		t.Fatalf("normalized mappings should not retain the legacy boolean: %#v", mappings)
	}
}

func TestHandleClientConfigureWritesCodexConfig(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "project")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			Addr: "127.0.0.1:8787",
			TextModelMappings: []textModelMapping{
				{Name: "gpt-5.5", Model: "gpt-5.5"},
				{Name: "gpt-5.4", Model: "gpt-5.4"},
			},
			VisionEnabled: boolPtr(true),
		}),
		clientProgramController: &recordingClientProgramController{running: false},
		configPath:              filepath.Join(home, "vision-relay-config.json"),
	}
	body := bytes.NewBufferString(`{"client":"codex","work_dir":` + strconv.Quote(projectDir) + `}`)
	req := httptest.NewRequest(http.MethodPost, "/api/client/configure", body)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	a.handleClientConfigure(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		RouteEnabled    bool   `json:"route_enabled"`
		RestartRequired bool   `json:"restart_required"`
		Builtin         bool   `json:"builtin"`
		WasRunning      bool   `json:"was_running"`
		AutoRestart     bool   `json:"auto_restart"`
		AutoStart       bool   `json:"auto_start"`
		ProgramAction   string `json:"program_action"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.RouteEnabled || result.RestartRequired || !result.Builtin || result.WasRunning || !result.AutoRestart || result.AutoStart || result.ProgramAction != "not-running" {
		t.Fatalf("Codex one-click config should be built in, enable routing, and keep a stopped client closed by default: %#v", result)
	}
	if !a.currentConfig().ClientRouteEnabled[clientCodex] {
		t.Fatalf("Codex route was not persisted: %#v", a.currentConfig().ClientRouteEnabled)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(raw)
	if !strings.Contains(config, `model_provider = "custom"`) ||
		!strings.Contains(config, `requires_openai_auth = true`) ||
		!strings.Contains(config, `model_catalog_json = "vision-relay-model.json"`) ||
		!strings.Contains(config, `web_search = "disabled"`) {
		t.Fatalf("codex config was not written:\n%s", config)
	}
	if !strings.Contains(config, `experimental_bearer_token = "`+codexLocalBearerToken+`"`) {
		t.Fatalf("local Codex config should isolate official auth with the relay bearer marker:\n%s", config)
	}
	catalogRaw, err := os.ReadFile(filepath.Join(home, ".codex", "vision-relay-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := string(catalogRaw)
	if !strings.Contains(catalog, `"slug": "gpt-5.5"`) || !strings.Contains(catalog, `"slug": "gpt-5.4"`) {
		t.Fatalf("codex model catalog should include hot-switch models:\n%s", catalog)
	}
}

func TestHandleClientConfigureWithProfileOnlyUpdatesSelectedClient(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	localAPIEnabled := false
	codexPath := filepath.Join(home, "clients", "codex.toml")
	openCodePath := filepath.Join(home, "clients", "opencode.json")
	claudePath := filepath.Join(home, "clients", "claude.json")
	if err := os.MkdirAll(filepath.Dir(codexPath), 0o700); err != nil {
		t.Fatal(err)
	}
	openCodeBefore := []byte(`{"sentinel":"opencode"}`)
	claudeBefore := []byte(`{"sentinel":"claude"}`)
	if err := os.WriteFile(openCodePath, openCodeBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, claudeBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := normalizeSeparateModelProfiles(config{
		Addr:            defaultAddr,
		LocalAPIEnabled: &localAPIEnabled,
		VisionEnabled:   boolPtr(true),
		TextModelProfiles: []textModelProfile{
			{ID: "codex-target", Name: "Codex target", Client: textProfileClientCodex, Provider: "openai", WireAPI: "responses", BaseURL: "https://codex.example/v1", APIKey: "sk-codex", ModelMappings: []textModelMapping{{Name: "gpt-target", Model: "gpt-target"}}},
			{ID: "claude-current", Name: "Claude current", Client: textProfileClientClaude, Provider: "anthropic", BaseURL: "https://claude.example", ModelMappings: []textModelMapping{{Name: "claude-target", Model: "claude-target"}}},
			{ID: "opencode-current", Name: "OpenCode current", Client: textProfileClientOpenCode, Provider: "openai", BaseURL: "https://opencode.example/v1", ModelMappings: []textModelMapping{{Name: "open-target", Model: "open-target"}}},
		},
		ActiveTextProfileID: "opencode-current",
		ActiveTextProfileByClient: map[string]string{
			textProfileClientCodex:    "codex-target",
			textProfileClientClaude:   "claude-current",
			textProfileClientOpenCode: "opencode-current",
		},
		ClientConfigPaths: map[string]string{
			clientCodex:      codexPath,
			clientOpenCode:   openCodePath,
			clientClaudeCode: claudePath,
		},
	})
	a := &app{
		cfg:                     cfg,
		configPath:              filepath.Join(home, "vision-relay.json"),
		clientProgramController: &recordingClientProgramController{},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/client/configure", bytes.NewBufferString(`{"client":"codex","profile_id":"codex-target"}`))
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	a.handleClientConfigure(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	codexRaw, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexRaw), `base_url = "https://codex.example/v1"`) {
		t.Fatalf("Codex did not receive the selected supplier:\n%s", codexRaw)
	}
	assertFileBytes(t, openCodePath, openCodeBefore)
	assertFileBytes(t, claudePath, claudeBefore)
	got := a.currentConfig()
	if got.ActiveTextProfileByClient[textProfileClientCodex] != "codex-target" ||
		got.ActiveTextProfileByClient[textProfileClientClaude] != "claude-current" ||
		got.ActiveTextProfileByClient[textProfileClientOpenCode] != "opencode-current" {
		t.Fatalf("client supplier selections changed unexpectedly: %#v", got.ActiveTextProfileByClient)
	}
}

func TestHandleClientConfigureRejectsProfileFromDifferentClientGroup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := defaultConfig()
	cfg.TextModelProfiles = normalizeTextProfiles([]textModelProfile{{
		ID: "claude-only", Name: "Claude only", Client: textProfileClientClaude, Provider: "anthropic",
	}})
	a := &app{cfg: cfg, configPath: filepath.Join(home, "vision-relay.json")}
	req := httptest.NewRequest(http.MethodPost, "/api/client/configure", bytes.NewBufferString(`{"client":"codex","profile_id":"claude-only"}`))
	rec := httptest.NewRecorder()
	a.handleClientConfigure(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleClientConfigureUsesDirectSupplierWhenLocalAPIDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	localAPIEnabled := false
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider:    "openai",
			TextBaseURL:     "https://supplier.example/v1",
			TextAPIKey:      "sk-upstream",
			TextWireAPI:     "chat_completions",
			LocalAPIEnabled: &localAPIEnabled,
			TextModelMappings: []textModelMapping{
				{Name: "vision-alias", Model: "upstream-vision", SupportsImages: true},
				{Name: "text-alias", Model: "upstream-text", SupportsImages: false},
			},
			VisionEnabled: boolPtr(true),
		}),
		clientProgramController: &recordingClientProgramController{running: false},
		configPath:              filepath.Join(home, "vision-relay-config.json"),
	}
	body := bytes.NewBufferString(`{"client":"opencode"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/client/configure", body)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	a.handleClientConfigure(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		DirectUpstream bool   `json:"direct_upstream"`
		Provider       string `json:"provider"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.DirectUpstream || result.Provider != "openai" {
		t.Fatalf("one-click config did not select direct supplier mode: %#v", result)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var openCode map[string]any
	if err := json.Unmarshal(raw, &openCode); err != nil {
		t.Fatal(err)
	}
	providers := openCode["provider"].(map[string]any)
	provider := providers[relayProviderID].(map[string]any)
	options := provider["options"].(map[string]any)
	if options["baseURL"] != "https://supplier.example/v1" || options["apiKey"] != "sk-upstream" {
		t.Fatalf("direct supplier credentials were not written: %#v", options)
	}
	models := provider["models"].(map[string]any)
	if len(models) != 2 {
		t.Fatalf("direct mode should include only the two configured models: %#v", models)
	}
	if _, exists := models["vision-alias"]; exists {
		t.Fatalf("relay alias should not be written in direct mode: %#v", models)
	}
	visionModel := models["upstream-vision"].(map[string]any)
	textModel := models["upstream-text"].(map[string]any)
	if visionModel["attachment"] != true || visionModel["vision"] != true {
		t.Fatalf("multimodal upstream model should retain image capability: %#v", visionModel)
	}
	if textModel["attachment"] != false || textModel["vision"] != false {
		t.Fatalf("text-only upstream model should not advertise image capability: %#v", textModel)
	}
	if openCode["model"] != "vision-relay/upstream-vision" {
		t.Fatalf("direct config did not select the real upstream model: %#v", openCode["model"])
	}
}

func TestValidateDirectClientRouteCompatibility(t *testing.T) {
	mappings := []textModelMapping{{Name: "model", Model: "model"}}
	tests := []struct {
		name     string
		client   string
		provider string
		wireAPI  string
		wantErr  string
	}{
		{name: "codex responses", client: clientCodex, provider: "openai", wireAPI: "responses"},
		{name: "codex chat completions", client: clientCodex, provider: "openai", wireAPI: "chat_completions", wantErr: "Responses"},
		{name: "codex anthropic", client: clientCodex, provider: "anthropic", wireAPI: "responses", wantErr: "Responses"},
		{name: "claude anthropic", client: clientClaudeCode, provider: "anthropic", wireAPI: "chat_completions"},
		{name: "claude openai", client: clientClaudeCode, provider: "openai", wireAPI: "responses", wantErr: "Anthropic"},
		{name: "opencode gemini", client: clientOpenCode, provider: "gemini", wireAPI: "chat_completions"},
		{name: "openclaw ollama", client: clientOpenClaw, provider: "ollama", wireAPI: "chat_completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDirectClientRoute(tt.client, config{TextProvider: tt.provider, TextWireAPI: tt.wireAPI}, mappings)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected compatibility error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("compatibility error = %v, want substring %q", err, tt.wantErr)
			}
			var validationErr *clientRouteValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("compatibility error should be a clientRouteValidationError: %T", err)
			}
		})
	}
}

func TestHandleClientConfigureRejectsEmptyDirectModelList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	localAPIEnabled := false
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			TextProvider:    "openai",
			TextBaseURL:     "https://supplier.example",
			TextAPIKey:      "sk-upstream",
			TextWireAPI:     "responses",
			LocalAPIEnabled: &localAPIEnabled,
		}),
		configPath: filepath.Join(home, "vision-relay-config.json"),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/client/configure", bytes.NewBufferString(`{"client":"opencode"}`))
	rec := httptest.NewRecorder()
	a.handleClientConfigure(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty direct model list status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "至少一个模型") {
		t.Fatalf("empty direct model error is not actionable: %s", rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "opencode.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid direct configuration should not write OpenCode config: %v", err)
	}
}

func TestHandleClientRoutesApplyOnlyUpdatesEnabledClientsInDirectMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	localAPIEnabled := false
	a := &app{
		cfg: normalizeSeparateModelProfiles(config{
			Addr:               "127.0.0.1:8787",
			TextProvider:       "openai",
			TextBaseURL:        "https://switched.example/v1",
			TextAPIKey:         "sk-switched-upstream",
			TextModelMappings:  []textModelMapping{{Name: "route-model", Model: "upstream-model", SupportsImages: true}},
			LocalAPIEnabled:    &localAPIEnabled,
			ClientRouteEnabled: map[string]bool{clientOpenCode: true},
			VisionEnabled:      boolPtr(true),
		}),
		configPath: filepath.Join(home, "vision-relay-config.json"),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/client/routes/apply", nil)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	a.handleClientRoutesApply(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		OK      bool                `json:"ok"`
		Clients []clientRouteResult `json:"clients"`
		Errors  []string            `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || len(result.Errors) != 0 || len(result.Clients) != 1 || result.Clients[0].Client != clientOpenCode {
		t.Fatalf("wrong route apply result: %#v", result)
	}
	if !result.Clients[0].DirectUpstream || result.Clients[0].Provider != "openai" {
		t.Fatalf("supplier switch should update the enabled route in direct mode: %#v", result.Clients[0])
	}
	openCodeRaw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	openCodeText := string(openCodeRaw)
	for _, want := range []string{`"upstream-model"`, `"https://switched.example/v1"`, `"sk-switched-upstream"`} {
		if !strings.Contains(openCodeText, want) {
			t.Fatalf("enabled OpenCode route is missing direct upstream value %s:\n%s", want, openCodeRaw)
		}
	}
	for _, forbidden := range []string{`"route-model"`, `"sk-opencode"`} {
		if strings.Contains(openCodeText, forbidden) {
			t.Fatalf("enabled OpenCode route retained relay-only value %s:\n%s", forbidden, openCodeRaw)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled Codex route should not be updated: %v", err)
	}
}

func TestWriteCodexConfigReplacesPreviousRelayBlock(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "project")
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := strings.Join([]string{
		"# user setting",
		"# Added by Vision Relay. Edit from the Client Access page.",
		`model = "deepseek-ai/deepseek-v4-pro"`,
		`model_catalog_json = "C:\\Users\\me\\.codex\\vision-relay-model-catalog.json"`,
		`model_provider = "vision-relay"`,
		"",
		"[model_providers.vision-relay]",
		`name = "Old Vision Relay"`,
		`base_url = "http://old/v1"`,
		"",
		"[model_providers.other]",
		`name = "Other"`,
		`base_url = "http://other/v1"`,
		"",
		"[windows]",
		`sandbox = "elevated"`,
	}, "\n")
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := clientConfigContext{HomeDir: home, ProjectDir: projectDir, Origin: "http://new", Key: "sk", Model: "new-model"}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	if strings.Contains(after, "old-model") || strings.Contains(after, "http://old/v1") {
		t.Fatalf("old relay config was not removed:\n%s", after)
	}
	if strings.Count(after, "model_catalog_json") != 1 || !strings.Contains(after, "vision-relay-model.json") {
		t.Fatalf("global model catalog config should be replaced with one Vision Relay entry:\n%s", after)
	}
	if !strings.Contains(after, `model_provider = "custom"`) ||
		!strings.Contains(after, `[model_providers.custom]`) ||
		!strings.Contains(after, `requires_openai_auth = true`) ||
		!strings.Contains(after, `base_url = "http://new/v1"`) {
		t.Fatalf("global codex config should route the custom provider through Vision Relay:\n%s", after)
	}
	if strings.Contains(after, `forced_login_method = "api"`) || strings.Contains(after, `cli_auth_credentials_store = "file"`) {
		t.Fatalf("global codex config should keep account login mode:\n%s", after)
	}
	if !strings.Contains(after, `model = "new-model"`) {
		t.Fatalf("global codex model should be written for Vision Relay mode:\n%s", after)
	}
	if !strings.Contains(after, `[windows]`) || !strings.Contains(after, `sandbox = "unelevated"`) || strings.Contains(after, `sandbox = "elevated"`) {
		t.Fatalf("global codex config should force unelevated Windows sandbox:\n%s", after)
	}
	if !strings.Contains(after, `[model_providers.other]`) || !strings.Contains(after, `base_url = "http://other/v1"`) {
		t.Fatalf("unrelated provider was removed:\n%s", after)
	}
	projectRaw, err := os.ReadFile(filepath.Join(projectDir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	project := string(projectRaw)
	if !strings.Contains(project, `model = "new-model"`) ||
		!strings.Contains(project, `model_catalog_json = "vision-relay-model.json"`) ||
		!strings.Contains(project, `sandbox = "unelevated"`) {
		t.Fatalf("project codex model was not updated:\n%s", project)
	}
	for _, forbidden := range []string{"model_provider =", "[model_providers.", "requires_openai_auth =", "experimental_bearer_token =", "base_url ="} {
		if strings.Contains(project, forbidden) {
			t.Fatalf("project config must not contain user-only Codex setting %q:\n%s", forbidden, project)
		}
	}
}

func TestWriteCodexConfigRepairsMisplacedRelayBlockAndDuplicateWindows(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "project")
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := strings.Join([]string{
		`model_provider = "openai"`,
		"",
		"[marketplaces.openai-bundled]",
		`source_type = "local"`,
		"",
		"[windows]",
		`sandbox = "elevated"`,
		"",
		"# Added by Vision Relay. Edit from the Client Access page.",
		`model = "gpt-5.5"`,
		`model_catalog_json = 'C:\\old\\vision-relay-model.json'`,
		`model_provider = "vision-relay"`,
		"",
		"[model_providers.vision-relay]",
		`name = "Old Vision Relay"`,
		`base_url = "http://old/v1"`,
		"",
		"[windows]",
		`sandbox = "unelevated"`,
	}, "\n")
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := clientConfigContext{HomeDir: home, ProjectDir: projectDir, Origin: "http://127.0.0.1:8787", Model: "z-ai/glm-5.2"}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	if strings.Count(after, "[windows]") != 1 || strings.Count(after, "sandbox =") != 1 || strings.Contains(after, `sandbox = "elevated"`) {
		t.Fatalf("windows sandbox should be written exactly once:\n%s", after)
	}
	if strings.Count(after, "model_provider =") != 1 || strings.Count(after, "model_catalog_json =") != 1 || strings.Count(after, "[model_providers.custom]") != 1 {
		t.Fatalf("relay configuration should be written exactly once:\n%s", after)
	}
	if strings.Contains(after, "http://old/v1") || strings.Index(after, `model_provider = "custom"`) > strings.Index(after, "[marketplaces.openai-bundled]") {
		t.Fatalf("relay root config should precede TOML tables:\n%s", after)
	}
	projectRaw, err := os.ReadFile(filepath.Join(projectDir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	project := string(projectRaw)
	if strings.Count(project, "[windows]") != 1 || strings.Count(project, "model_provider =") != 0 || strings.Count(project, "model_catalog_json =") != 1 || strings.Contains(project, "[model_providers.") {
		t.Fatalf("project config should be written idempotently without user-only provider settings:\n%s", project)
	}
}

func TestWriteCodexConfigTakesOverCCSwitchCustomProviderWithoutDuplicateKeys(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "project")
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ccSwitch := strings.Join([]string{
		`model_provider = "custom"`,
		`model = "gpt-5.6-sol"`,
		`disable_response_storage = true`,
		`model_reasoning_effort = "high"`,
		`model_catalog_json = "cc-switch-model-catalog.json"`,
		`web_search = "disabled"`,
		"",
		"[model_providers.custom]",
		`name = "custom"`,
		`wire_api = "responses"`,
		`requires_openai_auth = true`,
		`base_url = "http://127.0.0.1:15721/v1"`,
		`experimental_bearer_token = "PROXY_MANAGED"`,
		"",
		"[windows]",
		`sandbox = "unelevated"`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(ccSwitch), 0o600); err != nil {
		t.Fatal(err)
	}
	projectCodexDir := filepath.Join(projectDir, ".codex")
	if err := os.MkdirAll(projectCodexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectCodexDir, "config.toml"), []byte(ccSwitch), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := clientConfigContext{
		HomeDir: home, ProjectDir: projectDir, Origin: "http://127.0.0.1:8787", Model: "glm-5",
		ModelMappings: []textModelMapping{{Name: "glm-5", Model: "z-ai/glm-5"}},
	}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(codexDir, "config.toml")
	userRaw, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	user := string(userRaw)
	for _, key := range []string{"model =", "model_provider =", "model_catalog_json =", "disable_response_storage =", "model_reasoning_effort =", "web_search =", "[model_providers.custom]", "[windows]"} {
		if strings.Count(user, key) != 1 {
			t.Fatalf("%s should occur once in %s:\n%s", key, userPath, user)
		}
	}
	if strings.Contains(user, "15721") || strings.Contains(user, "cc-switch-model-catalog.json") || !strings.Contains(user, `base_url = "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("cc-switch provider was not replaced in %s:\n%s", userPath, user)
	}

	projectPath := filepath.Join(projectCodexDir, "config.toml")
	projectRaw, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	project := string(projectRaw)
	for _, key := range []string{"model =", "model_catalog_json =", "disable_response_storage =", "model_reasoning_effort =", "web_search =", "[windows]"} {
		if strings.Count(project, key) != 1 {
			t.Fatalf("%s should occur once in %s:\n%s", key, projectPath, project)
		}
	}
	for _, forbidden := range []string{"model_provider =", "[model_providers.", "requires_openai_auth =", "experimental_bearer_token =", "base_url =", "15721"} {
		if strings.Contains(project, forbidden) {
			t.Fatalf("project config retained user-only provider setting %q:\n%s", forbidden, project)
		}
	}
}

func TestWriteCodexConfigUsesCurrentRelayModel(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "project")
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := strings.Join([]string{
		`model = "gpt-5-codex"`,
		`model_provider = "openai"`,
	}, "\n")
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := clientConfigContext{HomeDir: home, ProjectDir: projectDir, Origin: "http://new", Key: "sk", Model: "deepseek-ai/deepseek-v4-pro"}
	projectPath, err := writeCodexConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	if !strings.Contains(after, `model = "deepseek-ai/deepseek-v4-pro"`) {
		t.Fatalf("relay model was not written:\n%s", after)
	}
	if !strings.Contains(after, `model_catalog_json`) {
		t.Fatalf("project codex config should point at the project model catalog:\n%s", after)
	}
	userRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	user := string(userRaw)
	if !strings.Contains(user, `model = "deepseek-ai/deepseek-v4-pro"`) || !strings.Contains(user, `model_catalog_json`) {
		t.Fatalf("user codex config should advertise current Vision Relay model:\n%s", user)
	}
	if strings.Count(user, "model = ") != 1 || strings.Count(user, "model_provider = ") != 1 || !strings.Contains(user, `model_provider = "custom"`) || !strings.Contains(user, `base_url = "http://new/v1"`) {
		t.Fatalf("user codex config should route custom provider through Vision Relay:\n%s", user)
	}
	if strings.Contains(user, `forced_login_method = "api"`) || strings.Contains(user, `cli_auth_credentials_store = "file"`) {
		t.Fatalf("user codex config should keep account login mode:\n%s", user)
	}
}

func TestRestoreCodexAccountConfigRemovesRelayAndUsesOpenAIAccount(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "账号"), 0o755); err != nil {
		t.Fatal(err)
	}
	current := strings.Join([]string{
		`model = "deepseek-ai/deepseek-v4-pro"`,
		`model_catalog_json = 'C:\Users\me\.codex\vision-relay-model.json'`,
		`model_provider = "vision-relay"`,
		`forced_login_method = "api"`,
		`cli_auth_credentials_store = "file"`,
		"",
		"[desktop]",
		`followUpQueueMode = "queue"`,
		"",
		"[model_providers.vision-relay]",
		`name = "Vision Relay"`,
		`base_url = "http://127.0.0.1:8787/v1"`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	template := strings.Join([]string{
		`model_provider = "custom"`,
		`model = "gpt-5.4"`,
		`model_reasoning_effort = "high"`,
		"",
		"[model_providers.custom]",
		`name = "custom"`,
		`wire_api = "responses"`,
		`requires_openai_auth = true`,
		`base_url = "https://ai.xshentx.org"`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "账号", "config.toml"), []byte(template), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := `{"models":[{"slug":"gpt-5.5"},{"slug":"gpt-5.4"}]}`
	if err := os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
	accountAuth := `{"OPENAI_API_KEY":null,"tokens":{"access_token":"chatgpt-token"}}`
	if err := os.WriteFile(filepath.Join(codexDir, "vision-relay-auth.json"), []byte(accountAuth), 0o600); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(projectDir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectCurrent := strings.Join([]string{
		"# Added by Vision Relay. Edit from the Client Access page.",
		`model = "deepseek-ai/deepseek-v4-pro"`,
		`model_catalog_json = 'C:\Users\me\project\.codex\vision-relay-model.json'`,
		`model_provider = "vision-relay"`,
		"",
		"[tools]",
		`web_search = true`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(projectDir, ".codex", "config.toml"), []byte(projectCurrent), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := restoreCodexAccountConfig(home, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	if strings.Contains(after, "vision-relay") || strings.Contains(after, "deepseek-ai/deepseek-v4-pro") || strings.Contains(after, "model_catalog_json") {
		t.Fatalf("relay config should be removed from account mode:\n%s", after)
	}
	if strings.Contains(after, "forced_login_method") || strings.Contains(after, "cli_auth_credentials_store") {
		t.Fatalf("api auth mode should be removed from account mode:\n%s", after)
	}
	if !strings.Contains(after, `model_provider = "openai"`) ||
		!strings.Contains(after, `model = "gpt-5.5"`) ||
		strings.Contains(after, `[model_providers.custom]`) {
		t.Fatalf("openai account model was not restored:\n%s", after)
	}
	if !strings.Contains(after, `[desktop]`) {
		t.Fatalf("unrelated codex config should be preserved:\n%s", after)
	}
	projectRaw, err := os.ReadFile(filepath.Join(projectDir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	projectAfter := string(projectRaw)
	if strings.Contains(projectAfter, "vision-relay") || strings.Contains(projectAfter, "deepseek-ai/deepseek-v4-pro") || strings.Contains(projectAfter, "openai_base_url") || !strings.Contains(projectAfter, "[tools]") {
		t.Fatalf("project relay config should be removed while preserving unrelated settings:\n%s", projectAfter)
	}
	authRaw, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(authRaw) != accountAuth {
		t.Fatalf("account auth should be restored, got: %s", string(authRaw))
	}
}

func TestWriteCodexConfigKeepsAccountAuth(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	accountAuth := `{"OPENAI_API_KEY":null,"tokens":{"access_token":"chatgpt-token"}}`
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(accountAuth), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := clientConfigContext{HomeDir: home, ProjectDir: filepath.Join(home, "project"), Origin: "http://127.0.0.1:8787", Model: "deepseek-ai/deepseek-v4-pro"}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != accountAuth {
		t.Fatalf("account auth should be kept, got: %s", string(raw))
	}
	if _, err := os.Stat(filepath.Join(codexDir, "vision-relay-auth.json")); !os.IsNotExist(err) {
		t.Fatalf("account auth backup should not be written, stat err: %v", err)
	}
}

func TestWriteCodexConfigDoesNotBackUpRelayModeAsAccount(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	account := strings.Join([]string{
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(account), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := clientConfigContext{HomeDir: home, ProjectDir: filepath.Join(home, "project"), Origin: "http://127.0.0.1:8787", Model: "deepseek-ai/deepseek-v4-pro"}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	backupRaw, err := os.ReadFile(codexAccountBackupPath(home))
	if err != nil {
		t.Fatal(err)
	}
	backup := string(backupRaw)
	if !strings.Contains(backup, `model = "gpt-5.5"`) || strings.Contains(backup, "deepseek-ai/deepseek-v4-pro") || strings.Contains(backup, "vision-relay-model") {
		t.Fatalf("relay mode should not overwrite account backup:\n%s", backup)
	}
}

func TestRestoreCodexAccountPrefersAccountTemplateOverStaleBackup(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "账号"), 0o755); err != nil {
		t.Fatal(err)
	}
	current := strings.Join([]string{
		"# Added by Vision Relay. Edit from the Client Access page.",
		`model = "deepseek-ai/deepseek-v4-pro"`,
		`model_catalog_json = 'C:\Users\me\.codex\vision-relay-model.json'`,
		`model_provider = "vision-relay"`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	staleBackup := strings.Join([]string{
		`model = "deepseek-ai/deepseek-v4-pro"`,
		`model_provider = "openai"`,
	}, "\n")
	if err := os.WriteFile(codexAccountBackupPath(home), []byte(staleBackup), 0o600); err != nil {
		t.Fatal(err)
	}
	accountTemplate := strings.Join([]string{
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "账号", "config.toml"), []byte(accountTemplate), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := restoreCodexAccountConfig(home, filepath.Join(home, "project"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	if !strings.Contains(after, `model = "gpt-5.5"`) || strings.Contains(after, "deepseek-ai/deepseek-v4-pro") || strings.Contains(after, "vision-relay-model") {
		t.Fatalf("account template should win over stale backup:\n%s", after)
	}
}

func TestCodexModelCacheRemoveKeepsAccountModels(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := `{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","description":"Account model"},{"slug":"legacy-relay","description":"Current Vision Relay upstream text model. Routes to old-model."}]}`
	if err := os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeCodexModelCache(home); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(codexDir, "models_cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	if strings.Contains(after, "legacy-relay") || !strings.Contains(after, `"slug": "gpt-5.5"`) || !strings.Contains(after, "Account model") {
		t.Fatalf("relay model should be removed without removing account models:\n%s", after)
	}
}

func TestWriteCodexConfigWritesMultipleHotSwitchModels(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "project")
	ctx := clientConfigContext{
		HomeDir:    home,
		ProjectDir: projectDir,
		Origin:     "http://127.0.0.1:8787",
		Model:      "gpt-5.5",
		ModelMappings: []textModelMapping{
			{Name: "gpt-5.5", Model: "gpt-5.5", ContextWindow: 128000},
			{Name: "gpt-5.4", Model: "gpt-5.4", ContextWindow: 128000},
			{Name: "DeepSeek V4", Model: "deepseek-ai/deepseek-v4-pro", ContextWindow: 64000},
		},
	}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	catalogRaw, err := os.ReadFile(filepath.Join(projectDir, ".codex", "vision-relay-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := string(catalogRaw)
	for _, want := range []string{`"slug": "gpt-5.5"`, `"slug": "gpt-5.4"`, `"slug": "DeepSeek V4"`, `"context_window": 64000`, "Routes to deepseek-ai/deepseek-v4-pro", `"base_instructions"`, `"shell_type": "shell_command"`, `"mode": "bytes"`} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("codex catalog missing %s:\n%s", want, catalog)
		}
	}
	for _, forbidden := range []string{`"slug": "gpt-5.4-mini"`, `"apply_patch_tool_type"`, `"web_search_tool_type"`} {
		if strings.Contains(catalog, forbidden) {
			t.Fatalf("codex catalog should not contain %s:\n%s", forbidden, catalog)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "models_cache.json")); !os.IsNotExist(err) {
		t.Fatalf("hot-switch catalog should not create models_cache.json, stat err: %v", err)
	}
}

func TestCodexConfigDirHonorsCODEXHome(t *testing.T) {
	home := t.TempDir()
	customDir := filepath.Join(t.TempDir(), "custom-codex")
	t.Setenv("CODEX_HOME", customDir)

	if got := codexConfigDir(home); got != customDir {
		t.Fatalf("wrong Codex config directory: got %q want %q", got, customDir)
	}
}

func TestWriteCodexConfigUsesCODEXHomeWithoutProjectConfig(t *testing.T) {
	home := t.TempDir()
	customDir := filepath.Join(t.TempDir(), "custom-codex")
	workingDir := t.TempDir()
	t.Setenv("CODEX_HOME", customDir)
	t.Chdir(workingDir)

	ctx := clientConfigContext{
		HomeDir: home,
		Origin:  "http://127.0.0.1:8787",
		Model:   "gpt-5.5",
	}
	path, err := writeCodexConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(customDir, "config.toml")
	if path != wantPath {
		t.Fatalf("wrong config path: got %q want %q", path, wantPath)
	}
	if _, err := os.Stat(filepath.Join(customDir, "vision-relay-model.json")); err != nil {
		t.Fatalf("custom Codex model catalog was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workingDir, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("working directory should not receive implicit Codex config, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("default Codex directory should not be used when CODEX_HOME is set, stat err: %v", err)
	}
}

func TestCodexProjectDirRequiresExplicitWorkDir(t *testing.T) {
	home := t.TempDir()
	if got := clientProjectDir(clientCodex, "", home); got != "" {
		t.Fatalf("Codex should not infer a project directory: %q", got)
	}
	projectDir := filepath.Join(home, "project")
	if got := clientProjectDir(clientCodex, projectDir, home); got != projectDir {
		t.Fatalf("wrong explicit Codex project directory: got %q want %q", got, projectDir)
	}
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func TestWriteOpenCodeConfigAdvertisesReasoningModels(t *testing.T) {
	home := t.TempDir()
	ctx := clientConfigContext{
		HomeDir: home,
		Origin:  "http://127.0.0.1:8787",
		ModelMappings: []textModelMapping{
			{Name: "deepseek-ai/deepseek-v4-pro", Model: "deepseek-ai/deepseek-v4-pro"},
			{Name: "z-ai/glm-5.2", Model: "z-ai/glm-5.2"},
			{Name: "grok-4.5", Model: "grok-4.5"},
			{Name: "plain-chat", Model: "plain-chat"},
			{Name: "grok-4.5-chat-only", Model: "grok-4.5", SupportsReasoning: boolPtr(false)},
		},
		VisionEnabled: true,
	}
	path, err := writeOpenCodeConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := readJSON(path, &cfg); err != nil {
		t.Fatal(err)
	}
	provider := cfg["provider"].(map[string]any)[relayProviderID].(map[string]any)
	models := provider["models"].(map[string]any)
	for _, id := range []string{"deepseek-ai/deepseek-v4-pro", "z-ai/glm-5.2", "grok-4.5"} {
		model := models[id].(map[string]any)
		if model["reasoning"] != true {
			t.Fatalf("reasoning model %q should be advertised as supported: %#v", id, model)
		}
	}
	if models["plain-chat"].(map[string]any)["reasoning"] != false {
		t.Fatalf("plain model should not be advertised as reasoning: %#v", models["plain-chat"])
	}
	if models["grok-4.5-chat-only"].(map[string]any)["reasoning"] != false {
		t.Fatalf("explicit reasoning override should win over name inference: %#v", models["grok-4.5-chat-only"])
	}
}

func TestTextConfigForClientRouteRejectsUnconfiguredGroup(t *testing.T) {
	cfg := providerRouterTestConfig([]textModelProfile{{
		ID: "open-selected", Client: textProfileClientOpenCode, Provider: "openai", WireAPI: "chat_completions", BaseURL: "https://open.example/v1",
	}}, map[string]string{textProfileClientOpenCode: "open-selected"})
	if _, err := textConfigForClientRoute(cfg, clientCodex); err == nil || !strings.Contains(err.Error(), "no model supplier configured for codex") {
		t.Fatalf("Codex route must reject another group's supplier, err=%v", err)
	}
	selected, err := textConfigForClientRoute(cfg, clientOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ActiveTextProfileID != "open-selected" || selected.TextBaseURL != "https://open.example/v1" {
		t.Fatalf("OpenCode selected wrong supplier: %#v", selected)
	}
}

func TestConfigureClientRouteUsesSelectedProfileWithoutExplicitProfileID(t *testing.T) {
	home := t.TempDir()
	localAPIEnabled := false
	openCodePath := filepath.Join(home, "opencode.json")
	cfg := normalizeSeparateModelProfiles(config{
		LocalAPIEnabled:     &localAPIEnabled,
		ActiveTextProfileID: "wrong-global",
		ActiveTextProfileByClient: map[string]string{
			textProfileClientOpenCode: "opencode-selected",
		},
		TextModelProfiles: []textModelProfile{
			{ID: "wrong-global", Client: textProfileClientCodex, Provider: "openai", WireAPI: "responses", BaseURL: "https://wrong.example/v1", APIKey: "sk-wrong", ModelMappings: []textModelMapping{{Name: "wrong", Model: "wrong"}}},
			{ID: "opencode-selected", Client: textProfileClientOpenCode, Provider: "openai", BaseURL: "https://selected.example/v1", APIKey: "sk-selected", ModelMappings: []textModelMapping{{Name: "selected", Model: "selected"}}},
		},
		ClientConfigPaths: map[string]string{clientOpenCode: openCodePath},
	})
	a := &app{cfg: cfg}

	if _, err := a.configureClientRoute(clientOpenCode, "", "http://127.0.0.1:8787", home); err != nil {
		t.Fatal(err)
	}
	var openCode map[string]any
	if err := readJSON(openCodePath, &openCode); err != nil {
		t.Fatal(err)
	}
	provider := openCode["provider"].(map[string]any)[relayProviderID].(map[string]any)
	options := provider["options"].(map[string]any)
	if options["baseURL"] != "https://selected.example/v1" || options["apiKey"] != "sk-selected" || openCode["model"] != "vision-relay/selected" {
		t.Fatalf("one-click route did not use the selected OpenCode profile: %#v", openCode)
	}
}

func TestWriteCodexConfigOnDarwinDoesNotAddWindowsSandbox(t *testing.T) {
	home := t.TempDir()
	userPath := filepath.Join(home, ".codex", "config.toml")
	projectDir := filepath.Join(home, "project")
	ctx := clientConfigContext{
		HomeDir:    home,
		ProjectDir: projectDir,
		ConfigPath: userPath,
		Origin:     "http://127.0.0.1:8787",
		Model:      "test-model",
		GOOS:       "darwin",
	}
	projectPath, err := writeCodexConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{userPath, projectPath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "[windows]") || strings.Contains(string(raw), "sandbox =") {
			t.Fatalf("darwin Codex config contains Windows-only sandbox settings at %s:\n%s", path, raw)
		}
	}

	existing := []string{"[windows]", "sandbox = \"elevated\""}
	if got := upsertCodexPlatformSettings(existing, "darwin"); strings.Join(got, "\n") != strings.Join(existing, "\n") {
		t.Fatalf("darwin rewrite changed an existing shared Windows section: %#v", got)
	}
}

func TestClaudeOneClickReturnsDesktopAndCLIConfigPaths(t *testing.T) {
	home := t.TempDir()
	desktopPath := filepath.Join(home, "Library", "Application Support", "Claude-3p", "configLibrary", "vision-relay.json")
	cliPath := filepath.Join(home, ".claude", "settings.json")
	cfg := defaultConfig()
	cfg.ClientConfigPaths = map[string]string{
		clientClaudeCode: desktopPath,
		clientClaudeCLI:  cliPath,
	}
	a := &app{cfg: cfg}
	result, err := a.configureClientRoute(clientClaudeCode, "", "http://127.0.0.1:8787", home)
	if err != nil {
		t.Fatal(err)
	}
	if result.ConfigPaths[clientClaudeCode] != desktopPath || result.ConfigPaths[clientClaudeCLI] != cliPath {
		t.Fatalf("Claude one-click config paths = %#v", result.ConfigPaths)
	}
	for _, path := range []string{desktopPath, cliPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Claude one-click did not write %s: %v", path, err)
		}
	}
}

func TestWriteClaudeCLIConfigUsesDarwinCLIPath(t *testing.T) {
	home := t.TempDir()
	clearClientPathEnvironment(t)
	path, err := writeClaudeCodeConfig(clientConfigContext{
		HomeDir: home,
		Origin:  "http://127.0.0.1:8787",
		Model:   "test-model",
		GOOS:    "darwin",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".claude", "settings.json")
	if path != want {
		t.Fatalf("darwin Claude CLI config path = %q, want %q", path, want)
	}
	if strings.Contains(path, "Claude-3p") {
		t.Fatalf("Claude CLI config was written into the Desktop profile library: %s", path)
	}
}
