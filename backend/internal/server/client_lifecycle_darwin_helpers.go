package server

import (
	"path/filepath"
	"regexp"
	"strings"
)

func darwinClientProcessNames(client, programPath string) []string {
	client = normalizeClientProgramID(client)
	var names []string
	if base := filepath.Base(strings.TrimSpace(programPath)); base != "" && base != "." && base != string(filepath.Separator) {
		names = append(names, base)
	}
	if darwinAppBundlePath(programPath) != "" || strings.TrimSpace(programPath) == "" {
		switch client {
		case clientCodex:
			names = append(names, "Codex", "ChatGPT")
		case clientOpenCode:
			names = append(names, "OpenCode", "opencode")
		case clientClaudeCode:
			names = append(names, "Claude")
		case clientOpenClaw:
			names = append(names, "OpenClaw", "openclaw")
		}
	}
	return uniqueDarwinStrings(names)
}

func darwinExactCommandMarkerPattern(marker string) string {
	return `(^|[[:space:]"'])` + regexp.QuoteMeta(strings.TrimSpace(marker)) + `([[:space:]"']|$)`
}

func darwinClientProcessMarkers(programPath string) []string {
	programPath = strings.TrimSpace(programPath)
	// Electron application helpers often begin with the main executable path
	// (for example "Claude Helper"). Match app bundles by their exact process
	// names instead so status checks and stop actions never include helpers.
	if programPath == "" || darwinAppBundlePath(programPath) != "" {
		return nil
	}
	markers := []string{filepath.Clean(programPath)}
	if resolved, err := filepath.EvalSymlinks(programPath); err == nil {
		markers = append(markers, filepath.Clean(resolved))
	}
	return uniqueDarwinStrings(markers)
}

func darwinAppBundlePath(programPath string) string {
	programPath = filepath.Clean(strings.TrimSpace(programPath))
	if programPath == "" || programPath == "." {
		return ""
	}
	lower := strings.ToLower(programPath)
	if strings.HasSuffix(lower, ".app") {
		return programPath
	}
	separator := string(filepath.Separator)
	if index := strings.Index(lower, ".app"+separator); index >= 0 {
		return programPath[:index+len(".app")]
	}
	return ""
}

func uniqueDarwinStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}
