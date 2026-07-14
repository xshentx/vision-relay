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

const currentClientPathDetectionVersion = 2

func normalizeClientPathMap(paths map[string]string) map[string]string {
	normalized := map[string]string{}
	for client, value := range paths {
		if id := normalizeClientID(client); id != "" {
			normalized[id] = strings.TrimSpace(value)
		}
	}
	return normalized
}

func configuredClientConfigPath(cfg config, client, homeDir string) string {
	client = normalizeClientID(client)
	if path := strings.TrimSpace(cfg.ClientConfigPaths[client]); path != "" {
		return resolveClientPath(path, homeDir)
	}
	return defaultClientConfigPath(client, homeDir)
}

func defaultClientConfigPath(client, homeDir string) string {
	switch normalizeClientID(client) {
	case clientCodex:
		return filepath.Join(codexConfigDir(homeDir), "config.toml")
	case clientOpenCode:
		return filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	case clientClaudeCode:
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
	programPaths := normalizeClientPathMap(cfg.ClientProgramPaths)
	for _, client := range clientRouteOrder {
		if force || strings.TrimSpace(configPaths[client]) == "" {
			configPaths[client] = detectClientConfigPath(client, homeDir)
		}
		if force || strings.TrimSpace(programPaths[client]) == "" {
			programPaths[client] = detectClientProgramPath(client, homeDir)
		}
	}
	cfg.ClientConfigPaths = configPaths
	cfg.ClientProgramPaths = programPaths
	cfg.ClientPathsDetected = true
	cfg.ClientPathDetectionVersion = currentClientPathDetectionVersion
	return cfg
}

func detectClientConfigPath(client, homeDir string) string {
	candidates := make([]string, 0, 5)
	switch normalizeClientID(client) {
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
	case clientOpenClaw:
		candidates = append(candidates, openClawConfigPath(homeDir))
	}
	if path := firstExistingFile(candidates...); path != "" {
		return path
	}
	return defaultClientConfigPath(client, homeDir)
}

func detectClientProgramPath(client, homeDir string) string {
	client = normalizeClientID(client)
	if path := firstExistingFile(clientProgramCandidates(client, homeDir)...); path != "" {
		return path
	}
	command := map[string]string{
		clientCodex:      "codex",
		clientOpenCode:   "opencode",
		clientClaudeCode: "claude",
		clientOpenClaw:   "openclaw",
	}[client]
	path, err := exec.LookPath(command)
	if err != nil {
		return ""
	}
	path, _ = filepath.Abs(path)
	if client == clientCodex {
		if desktopPath := codexDesktopFromCLI(path); desktopPath != "" {
			return desktopPath
		}
	}
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
	default:
		return nil
	}
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
