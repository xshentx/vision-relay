package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	appDisplayName      = "Vision Relay"
	appSlug             = "vision-relay"
	legacyAppSlug       = "codex-proxy"
	defaultAddr         = "127.0.0.1:8787"
	defaultTextProvider = "openai"
	defaultTextWireAPI  = "chat_completions"
	defaultVisionModel  = "gpt-4o-mini"
	defaultVisionPrompt = "你只是图片识别器，不是最终回答模型。只提取图片事实，禁止回答用户需求、禁止写代码、禁止给方案、禁止推理下一步。按图片复杂度输出必要细节，用简洁中文列出：1. 可见文字；2. 主要对象/页面结构；3. 颜色和布局；4. 与用户需求直接相关的细节。"
)

var codexAccountModelAliases = []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini"}

func defaultConfig() config {
	cfg := config{
		Addr:                              envAny(defaultAddr, "VISION_RELAY_ADDR", "CODEX_PROXY_ADDR"),
		TextProvider:                      env("TEXT_PROVIDER", defaultTextProvider),
		TextBaseURL:                       env("TEXT_BASE_URL", "https://api.openai.com"),
		TextAPIKey:                        env("TEXT_API_KEY", ""),
		TextModelOverride:                 env("TEXT_MODEL_OVERRIDE", ""),
		TextWireAPI:                       env("TEXT_WIRE_API", defaultTextWireAPI),
		ProxyURL:                          env("PROXY_URL", ""),
		VisionProvider:                    env("VISION_PROVIDER", "openai"),
		VisionBaseURL:                     env("VISION_BASE_URL", "https://api.openai.com"),
		VisionAPIKey:                      env("VISION_API_KEY", ""),
		VisionModel:                       env("VISION_MODEL", defaultVisionModel),
		VisionPrompt:                      defaultVisionPrompt,
		VisionEnabled:                     boolPtr(env("VISION_ENABLED", "true") != "false"),
		PreserveCodexOfficialAuthOnSwitch: boolPtr(true),
		ClientRouteEnabled:                defaultClientRouteEnabled(),
		LocalAPIEnabled:                   boolPtr(env("LOCAL_API_ENABLED", "true") != "false"),
		ClientConfigPaths:                 map[string]string{},
		ClientProgramPaths:                map[string]string{},
		ClientAutoRestart:                 defaultClientAutoRestart(),
		ClientAutoStart:                   defaultClientAutoStart(),
		AutoCheckUpdates:                  boolPtr(true),
		OpenWindow:                        env("OPEN_WINDOW", "true") != "false",
		OpenBrowser:                       env("OPEN_BROWSER", "false") == "true",
	}
	cfg.TextModelProfiles = []textModelProfile{textProfileFromConfig(cfg, "text-default", "默认文本模型")}
	cfg.ActiveTextProfileID = cfg.TextModelProfiles[0].ID
	cfg.VisionModelProfiles = []visionModelProfile{visionProfileFromConfig(cfg, "vision-default", "默认视觉模型")}
	cfg.ActiveVisionProfileID = cfg.VisionModelProfiles[0].ID
	return cfg
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return appSlug + ".json"
	}
	return filepath.Join(dir, appSlug, "config.json")
}

func legacyConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return legacyAppSlug + ".json"
	}
	return filepath.Join(dir, legacyAppSlug, "config.json")
}

func loadConfig(path string) (config, error) {
	var cfg config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	if err == nil && !jsonHasField(b, "open_window") {
		cfg.OpenWindow = true
	}
	return cfg, err
}

func jsonHasField(b []byte, field string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return false
	}
	_, ok := raw[field]
	return ok
}

func saveConfig(path string, cfg config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func mergeConfig(base, loaded config) config {
	if loaded.Addr != "" {
		base.Addr = loaded.Addr
	}
	if loaded.ActiveModelProfileID != "" {
		base.ActiveModelProfileID = loaded.ActiveModelProfileID
	}
	if len(loaded.ModelProfiles) > 0 {
		base.ModelProfiles = loaded.ModelProfiles
	}
	if loaded.ActiveTextProfileID != "" {
		base.ActiveTextProfileID = loaded.ActiveTextProfileID
	}
	if len(loaded.TextModelProfiles) > 0 {
		base.TextModelProfiles = loaded.TextModelProfiles
	}
	if loaded.ActiveVisionProfileID != "" {
		base.ActiveVisionProfileID = loaded.ActiveVisionProfileID
	}
	if len(loaded.VisionModelProfiles) > 0 {
		base.VisionModelProfiles = loaded.VisionModelProfiles
	}
	if loaded.TextProvider != "" {
		base.TextProvider = loaded.TextProvider
	}
	if loaded.TextBaseURL != "" {
		base.TextBaseURL = loaded.TextBaseURL
	}
	if loaded.TextAPIKey != "" {
		base.TextAPIKey = loaded.TextAPIKey
	}
	if loaded.TextModelOverride != "" {
		base.TextModelOverride = loaded.TextModelOverride
	}
	if len(loaded.TextModelOverrides) > 0 {
		base.TextModelOverrides = loaded.TextModelOverrides
	}
	if len(loaded.TextModelMappings) > 0 {
		base.TextModelMappings = loaded.TextModelMappings
	}
	if loaded.TextWireAPI != "" {
		base.TextWireAPI = loaded.TextWireAPI
	}
	base.TextSupportsImages = loaded.TextSupportsImages
	if loaded.ProxyURL != "" {
		base.ProxyURL = loaded.ProxyURL
	}
	if loaded.VisionProvider != "" {
		base.VisionProvider = loaded.VisionProvider
	}
	if loaded.VisionBaseURL != "" {
		base.VisionBaseURL = loaded.VisionBaseURL
	}
	if loaded.VisionAPIKey != "" {
		base.VisionAPIKey = loaded.VisionAPIKey
	}
	if loaded.VisionModel != "" {
		base.VisionModel = loaded.VisionModel
	}
	if loaded.VisionPrompt != "" {
		base.VisionPrompt = loaded.VisionPrompt
	}
	if loaded.VisionEnabled != nil {
		base.VisionEnabled = loaded.VisionEnabled
	}
	if loaded.PreserveCodexOfficialAuthOnSwitch != nil {
		base.PreserveCodexOfficialAuthOnSwitch = loaded.PreserveCodexOfficialAuthOnSwitch
	}
	base.UnifyCodexSessionHistory = loaded.UnifyCodexSessionHistory
	if loaded.ClientRouteEnabled != nil {
		base.ClientRouteEnabled = normalizeClientRouteEnabled(loaded.ClientRouteEnabled)
	}
	if loaded.LocalAPIEnabled != nil {
		base.LocalAPIEnabled = loaded.LocalAPIEnabled
	}
	if loaded.ClientConfigPaths != nil {
		base.ClientConfigPaths = normalizeClientPathMap(loaded.ClientConfigPaths)
	}
	if loaded.ClientProgramPaths != nil {
		base.ClientProgramPaths = normalizeClientProgramPathMap(loaded.ClientProgramPaths)
	}
	if loaded.ClientAutoRestart != nil {
		base.ClientAutoRestart = normalizeClientBehavior(loaded.ClientAutoRestart, true)
	}
	if loaded.ClientAutoStart != nil {
		base.ClientAutoStart = normalizeClientBehavior(loaded.ClientAutoStart, false)
	}
	base.ClientPathsDetected = loaded.ClientPathsDetected
	base.ClientPathDetectionVersion = loaded.ClientPathDetectionVersion
	if loaded.AutoCheckUpdates != nil {
		base.AutoCheckUpdates = loaded.AutoCheckUpdates
	}
	base.OpenWindow = loaded.OpenWindow
	base.OpenBrowser = loaded.OpenBrowser
	base = normalizeSeparateModelProfiles(base)
	return base
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envAny(fallback string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func splitKeys(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if key := strings.TrimSpace(part); key != "" {
			out = append(out, key)
		}
	}
	return out
}

func (a *app) currentConfig() config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *app) setConfig(cfg config) error {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	addr, err := normalizeListenAddress(cfg.Addr)
	if err != nil {
		return err
	}
	cfg.Addr = addr
	if cfg.TextProvider == "" {
		cfg.TextProvider = defaultTextProvider
	}
	if cfg.TextBaseURL == "" {
		cfg.TextBaseURL = defaultBaseURL(cfg.TextProvider)
	}
	cfg.TextWireAPI = normalizeWireAPI(cfg.TextWireAPI)
	if cfg.VisionProvider == "" {
		cfg.VisionProvider = "openai"
	}
	if cfg.VisionBaseURL == "" {
		cfg.VisionBaseURL = defaultBaseURL(cfg.VisionProvider)
	}
	if cfg.VisionModel == "" {
		cfg.VisionModel = defaultVisionModel
	}
	if cfg.VisionPrompt == "" {
		cfg.VisionPrompt = defaultVisionPrompt
	}
	if cfg.VisionEnabled == nil {
		cfg.VisionEnabled = boolPtr(true)
	}
	if cfg.PreserveCodexOfficialAuthOnSwitch == nil {
		cfg.PreserveCodexOfficialAuthOnSwitch = boolPtr(true)
	}
	cfg.VisionPrompt = defaultVisionPrompt
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	cfg.ClientRouteEnabled = normalizeClientRouteEnabled(cfg.ClientRouteEnabled)
	if cfg.LocalAPIEnabled == nil {
		cfg.LocalAPIEnabled = boolPtr(true)
	}
	cfg.ClientConfigPaths = normalizeClientPathMap(cfg.ClientConfigPaths)
	cfg.ClientProgramPaths = normalizeClientProgramPathMap(cfg.ClientProgramPaths)
	cfg.ClientAutoRestart = normalizeClientBehavior(cfg.ClientAutoRestart, true)
	cfg.ClientAutoStart = normalizeClientBehavior(cfg.ClientAutoStart, false)
	cfg.ClientPathsDetected = true
	cfg.ClientPathDetectionVersion = currentClientPathDetectionVersion
	if cfg.AutoCheckUpdates == nil {
		cfg.AutoCheckUpdates = boolPtr(true)
	}
	cfg.TextProvider = normalizeProvider(cfg.TextProvider)
	cfg.VisionProvider = normalizeProvider(cfg.VisionProvider)
	cfg = normalizeSeparateModelProfiles(cfg)

	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	if a.db != nil {
		return saveConfigToDB(a.db, cfg)
	}
	return saveConfig(a.configPath, cfg)
}

func normalizeListenAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultAddr
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("API 监听地址必须使用 主机:端口 格式: %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("API 端口必须在 1 到 65535 之间")
	}
	return net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(portNumber)), nil
}
func boolPtr(value bool) *bool {
	return &value
}

func visionEnabled(cfg config) bool {
	return cfg.VisionEnabled == nil || *cfg.VisionEnabled
}

func preserveCodexOfficialAuth(cfg config) bool {
	return cfg.PreserveCodexOfficialAuthOnSwitch == nil || *cfg.PreserveCodexOfficialAuthOnSwitch
}

func defaultClientRouteEnabled() map[string]bool {
	return map[string]bool{
		clientCodex:      false,
		clientOpenCode:   false,
		clientClaudeCode: false,
		clientOpenClaw:   false,
	}
}

func normalizeClientRouteEnabled(routes map[string]bool) map[string]bool {
	normalized := defaultClientRouteEnabled()
	for client, enabled := range routes {
		if id := normalizeClientID(client); id != "" {
			normalized[id] = enabled
		}
	}
	return normalized
}

func defaultClientAutoRestart() map[string]bool {
	return normalizeClientBehavior(nil, true)
}

func defaultClientAutoStart() map[string]bool {
	return normalizeClientBehavior(nil, false)
}

func normalizeClientBehavior(values map[string]bool, fallback bool) map[string]bool {
	normalized := make(map[string]bool, len(clientProgramOrder))
	for _, client := range clientProgramOrder {
		normalized[client] = fallback
	}
	for client, enabled := range values {
		if id := normalizeClientProgramID(client); id != "" {
			normalized[id] = enabled
		}
	}
	return normalized
}

func textSupportsImages(cfg config, requested string) bool {
	mapping, ok := effectiveTextModelMapping(cfg, requested)
	return ok && mapping.SupportsImages
}

func textModelReasoningEffort(mapping textModelMapping) string {
	if effort := normalizeTextModelReasoningEffort(mapping.ReasoningEffort); effort != "" {
		return effort
	}
	if mapping.SupportsReasoning != nil {
		if *mapping.SupportsReasoning {
			return "high"
		}
		return "none"
	}
	if inferTextModelReasoningSupport(mapping.Name, mapping.Model) {
		return "high"
	}
	return "none"
}

func normalizeTextModelReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "unsupported":
		return "none"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "xhigh", "extra-high", "extra_high":
		return "xhigh"
	default:
		return ""
	}
}

func textModelSupportsReasoning(mapping textModelMapping) bool {
	return textModelReasoningEffort(mapping) != "none"
}

func inferTextModelReasoningSupport(values ...string) bool {
	value := strings.ToLower(strings.Join(values, " "))
	for _, marker := range []string{
		"reasoning", "reasoner", "thinking",
		"deepseek-r1", "deepseek-v4",
		"glm-4.5", "glm-4.6", "glm-4.7", "glm-5",
		"grok-3-mini", "grok-4",
		"gpt-5", "qwen3", "gemini-2.5", "gemini-3",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case '/', '\\', '-', '_', '.', ':', ' ', '\t':
			return true
		default:
			return false
		}
	}) {
		if token == "o1" || token == "o3" || token == "o4" {
			return true
		}
	}
	return false
}

func relayImageInputEnabled(cfg config, requested string) bool {
	return textSupportsImages(cfg, requested) || visionEnabled(cfg)
}

func shouldAugmentImages(cfg config, requested string) bool {
	return !textSupportsImages(cfg, requested) && visionEnabled(cfg)
}

func normalizeSeparateModelProfiles(cfg config) config {
	if len(cfg.TextModelProfiles) == 0 || len(cfg.VisionModelProfiles) == 0 {
		cfg = migrateCombinedProfiles(cfg)
	}
	if len(cfg.TextModelProfiles) == 0 {
		cfg.TextModelProfiles = []textModelProfile{textProfileFromConfig(cfg, "text-default", "默认文本模型")}
	}
	if len(cfg.VisionModelProfiles) == 0 {
		cfg.VisionModelProfiles = []visionModelProfile{visionProfileFromConfig(cfg, "vision-default", "默认视觉模型")}
	}
	cfg.TextModelProfiles = normalizeTextProfiles(cfg.TextModelProfiles)
	cfg.VisionModelProfiles = normalizeVisionProfiles(cfg.VisionModelProfiles)
	if cfg.ActiveTextProfileID == "" || !hasTextProfile(cfg.TextModelProfiles, cfg.ActiveTextProfileID) {
		cfg.ActiveTextProfileID = cfg.TextModelProfiles[0].ID
	}
	if cfg.ActiveVisionProfileID == "" || !hasVisionProfile(cfg.VisionModelProfiles, cfg.ActiveVisionProfileID) {
		cfg.ActiveVisionProfileID = cfg.VisionModelProfiles[0].ID
	}
	if cfg.TextSupportsImages {
		for i := range cfg.TextModelProfiles {
			if cfg.TextModelProfiles[i].ID != cfg.ActiveTextProfileID {
				continue
			}
			for j := range cfg.TextModelProfiles[i].ModelMappings {
				cfg.TextModelProfiles[i].ModelMappings[j].SupportsImages = true
			}
			break
		}
		cfg.TextSupportsImages = false
	}
	for _, profile := range cfg.TextModelProfiles {
		if profile.ID == cfg.ActiveTextProfileID {
			cfg = applyTextProfileToConfig(cfg, profile)
			break
		}
	}
	for _, profile := range cfg.VisionModelProfiles {
		if profile.ID == cfg.ActiveVisionProfileID {
			cfg = applyVisionProfileToConfig(cfg, profile)
			break
		}
	}
	return cfg
}

func migrateCombinedProfiles(cfg config) config {
	if len(cfg.ModelProfiles) == 0 {
		return cfg
	}
	textProfiles := make([]textModelProfile, 0, len(cfg.ModelProfiles))
	visionProfiles := make([]visionModelProfile, 0, len(cfg.ModelProfiles))
	for _, profile := range cfg.ModelProfiles {
		profile = normalizeModelProfile(profile)
		textProfiles = append(textProfiles, textModelProfile{
			ID:             "text-" + profile.ID,
			Name:           profile.Name,
			Provider:       profile.TextProvider,
			BaseURL:        profile.TextBaseURL,
			APIKey:         profile.TextAPIKey,
			ModelOverride:  profile.TextModelOverride,
			ModelOverrides: profile.TextModelOverrides,
			ModelMappings:  profile.TextModelMappings,
			WireAPI:        profile.TextWireAPI,
			SupportsImages: profile.TextSupportsImages,
			ProxyURL:       profile.ProxyURL,
		})
		visionProfiles = append(visionProfiles, visionModelProfile{
			ID:       "vision-" + profile.ID,
			Name:     profile.Name,
			Provider: profile.VisionProvider,
			BaseURL:  profile.VisionBaseURL,
			APIKey:   profile.VisionAPIKey,
			Model:    profile.VisionModel,
		})
	}
	if len(cfg.TextModelProfiles) == 0 {
		cfg.TextModelProfiles = textProfiles
	}
	if len(cfg.VisionModelProfiles) == 0 {
		cfg.VisionModelProfiles = visionProfiles
	}
	if cfg.ActiveModelProfileID != "" {
		cfg.ActiveTextProfileID = "text-" + cfg.ActiveModelProfileID
		cfg.ActiveVisionProfileID = "vision-" + cfg.ActiveModelProfileID
	}
	return cfg
}

func normalizeTextProfiles(profiles []textModelProfile) []textModelProfile {
	seen := map[string]bool{}
	out := make([]textModelProfile, 0, len(profiles))
	for i, profile := range profiles {
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			profile.ID = "text-" + strconv.Itoa(i+1)
		}
		if seen[profile.ID] {
			profile.ID += "-" + strconv.Itoa(i+1)
		}
		seen[profile.ID] = true
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			profile.Name = "文本模型 " + strconv.Itoa(i+1)
		}
		profile.Provider = normalizeProvider(profile.Provider)
		if profile.Provider == "" {
			profile.Provider = defaultTextProvider
		}
		profile.BaseURL = strings.TrimSpace(profile.BaseURL)
		if profile.BaseURL == "" {
			profile.BaseURL = defaultBaseURL(profile.Provider)
		}
		profile.APIKey = strings.TrimSpace(profile.APIKey)
		profile.ModelOverride = strings.TrimSpace(profile.ModelOverride)
		profile.ModelMappings = normalizeTextModelMappings(profile.ModelMappings, profile.ModelOverrides, profile.ModelOverride)
		if profile.SupportsImages {
			for j := range profile.ModelMappings {
				profile.ModelMappings[j].SupportsImages = true
			}
			profile.SupportsImages = false
		}
		profile.ModelOverrides = modelOverridesFromMappings(profile.ModelMappings)
		profile.ModelOverride = firstModelOverride(profile.ModelOverrides)
		profile.WireAPI = normalizeWireAPI(profile.WireAPI)
		profile.ProxyURL = strings.TrimSpace(profile.ProxyURL)
		out = append(out, profile)
	}
	return out
}

func normalizeVisionProfiles(profiles []visionModelProfile) []visionModelProfile {
	seen := map[string]bool{}
	out := make([]visionModelProfile, 0, len(profiles))
	for i, profile := range profiles {
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			profile.ID = "vision-" + strconv.Itoa(i+1)
		}
		if seen[profile.ID] {
			profile.ID += "-" + strconv.Itoa(i+1)
		}
		seen[profile.ID] = true
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			profile.Name = "视觉模型 " + strconv.Itoa(i+1)
		}
		profile.Provider = normalizeProvider(profile.Provider)
		if profile.Provider == "" {
			profile.Provider = "openai"
		}
		profile.BaseURL = strings.TrimSpace(profile.BaseURL)
		if profile.BaseURL == "" {
			profile.BaseURL = defaultBaseURL(profile.Provider)
		}
		profile.APIKey = strings.TrimSpace(profile.APIKey)
		profile.Model = strings.TrimSpace(profile.Model)
		if profile.Model == "" {
			profile.Model = defaultVisionModel
		}
		out = append(out, profile)
	}
	return out
}

func hasTextProfile(profiles []textModelProfile, id string) bool {
	for _, profile := range profiles {
		if profile.ID == id {
			return true
		}
	}
	return false
}

func hasVisionProfile(profiles []visionModelProfile, id string) bool {
	for _, profile := range profiles {
		if profile.ID == id {
			return true
		}
	}
	return false
}

func textProfileFromConfig(cfg config, id, name string) textModelProfile {
	return normalizeTextProfiles([]textModelProfile{{
		ID:             id,
		Name:           name,
		Provider:       cfg.TextProvider,
		BaseURL:        cfg.TextBaseURL,
		APIKey:         cfg.TextAPIKey,
		ModelOverride:  cfg.TextModelOverride,
		ModelOverrides: cfg.TextModelOverrides,
		ModelMappings:  cfg.TextModelMappings,
		WireAPI:        cfg.TextWireAPI,
		SupportsImages: cfg.TextSupportsImages,
		ProxyURL:       cfg.ProxyURL,
	}})[0]
}

func visionProfileFromConfig(cfg config, id, name string) visionModelProfile {
	return normalizeVisionProfiles([]visionModelProfile{{
		ID:       id,
		Name:     name,
		Provider: cfg.VisionProvider,
		BaseURL:  cfg.VisionBaseURL,
		APIKey:   cfg.VisionAPIKey,
		Model:    cfg.VisionModel,
	}})[0]
}

func applyTextProfileToConfig(cfg config, profile textModelProfile) config {
	profile = normalizeTextProfiles([]textModelProfile{profile})[0]
	cfg.TextProvider = profile.Provider
	cfg.TextBaseURL = profile.BaseURL
	cfg.TextAPIKey = profile.APIKey
	cfg.TextModelOverride = profile.ModelOverride
	cfg.TextModelOverrides = profile.ModelOverrides
	cfg.TextModelMappings = profile.ModelMappings
	cfg.TextWireAPI = profile.WireAPI
	cfg.TextSupportsImages = false
	cfg.ProxyURL = profile.ProxyURL
	return cfg
}

func applyVisionProfileToConfig(cfg config, profile visionModelProfile) config {
	profile = normalizeVisionProfiles([]visionModelProfile{profile})[0]
	cfg.VisionProvider = profile.Provider
	cfg.VisionBaseURL = profile.BaseURL
	cfg.VisionAPIKey = profile.APIKey
	cfg.VisionModel = profile.Model
	return cfg
}

func normalizeModelProfiles(cfg config) config {
	if len(cfg.ModelProfiles) == 0 {
		cfg.ModelProfiles = []modelProfile{profileFromConfig(cfg, "default", "默认模型")}
	}
	seen := map[string]bool{}
	out := make([]modelProfile, 0, len(cfg.ModelProfiles))
	for i, profile := range cfg.ModelProfiles {
		profile = normalizeModelProfile(profile)
		if profile.ID == "" {
			profile.ID = "profile-" + strconv.Itoa(i+1)
		}
		if profile.Name == "" {
			profile.Name = "模型 " + strconv.Itoa(i+1)
		}
		if seen[profile.ID] {
			profile.ID = profile.ID + "-" + strconv.Itoa(i+1)
		}
		seen[profile.ID] = true
		out = append(out, profile)
	}
	cfg.ModelProfiles = out
	if cfg.ActiveModelProfileID == "" || !seen[cfg.ActiveModelProfileID] {
		cfg.ActiveModelProfileID = cfg.ModelProfiles[0].ID
	}
	for _, profile := range cfg.ModelProfiles {
		if profile.ID == cfg.ActiveModelProfileID {
			return applyProfileToConfig(cfg, profile)
		}
	}
	return cfg
}

func normalizeModelProfile(profile modelProfile) modelProfile {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.TextProvider = normalizeProvider(profile.TextProvider)
	if profile.TextProvider == "" {
		profile.TextProvider = defaultTextProvider
	}
	profile.TextBaseURL = strings.TrimSpace(profile.TextBaseURL)
	if profile.TextBaseURL == "" {
		profile.TextBaseURL = defaultBaseURL(profile.TextProvider)
	}
	profile.TextAPIKey = strings.TrimSpace(profile.TextAPIKey)
	profile.TextModelOverride = strings.TrimSpace(profile.TextModelOverride)
	profile.TextModelMappings = normalizeTextModelMappings(profile.TextModelMappings, profile.TextModelOverrides, profile.TextModelOverride)
	if profile.TextSupportsImages {
		for i := range profile.TextModelMappings {
			profile.TextModelMappings[i].SupportsImages = true
		}
		profile.TextSupportsImages = false
	}
	profile.TextModelOverrides = modelOverridesFromMappings(profile.TextModelMappings)
	profile.TextModelOverride = firstModelOverride(profile.TextModelOverrides)
	profile.TextWireAPI = normalizeWireAPI(profile.TextWireAPI)
	profile.ProxyURL = strings.TrimSpace(profile.ProxyURL)
	profile.VisionProvider = normalizeProvider(profile.VisionProvider)
	if profile.VisionProvider == "" {
		profile.VisionProvider = "openai"
	}
	profile.VisionBaseURL = strings.TrimSpace(profile.VisionBaseURL)
	if profile.VisionBaseURL == "" {
		profile.VisionBaseURL = defaultBaseURL(profile.VisionProvider)
	}
	profile.VisionAPIKey = strings.TrimSpace(profile.VisionAPIKey)
	profile.VisionModel = strings.TrimSpace(profile.VisionModel)
	if profile.VisionModel == "" {
		profile.VisionModel = defaultVisionModel
	}
	return profile
}

func profileFromConfig(cfg config, id, name string) modelProfile {
	return normalizeModelProfile(modelProfile{
		ID:                 id,
		Name:               name,
		TextProvider:       cfg.TextProvider,
		TextBaseURL:        cfg.TextBaseURL,
		TextAPIKey:         cfg.TextAPIKey,
		TextModelOverride:  cfg.TextModelOverride,
		TextModelOverrides: cfg.TextModelOverrides,
		TextModelMappings:  cfg.TextModelMappings,
		TextWireAPI:        cfg.TextWireAPI,
		TextSupportsImages: cfg.TextSupportsImages,
		ProxyURL:           cfg.ProxyURL,
		VisionProvider:     cfg.VisionProvider,
		VisionBaseURL:      cfg.VisionBaseURL,
		VisionAPIKey:       cfg.VisionAPIKey,
		VisionModel:        cfg.VisionModel,
	})
}

func applyProfileToConfig(cfg config, profile modelProfile) config {
	profile = normalizeModelProfile(profile)
	cfg.TextProvider = profile.TextProvider
	cfg.TextBaseURL = profile.TextBaseURL
	cfg.TextAPIKey = profile.TextAPIKey
	cfg.TextModelOverride = profile.TextModelOverride
	cfg.TextModelOverrides = profile.TextModelOverrides
	cfg.TextModelMappings = profile.TextModelMappings
	cfg.TextWireAPI = profile.TextWireAPI
	cfg.TextSupportsImages = false
	cfg.ProxyURL = profile.ProxyURL
	cfg.VisionProvider = profile.VisionProvider
	cfg.VisionBaseURL = profile.VisionBaseURL
	cfg.VisionAPIKey = profile.VisionAPIKey
	cfg.VisionModel = profile.VisionModel
	return cfg
}

func defaultBaseURL(provider string) string {
	switch normalizeProvider(provider) {
	case "anthropic":
		return "https://api.anthropic.com"
	case "gemini":
		return "https://generativelanguage.googleapis.com"
	case "ollama":
		return "http://127.0.0.1:11434"
	default:
		return "https://api.openai.com"
	}
}

func (a *app) textEndpoint(cfg config) endpoint {
	return endpoint{
		Provider:      normalizeProvider(cfg.TextProvider),
		BaseURL:       cfg.TextBaseURL,
		APIKey:        cfg.TextAPIKey,
		ModelOverride: cfg.TextModelOverride,
		WireAPI:       cfg.TextWireAPI,
		ProxyURL:      cfg.ProxyURL,
	}
}

func normalizeModelOverrides(models []string, legacy string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(models)+1)
	for _, model := range append([]string{legacy}, models...) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func normalizeTextModelMappings(mappings []textModelMapping, models []string, legacy string) []textModelMapping {
	if len(mappings) == 0 {
		for _, model := range normalizeModelOverrides(models, legacy) {
			mappings = append(mappings, textModelMapping{Name: model, Model: model})
		}
	}
	seen := map[string]bool{}
	out := make([]textModelMapping, 0, len(mappings))
	for _, mapping := range mappings {
		mapping.Name = strings.TrimSpace(mapping.Name)
		mapping.Model = strings.TrimSpace(mapping.Model)
		if mapping.Model == "" {
			mapping.Model = mapping.Name
		}
		if mapping.Name == "" {
			mapping.Name = mapping.Model
		}
		if mapping.Model == "" || seen[mapping.Name] {
			continue
		}
		seen[mapping.Name] = true
		if mapping.ContextWindow < 0 {
			mapping.ContextWindow = 0
		}
		mapping.ReasoningEffort = textModelReasoningEffort(mapping)
		mapping.SupportsReasoning = nil
		out = append(out, mapping)
	}
	return out
}

func modelOverridesFromMappings(mappings []textModelMapping) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		model := strings.TrimSpace(mapping.Model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func firstModelOverride(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func textModelMappings(cfg config) []textModelMapping {
	return normalizeTextModelMappings(cfg.TextModelMappings, cfg.TextModelOverrides, cfg.TextModelOverride)
}

func textModelOverrides(cfg config) []string {
	return modelOverridesFromMappings(textModelMappings(cfg))
}

func effectiveTextModel(cfg config, requested string) string {
	mapping, ok := effectiveTextModelMapping(cfg, requested)
	if !ok {
		return ""
	}
	return mapping.Model
}

func effectiveTextModelMapping(cfg config, requested string) (textModelMapping, bool) {
	mappings := textModelMappings(cfg)
	if len(mappings) == 0 {
		return textModelMapping{}, false
	}
	requested = strings.TrimSpace(requested)
	for _, mapping := range mappings {
		if requested == mapping.Name || requested == mapping.Model {
			return mapping, true
		}
	}
	if index, ok := codexAccountModelAliasIndex(requested); ok && index < len(mappings) {
		return mappings[index], true
	}
	return mappings[0], true
}

func codexAccountAliasTextModel(mappings []textModelMapping, requested string) (string, bool) {
	index, ok := codexAccountModelAliasIndex(requested)
	if !ok || index >= len(mappings) {
		return "", false
	}
	model := strings.TrimSpace(mappings[index].Model)
	return model, model != ""
}

func codexAccountModelAlias(index int) string {
	if index < 0 || index >= len(codexAccountModelAliases) {
		return ""
	}
	return codexAccountModelAliases[index]
}

func codexAccountModelAliasIndex(requested string) (int, bool) {
	value := strings.ToLower(strings.TrimSpace(requested))
	value = strings.TrimPrefix(value, "openai/")
	for index, alias := range codexAccountModelAliases {
		if value == alias || value == strings.TrimPrefix(alias, "gpt-") {
			return index, true
		}
	}
	return 0, false
}

func codexAccountModelDisplayName(alias string) string {
	switch strings.ToLower(strings.TrimSpace(alias)) {
	case "gpt-5.5":
		return "GPT-5.5"
	case "gpt-5.4":
		return "GPT-5.4"
	case "gpt-5.4-mini":
		return "GPT-5.4-Mini"
	default:
		return alias
	}
}

func normalizeWireAPI(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "responses", "response", "openai_responses":
		return "responses"
	default:
		return defaultTextWireAPI
	}
}

func (a *app) visionEndpoint(cfg config) endpoint {
	return endpoint{
		Provider:      normalizeProvider(cfg.VisionProvider),
		BaseURL:       cfg.VisionBaseURL,
		APIKey:        cfg.VisionAPIKey,
		ModelOverride: cfg.VisionModel,
		ProxyURL:      cfg.ProxyURL,
	}
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "openai-compatible", "openai_compatible":
		return "openai"
	case "claude":
		return "anthropic"
	case "google":
		return "gemini"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}
