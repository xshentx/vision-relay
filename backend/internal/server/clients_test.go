package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestWriteClientConfigs(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "project")
	ctx := clientConfigContext{
		HomeDir:       home,
		ProjectDir:    projectDir,
		Origin:        "http://127.0.0.1:8787",
		Key:           "sk-local",
		Model:         "z-ai/glm-5.2",
		VisionEnabled: true,
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
		!strings.Contains(codexUser, `base_url = "http://127.0.0.1:8787/v1"`) ||
		!strings.Contains(codexUser, `experimental_bearer_token = "PROXY_MANAGED"`) {
		t.Fatalf("bad codex user config:\n%s", codexUser)
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
		!strings.Contains(codexProject, `model_provider = "custom"`) ||
		!strings.Contains(codexProject, `[model_providers.custom]`) ||
		!strings.Contains(codexProject, `requires_openai_auth = true`) ||
		!strings.Contains(codexProject, `sandbox = "unelevated"`) ||
		!strings.Contains(codexProject, `base_url = "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("bad codex project config:\n%s", codexProject)
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
	if options["baseURL"] != "http://127.0.0.1:8787/v1" || options["apiKey"] != "sk-local" {
		t.Fatalf("bad opencode options: %#v", options)
	}
	model := provider["models"].(map[string]any)["z-ai/glm-5.2"].(map[string]any)
	if model["attachment"] != true || model["vision"] != true {
		t.Fatalf("opencode model does not advertise image support: %#v", model)
	}

	claudePath, err := writeClientConfig(clientClaudeCode, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var claude map[string]any
	if err := readJSON(claudePath, &claude); err != nil {
		t.Fatal(err)
	}
	env := claude["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787" || env["ANTHROPIC_AUTH_TOKEN"] != "sk-local" {
		t.Fatalf("bad claude env: %#v", env)
	}
}

func TestWriteOpenCodeConfigCanDisableImageSupport(t *testing.T) {
	home := t.TempDir()
	ctx := clientConfigContext{
		HomeDir:       home,
		Origin:        "http://127.0.0.1:8787",
		Key:           "sk-local",
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
	for _, entry := range entries {
		if got := entry["input_modalities"].([]string); len(got) != 1 || got[0] != "text" {
			t.Fatalf("vision-disabled model should advertise text input only: %#v", entry)
		}
		if entry["supports_image_detail_original"] != false {
			t.Fatalf("vision-disabled model should not support original image detail: %#v", entry)
		}
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
			ClientAPIKeyEntries: []clientAPIKeyEntry{{Name: "Local", Key: "sk-local"}},
			VisionEnabled:       boolPtr(true),
		}),
	}
	body := bytes.NewBufferString(`{"client":"codex","start":false,"stop":false,"work_dir":` + strconv.Quote(projectDir) + `}`)
	req := httptest.NewRequest(http.MethodPost, "/api/client/configure", body)
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	a.handleClientConfigure(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bad status %d: %s", rec.Code, rec.Body.String())
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
	catalogRaw, err := os.ReadFile(filepath.Join(home, ".codex", "vision-relay-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := string(catalogRaw)
	if !strings.Contains(catalog, `"slug": "gpt-5.5"`) || !strings.Contains(catalog, `"slug": "gpt-5.4"`) {
		t.Fatalf("codex model catalog should include hot-switch models:\n%s", catalog)
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
		!strings.Contains(project, `model_provider = "custom"`) ||
		!strings.Contains(project, `requires_openai_auth = true`) ||
		!strings.Contains(project, `sandbox = "unelevated"`) ||
		!strings.Contains(project, `base_url = "http://new/v1"`) {
		t.Fatalf("project codex model was not updated:\n%s", project)
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
	if strings.Count(project, "[windows]") != 1 || strings.Count(project, "model_provider =") != 1 || strings.Count(project, "model_catalog_json =") != 1 {
		t.Fatalf("project config should be written idempotently:\n%s", project)
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
	for _, path := range []string{filepath.Join(codexDir, "config.toml"), filepath.Join(projectCodexDir, "config.toml")} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		after := string(raw)
		for _, key := range []string{"model =", "model_provider =", "model_catalog_json =", "disable_response_storage =", "model_reasoning_effort =", "web_search =", "[model_providers.custom]", "[windows]"} {
			if strings.Count(after, key) != 1 {
				t.Fatalf("%s should occur once in %s:\n%s", key, path, after)
			}
		}
		if strings.Contains(after, "15721") || strings.Contains(after, "cc-switch-model-catalog.json") || !strings.Contains(after, `base_url = "http://127.0.0.1:8787/v1"`) {
			t.Fatalf("cc-switch provider was not replaced in %s:\n%s", path, after)
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

	ctx := clientConfigContext{HomeDir: home, ProjectDir: filepath.Join(home, "project"), Origin: "http://127.0.0.1:8787", Key: "sk-local", Model: "deepseek-ai/deepseek-v4-pro"}
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
	ctx := clientConfigContext{HomeDir: home, ProjectDir: filepath.Join(home, "project"), Origin: "http://127.0.0.1:8787", Key: "sk-local", Model: "deepseek-ai/deepseek-v4-pro"}
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
		Key:     "sk-local",
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

func TestCodexLauncherAcceptsAppsFolderIDWithoutDesktopPath(t *testing.T) {
	ctx := clientConfigContext{LaunchAppID: "OpenAI.Codex_2p2nqsd0c76g0!App"}
	if !codexLauncherAvailable(ctx) {
		t.Fatal("Windows AppsFolder ID should be sufficient to launch Codex")
	}
}
func TestCodexDesktopPathUsesWindowsAppLayout(t *testing.T) {
	home := t.TempDir()
	appDir := filepath.Join(home, "OpenAI.Codex", "app")
	resourcesDir := filepath.Join(appDir, "resources")
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cliName := "codex"
	if runtime.GOOS == "windows" {
		cliName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(resourcesDir, cliName), []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	desktopPath := filepath.Join(appDir, "ChatGPT.exe")
	if err := os.WriteFile(desktopPath, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", resourcesDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, ok := codexDesktopPath("codex", "")
	if !ok {
		t.Fatal("expected Codex desktop app path")
	}
	if got != desktopPath {
		t.Fatalf("wrong desktop path: got %q want %q", got, desktopPath)
	}
}

func TestCodexClientTargetsDesktopShellOnly(t *testing.T) {
	names := clientProcessNames(clientCodex)
	if len(names) == 0 || names[0] != "ChatGPT.exe" {
		t.Fatalf("Codex desktop shell should be the first restart target: %#v", names)
	}
	if len(names) != 1 {
		t.Fatalf("only the desktop shell may be restarted: %#v", names)
	}
}

func TestCodexAppsFolderTarget(t *testing.T) {
	got := codexAppsFolderTarget("OpenAI.Codex_2p2nqsd0c76g0!App")
	want := `shell:AppsFolder\OpenAI.Codex_2p2nqsd0c76g0!App`
	if got != want {
		t.Fatalf("wrong packaged app launch target: got %q want %q", got, want)
	}
}

func TestCodexDesktopPathPrefersCapturedPath(t *testing.T) {
	home := t.TempDir()
	preferred := filepath.Join(home, "Codex.exe")
	if err := os.WriteFile(preferred, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := codexDesktopPath("codex-missing", preferred)
	if !ok {
		t.Fatal("expected captured Codex desktop path")
	}
	if got != preferred {
		t.Fatalf("wrong desktop path: got %q want %q", got, preferred)
	}
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
