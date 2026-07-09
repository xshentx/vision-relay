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
	clientCodex      = "codex"
	clientOpenCode   = "opencode"
	clientClaudeCode = "claude-code"
	relayProviderID  = "vision-relay"
	relayEnvKey      = "VISION_RELAY_API_KEY"
)

type clientConfigContext struct {
	HomeDir       string
	ProjectDir    string
	Origin        string
	Key           string
	Model         string
	VisionEnabled bool
	LaunchPath    string
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
	userPath := filepath.Join(ctx.HomeDir, ".codex", "config.toml")
	if err := saveCodexAccountConfigBackup(ctx.HomeDir, userPath); err != nil {
		return "", err
	}
	globalCatalogPath, err := writeCodexModelCatalog(ctx, filepath.Join(ctx.HomeDir, ".codex"))
	if err != nil {
		return "", err
	}
	if err := upsertCodexModelCache(ctx); err != nil {
		return "", err
	}
	if err := writeCodexAPIAuth(ctx); err != nil {
		return "", err
	}
	lines := []string{}
	if b, err := os.ReadFile(userPath); err == nil {
		lines = strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	lines = removeCodexRelayConfig(lines)
	block := []string{
		"# Added by Vision Relay. Edit from the Client Access page.",
		fmt.Sprintf("model = %q", ctx.Model),
		"model_catalog_json = " + tomlLiteralString(globalCatalogPath),
		`model_provider = "openai"`,
		fmt.Sprintf("openai_base_url = %q", strings.TrimRight(ctx.Origin, "/")+"/v1"),
		`forced_login_method = "api"`,
		`cli_auth_credentials_store = "file"`,
	}
	block = append(block, "")
	content := strings.TrimRight(strings.Join(append(block, lines...), "\n"), "\n") + "\n"
	if err := writeConfigFile(userPath, []byte(content)); err != nil {
		return "", err
	}
	projectPath, err := writeCodexProjectConfig(ctx)
	if err != nil {
		return "", err
	}
	return projectPath, nil
}

func writeCodexProjectConfig(ctx clientConfigContext) (string, error) {
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	if projectDir == "" {
		projectDir = ctx.HomeDir
	}
	catalogPath, err := writeCodexModelCatalog(ctx, filepath.Join(projectDir, ".codex"))
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
	block := []string{
		"# Added by Vision Relay. Edit from the Client Access page.",
		fmt.Sprintf("model = %q", ctx.Model),
		"model_catalog_json = " + tomlLiteralString(catalogPath),
		`model_provider = "openai"`,
		fmt.Sprintf("openai_base_url = %q", strings.TrimRight(ctx.Origin, "/")+"/v1"),
		"",
	}
	content := strings.TrimRight(strings.Join(append(block, lines...), "\n"), "\n") + "\n"
	return path, writeConfigFile(path, []byte(content))
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
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
			section := strings.Trim(trimmed, "[]")
			skipSection = section == "model_providers."+relayProviderID || strings.HasPrefix(section, "model_providers."+relayProviderID+".")
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
			case strings.HasPrefix(trimmed, "openai_base_url ="):
				continue
			case strings.HasPrefix(trimmed, "forced_login_method ="):
				continue
			case strings.HasPrefix(trimmed, "cli_auth_credentials_store ="):
				continue
			case strings.HasPrefix(trimmed, "# Added by Vision Relay."):
				skipGeneratedBlock = true
				continue
			case strings.HasPrefix(trimmed, "# Restored by Vision Relay."):
				skipGeneratedBlock = true
				continue
			case strings.HasPrefix(trimmed, "# Vision Relay forwards requests to the configured upstream text model:"):
				continue
			}
		}
		out = append(out, line)
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
	userPath := filepath.Join(homeDir, ".codex", "config.toml")
	lines := []string{}
	if raw, err := os.ReadFile(userPath); err == nil {
		lines = strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	lines = removeCodexRelayConfig(lines)
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
	content := strings.TrimRight(strings.Join(append(append(accountBlock, ""), lines...), "\n"), "\n") + "\n"
	if err := writeConfigFile(userPath, []byte(content)); err != nil {
		return "", err
	}
	if err := restoreCodexProjectConfig(projectDir); err != nil {
		return "", err
	}
	return userPath, nil
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
		filepath.Join(homeDir, ".codex", "账号", "config.toml"),
		filepath.Join(homeDir, ".codex", "config", "config.toml"),
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
	path := filepath.Join(homeDir, ".codex", "models_cache.json")
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
	return filepath.Join(homeDir, ".codex", "vision-relay-account-config.toml")
}

func codexAuthBackupPath(homeDir string) string {
	return filepath.Join(homeDir, ".codex", "vision-relay-auth.json")
}

func writeCodexAPIAuth(ctx clientConfigContext) error {
	authPath := filepath.Join(ctx.HomeDir, ".codex", "auth.json")
	if raw, err := os.ReadFile(authPath); err == nil {
		if !codexAuthIsRelayAPI(raw, ctx.Key) {
			if err := writeConfigFile(codexAuthBackupPath(ctx.HomeDir), raw); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	auth := map[string]any{
		"OPENAI_API_KEY": ctx.Key,
		"tokens":         nil,
		"last_refresh":   time.Now().UTC().Format(time.RFC3339Nano),
	}
	return writeJSONFile(authPath, auth)
}

func codexAuthIsRelayAPI(raw []byte, key string) bool {
	var auth map[string]any
	if err := json.Unmarshal(raw, &auth); err != nil {
		return false
	}
	got, _ := auth["OPENAI_API_KEY"].(string)
	return got != "" && got == key
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
	return writeConfigFile(filepath.Join(homeDir, ".codex", "auth.json"), raw)
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
	providerSection := extractTomlSection(lines, "model_providers."+providerID)
	if len(providerSection) > 0 {
		root = append(root, "")
		root = append(root, providerSection...)
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
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
			section := strings.Trim(trimmed, "[]")
			skipSection = section == "model_providers."+relayProviderID || strings.HasPrefix(section, "model_providers."+relayProviderID+".")
		}
		if skipSection {
			continue
		}
		if inRoot {
			switch {
			case strings.HasPrefix(trimmed, "# Added by Vision Relay."):
				skipGeneratedBlock = true
				continue
			case strings.HasPrefix(trimmed, "model_catalog_json =") && isVisionRelayCatalogLine(trimmed):
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
	return filepath.Base(value) == "vision-relay-model-catalog.json"
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
	path := filepath.Join(dir, "vision-relay-model-catalog.json")
	catalog := map[string]any{
		"models": []any{
			map[string]any{
				"slug":                             ctx.Model,
				"display_name":                     ctx.Model,
				"description":                      "Current Vision Relay upstream text model.",
				"default_reasoning_level":          "high",
				"supported_reasoning_levels":       codexReasoningLevels(),
				"shell_type":                       "shell_command",
				"visibility":                       "list",
				"supported_in_api":                 true,
				"priority":                         100,
				"additional_speed_tiers":           []any{},
				"service_tiers":                    []any{},
				"availability_nux":                 nil,
				"upgrade":                          nil,
				"base_instructions":                "",
				"supports_reasoning_summaries":     false,
				"default_reasoning_summary":        "none",
				"support_verbosity":                true,
				"default_verbosity":                "low",
				"apply_patch_tool_type":            "freeform",
				"web_search_tool_type":             "text_and_image",
				"truncation_policy":                map[string]any{"mode": "tokens", "limit": 10000},
				"supports_parallel_tool_calls":     true,
				"supports_image_detail_original":   true,
				"context_window":                   128000,
				"max_context_window":               128000,
				"effective_context_window_percent": 95,
				"experimental_supported_tools":     []any{},
				"input_modalities":                 relayInputModalities(ctx.VisionEnabled),
				"supports_search_tool":             true,
				"use_responses_lite":               false,
			},
		},
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

func upsertCodexModelCache(ctx clientConfigContext) error {
	path := filepath.Join(ctx.HomeDir, ".codex", "models_cache.json")
	cache := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &cache); err != nil {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	models, _ := cache["models"].([]any)
	entry := codexModelCacheEntry(ctx, firstCodexModelTemplate(models))
	out := make([]any, 0, len(models)+1)
	out = append(out, entry)
	for _, item := range models {
		model, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if modelString(model, "slug") == ctx.Model || isVisionRelayCacheModel(model) {
			continue
		}
		out = append(out, item)
	}
	cache["models"] = out
	if _, ok := cache["fetched_at"]; !ok {
		cache["fetched_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if _, ok := cache["client_version"]; !ok {
		cache["client_version"] = "vision-relay"
	}
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigFile(path, append(b, '\n'))
}

func removeCodexModelCache(homeDir string) error {
	path := filepath.Join(homeDir, ".codex", "models_cache.json")
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

func firstCodexModelTemplate(models []any) map[string]any {
	for _, item := range models {
		model, ok := item.(map[string]any)
		if !ok || modelString(model, "slug") == "codex-auto-review" {
			continue
		}
		return cloneStringAnyMap(model)
	}
	return map[string]any{}
}

func codexModelCacheEntry(ctx clientConfigContext, template map[string]any) map[string]any {
	model := cloneStringAnyMap(template)
	model["slug"] = ctx.Model
	model["display_name"] = ctx.Model
	model["description"] = "Current Vision Relay upstream text model."
	model["default_reasoning_level"] = "high"
	model["supported_reasoning_levels"] = codexReasoningLevels()
	model["visibility"] = "list"
	model["supported_in_api"] = true
	model["priority"] = 100
	model["input_modalities"] = relayInputModalities(ctx.VisionEnabled)
	model["context_window"] = 128000
	model["max_context_window"] = 128000
	model["effective_context_window_percent"] = 95
	model["additional_speed_tiers"] = []any{}
	model["service_tiers"] = []any{}
	model["availability_nux"] = nil
	model["upgrade"] = nil
	model["supports_reasoning_summaries"] = false
	model["default_reasoning_summary"] = "none"
	model["support_verbosity"] = true
	model["default_verbosity"] = "low"
	model["apply_patch_tool_type"] = "freeform"
	model["web_search_tool_type"] = "text_and_image"
	model["truncation_policy"] = map[string]any{"mode": "tokens", "limit": 10000}
	model["supports_parallel_tool_calls"] = true
	model["supports_image_detail_original"] = true
	model["supports_search_tool"] = true
	model["use_responses_lite"] = false
	return model
}

func isVisionRelayCacheModel(model map[string]any) bool {
	return modelString(model, "description") == "Current Vision Relay upstream text model."
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
		map[string]any{"effort": "low", "description": "Low reasoning"},
		map[string]any{"effort": "medium", "description": "Medium reasoning"},
		map[string]any{"effort": "high", "description": "High reasoning"},
		map[string]any{"effort": "xhigh", "description": "Extra high reasoning"},
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
	model["name"] = ctx.Model
	model["attachment"] = ctx.VisionEnabled
	model["attachments"] = ctx.VisionEnabled
	model["vision"] = ctx.VisionEnabled
	model["input_modalities"] = relayInputModalities(ctx.VisionEnabled)
	model["output_modalities"] = []string{"text"}
	model["modalities"] = map[string]any{
		"input":  relayInputModalities(ctx.VisionEnabled),
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
		return []string{"Codex.exe", "codex.exe"}
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
		if client != clientCodex || strings.TrimSpace(ctx.LaunchPath) == "" {
			return false, command, []string{command + " was not found in PATH. Config was written, but the client was not started."}
		}
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = clientWorkDir(ctx.ProjectDir, ctx.HomeDir)
	}
	if runtime.GOOS == "windows" {
		if client == clientCodex {
			if desktopPath, ok := codexDesktopPath(command, ctx.LaunchPath); ok {
				cmd := exec.Command(desktopPath)
				cmd.Dir = workDir
				cmd.Env = clientEnv(ctx)
				if err := cmd.Start(); err != nil {
					return false, filepath.Base(desktopPath), []string{err.Error()}
				}
				return true, filepath.Base(desktopPath), nil
			}
		}
		cmd := exec.Command("cmd", "/c", "start", "Vision Relay "+command, "cmd", "/k", command)
		cmd.Dir = workDir
		cmd.Env = clientEnv(ctx)
		if err := cmd.Start(); err != nil {
			return false, command, []string{err.Error()}
		}
		return true, command, nil
	}
	cmd := exec.Command(command)
	cmd.Dir = workDir
	cmd.Env = clientEnv(ctx)
	if err := cmd.Start(); err != nil {
		return false, command, []string{err.Error()}
	}
	return true, command, nil
}

func codexDesktopPath(command, preferred string) (string, bool) {
	if path := strings.TrimSpace(preferred); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	if path, err := exec.LookPath(command); err == nil {
		candidate := filepath.Join(filepath.Dir(filepath.Dir(path)), "Codex.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
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
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "(Get-Process -Name Codex -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '*\\Codex.exe' } | Select-Object -First 1 -ExpandProperty Path)").Output()
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

func clientEnv(ctx clientConfigContext) []string {
	env := os.Environ()
	env = append(env, relayEnvKey+"="+ctx.Key)
	env = append(env, "OPENAI_API_KEY="+ctx.Key)
	env = append(env, "ANTHROPIC_BASE_URL="+strings.TrimRight(ctx.Origin, "/"))
	env = append(env, "ANTHROPIC_AUTH_TOKEN="+ctx.Key)
	env = append(env, "ANTHROPIC_CUSTOM_MODEL_OPTION="+ctx.Model)
	env = append(env, "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME=Vision Relay "+ctx.Model)
	env = append(env, "ANTHROPIC_DEFAULT_SONNET_MODEL="+ctx.Model)
	return env
}
