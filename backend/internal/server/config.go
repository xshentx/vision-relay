package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultAddr         = "127.0.0.1:8787"
	defaultTextProvider = "openai"
	defaultVisionModel  = "gpt-4o-mini"
	defaultVisionPrompt = "你只是图片识别器，不是最终回答模型。只提取图片事实，禁止回答用户需求、禁止写代码、禁止给方案、禁止推理下一步。按图片复杂度输出必要细节，用简洁中文列出：1. 可见文字；2. 主要对象/页面结构；3. 颜色和布局；4. 与用户需求直接相关的细节。"
)

func defaultConfig() config {
	cfg := config{
		Addr:                env("CODEX_PROXY_ADDR", defaultAddr),
		TextProvider:        env("TEXT_PROVIDER", defaultTextProvider),
		TextBaseURL:         env("TEXT_BASE_URL", "https://api.openai.com"),
		TextAPIKey:          env("TEXT_API_KEY", ""),
		TextModelOverride:   env("TEXT_MODEL_OVERRIDE", ""),
		ProxyURL:            env("PROXY_URL", ""),
		VisionProvider:      env("VISION_PROVIDER", "openai"),
		VisionBaseURL:       env("VISION_BASE_URL", "https://api.openai.com"),
		VisionAPIKey:        env("VISION_API_KEY", ""),
		VisionModel:         env("VISION_MODEL", defaultVisionModel),
		VisionPrompt:        defaultVisionPrompt,
		ClientAPIKeyEntries: keysToEntries(splitKeys(env("CLIENT_API_KEYS", ""))),
		OpenWindow:          env("OPEN_WINDOW", "true") != "false",
		OpenBrowser:         env("OPEN_BROWSER", "false") == "true",
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
		return "codex-proxy.json"
	}
	return filepath.Join(dir, "codex-proxy", "config.json")
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
	if len(loaded.ClientAPIKeyEntries) > 0 {
		base.ClientAPIKeyEntries = loaded.ClientAPIKeyEntries
	} else if len(loaded.ClientAPIKeys) > 0 {
		base.ClientAPIKeyEntries = keysToEntries(loaded.ClientAPIKeys)
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
	if cfg.TextProvider == "" {
		cfg.TextProvider = defaultTextProvider
	}
	if cfg.TextBaseURL == "" {
		cfg.TextBaseURL = defaultBaseURL(cfg.TextProvider)
	}
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
	cfg.VisionPrompt = defaultVisionPrompt
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	cfg.ClientAPIKeyEntries = normalizeClientAPIKeyEntries(cfg.ClientAPIKeyEntries)
	cfg.ClientAPIKeys = nil
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
			ID:            "text-" + profile.ID,
			Name:          profile.Name,
			Provider:      profile.TextProvider,
			BaseURL:       profile.TextBaseURL,
			APIKey:        profile.TextAPIKey,
			ModelOverride: profile.TextModelOverride,
			ProxyURL:      profile.ProxyURL,
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
		ID:            id,
		Name:          name,
		Provider:      cfg.TextProvider,
		BaseURL:       cfg.TextBaseURL,
		APIKey:        cfg.TextAPIKey,
		ModelOverride: cfg.TextModelOverride,
		ProxyURL:      cfg.ProxyURL,
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
		ID:                id,
		Name:              name,
		TextProvider:      cfg.TextProvider,
		TextBaseURL:       cfg.TextBaseURL,
		TextAPIKey:        cfg.TextAPIKey,
		TextModelOverride: cfg.TextModelOverride,
		ProxyURL:          cfg.ProxyURL,
		VisionProvider:    cfg.VisionProvider,
		VisionBaseURL:     cfg.VisionBaseURL,
		VisionAPIKey:      cfg.VisionAPIKey,
		VisionModel:       cfg.VisionModel,
	})
}

func applyProfileToConfig(cfg config, profile modelProfile) config {
	profile = normalizeModelProfile(profile)
	cfg.TextProvider = profile.TextProvider
	cfg.TextBaseURL = profile.TextBaseURL
	cfg.TextAPIKey = profile.TextAPIKey
	cfg.TextModelOverride = profile.TextModelOverride
	cfg.ProxyURL = profile.ProxyURL
	cfg.VisionProvider = profile.VisionProvider
	cfg.VisionBaseURL = profile.VisionBaseURL
	cfg.VisionAPIKey = profile.VisionAPIKey
	cfg.VisionModel = profile.VisionModel
	return cfg
}

func keysToEntries(keys []string) []clientAPIKeyEntry {
	entries := make([]clientAPIKeyEntry, 0, len(keys))
	for i, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		entries = append(entries, clientAPIKeyEntry{
			Name: "旧令牌 " + strconv.Itoa(i+1),
			Key:  key,
		})
	}
	return entries
}

func normalizeClientAPIKeyEntries(entries []clientAPIKeyEntry) []clientAPIKeyEntry {
	seen := map[string]bool{}
	out := make([]clientAPIKeyEntry, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		key := strings.TrimSpace(entry.Key)
		if key == "" || seen[key] {
			continue
		}
		if name == "" {
			name = "未命名客户端"
		}
		seen[key] = true
		out = append(out, clientAPIKeyEntry{Name: name, Key: key})
	}
	return out
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
		ProxyURL:      cfg.ProxyURL,
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
