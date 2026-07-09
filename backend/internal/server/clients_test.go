package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	if !strings.Contains(codexUser, `model_provider = "openai"`) ||
		!strings.Contains(codexUser, `model = "z-ai/glm-5.2"`) ||
		!strings.Contains(codexUser, `model_catalog_json = `) ||
		!strings.Contains(codexUser, `openai_base_url = "http://127.0.0.1:8787/v1"`) ||
		!strings.Contains(codexUser, `forced_login_method = "api"`) ||
		!strings.Contains(codexUser, `cli_auth_credentials_store = "file"`) {
		t.Fatalf("bad codex user config:\n%s", codexUser)
	}
	var codexAuth map[string]any
	if err := readJSON(filepath.Join(home, ".codex", "auth.json"), &codexAuth); err != nil {
		t.Fatal(err)
	}
	if codexAuth["OPENAI_API_KEY"] != "sk-local" {
		t.Fatalf("bad codex api auth: %#v", codexAuth)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "vision-relay-model-catalog.json")); err != nil {
		t.Fatalf("global codex model catalog should be written, stat err: %v", err)
	}
	cacheRaw, err := os.ReadFile(filepath.Join(home, ".codex", "models_cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cacheRaw), `"slug": "z-ai/glm-5.2"`) {
		t.Fatalf("codex model cache should include relay model:\n%s", string(cacheRaw))
	}
	codexProjectRaw, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	codexProject := string(codexProjectRaw)
	if !strings.Contains(codexProject, `model = "z-ai/glm-5.2"`) ||
		!strings.Contains(codexProject, `model_catalog_json = `) ||
		!strings.Contains(codexProject, `model_provider = "openai"`) ||
		!strings.Contains(codexProject, `openai_base_url = "http://127.0.0.1:8787/v1"`) {
		t.Fatalf("bad codex project config:\n%s", codexProject)
	}
	catalogRaw, err := os.ReadFile(filepath.Join(projectDir, ".codex", "vision-relay-model-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(catalogRaw), `"slug": "z-ai/glm-5.2"`) {
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
	if strings.Count(after, "model_catalog_json") != 1 || !strings.Contains(after, "vision-relay-model-catalog.json") {
		t.Fatalf("global model catalog config should be replaced with one Vision Relay entry:\n%s", after)
	}
	if !strings.Contains(after, `model_provider = "openai"`) || !strings.Contains(after, `openai_base_url = "http://new/v1"`) {
		t.Fatalf("global codex config should route the built-in OpenAI provider through Vision Relay:\n%s", after)
	}
	if !strings.Contains(after, `forced_login_method = "api"`) || !strings.Contains(after, `cli_auth_credentials_store = "file"`) {
		t.Fatalf("global codex config should force API key mode:\n%s", after)
	}
	if !strings.Contains(after, `model = "new-model"`) {
		t.Fatalf("global codex model should be written for Vision Relay mode:\n%s", after)
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
		!strings.Contains(project, `model_catalog_json = `) ||
		!strings.Contains(project, `model_provider = "openai"`) ||
		!strings.Contains(project, `openai_base_url = "http://new/v1"`) {
		t.Fatalf("project codex model was not updated:\n%s", project)
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
	if strings.Count(user, "model = ") != 1 || strings.Count(user, "model_provider = ") != 1 || !strings.Contains(user, `model_provider = "openai"`) || !strings.Contains(user, `openai_base_url = "http://new/v1"`) {
		t.Fatalf("user codex config should route OpenAI provider through Vision Relay:\n%s", user)
	}
	if !strings.Contains(user, `forced_login_method = "api"`) || !strings.Contains(user, `cli_auth_credentials_store = "file"`) {
		t.Fatalf("user codex config should force API key mode:\n%s", user)
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
		`model_catalog_json = 'C:\Users\me\.codex\vision-relay-model-catalog.json'`,
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
		`model_catalog_json = 'C:\Users\me\project\.codex\vision-relay-model-catalog.json'`,
		`model_provider = "openai"`,
		`openai_base_url = "http://127.0.0.1:8787/v1"`,
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

func TestWriteCodexConfigBacksUpAndWritesAPIAuth(t *testing.T) {
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
	backupRaw, err := os.ReadFile(filepath.Join(codexDir, "vision-relay-auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(backupRaw) != accountAuth {
		t.Fatalf("account auth backup mismatch: %s", string(backupRaw))
	}
	var apiAuth map[string]any
	if err := readJSON(filepath.Join(codexDir, "auth.json"), &apiAuth); err != nil {
		t.Fatal(err)
	}
	if apiAuth["OPENAI_API_KEY"] != "sk-local" {
		t.Fatalf("api auth was not written: %#v", apiAuth)
	}
	if err := restoreCodexAuth(home); err != nil {
		t.Fatal(err)
	}
	restoredRaw, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredRaw) != accountAuth {
		t.Fatalf("account auth restore mismatch: %s", string(restoredRaw))
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
	if !strings.Contains(backup, `model = "gpt-5.5"`) || strings.Contains(backup, "deepseek-ai/deepseek-v4-pro") || strings.Contains(backup, "vision-relay-model-catalog.json") {
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
		`model_catalog_json = 'C:\Users\me\.codex\vision-relay-model-catalog.json'`,
		`model_provider = "openai"`,
		`openai_base_url = "http://127.0.0.1:8787/v1"`,
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
	if !strings.Contains(after, `model = "gpt-5.5"`) || strings.Contains(after, "deepseek-ai/deepseek-v4-pro") || strings.Contains(after, "vision-relay-model-catalog.json") {
		t.Fatalf("account template should win over stale backup:\n%s", after)
	}
}

func TestCodexModelCacheUpsertAndRemove(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := `{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","description":"Account model","supported_reasoning_levels":[{"effort":"high","description":"High"}]}]}`
	if err := os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := clientConfigContext{HomeDir: home, Model: "deepseek-ai/deepseek-v4-pro", VisionEnabled: true}
	if err := upsertCodexModelCache(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(codexDir, "models_cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	if !strings.Contains(after, `"slug": "deepseek-ai/deepseek-v4-pro"`) || !strings.Contains(after, `"slug": "gpt-5.5"`) {
		t.Fatalf("relay model should be inserted without removing account models:\n%s", after)
	}
	if err := removeCodexModelCache(home); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(codexDir, "models_cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	after = string(raw)
	if strings.Contains(after, "deepseek-ai/deepseek-v4-pro") || !strings.Contains(after, `"slug": "gpt-5.5"`) {
		t.Fatalf("relay model should be removed without removing account models:\n%s", after)
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
	desktopPath := filepath.Join(appDir, "Codex.exe")
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
