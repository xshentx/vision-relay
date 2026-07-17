package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/titanous/json5"
)

const (
	clientCodex               = "codex"
	clientOpenCode            = "opencode"
	clientClaudeCode          = "claude-code"
	clientOpenClaw            = "openclaw"
	relayProviderID           = "vision-relay"
	codexProviderID           = "custom"
	codexBaseInstructions     = "You are Codex, a coding agent. You and the user share the same workspace and collaborate to achieve the user's goals."
	codexUnifiedHistoryMarker = "# Added by Vision Relay for unified Codex history."
)

type clientConfigContext struct {
	HomeDir              string
	ProjectDir           string
	ConfigPath           string
	Origin               string
	Key                  string
	Provider             string
	WireAPI              string
	DirectUpstream       bool
	Model                string
	ModelMappings        []textModelMapping
	VisionEnabled        bool
	PreserveOfficialAuth *bool
}

type clientConfigRequest struct {
	Client  string `json:"client"`
	WorkDir string `json:"work_dir"`
}

type clientRouteResult struct {
	Client         string `json:"client"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	ProjectDir     string `json:"project_dir,omitempty"`
	DirectUpstream bool   `json:"direct_upstream"`
	Provider       string `json:"provider,omitempty"`
}

type clientRouteValidationError struct {
	message string
}

func (e *clientRouteValidationError) Error() string {
	return e.message
}

func newClientRouteValidationError(message string) error {
	return &clientRouteValidationError{message: message}
}

var clientRouteOrder = []string{clientCodex, clientOpenCode, clientClaudeCode, clientOpenClaw}

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
	result, err := a.configureClientRoute(client, req.WorkDir, requestOrigin(r, a.currentConfig()), home)
	if err != nil {
		status := http.StatusInternalServerError
		var validationErr *clientRouteValidationError
		if errors.As(err, &validationErr) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	if err := a.setClientRouteEnabled(client, true); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	cfg := a.currentConfig()
	autoRestart := normalizeClientBehavior(cfg.ClientAutoRestart, true)[client]
	autoStart := normalizeClientBehavior(cfg.ClientAutoStart, false)[client]
	programResult := applyClientProgramBehavior(
		a.configuredProgramController(),
		client,
		configuredClientProgramPath(cfg, client, home),
		result.ProjectDir,
		autoRestart,
		autoStart,
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"client":           result.Client,
		"path":             result.Path,
		"project_dir":      result.ProjectDir,
		"direct_upstream":  result.DirectUpstream,
		"provider":         result.Provider,
		"route_enabled":    true,
		"restart_required": programResult.RestartRequired,
		"builtin":          true,
		"program_path":     programResult.ProgramPath,
		"was_running":      programResult.WasRunning,
		"auto_restart":     programResult.AutoRestart,
		"auto_start":       programResult.AutoStart,
		"stopped":          programResult.Stopped,
		"started":          programResult.Started,
		"restarted":        programResult.Restarted,
		"program_action":   programResult.Action,
		"program_warning":  programResult.Warning,
	})
}

func (a *app) handleClientRoutesApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	cfg := a.currentConfig()
	routes := normalizeClientRouteEnabled(cfg.ClientRouteEnabled)
	origin := requestOrigin(r, cfg)
	results := make([]clientRouteResult, 0, len(clientRouteOrder))
	applyErrors := make([]string, 0)
	for _, client := range clientRouteOrder {
		if !routes[client] {
			continue
		}
		result, configureErr := a.configureClientRoute(client, "", origin, home)
		if configureErr != nil {
			applyErrors = append(applyErrors, clientKeyName(client)+": "+configureErr.Error())
			continue
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               len(applyErrors) == 0,
		"clients":          results,
		"errors":           applyErrors,
		"restart_required": len(results) > 0,
	})
}

func (a *app) configureClientRoute(client, workDir, origin, home string) (clientRouteResult, error) {
	cfg := a.currentConfig()
	directUpstream := !localAPIEnabled(cfg)
	key := ""
	modelMappings := textModelMappings(cfg)
	model := relayModelName(cfg)
	visionAvailable := false
	if !directUpstream {
		origin = strings.TrimSpace(origin)
		visionAvailable = visionEnabled(cfg)
	} else {
		key = strings.TrimSpace(cfg.TextAPIKey)
		modelMappings = directClientTextModelMappings(cfg)
		if err := validateDirectClientRoute(client, cfg, modelMappings); err != nil {
			return clientRouteResult{}, err
		}
		model = strings.TrimSpace(modelMappings[0].Model)
		origin = strings.TrimSpace(cfg.TextBaseURL)
		if origin == "" {
			origin = defaultBaseURL(cfg.TextProvider)
		}
	}
	projectDir := clientProjectDir(client, workDir, home)
	ctx := clientConfigContext{
		HomeDir:              home,
		ProjectDir:           projectDir,
		ConfigPath:           configuredClientConfigPath(cfg, client, home),
		Origin:               origin,
		Key:                  key,
		Provider:             strings.TrimSpace(cfg.TextProvider),
		WireAPI:              strings.TrimSpace(cfg.TextWireAPI),
		DirectUpstream:       directUpstream,
		Model:                model,
		ModelMappings:        modelMappings,
		VisionEnabled:        visionAvailable,
		PreserveOfficialAuth: boolPtr(preserveCodexOfficialAuth(cfg)),
	}
	path, err := writeClientConfig(client, ctx)
	if err != nil {
		return clientRouteResult{}, err
	}
	return clientRouteResult{
		Client:         client,
		Name:           clientKeyName(client),
		Path:           path,
		ProjectDir:     projectDir,
		DirectUpstream: directUpstream,
		Provider:       strings.TrimSpace(cfg.TextProvider),
	}, nil
}

func validateDirectClientRoute(client string, cfg config, mappings []textModelMapping) error {
	if len(mappings) == 0 {
		return newClientRouteValidationError("关闭本地 API 后，请先为当前文本供应商添加至少一个模型")
	}
	provider := normalizeProvider(cfg.TextProvider)
	switch client {
	case clientCodex:
		if provider != "openai" || normalizeWireAPI(cfg.TextWireAPI) != "responses" {
			return newClientRouteValidationError("关闭本地 API 后，Codex 仅支持直连使用 Responses 协议的 OpenAI 兼容供应商")
		}
	case clientClaudeCode:
		if provider != "anthropic" {
			return newClientRouteValidationError("关闭本地 API 后，Claude Code 仅支持直连 Anthropic 协议供应商")
		}
	}
	return nil
}

// directClientTextModelMappings removes relay-only aliases. Without the local
// API there is no model-mapping layer, so clients must send the upstream model
// ID exactly as configured by the active supplier.
func directClientTextModelMappings(cfg config) []textModelMapping {
	mappings := textModelMappings(cfg)
	out := make([]textModelMapping, 0, len(mappings))
	seen := map[string]bool{}
	for _, mapping := range mappings {
		model := strings.TrimSpace(mapping.Model)
		if model == "" {
			model = strings.TrimSpace(mapping.Name)
		}
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		mapping.Name = model
		mapping.Model = model
		out = append(out, mapping)
	}
	return out
}

func (a *app) setClientRouteEnabled(client string, enabled bool) error {
	client = normalizeClientID(client)
	if client == "" {
		return errors.New("unsupported client")
	}
	cfg := a.currentConfig()
	routes := normalizeClientRouteEnabled(cfg.ClientRouteEnabled)
	routes[client] = enabled
	cfg.ClientRouteEnabled = routes
	return a.setConfig(cfg)
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
	cfg := a.currentConfig()
	projectDir := clientProjectDir(client, req.WorkDir, home)
	configPath := configuredClientConfigPath(cfg, client, home)
	path, err := restoreCodexAccountConfigAtPath(home, configPath, projectDir, cfg.UnifyCodexSessionHistory)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.setClientRouteEnabled(client, false); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"client":        client,
		"path":          path,
		"project_dir":   projectDir,
		"route_enabled": false,
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
	case "openclaw", "open-claw":
		return clientOpenClaw
	default:
		return ""
	}
}

func clientKeyName(client string) string {
	switch client {
	case clientCodex:
		return "Codex"
	case clientOpenCode:
		return "OpenCode"
	case clientClaudeCode:
		return "Claude Code"
	case clientOpenClaw:
		return "OpenClaw"
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

func clientVersionedBaseURL(ctx clientConfigContext) string {
	baseURL := strings.TrimRight(strings.TrimSpace(ctx.Origin), "/")
	if baseURL == "" {
		return ""
	}
	if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/v1beta") {
		return baseURL
	}
	if ctx.DirectUpstream && strings.EqualFold(strings.TrimSpace(ctx.Provider), "gemini") {
		return baseURL + "/v1beta"
	}
	return baseURL + "/v1"
}

func clientProviderDisplayName(ctx clientConfigContext) string {
	if ctx.DirectUpstream {
		if provider := strings.TrimSpace(ctx.Provider); provider != "" {
			return provider + " (direct)"
		}
		return "Upstream supplier (direct)"
	}
	return "Vision Relay"
}

func openCodeProviderNPM(ctx clientConfigContext) string {
	if !ctx.DirectUpstream {
		return "@ai-sdk/openai-compatible"
	}
	switch strings.ToLower(strings.TrimSpace(ctx.Provider)) {
	case "anthropic":
		return "@ai-sdk/anthropic"
	case "gemini":
		return "@ai-sdk/google"
	default:
		return "@ai-sdk/openai-compatible"
	}
}

func openClawProviderAPI(ctx clientConfigContext) string {
	if !ctx.DirectUpstream {
		return "openai-completions"
	}
	switch strings.ToLower(strings.TrimSpace(ctx.Provider)) {
	case "anthropic":
		return "anthropic-messages"
	case "gemini":
		return "google-generative-ai"
	default:
		if strings.EqualFold(strings.TrimSpace(ctx.WireAPI), "responses") {
			return "openai-responses"
		}
		return "openai-completions"
	}
}

func openClawProviderBaseURL(ctx clientConfigContext) string {
	provider := strings.ToLower(strings.TrimSpace(ctx.Provider))
	if ctx.DirectUpstream && (provider == "anthropic" || provider == "gemini") {
		return strings.TrimRight(strings.TrimSpace(ctx.Origin), "/")
	}
	return clientVersionedBaseURL(ctx)
}

func writeClientConfig(client string, ctx clientConfigContext) (string, error) {
	switch client {
	case clientCodex:
		return writeCodexConfig(ctx)
	case clientOpenCode:
		return writeOpenCodeConfig(ctx)
	case clientClaudeCode:
		return writeClaudeCodeConfig(ctx)
	case clientOpenClaw:
		return writeOpenClawConfig(ctx)
	default:
		return "", errors.New("unsupported client")
	}
}

func writeCodexConfig(ctx clientConfigContext) (string, error) {
	userPath := strings.TrimSpace(ctx.ConfigPath)
	if userPath == "" {
		userPath = defaultClientConfigPath(clientCodex, ctx.HomeDir)
	}
	configDir := filepath.Dir(userPath)
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
	}
	if effort := codexDefaultModelReasoningEffort(ctx); effort != "none" {
		block = append(block, fmt.Sprintf("model_reasoning_effort = %q", effort))
	}
	block = append(block,
		`web_search = "disabled"`,
		"",
		"[model_providers."+codexProviderID+"]",
		fmt.Sprintf("name = %q", clientProviderDisplayName(ctx)),
		`wire_api = "responses"`,
		fmt.Sprintf("requires_openai_auth = %t", ctx.DirectUpstream),
		fmt.Sprintf("base_url = %q", clientVersionedBaseURL(ctx)),
	)
	if ctx.DirectUpstream && preserveCodexOfficialAuthForContext(ctx) {
		block = append(block, fmt.Sprintf("experimental_bearer_token = %q", ctx.Key))
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

func codexDefaultModelReasoningEffort(ctx clientConfigContext) string {
	mappings := normalizeTextModelMappings(ctx.ModelMappings, nil, ctx.Model)
	if len(mappings) == 0 {
		return "none"
	}
	return textModelReasoningEffort(mappings[0])
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
	return restoreCodexAccountConfigAtPath(homeDir, defaultClientConfigPath(clientCodex, homeDir), projectDir, unifySessionHistory)
}

func restoreCodexAccountConfigAtPath(homeDir, userPath, projectDir string, unifySessionHistory bool) (string, error) {
	if strings.TrimSpace(userPath) == "" {
		userPath = defaultClientConfigPath(clientCodex, homeDir)
	}
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
	if !ctx.DirectUpstream {
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
	reasoningEffort := textModelReasoningEffort(mapping)
	supportsReasoning := reasoningEffort != "none"
	if supportsReasoning {
		model["default_reasoning_level"] = reasoningEffort
		model["supported_reasoning_levels"] = codexReasoningLevels(reasoningEffort)
	} else {
		delete(model, "default_reasoning_level")
		model["supported_reasoning_levels"] = []any{}
	}
	model["visibility"] = "list"
	model["supported_in_api"] = true
	model["priority"] = priority
	imageEnabled := clientMappingSupportsImages(ctx, mapping)
	model["input_modalities"] = relayInputModalities(imageEnabled)
	model["context_window"] = contextWindow
	model["max_context_window"] = contextWindow
	model["effective_context_window_percent"] = 95
	model["additional_speed_tiers"] = []any{}
	model["service_tiers"] = []any{}
	model["availability_nux"] = nil
	model["upgrade"] = nil
	// Keep the legacy field for older Codex desktop builds and write the
	// current field used by newer Codex model catalogs as well.
	model["supports_reasoning_summaries"] = supportsReasoning
	model["supports_reasoning_summary_parameter"] = supportsReasoning
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

func codexReasoningLevels(effort string) []any {
	description := map[string]string{
		"low":    "Low reasoning",
		"medium": "Medium reasoning",
		"high":   "High reasoning",
		"xhigh":  "Extra high reasoning",
	}[effort]
	if description == "" {
		description = "Enable reasoning"
	}
	return []any{
		map[string]any{"effort": "none", "description": "Disable reasoning"},
		map[string]any{"effort": effort, "description": description},
	}
}

func writeOpenCodeConfig(ctx clientConfigContext) (string, error) {
	path := strings.TrimSpace(ctx.ConfigPath)
	if path == "" {
		path = defaultClientConfigPath(clientOpenCode, ctx.HomeDir)
	}
	cfg, err := readJSONMap(path)
	if err != nil {
		return "", err
	}
	cfg["$schema"] = "https://opencode.ai/config.json"
	providers := ensureJSONMap(cfg, "provider")
	mappings := clientTextModelMappings(ctx)
	configuredModels := make(map[string]any, len(mappings))
	for _, mapping := range mappings {
		modelID := clientTextModelID(mapping)
		imageEnabled := clientMappingSupportsImages(ctx, mapping)
		model := map[string]any{
			"name":              modelID,
			"reasoning":         textModelSupportsReasoning(mapping),
			"attachment":        imageEnabled,
			"attachments":       imageEnabled,
			"vision":            imageEnabled,
			"input_modalities":  relayInputModalities(imageEnabled),
			"output_modalities": []string{"text"},
			"modalities": map[string]any{
				"input":  relayInputModalities(imageEnabled),
				"output": []string{"text"},
			},
		}
		if mapping.ContextWindow > 0 {
			model["limit"] = map[string]any{"context": int(mapping.ContextWindow)}
		}
		configuredModels[modelID] = model
	}
	// The Vision Relay provider is fully regenerated on every one-click setup.
	// Preserving its previous model map leaves stale models selectable after the
	// active upstream profile changes, so only unrelated providers are retained.
	options := map[string]any{
		"baseURL": clientVersionedBaseURL(ctx),
	}
	if ctx.DirectUpstream {
		options["apiKey"] = ctx.Key
	}
	providers[relayProviderID] = map[string]any{
		"npm":     openCodeProviderNPM(ctx),
		"name":    clientProviderDisplayName(ctx),
		"options": options,
		"models":  configuredModels,
	}
	cfg["model"] = relayProviderID + "/" + clientTextModelID(mappings[0])
	return path, writeJSONFile(path, cfg)
}

func clientMappingSupportsImages(ctx clientConfigContext, mapping textModelMapping) bool {
	if ctx.DirectUpstream {
		return mapping.SupportsImages
	}
	return mapping.SupportsImages || ctx.VisionEnabled
}

func relayInputModalities(enabled bool) []string {
	if enabled {
		return []string{"text", "image"}
	}
	return []string{"text"}
}

func writeClaudeCodeConfig(ctx clientConfigContext) (string, error) {
	path := strings.TrimSpace(ctx.ConfigPath)
	if path == "" {
		path = defaultClientConfigPath(clientClaudeCode, ctx.HomeDir)
	}
	cfg, err := readJSONMap(path)
	if err != nil {
		return "", err
	}
	cfg["$schema"] = "https://json.schemastore.org/claude-code-settings.json"
	mappings := clientTextModelMappings(ctx)
	modelIDs := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		modelIDs = append(modelIDs, clientTextModelID(mapping))
	}
	cfg["model"] = modelIDs[0]
	// Current Claude Code releases surface arbitrary IDs listed here in /model.
	// Older releases still support direct `/model <id>` selection, while the
	// custom/family slots below keep the first four routes visible in the picker.
	cfg["availableModels"] = modelIDs
	env := ensureJSONMap(cfg, "env")
	env["ANTHROPIC_BASE_URL"] = strings.TrimRight(ctx.Origin, "/")
	if ctx.DirectUpstream {
		env["ANTHROPIC_AUTH_TOKEN"] = ctx.Key
	} else {
		delete(env, "ANTHROPIC_AUTH_TOKEN")
	}
	configureClaudeCodeModelSlots(env, modelIDs)
	return path, writeJSONFile(path, cfg)
}

func configureClaudeCodeModelSlots(env map[string]any, modelIDs []string) {
	slots := []struct {
		model string
		name  string
	}{
		{"ANTHROPIC_CUSTOM_MODEL_OPTION", "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME"},
		{"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL_NAME"},
		{"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL_NAME"},
		{"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME"},
	}
	for index, slot := range slots {
		if index >= len(modelIDs) {
			delete(env, slot.model)
			delete(env, slot.name)
			continue
		}
		env[slot.model] = modelIDs[index]
		env[slot.name] = "Vision Relay " + modelIDs[index]
	}
}

func clientTextModelMappings(ctx clientConfigContext) []textModelMapping {
	mappings := normalizeTextModelMappings(ctx.ModelMappings, nil, ctx.Model)
	if len(mappings) == 0 {
		mappings = []textModelMapping{{Name: "z-ai/glm-5.2", Model: "z-ai/glm-5.2"}}
	}
	return mappings
}

func clientTextModelID(mapping textModelMapping) string {
	if modelID := strings.TrimSpace(mapping.Name); modelID != "" {
		return modelID
	}
	return strings.TrimSpace(mapping.Model)
}

func writeOpenClawConfig(ctx clientConfigContext) (string, error) {
	path := strings.TrimSpace(ctx.ConfigPath)
	if path == "" {
		path = defaultClientConfigPath(clientOpenClaw, ctx.HomeDir)
	}
	cfg, err := readJSON5Map(path)
	if err != nil {
		return "", err
	}

	mappings := clientTextModelMappings(ctx)
	primaryModel := strings.TrimSpace(mappings[0].Name)
	if primaryModel == "" {
		primaryModel = strings.TrimSpace(mappings[0].Model)
	}

	agents := ensureJSONMap(cfg, "agents")
	defaults := ensureJSONMap(agents, "defaults")
	modelSelection := ensureJSONMap(defaults, "model")
	modelSelection["primary"] = relayProviderID + "/" + primaryModel
	if allowedModels, ok := defaults["models"].(map[string]any); ok {
		for ref := range allowedModels {
			if strings.HasPrefix(ref, relayProviderID+"/") {
				delete(allowedModels, ref)
			}
		}
		for _, mapping := range mappings {
			modelID := strings.TrimSpace(mapping.Name)
			if modelID == "" {
				modelID = strings.TrimSpace(mapping.Model)
			}
			ref := relayProviderID + "/" + modelID
			if _, exists := allowedModels[ref]; !exists {
				allowedModels[ref] = map[string]any{}
			}
		}
	}

	models := ensureJSONMap(cfg, "models")
	if _, exists := models["mode"]; !exists {
		models["mode"] = "merge"
	}
	providers := ensureJSONMap(models, "providers")
	provider := map[string]any{
		"baseUrl": openClawProviderBaseURL(ctx),
		"api":     openClawProviderAPI(ctx),
		"models":  openClawModels(ctx, mappings),
	}
	if ctx.DirectUpstream {
		provider["apiKey"] = ctx.Key
	}
	providers[relayProviderID] = provider
	return path, writeJSONFile(path, cfg)
}

func openClawModels(ctx clientConfigContext, mappings []textModelMapping) []any {
	items := make([]any, 0, len(mappings))
	for _, mapping := range mappings {
		modelID := strings.TrimSpace(mapping.Name)
		if modelID == "" {
			modelID = strings.TrimSpace(mapping.Model)
		}
		contextWindow := int(mapping.ContextWindow)
		if contextWindow <= 0 {
			contextWindow = 128000
		}
		items = append(items, map[string]any{
			"id":            modelID,
			"name":          modelID,
			"input":         relayInputModalities(clientMappingSupportsImages(ctx, mapping)),
			"cost":          map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
			"contextWindow": contextWindow,
			"maxTokens":     8192,
		})
	}
	return items
}

func openClawConfigPath(homeDir string) string {
	effectiveHome := strings.TrimSpace(os.Getenv("OPENCLAW_HOME"))
	if effectiveHome == "" {
		effectiveHome = homeDir
	} else {
		effectiveHome = resolveClientPath(effectiveHome, homeDir)
	}
	if override := strings.TrimSpace(os.Getenv("OPENCLAW_CONFIG_PATH")); override != "" {
		return resolveClientPath(override, effectiveHome)
	}

	stateDir := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR"))
	if stateDir != "" {
		stateDir = resolveClientPath(stateDir, effectiveHome)
	} else {
		stateDir = filepath.Join(effectiveHome, ".openclaw")
		if _, err := os.Stat(stateDir); errors.Is(err, os.ErrNotExist) {
			legacyDir := filepath.Join(effectiveHome, ".clawdbot")
			if _, legacyErr := os.Stat(legacyDir); legacyErr == nil {
				stateDir = legacyDir
			}
		}
	}
	for _, name := range []string{"openclaw.json", "clawdbot.json"} {
		candidate := filepath.Join(stateDir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(stateDir, "openclaw.json")
}

func resolveClientPath(value, homeDir string) string {
	value = os.ExpandEnv(strings.TrimSpace(value))
	if value == "~" {
		value = homeDir
	} else if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		value = filepath.Join(homeDir, value[2:])
	}
	if abs, err := filepath.Abs(value); err == nil {
		return abs
	}
	return filepath.Clean(value)
}

func readJSON5Map(path string) (map[string]any, error) {
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
	if err := json5.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse OpenClaw config %s: %w", path, err)
	}
	return cfg, nil
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
