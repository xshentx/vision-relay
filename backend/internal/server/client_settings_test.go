package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeListenAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "IPv4", input: "127.0.0.1:8787", want: "127.0.0.1:8787"},
		{name: "IPv6", input: "[::1]:8787", want: "[::1]:8787"},
		{name: "wildcard host", input: ":8787", want: ":8787"},
		{name: "missing port", input: "127.0.0.1", wantErr: true},
		{name: "port too low", input: "127.0.0.1:0", wantErr: true},
		{name: "port too high", input: "127.0.0.1:65536", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeListenAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeListenAddress(%q) unexpectedly succeeded with %q", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("normalizeListenAddress(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLegacyConfigGetsOneTimeClientPathDetection(t *testing.T) {
	home := t.TempDir()
	clearClientPathEnvironment(t)

	cfg := mergeConfig(defaultConfig(), config{})
	if cfg.ClientPathsDetected {
		t.Fatal("legacy config should be treated as not yet detected")
	}
	cfg = detectClientPaths(cfg, home, true)
	if !cfg.ClientPathsDetected {
		t.Fatal("path detection should mark the migration complete")
	}
	for _, client := range clientRouteOrder {
		if strings.TrimSpace(cfg.ClientConfigPaths[client]) == "" {
			t.Fatalf("default config path for %s was not populated", client)
		}
	}
}

func TestDetectClientConfigPathPrefersExistingCodexConfig(t *testing.T) {
	home := t.TempDir()
	clearClientPathEnvironment(t)
	want := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("model = \"gpt-5.5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := detectClientConfigPath(clientCodex, home)
	if got != want {
		t.Fatalf("detected Codex config = %q, want %q", got, want)
	}
}

func TestCodexDesktopFromCLI(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "app", "resources", "codex.exe")
	desktop := filepath.Join(root, "app", "ChatGPT.exe")
	if err := os.MkdirAll(filepath.Dir(cli), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cli, desktop} {
		if err := os.WriteFile(path, []byte("test"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if got := codexDesktopFromCLI(cli); got != desktop {
		t.Fatalf("Codex desktop path = %q, want %q", got, desktop)
	}
}

func TestCodexStoreExecutableCandidates(t *testing.T) {
	oldRoot := filepath.Join(`C:\Program Files\WindowsApps`, `OpenAI.Codex_26.1.0.0_x64__2p2nqsd0c76g0`)
	newRoot := filepath.Join(`C:\Program Files\WindowsApps`, `OpenAI.Codex_26.707.9981.0_x64__2p2nqsd0c76g0`)
	got := codexStoreExecutableCandidates([]string{oldRoot, newRoot, newRoot, ""})
	want := []string{
		filepath.Join(newRoot, "app", "ChatGPT.exe"),
		filepath.Join(newRoot, "app", "Codex.exe"),
		filepath.Join(oldRoot, "app", "ChatGPT.exe"),
		filepath.Join(oldRoot, "app", "Codex.exe"),
	}
	if len(got) != len(want) {
		t.Fatalf("Store candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Store candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPathDetectionRevisionPreservesCustomPathsAndFillsMissingValues(t *testing.T) {
	home := t.TempDir()
	clearClientPathEnvironment(t)
	custom := filepath.Join(home, "custom", "opencode.json")
	cfg := defaultConfig()
	cfg.ClientPathsDetected = true
	cfg.ClientPathDetectionVersion = currentClientPathDetectionVersion - 1
	cfg.ClientConfigPaths = map[string]string{clientOpenCode: custom}
	cfg = detectClientPaths(cfg, home, false)

	if cfg.ClientConfigPaths[clientOpenCode] != custom {
		t.Fatalf("custom path was overwritten: %q", cfg.ClientConfigPaths[clientOpenCode])
	}
	if cfg.ClientConfigPaths[clientCodex] == "" {
		t.Fatal("missing Codex config path was not filled")
	}
	if cfg.ClientPathDetectionVersion != currentClientPathDetectionVersion {
		t.Fatalf("detection version = %d, want %d", cfg.ClientPathDetectionVersion, currentClientPathDetectionVersion)
	}
}

func TestConfigureClientRouteUsesCustomOpenCodePath(t *testing.T) {
	home := t.TempDir()
	customPath := filepath.Join(home, "custom", "opencode.json")
	cfg := defaultConfig()
	cfg.ClientConfigPaths = map[string]string{clientOpenCode: customPath}
	a := &app{
		cfg:        cfg,
		configPath: filepath.Join(home, "vision-relay.json"),
	}

	result, err := a.configureClientRoute(clientOpenCode, "", "http://127.0.0.1:8787", home)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != customPath {
		t.Fatalf("configured path = %q, want %q", result.Path, customPath)
	}
	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("custom OpenCode config was not written: %v", err)
	}
	defaultPath := defaultClientConfigPath(clientOpenCode, home)
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Fatalf("default OpenCode path should not be written, stat err: %v", err)
	}
}

func TestDisabledLocalAPIStillServesManagementUI(t *testing.T) {
	cfg := defaultConfig()
	cfg.LocalAPIEnabled = boolPtr(false)
	a := &app{cfg: cfg}

	apiRecorder := httptest.NewRecorder()
	a.handleRoute(apiRecorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if apiRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled local API status = %d, want %d", apiRecorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(apiRecorder.Body.String(), "local API interface is disabled") {
		t.Fatalf("unexpected disabled API response: %s", apiRecorder.Body.String())
	}

	uiRecorder := httptest.NewRecorder()
	a.handleRoute(uiRecorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if uiRecorder.Code != http.StatusOK {
		t.Fatalf("management UI status = %d, want %d", uiRecorder.Code, http.StatusOK)
	}
}

func clearClientPathEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CODEX_HOME",
		"OPENCODE_CONFIG",
		"CLAUDE_CONFIG_DIR",
		"OPENCLAW_CONFIG_PATH",
		"OPENCLAW_STATE_DIR",
		"OPENCLAW_HOME",
		"APPDATA",
		"LOCALAPPDATA",
	} {
		t.Setenv(key, "")
	}
}
