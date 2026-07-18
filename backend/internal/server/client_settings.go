package server

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	currentClientPathDetectionVersion         = 5
	claudeDesktopCLISplitPathDetectionVersion = 5
)

// clientConfigOrder includes configuration files that are not route targets.
// Claude Desktop (the route target) and Claude Code CLI use different files,
// even though they are configured by the same provider route.
var clientConfigOrder = []string{clientCodex, clientOpenCode, clientClaudeCode, clientClaudeCLI, clientOpenClaw}

func normalizeClientPathMap(paths map[string]string) map[string]string {
	normalized := map[string]string{}
	for client, value := range paths {
		id := normalizeClientID(client)
		if id == "" && strings.EqualFold(strings.TrimSpace(client), clientClaudeCLI) {
			id = clientClaudeCLI
		}
		if id != "" {
			normalized[id] = strings.TrimSpace(value)
		}
	}
	return normalized
}

func normalizeClientProgramPathMap(paths map[string]string) map[string]string {
	normalized := map[string]string{}
	for client, value := range paths {
		if id := normalizeClientProgramID(client); id != "" {
			normalized[id] = strings.TrimSpace(value)
		}
	}
	return normalized
}

func configuredClientConfigPath(cfg config, client, homeDir string) string {
	client = normalizeClientProgramID(client)
	path := strings.TrimSpace(cfg.ClientConfigPaths[client])
	if client == clientClaudeCode && isClaudeCLIConfigPath(path) {
		path = ""
	}
	if path != "" {
		return resolveClientPath(path, homeDir)
	}
	return defaultClientConfigPath(client, homeDir)
}

func defaultClientConfigPath(client, homeDir string) string {
	switch normalizeClientProgramID(client) {
	case clientCodex:
		return filepath.Join(codexConfigDir(homeDir), "config.toml")
	case clientOpenCode:
		return filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	case clientClaudeCode:
		return claudeDesktopConfigPath(homeDir)
	case clientClaudeCLI:
		if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
			return filepath.Join(resolveClientPath(dir, homeDir), "settings.json")
		}
		return filepath.Join(homeDir, ".claude", "settings.json")
	case clientOpenClaw:
		return openClawConfigPath(homeDir)
	default:
		return ""
	}
}

func detectClientPaths(cfg config, homeDir string, force bool) config {
	configPaths := normalizeClientPathMap(cfg.ClientConfigPaths)
	programPaths := normalizeClientProgramPathMap(cfg.ClientProgramPaths)
	autoRestart := normalizeClientBehavior(cfg.ClientAutoRestart, true)
	autoStart := normalizeClientBehavior(cfg.ClientAutoStart, false)
	if cfg.ClientPathDetectionVersion < claudeDesktopCLISplitPathDetectionVersion {
		// Before the desktop/CLI split, the claude-code key always represented
		// Claude Code CLI. Migrate it by version rather than guessing from the
		// path: custom CLAUDE_CONFIG_DIR values and native CLI executables can use
		// arbitrary names that are indistinguishable from desktop paths.
		if legacyPath := strings.TrimSpace(configPaths[clientClaudeCode]); legacyPath != "" {
			if strings.TrimSpace(configPaths[clientClaudeCLI]) == "" {
				configPaths[clientClaudeCLI] = legacyPath
			}
			configPaths[clientClaudeCode] = ""
		}
		if legacyPath := strings.TrimSpace(programPaths[clientClaudeCode]); legacyPath != "" {
			if strings.TrimSpace(programPaths[clientClaudeCLI]) == "" {
				programPaths[clientClaudeCLI] = legacyPath
			}
			programPaths[clientClaudeCode] = ""
		}

		// Lifecycle preferences under the old key also belonged to the CLI.
		// Preserve those choices and give the newly introduced desktop target its
		// normal defaults.
		autoRestart[clientClaudeCLI] = autoRestart[clientClaudeCode]
		autoStart[clientClaudeCLI] = autoStart[clientClaudeCode]
		autoRestart[clientClaudeCode] = true
		autoStart[clientClaudeCode] = false
	}
	for _, client := range clientConfigOrder {
		if force || strings.TrimSpace(configPaths[client]) == "" {
			configPaths[client] = detectClientConfigPath(client, homeDir)
		}
	}
	for _, client := range clientProgramOrder {
		if force || strings.TrimSpace(programPaths[client]) == "" {
			programPaths[client] = detectClientProgramPath(client, homeDir)
		}
	}
	cfg.ClientConfigPaths = configPaths
	cfg.ClientProgramPaths = programPaths
	cfg.ClientAutoRestart = autoRestart
	cfg.ClientAutoStart = autoStart
	cfg.ClientPathsDetected = true
	cfg.ClientPathDetectionVersion = currentClientPathDetectionVersion
	return cfg
}

func detectClientConfigPath(client, homeDir string) string {
	candidates := make([]string, 0, 5)
	switch normalizeClientProgramID(client) {
	case clientCodex:
		candidates = append(candidates, defaultClientConfigPath(client, homeDir))
	case clientOpenCode:
		if override := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG")); override != "" {
			candidates = append(candidates, resolveClientPath(override, homeDir))
		}
		candidates = append(candidates, defaultClientConfigPath(client, homeDir))
		if appData := clientAppDataDir(homeDir); appData != "" {
			candidates = append(candidates, filepath.Join(appData, "opencode", "opencode.json"))
		}
	case clientClaudeCode:
		candidates = append(candidates, defaultClientConfigPath(client, homeDir))
	case clientClaudeCLI:
		candidates = append(candidates, defaultClientConfigPath(client, homeDir))
	case clientOpenClaw:
		candidates = append(candidates, openClawConfigPath(homeDir))
	}
	if path := firstExistingFile(candidates...); path != "" {
		return path
	}
	return defaultClientConfigPath(client, homeDir)
}

func claudeDesktopConfigLibraryDir(homeDir string) string {
	if runtime.GOOS == "windows" {
		if value := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); value != "" {
			return filepath.Join(value, "Claude-3p", "configLibrary")
		}
		return filepath.Join(homeDir, "AppData", "Local", "Claude-3p", "configLibrary")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(homeDir, "Library", "Application Support", "Claude-3p", "configLibrary")
	}
	return filepath.Join(homeDir, ".config", "Claude-3p", "configLibrary")
}

func claudeDesktopConfigPath(homeDir string) string {
	dir := claudeDesktopConfigLibraryDir(homeDir)
	meta, err := readJSONMap(filepath.Join(dir, "_meta.json"))
	if err == nil {
		if id, ok := meta["appliedId"].(string); ok && strings.TrimSpace(id) != "" {
			return filepath.Join(dir, strings.TrimSpace(id)+".json")
		}
	}
	return filepath.Join(dir, "vision-relay.json")
}

func isClaudeCLIConfigPath(path string) bool {
	path = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "/", `\`))
	return strings.HasSuffix(path, `\.claude\settings.json`) ||
		strings.HasSuffix(path, `\claude\settings.json`) ||
		strings.Contains(path, `\claude_config_dir\`)
}

func detectClientProgramPath(client, homeDir string) string {
	client = normalizeClientProgramID(client)
	if path := firstExistingFile(clientProgramCandidates(client, homeDir)...); path != "" {
		return path
	}
	command := map[string]string{
		clientCodexCLI:  "codex",
		clientOpenCode:  "opencode",
		clientClaudeCLI: "claude",
		clientOpenClaw:  "openclaw",
	}[client]
	if command == "" {
		return ""
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return ""
	}
	path, _ = filepath.Abs(path)
	return path
}

func clientProgramCandidates(client, homeDir string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" && strings.TrimSpace(homeDir) != "" {
		localAppData = filepath.Join(homeDir, "AppData", "Local")
	}
	if localAppData == "" {
		return nil
	}
	switch client {
	case clientCodex:
		return append(codexStoreClientProgramCandidates(), []string{
			filepath.Join(localAppData, "Programs", "OpenAI Codex", "ChatGPT.exe"),
			filepath.Join(localAppData, "Programs", "OpenAI Codex", "Codex.exe"),
			filepath.Join(localAppData, "Programs", "Codex", "Codex.exe"),
			filepath.Join(localAppData, "OpenAI", "Codex", "Codex.exe"),
		}...)
	case clientOpenCode:
		return []string{
			filepath.Join(localAppData, "Programs", "@opencode-aidesktop", "OpenCode.exe"),
			filepath.Join(localAppData, "Programs", "OpenCode", "OpenCode.exe"),
			filepath.Join(localAppData, "OpenCode", "OpenCode.exe"),
		}
	case clientClaudeCode:
		return claudeDesktopProgramCandidates(localAppData)
	default:
		return nil
	}
}

func claudeDesktopProgramCandidates(localAppData string) []string {
	installRoot := filepath.Join(localAppData, "AnthropicClaude")
	candidates := []string{
		// The Squirrel launcher is stable across application updates and starts
		// the newest app-* version. Process matching handles its versioned child.
		filepath.Join(installRoot, "claude.exe"),
		filepath.Join(localAppData, "Programs", "Claude", "Claude.exe"),
		filepath.Join(localAppData, "Anthropic", "Claude", "Claude.exe"),
		filepath.Join(localAppData, "Claude", "Claude.exe"),
	}
	versioned, _ := filepath.Glob(filepath.Join(installRoot, "app-*", "claude.exe"))
	sort.Slice(versioned, func(i, j int) bool {
		return strings.ToLower(versioned[i]) > strings.ToLower(versioned[j])
	})
	return append(candidates, versioned...)
}

func isClaudeCLIProgramPath(programPath string) bool {
	programPath = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(programPath), "/", `\`))
	programPath = strings.ReplaceAll(programPath, `\\?\`, "")
	if programPath == "" {
		return false
	}
	switch strings.ToLower(filepath.Ext(programPath)) {
	case ".cmd", ".bat", ".ps1":
		return true
	}
	return strings.Contains(programPath, `\node_modules\@anthropic-ai\claude-code\`) ||
		strings.Contains(programPath, `\appdata\roaming\npm\claude`) ||
		strings.Contains(programPath, `\.local\bin\claude`)
}

func codexStoreExecutableCandidates(packageRoots []string) []string {
	roots := make([]string, 0, len(packageRoots))
	seen := map[string]bool{}
	for _, root := range packageRoots {
		root = filepath.Clean(strings.TrimSpace(root))
		key := strings.ToLower(root)
		if root == "." || seen[key] {
			continue
		}
		seen[key] = true
		roots = append(roots, root)
	}
	// Package versions are embedded in the folder name. Descending lexical
	// order selects the newest registered package in normal Store installs.
	sort.Slice(roots, func(i, j int) bool {
		return strings.ToLower(roots[i]) > strings.ToLower(roots[j])
	})
	candidates := make([]string, 0, len(roots)*2)
	for _, root := range roots {
		candidates = append(candidates,
			filepath.Join(root, "app", "ChatGPT.exe"),
			filepath.Join(root, "app", "Codex.exe"),
		)
	}
	return candidates
}

func codexDesktopFromCLI(cliPath string) string {
	if strings.TrimSpace(cliPath) == "" {
		return ""
	}
	appDir := filepath.Dir(filepath.Dir(cliPath))
	return firstExistingFile(
		filepath.Join(appDir, "ChatGPT.exe"),
		filepath.Join(appDir, "Codex.exe"),
	)
}

func firstExistingFile(paths ...string) string {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			if abs, absErr := filepath.Abs(path); absErr == nil {
				return abs
			}
			return filepath.Clean(path)
		}
	}
	return ""
}

func clientAppDataDir(homeDir string) string {
	if value := strings.TrimSpace(os.Getenv("APPDATA")); value != "" {
		return value
	}
	if runtime.GOOS == "windows" && strings.TrimSpace(homeDir) != "" {
		return filepath.Join(homeDir, "AppData", "Roaming")
	}
	return ""
}

func localAPIEnabled(cfg config) bool {
	return cfg.LocalAPIEnabled == nil || *cfg.LocalAPIEnabled
}

func (a *app) handleClientPathDetection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	cfg := detectClientPaths(a.currentConfig(), homeDir, true)
	if err := a.setConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": a.currentConfig()})
}
