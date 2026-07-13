package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	clientCodex               = "codex"
	clientOpenCode            = "opencode"
	clientClaudeCode          = "claude-code"
	relayProviderID           = "vision-relay"
	codexProviderID           = "custom"
	relayEnvKey               = "VISION_RELAY_API_KEY"
	codexManagedBearerToken   = "PROXY_MANAGED"
	codexBaseInstructions     = "You are Codex, a coding agent. You and the user share the same workspace and collaborate to achieve the user's goals."
	codexUnifiedHistoryMarker = "# Added by Vision Relay for unified Codex history."
)

type clientConfigContext struct {
	HomeDir              string
	ProjectDir           string
	Origin               string
	Key                  string
	Model                string
	ModelMappings        []textModelMapping
	VisionEnabled        bool
	PreserveOfficialAuth *bool
	LaunchPath           string
	LaunchAppID          string
}

type clientConfigRequest struct {
	Client  string `json:"client"`
	WorkDir string `json:"work_dir"`
	Start   *bool  `json:"start"`
	Stop    *bool  `json:"stop"`
}

func (a *app) ensureClientKey() (string, bool, error) {
	cfg := a.currentConfig()
	entries := normalizeClientAPIKeyEntries(cfg.ClientAPIKeyEntries)
	if len(entries) > 0 {
		return entries[0].Key, false, nil
	}
	key, err := generateClientAPIKey()
	if err != nil {
		return "", false, err
	}
	cfg.ClientAPIKeyEntries = append(entries, clientAPIKeyEntry{
		Name: "Client Access",
		Key:  key,
	})
	if err := a.setConfig(cfg); err != nil {
		return "", false, err
	}
	return key, true, nil
}

func (a *app) handleClientConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req clientConfigRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	client := normalizeClientID(req.Client)
	if client == "" {
		client = clientCodex
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	key, created, err := a.ensureClientKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	cfg := a.currentConfig()
	projectDir := clientProjectDir(client, req.WorkDir, home)
	ctx := clientConfigContext{
		HomeDir:              home,
		ProjectDir:           projectDir,
		Origin:               requestOrigin(r, cfg),
		Key:                  key,
		Model:                relayModelName(cfg),
		ModelMappings:        textModelMappings(cfg),
		VisionEnabled:        visionEnabled(cfg),
		PreserveOfficialAuth: boolPtr(preserveCodexOfficialAuth(cfg)),
		LaunchPath:           currentCodexDesktopPath(),
		LaunchAppID:          currentCodexAppID(),
	}
	path, err := writeClientConfig(client, ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	warnings := []string{}
	if client != clientCodex {
		warnings = append(warnings, persistClientEnv(key)...)
	}
	restartCodex := client == clientCodex
	stopRequested := optionalBool(req.Stop, restartCodex)
	startRequested := optionalBool(req.Start, restartCodex)
	stopped := false
	if stopRequested {
		stopped = stopClient(client)
		if stopped {
			waitForClientStopped(client, 5*time.Second)
		}
	}
	started := false
	command := clientCommand(client)
	if startRequested {
		var startWarnings []string
		started, command, startWarnings = startClient(client, projectDir, ctx)
		warnings = append(warnings, startWarnings...)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"client":      client,
		"path":        path,
		"project_dir": projectDir,
		"key_created": created,
		"stopped":     stopped,
		"started":     started,
		"command":     command,
		"warnings":    warnings,
	})
}

func optionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (a *app) handleClientRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req clientConfigRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	client := normalizeClientID(req.Client)
	if client == "" {
		client = clientCodex
	}
	if client != clientCodex {
		writeError(w, http.StatusBadRequest, errors.New("only Codex account restore is supported"))
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	projectDir := clientProjectDir(client, req.WorkDir, home)
	path, err := restoreCodexAccountConfigWithOptions(home, projectDir, a.currentConfig().UnifyCodexSessionHistory)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"client":      client,
		"path":        path,
		"project_dir": projectDir,
	})
}

func normalizeClientID(client string) string {
	switch strings.ToLower(strings.TrimSpace(client)) {
	case "codex":
		return clientCodex
	case "opencode", "open-code":
		return clientOpenCode
	case "claude", "claude-code", "claudecode":
		return clientClaudeCode
	default:
		return ""
	}
}

func relayModelName(cfg config) string {
	model := strings.TrimSpace(cfg.TextModelOverride)
	if model == "" {
		model = "z-ai/glm-5.2"
	}
	return model
}

func requestOrigin(r *http.Request, cfg config) string {
	if host := strings.TrimSpace(r.Host); host != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
			scheme = strings.Split(forwarded, ",")[0]
		}
		return scheme + "://" + host
	}
	return "http://" + cfg.Addr
}

func clientWorkDir(workDir, fallback string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			workDir = cwd
		} else {
			workDir = fallback
		}
	}
	if abs, err := filepath.Abs(workDir); err == nil {
		return abs
	}
	return workDir
}

func clientProjectDir(client, workDir, fallback string) string {
	if client == clientCodex && strings.TrimSpace(workDir) == "" {
		return ""
	}
	return clientWorkDir(workDir, fallback)
}

func codexConfigDir(homeDir string) string {
	dir := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if dir == "" {
		return filepath.Join(homeDir, ".codex")
	}
	dir = os.ExpandEnv(dir)
	if dir == "~" {
		return homeDir
	}
	if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		dir = filepath.Join(homeDir, dir[2:])
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

func writeClientConfig(client string, ctx clientConfigContext) (string, error) {
	switch client {
	case clientCodex:
		return writeCodexConfig(ctx)
	case clientOpenCode:
		return writeOpenCodeConfig(ctx)
	case clientClaudeCode:
		return writeClaudeCodeConfig(ctx)
	default:
		return "", errors.New("unsupported client")
	}
}

func writeCodexConfig(ctx clientConfigContext) (string, error) {
	configDir := codexConfigDir(ctx.HomeDir)
	userPath := filepath.Join(configDir, "config.toml")
	if err := saveCodexAccountConfigBackup(ctx.HomeDir, userPath); err != nil {
		return "", err
	}
	model := codexConfigModel(ctx)
	_, err := writeCodexModelCatalog(ctx, configDir)
	if err != nil {
		return "", err
	}
	// Older Vision Relay builds injected models into Codex's account cache. The
	// dedicated catalog is the single source of truth now, matching cc-switch.
	if err := removeCodexModelCache(ctx.HomeDir); err != nil {
		return "", err
	}
	if err := configureCodexAuth(ctx); err != nil {
		return "", err
	}
	lines := []string{}
	if b, err := os.ReadFile(userPath); err == nil {
		lines = strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	lines = removeCodexRelayConfig(lines)
	lines = upsertWindowsSandbox(lines, "unelevated")
	block := codexRelayConfigBlock(ctx, model)
	block = append(block, "")
	content := strings.TrimRight(strings.Join(append(block, lines...), "\n"), "\n") + "\n"
	if err := writeConfigFile(userPath, []byte(content)); err != nil {
		return "", err
	}
	projectPath, err := writeCodexProjectConfig(ctx)
	if err != nil {
		return "", err
	}
	if projectPath != "" {
		return projectPath, nil
	}
	return userPath, nil
}

func codexRelayConfigBlock(ctx clientConfigContext, model string) []string {
	block := []string{
		"# Added by Vision Relay. Edit from the Client Access page.",
		fmt.Sprintf("model = %q", model),
		fmt.Sprintf("model_catalog_json = %q", codexModelCatalogFilename()),
		fmt.Sprintf("model_provider = %q", codexProviderID),
		`disable_response_storage = true`,
		`model_reasoning_effort = "high"`,
		`web_search = "disabled"`,
		"",
		"[model_providers." + codexProviderID + "]",
		`name = "Vision Relay"`,
		`wire_api = "responses"`,
		`requires_openai_auth = true`,
		fmt.Sprintf("base_url = %q", strings.TrimRight(ctx.Origin, "/")+"/v1"),
	}
	if preserveCodexOfficialAuthForContext(ctx) {
		block = append(block, fmt.Sprintf("experimental_bearer_token = %q", codexManagedBearerToken))
	}
	return block
}

func writeCodexProjectConfig(ctx clientConfigContext) (string, error) {
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	if projectDir == "" {
		return "", nil
	}
	model := codexConfigModel(ctx)
	_, err := writeCodexModelCatalog(ctx, filepath.Join(projectDir, ".codex"))
	if err != nil {
		return "", err
	}
	path := filepath.Join(projectDir, ".codex", "config.toml")
	lines := []string{}
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	lines = removeCodexRelayProjectConfig(lines)
	lines = upsertWindowsSandbox(lines, "unelevated")
	block := append(codexRelayConfigBlock(ctx, model), "")
	content := strings.TrimRight(strings.Join(append(block, lines...), "\n"), "\n") + "\n"
	return path, writeConfigFile(path, []byte(content))
}

func codexConfigModel(ctx clientConfigContext) string {
	mappings := normalizeTextModelMappings(ctx.ModelMappings, nil, ctx.Model)
	if len(mappings) > 0 {
		if name := strings.TrimSpace(mappings[0].Name); name != "" {
			return name
		}
		if model := strings.TrimSpace(mappings[0].Model); model != "" {
			return model
		}
	}
	return strings.TrimSpace(ctx.Model)
}

func removeCodexRelayConfig(lines []string) []string {
	out := make([]string, 0, len(lines))
	inRoot := true
	skipSection := false
	skipGeneratedBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if skipGeneratedBlock {
			if trimmed == "" {
				skipGeneratedBlock = false
			}
			continue
		}
		// Older builds could append this root block after a TOML table. Recognize
		// it anywhere so a subsequent one-click configuration repairs that file.
		if strings.HasPrefix(trimmed, "# Added by Vision Relay.") || strings.HasPrefix(trimmed, "# Restored by Vision Relay.") {
			skipGeneratedBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
			section := strings.Trim(trimmed, "[]")
			skipSection = isCodexRelayProviderSection(section)
		}
		if skipSection {
			continue
		}
		if inRoot {
			switch {
			case trimmed == codexUnifiedHistoryMarker:
				continue
			case strings.HasPrefix(trimmed, "model ="):
				continue
			case strings.HasPrefix(trimmed, "model_catalog_json ="):
				continue
			case strings.HasPrefix(trimmed, "model_provider ="):
				continue
			case strings.HasPrefix(trimmed, "disable_response_storage ="):
				continue
			case strings.HasPrefix(trimmed, "model_reasoning_effort ="):
				continue
			case strings.HasPrefix(trimmed, "web_search ="):
				continue
			case strings.HasPrefix(trimmed, "openai_base_url ="):
				continue
			case strings.HasPrefix(trimmed, "forced_login_method ="):
				continue
			case strings.HasPrefix(trimmed, "cli_auth_credentials_store ="):
				continue
			case strings.HasPrefix(trimmed, "# Vision Relay forwards requests to the configured upstream text model:"):
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

func isCodexRelayProviderSection(section string) bool {
	for _, providerID := range []string{codexProviderID, relayProviderID} {
		name := "model_providers." + providerID
		if section == name || strings.HasPrefix(section, name+".") {
			return true
		}
	}
	return false
}

func upsertWindowsSandbox(lines []string, value string) []string {
	out := make([]string, 0, len(lines)+3)
	windowsBody := make([]string, 0, 4)
	inWindows := false
	windowsSeen := false
	sandboxLine := fmt.Sprintf("sandbox = %q", value)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section := strings.Trim(trimmed, "[]")
			inWindows = section == "windows"
			if inWindows {
				windowsSeen = true
				continue
			}
			out = append(out, line)
			continue
		}
		if inWindows {
			if trimmed != "" && !strings.HasPrefix(trimmed, "sandbox =") {
				windowsBody = append(windowsBody, line)
			}
			continue
		}
		out = append(out, line)
	}
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
		out = append(out, "")
	}
	if windowsSeen || value != "" {
		out = append(out, "[windows]", sandboxLine)
		out = append(out, windowsBody...)
	}
	return out
}

func saveCodexAccountConfigBackup(homeDir, userPath string) error {
	raw, err := os.ReadFile(userPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	accountBlock := codexAccountBlockFromLines(lines)
	if len(accountBlock) == 0 {
		return nil
	}
	return writeConfigFile(codexAccountBackupPath(homeDir), []byte(strings.TrimRight(strings.Join(accountBlock, "\n"), "\n")+"\n"))
}

func restoreCodexAccountConfig(homeDir, projectDir string) (string, error) {
	return restoreCodexAccountConfigWithOptions(homeDir, projectDir, false)
}

func restoreCodexAccountConfigWithOptions(homeDir, projectDir string, unifySessionHistory bool) (string, error) {
	userPath := filepath.Join(codexConfigDir(homeDir), "config.toml")
	lines := []string{}
	if raw, err := os.ReadFile(userPath); err == nil {
		lines = strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	lines = removeCodexRelayConfig(lines)
	lines = upsertWindowsSandbox(lines, "unelevated")
	if err := removeCodexModelCache(homeDir); err != nil {
		return "", err
	}
	if err := restoreCodexAuth(homeDir); err != nil {
		return "", err
	}
	accountBlock, err := codexAccountRestoreBlock(homeDir)
	if err != nil {
		return "", err
	}
	if providerID := rootValueFromLines(accountBlock, "model_provider"); providerID != "" && providerID != "openai" {
		lines = removeTomlSection(lines, "model_providers."+providerID)
	}
	if unifySessionHistory {
		accountBlock = codexUnifiedOpenAIAccountBlock(accountBlock)
	}
	content := strings.TrimRight(strings.Join(append(append(accountBlock, ""), lines...), "\n"), "\n") + "\n"
	if err := writeConfigFile(userPath, []byte(content)); err != nil {
		return "", err
	}
	if err := restoreCodexProjectConfig(projectDir); err != nil {
		return "", err
	}
	return userPath, nil
}

func codexUnifiedOpenAIAccountBlock(accountBlock []string) []string {
	root := make([]string, 0, len(accountBlock)+6)
	inRoot := true
	providerWritten := false
	for _, line := range accountBlock {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
		}
		if !inRoot {
			continue
		}
		if strings.HasPrefix(trimmed, "model_provider =") {
			root = append(root, `model_provider = "custom"`)
			providerWritten = true
			continue
		}
		root = append(root, line)
	}
	if !providerWritten {
		root = append(root, `model_provider = "custom"`)
	}
	root = append(root, "")
	root = append(root, codexUnifiedOpenAIProviderBlock()...)
	return root
}

func codexUnifiedOpenAIProviderBlock() []string {
	return []string{
		codexUnifiedHistoryMarker,
		"[model_providers.custom]",
		`name = "OpenAI"`,
		`wire_api = "responses"`,
		`requires_openai_auth = true`,
		`supports_websockets = true`,
	}
}

func restoreCodexProjectConfig(projectDir string) error {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return nil
	}
	path := filepath.Join(projectDir, ".codex", "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	lines = removeCodexRelayProjectConfig(lines)
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if content != "" {
		content += "\n"
	}
	return writeConfigFile(path, []byte(content))
}

func codexAccountRestoreBlock(homeDir string) ([]string, error) {
	if block, err := codexOpenAIAccountBlockFromCache(homeDir); err != nil {
		return nil, err
	} else if len(block) > 0 {
		return block, nil
	}
	candidates := []string{
		filepath.Join(codexConfigDir(homeDir), "账号", "config.toml"),
		filepath.Join(codexConfigDir(homeDir), "config", "config.toml"),
		codexAccountBackupPath(homeDir),
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		block := codexAccountBlockFromLines(strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"))
		if len(block) > 0 && rootValueFromLines(block, "model_provider") == "openai" {
			return block, nil
		}
	}
	return defaultCodexOpenAIAccountBlock(), nil
}

func codexOpenAIAccountBlockFromCache(homeDir string) ([]string, error) {
	path := filepath.Join(codexConfigDir(homeDir), "models_cache.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cache any
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil, nil
	}
	for _, model := range []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini"} {
		if jsonTreeContainsString(cache, model) {
			return []string{
				"# Restored by Vision Relay. Edit from Codex account settings if needed.",
				fmt.Sprintf("model = %q", model),
				`model_provider = "openai"`,
				`model_reasoning_effort = "high"`,
			}, nil
		}
	}
	return nil, nil
}

func defaultCodexOpenAIAccountBlock() []string {
	return []string{
		"# Restored by Vision Relay. Edit from Codex account settings if needed.",
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		`model_reasoning_effort = "high"`,
	}
}

func jsonTreeContainsString(value any, needle string) bool {
	switch v := value.(type) {
	case string:
		return v == needle
	case []any:
		for _, item := range v {
			if jsonTreeContainsString(item, needle) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			if key == needle || jsonTreeContainsString(item, needle) {
				return true
			}
		}
	}
	return false
}

func codexAccountBackupPath(homeDir string) string {
	return filepath.Join(codexConfigDir(homeDir), "vision-relay-account-config.toml")
}

func codexAuthBackupPath(homeDir string) string {
	return filepath.Join(codexConfigDir(homeDir), "vision-relay-auth.json")
}

func preserveCodexOfficialAuthForContext(ctx clientConfigContext) bool {
	return ctx.PreserveOfficialAuth == nil || *ctx.PreserveOfficialAuth
}

func configureCodexAuth(ctx clientConfigContext) error {
	authPath := filepath.Join(codexConfigDir(ctx.HomeDir), "auth.json")
	raw, err := os.ReadFile(authPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	managed := err == nil && codexAuthIsRelayManaged(raw)
	if preserveCodexOfficialAuthForContext(ctx) {
		if !managed {
			return nil
		}
		if err := restoreCodexAuth(ctx.HomeDir); err != nil {
			return err
		}
		if _, err := os.Stat(codexAuthBackupPath(ctx.HomeDir)); errors.Is(err, os.ErrNotExist) {
			return os.Remove(authPath)
		}
		return nil
	}
	if err == nil && !managed {
		if err := writeConfigFile(codexAuthBackupPath(ctx.HomeDir), raw); err != nil {
			return err
		}
	}
	key := strings.TrimSpace(ctx.Key)
	if key == "" {
		key = "sk-local"
	}
	managedAuth, err := json.MarshalIndent(map[string]any{
		"OPENAI_API_KEY":       key,
		"vision_relay_managed": true,
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(authPath, append(managedAuth, '\n'))
}

func codexAuthIsRelayManaged(raw []byte) bool {
	var auth map[string]any
	if json.Unmarshal(raw, &auth) != nil {
		return false
	}
	managed, _ := auth["vision_relay_managed"].(bool)
	return managed
}

func restoreCodexAuth(homeDir string) error {
	backupPath := codexAuthBackupPath(homeDir)
	raw, err := os.ReadFile(backupPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeConfigFile(filepath.Join(codexConfigDir(homeDir), "auth.json"), raw)
}

func codexAccountBlockFromLines(lines []string) []string {
	if codexLinesContainRelayRootConfig(lines) {
		return nil
	}
	providerID := ""
	root := []string{"# Restored by Vision Relay. Edit from Codex account settings if needed."}
	inRoot := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
		}
		if !inRoot || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "model_provider ="):
			providerID = rootTomlStringValue(line, "model_provider")
			if providerID == relayProviderID {
				return nil
			}
			root = append(root, line)
		case strings.HasPrefix(trimmed, "model ="),
			strings.HasPrefix(trimmed, "model_reasoning_effort ="),
			strings.HasPrefix(trimmed, "disable_response_storage ="):
			root = append(root, line)
		case strings.HasPrefix(trimmed, "model_catalog_json ="):
			continue
		}
	}
	if providerID == "" || len(root) == 1 {
		return nil
	}
	if providerID == codexProviderID && codexHistoryLinesContainMarker(lines) {
		return codexStandardOpenAIAccountBlock(root)
	}
	providerSection := extractTomlSection(lines, "model_providers."+providerID)
	if len(providerSection) > 0 {
		root = append(root, "")
		root = append(root, providerSection...)
	}
	return root
}

func codexStandardOpenAIAccountBlock(accountBlock []string) []string {
	root := make([]string, 0, len(accountBlock))
	inRoot := true
	providerWritten := false
	for _, line := range accountBlock {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
		}
		if !inRoot {
			continue
		}
		if strings.HasPrefix(trimmed, "model_provider =") {
			root = append(root, `model_provider = "openai"`)
			providerWritten = true
			continue
		}
		root = append(root, line)
	}
	if !providerWritten {
		root = append(root, `model_provider = "openai"`)
	}
	return root
}

func codexLinesContainRelayRootConfig(lines []string) bool {
	inRoot := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
		}
		if !inRoot {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "# Added by Vision Relay."):
			return true
		case strings.HasPrefix(trimmed, "model_provider =") && rootTomlStringValue(line, "model_provider") == relayProviderID:
			return true
		case strings.HasPrefix(trimmed, "model_catalog_json =") && isVisionRelayCatalogLine(trimmed):
			return true
		case strings.HasPrefix(trimmed, "openai_base_url =") && isVisionRelayBaseURLLine(trimmed):
			return true
		}
	}
	return false
}

func extractTomlSection(lines []string, sectionName string) []string {
	out := []string{}
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			current := strings.Trim(trimmed, "[]")
			if inSection && current != sectionName && !strings.HasPrefix(current, sectionName+".") {
				break
			}
			inSection = current == sectionName || strings.HasPrefix(current, sectionName+".")
		}
		if inSection {
			out = append(out, line)
		}
	}
	return out
}

func removeTomlSection(lines []string, sectionName string) []string {
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			current := strings.Trim(trimmed, "[]")
			skip = current == sectionName || strings.HasPrefix(current, sectionName+".")
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return out
}

func rootValueFromLines(lines []string, key string) string {
	inRoot := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
		}
		if inRoot {
			if value := rootTomlStringValue(line, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func removeCodexRelayProjectConfig(lines []string) []string {
	out := make([]string, 0, len(lines))
	inRoot := true
	skipSection := false
	skipGeneratedBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if skipGeneratedBlock {
			if trimmed == "" {
				skipGeneratedBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "# Added by Vision Relay.") || strings.HasPrefix(trimmed, "# Restored by Vision Relay.") {
			skipGeneratedBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
			section := strings.Trim(trimmed, "[]")
			skipSection = isCodexRelayProviderSection(section)
		}
		if skipSection {
			continue
		}
		if inRoot {
			switch {
			case strings.HasPrefix(trimmed, "model ="):
				continue
			case strings.HasPrefix(trimmed, "model_catalog_json ="):
				continue
			case strings.HasPrefix(trimmed, "model_provider ="):
				continue
			case strings.HasPrefix(trimmed, "disable_response_storage ="):
				continue
			case strings.HasPrefix(trimmed, "model_reasoning_effort ="):
				continue
			case strings.HasPrefix(trimmed, "web_search ="):
				continue
			case strings.HasPrefix(trimmed, "openai_base_url =") && isVisionRelayBaseURLLine(trimmed):
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

func rootTomlStringValue(line, key string) string {
	prefix := key + " ="
	if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
		return ""
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix))
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return strings.Trim(value, `"'`)
}

func tomlLiteralString(value string) string {
	if strings.Contains(value, "'") {
		return strconv.Quote(value)
	}
	return "'" + value + "'"
}

func isVisionRelayCatalogLine(line string) bool {
	_, value, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = unquoted
	} else {
		value = strings.Trim(value, `"'`)
	}
	base := filepath.Base(value)
	return base == codexModelCatalogFilename() || base == "vision-relay-model-catalog.json"
}

func isVisionRelayBaseURLLine(line string) bool {
	_, value, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = unquoted
	} else {
		value = strings.Trim(value, `"'`)
	}
	return strings.HasPrefix(value, "http://127.0.0.1:") || strings.HasPrefix(value, "http://localhost:")
}

func writeCodexModelCatalog(ctx clientConfigContext, dir string) (string, error) {
	path := filepath.Join(dir, codexModelCatalogFilename())
	catalog := map[string]any{
		"models": codexModelCatalogEntries(ctx, nil),
	}
	b, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeConfigFile(path, append(b, '\n')); err != nil {
		return "", err
	}
	return path, nil
}

func codexModelCatalogFilename() string {
	return appSlug + "-model.json"
}

func removeCodexModelCache(homeDir string) error {
	path := filepath.Join(codexConfigDir(homeDir), "models_cache.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	cache := map[string]any{}
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil
	}
	models, _ := cache["models"].([]any)
	out := make([]any, 0, len(models))
	changed := false
	for _, item := range models {
		model, ok := item.(map[string]any)
		if ok && isVisionRelayCacheModel(model) {
			changed = true
			continue
		}
		out = append(out, item)
	}
	if !changed {
		return nil
	}
	cache["models"] = out
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(path, append(b, '\n'))
}

func codexModelCatalogEntries(ctx clientConfigContext, template map[string]any) []map[string]any {
	mappings := normalizeTextModelMappings(ctx.ModelMappings, nil, ctx.Model)
	entries := make([]map[string]any, 0, len(mappings))
	for i, mapping := range mappings {
		entries = append(entries, codexModelCatalogEntry(ctx, template, mapping, 1000+i))
	}
	return entries
}

func codexModelCatalogEntry(ctx clientConfigContext, template map[string]any, mapping textModelMapping, priority int) map[string]any {
	model := cloneStringAnyMap(template)
	for _, key := range []string{
		"apply_patch_tool_type",
		"web_search_tool_type",
		"tools",
		"model_messages",
		"default_verbosity",
		"use_responses_lite",
	} {
		delete(model, key)
	}
	slug := strings.TrimSpace(mapping.Name)
	if slug == "" {
		slug = strings.TrimSpace(mapping.Model)
	}
	description := "Current Vision Relay upstream text model."
	if mapping.Model != "" && mapping.Model != slug {
		description += " Routes to " + mapping.Model + "."
	}
	contextWindow := int(mapping.ContextWindow)
	if contextWindow <= 0 {
		contextWindow = 128000
	}
	model["slug"] = slug
	model["display_name"] = codexAccountModelDisplayName(slug)
	model["description"] = description
	model["base_instructions"] = codexBaseInstructions
	model["default_reasoning_level"] = "high"
	model["supported_reasoning_levels"] = codexReasoningLevels()
	model["visibility"] = "list"
	model["supported_in_api"] = true
	model["priority"] = priority
	imageEnabled := ctx.VisionEnabled
	model["input_modalities"] = relayInputModalities(imageEnabled)
	model["context_window"] = contextWindow
	model["max_context_window"] = contextWindow
	model["effective_context_window_percent"] = 95
	model["additional_speed_tiers"] = []any{}
	model["service_tiers"] = []any{}
	model["availability_nux"] = nil
	model["upgrade"] = nil
	model["supports_reasoning_summaries"] = true
	model["default_reasoning_summary"] = "none"
	model["support_verbosity"] = false
	model["shell_type"] = "shell_command"
	model["truncation_policy"] = map[string]any{"mode": "bytes", "limit": 10000}
	model["supports_parallel_tool_calls"] = false
	model["supports_image_detail_original"] = imageEnabled
	model["supports_search_tool"] = false
	model["experimental_supported_tools"] = []any{}
	return model
}

func isVisionRelayCacheModel(model map[string]any) bool {
	return strings.HasPrefix(modelString(model, "description"), "Current Vision Relay upstream text model.")
}

func modelString(model map[string]any, key string) string {
	value, _ := model[key].(string)
	return value
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func codexReasoningLevels() []any {
	return []any{
		map[string]any{"effort": "none", "description": "Disable reasoning"},
		map[string]any{"effort": "high", "description": "Enable reasoning"},
	}
}

func writeOpenCodeConfig(ctx clientConfigContext) (string, error) {
	path := filepath.Join(ctx.HomeDir, ".config", "opencode", "opencode.json")
	cfg, err := readJSONMap(path)
	if err != nil {
		return "", err
	}
	cfg["$schema"] = "https://opencode.ai/config.json"
	providers := ensureJSONMap(cfg, "provider")
	provider := ensureJSONMap(providers, relayProviderID)
	provider["npm"] = "@ai-sdk/openai-compatible"
	provider["name"] = "Vision Relay"
	options := ensureJSONMap(provider, "options")
	options["baseURL"] = strings.TrimRight(ctx.Origin, "/") + "/v1"
	options["apiKey"] = ctx.Key
	models := ensureJSONMap(provider, "models")
	model := ensureJSONMap(models, ctx.Model)
	imageEnabled := ctx.VisionEnabled
	model["name"] = ctx.Model
	model["attachment"] = imageEnabled
	model["attachments"] = imageEnabled
	model["vision"] = imageEnabled
	model["input_modalities"] = relayInputModalities(imageEnabled)
	model["output_modalities"] = []string{"text"}
	model["modalities"] = map[string]any{
		"input":  relayInputModalities(imageEnabled),
		"output": []string{"text"},
	}
	cfg["model"] = relayProviderID + "/" + ctx.Model
	return path, writeJSONFile(path, cfg)
}

func relayInputModalities(enabled bool) []string {
	if enabled {
		return []string{"text", "image"}
	}
	return []string{"text"}
}

func writeClaudeCodeConfig(ctx clientConfigContext) (string, error) {
	path := filepath.Join(ctx.HomeDir, ".claude", "settings.json")
	cfg, err := readJSONMap(path)
	if err != nil {
		return "", err
	}
	cfg["$schema"] = "https://json.schemastore.org/claude-code-settings.json"
	cfg["model"] = ctx.Model
	env := ensureJSONMap(cfg, "env")
	env["ANTHROPIC_BASE_URL"] = strings.TrimRight(ctx.Origin, "/")
	env["ANTHROPIC_AUTH_TOKEN"] = ctx.Key
	env["ANTHROPIC_CUSTOM_MODEL_OPTION"] = ctx.Model
	env["ANTHROPIC_CUSTOM_MODEL_OPTION_NAME"] = "Vision Relay " + ctx.Model
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = ctx.Model
	return path, writeJSONFile(path, cfg)
}

func readJSONMap(path string) (map[string]any, error) {
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func ensureJSONMap(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	parent[key] = value
	return value
}

func writeJSONFile(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(path, append(b, '\n'))
}

func writeConfigFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		backup := path + ".bak." + time.Now().Format("20060102-150405")
		if err := copyFile(path, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

func persistClientEnv(key string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	if err := exec.Command("setx", relayEnvKey, key).Run(); err != nil {
		return []string{"VISION_RELAY_API_KEY was set for launched clients, but user environment persistence failed."}
	}
	return nil
}

func stopClient(client string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	names := clientProcessNames(client)
	if len(names) == 0 {
		return false
	}
	stopped := false
	for _, name := range names {
		if err := exec.Command("taskkill", "/F", "/T", "/IM", name).Run(); err == nil {
			stopped = true
		}
	}
	return stopped
}

func waitForClientStopped(client string, timeout time.Duration) {
	names := clientProcessNames(client)
	if len(names) == 0 {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running := false
		for _, name := range names {
			if windowsProcessRunning(name) {
				running = true
				break
			}
		}
		if !running {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func windowsProcessRunning(imageName string) bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+imageName, "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(imageName))
}

func clientProcessName(client string) string {
	names := clientProcessNames(client)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func clientProcessNames(client string) []string {
	switch client {
	case clientCodex:
		// The desktop shell is ChatGPT.exe in current Windows packages. Do not
		// target codex.exe: it is the in-app server, and killing it leaves the
		// desktop shell alive on its "code-mode host closed" error page.
		return []string{"ChatGPT.exe"}
	case clientOpenCode:
		return []string{"opencode.exe"}
	case clientClaudeCode:
		return []string{"claude.exe"}
	default:
		return nil
	}
}

func startClient(client, workDir string, ctx clientConfigContext) (bool, string, []string) {
	command := clientCommand(client)
	if command == "" {
		return false, "", []string{"Unsupported client."}
	}
	if _, err := exec.LookPath(command); err != nil {
		if client != clientCodex || !codexLauncherAvailable(ctx) {
			return false, command, []string{command + " was not found in PATH. Config was written, but the client was not started."}
		}
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = ctx.HomeDir
	}
	if runtime.GOOS == "windows" {
		if client == clientCodex {
			if appID := strings.TrimSpace(ctx.LaunchAppID); appID != "" {
				cmd := exec.Command("explorer.exe", codexAppsFolderTarget(appID))
				cmd.Dir = workDir
				if err := cmd.Start(); err != nil {
					return false, "ChatGPT", []string{err.Error()}
				}
				return true, "ChatGPT", nil
			}
			if desktopPath, ok := codexDesktopPath(command, ctx.LaunchPath); ok {
				cmd := exec.Command(desktopPath)
				cmd.Dir = workDir
				cmd.Env = clientEnv(client, ctx)
				if err := cmd.Start(); err != nil {
					return false, filepath.Base(desktopPath), []string{err.Error()}
				}
				return true, filepath.Base(desktopPath), nil
			}
		}
		cmd := exec.Command("cmd", "/c", "start", "Vision Relay "+command, "cmd", "/k", command)
		cmd.Dir = workDir
		cmd.Env = clientEnv(client, ctx)
		if err := cmd.Start(); err != nil {
			return false, command, []string{err.Error()}
		}
		return true, command, nil
	}
	cmd := exec.Command(command)
	cmd.Dir = workDir
	cmd.Env = clientEnv(client, ctx)
	if err := cmd.Start(); err != nil {
		return false, command, []string{err.Error()}
	}
	return true, command, nil
}

func codexLauncherAvailable(ctx clientConfigContext) bool {
	return strings.TrimSpace(ctx.LaunchPath) != "" || strings.TrimSpace(ctx.LaunchAppID) != ""
}

func codexAppsFolderTarget(appID string) string {
	return `shell:AppsFolder\` + strings.TrimSpace(appID)
}

func codexDesktopPath(command, preferred string) (string, bool) {
	if path := strings.TrimSpace(preferred); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	if path, err := exec.LookPath(command); err == nil {
		appDir := filepath.Dir(filepath.Dir(path))
		for _, name := range []string{"ChatGPT.exe", "Codex.exe"} {
			candidate := filepath.Join(appDir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, true
			}
		}
	}
	if path := currentCodexDesktopPath(); path != "" {
		return path, true
	}
	return "", false
}

func currentCodexDesktopPath() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	command := "(Get-Process -Name ChatGPT,Codex -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '*\\ChatGPT.exe' -or $_.Path -like '*\\Codex.exe' } | Select-Object -First 1 -ExpandProperty Path)"
	out, err := exec.Command("powershell", "-NoProfile", "-Command", command).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func currentCodexAppID() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	command := "(Get-StartApps | Where-Object { $_.AppID -like 'OpenAI.Codex_*!App' } | Select-Object -First 1 -ExpandProperty AppID)"
	out, err := exec.Command("powershell", "-NoProfile", "-Command", command).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func clientCommand(client string) string {
	switch client {
	case clientCodex:
		return "codex"
	case clientOpenCode:
		return "opencode"
	case clientClaudeCode:
		return "claude"
	default:
		return ""
	}
}

func clientEnv(client string, ctx clientConfigContext) []string {
	env := os.Environ()
	env = append(env, relayEnvKey+"="+ctx.Key)
	if client == clientCodex {
		return env
	}
	env = append(env, "OPENAI_API_KEY="+ctx.Key)
	env = append(env, "ANTHROPIC_BASE_URL="+strings.TrimRight(ctx.Origin, "/"))
	env = append(env, "ANTHROPIC_AUTH_TOKEN="+ctx.Key)
	env = append(env, "ANTHROPIC_CUSTOM_MODEL_OPTION="+ctx.Model)
	env = append(env, "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME=Vision Relay "+ctx.Model)
	env = append(env, "ANTHROPIC_DEFAULT_SONNET_MODEL="+ctx.Model)
	return env
}
