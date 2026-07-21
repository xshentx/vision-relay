package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func breakArmorTestConfig(homeDir string) config {
	return config{
		ClientConfigPaths:  map[string]string{clientCodex: filepath.Join(homeDir, ".codex", "config.toml")},
		ClientProgramPaths: map[string]string{},
		ClientAutoRestart:  map[string]bool{},
		ClientAutoStart:    map[string]bool{},
	}
}

func testCodexOneClickContext(homeDir, configPath, model string) clientConfigContext {
	return clientConfigContext{
		HomeDir: homeDir, ConfigPath: configPath, Origin: "http://127.0.0.1:8787",
		Provider: "openai", WireAPI: "responses", Model: model,
		ModelMappings: []textModelMapping{{Name: model, Model: model}},
	}
}

func TestRemovedBreakArmorEndpointsReturnNotFoundInsteadOfProxying(t *testing.T) {
	a := &app{}
	for _, path := range []string{"/api/break-armor/ai/settings", "/api/break-armor/ai/rewrite", "/api/break-armor/prompt/rewrite", "/api/break-armor/unknown"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		a.handleRoute(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}
}

func TestCodexProfileBreakArmorNeverTouchesOneClickConfig(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	configPath := cfg.ClientConfigPaths[clientCodex]
	original := []byte("model = \"existing-model\"\nmodel_provider = \"existing-provider\"\n\n[model_providers.existing-provider]\nbase_url = \"http://127.0.0.1:9999/v1\"\n")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	status, paths, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v5", Mode: "profile"})
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mode != "profile" || paths.ConfigPath != filepath.Join(homeDir, ".codex", "ctf.config.toml") || paths.PromptPath != filepath.Join(homeDir, ".codex", "prompts", "vision-relay-ctf.md") {
		t.Fatalf("unexpected profile paths: %#v", paths)
	}
	if !status.Broken || !status.ProfileBroken || status.GlobalBroken {
		t.Fatalf("status after profile apply = %#v", status)
	}
	configured, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configured, original) {
		t.Fatalf("profile break armor changed one-click config\ngot:\n%s\nwant:\n%s", configured, original)
	}
	profileRaw, err := os.ReadFile(paths.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(profileRaw), breakArmorCodexBlockBegin) || !strings.Contains(string(profileRaw), "model_instructions_file") {
		t.Fatalf("profile config is incomplete:\n%s", profileRaw)
	}
	manifest, found, err := latestBreakArmorSnapshot(homeDir, breakArmorClientCodex, "profile")
	if err != nil || !found {
		t.Fatalf("latest profile snapshot: found=%t err=%v", found, err)
	}
	if len(manifest.Files) != 2 || manifest.Mode != "profile" {
		t.Fatalf("profile snapshot must contain prompt and profile config: %#v", manifest)
	}
	for _, entry := range manifest.Files {
		if filepath.Clean(entry.Path) == filepath.Clean(configPath) {
			t.Fatalf("profile snapshot captured one-click config: %#v", manifest.Files)
		}
	}
}

func TestCodexProfileAndOneClickConfigDoNotReadWriteOrRestoreEachOther(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	configPath := cfg.ClientConfigPaths[clientCodex]
	if _, err := writeCodexConfig(testCodexOneClickContext(homeDir, configPath, "route-before-break")); err != nil {
		t.Fatal(err)
	}
	beforeBreak, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_, paths, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v35", Mode: "profile"})
	if err != nil {
		t.Fatal(err)
	}
	if raw, readErr := os.ReadFile(configPath); readErr != nil || !bytes.Equal(raw, beforeBreak) {
		t.Fatalf("profile changed one-click config: err=%v", readErr)
	}
	promptBefore, _ := os.ReadFile(paths.PromptPath)
	profileBefore, _ := os.ReadFile(paths.ConfigPath)
	if _, err := writeCodexConfig(testCodexOneClickContext(homeDir, configPath, "route-after-break")); err != nil {
		t.Fatal(err)
	}
	promptAfter, _ := os.ReadFile(paths.PromptPath)
	profileAfter, _ := os.ReadFile(paths.ConfigPath)
	if !bytes.Equal(promptBefore, promptAfter) || !bytes.Equal(profileBefore, profileAfter) {
		t.Fatal("one-click configuration changed Codex break-armor profile files")
	}
	routeAfter, _ := os.ReadFile(configPath)
	if !strings.Contains(string(routeAfter), `model = "route-after-break"`) {
		t.Fatalf("one-click route not written:\n%s", routeAfter)
	}
	if strings.Contains(string(routeAfter), breakArmorCodexBlockBegin) {
		t.Fatalf("profile data leaked into one-click config:\n%s", routeAfter)
	}
	if _, err := restoreBreakArmorSnapshot(homeDir, breakArmorClientCodex, "profile"); err != nil {
		t.Fatal(err)
	}
	routeRestored, _ := os.ReadFile(configPath)
	if !bytes.Equal(routeRestored, routeAfter) {
		t.Fatal("profile restore changed one-click configuration")
	}
	if _, err := os.Stat(paths.PromptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile prompt should be removed: %v", err)
	}
	if _, err := os.Stat(paths.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile config should be removed: %v", err)
	}
}

func TestCodexProfilePreservesAndRestoresExistingProfileSettings(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	paths, _ := breakArmorClientPathsForMode(cfg, homeDir, "codex", "profile")
	originalConfig := []byte("# Team profile\nmodel = \"team-model\"\nsandbox = \"workspace-write\"\n\n[tools]\nweb_search = true\n")
	originalPrompt := []byte("# Team prompt\n")
	if err := os.MkdirAll(filepath.Dir(paths.ConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.PromptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigPath, originalConfig, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PromptPath, originalPrompt, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v5", Mode: "profile", InjectionMode: "append"}); err != nil {
		t.Fatal(err)
	}
	applied, _ := os.ReadFile(paths.ConfigPath)
	for _, want := range []string{`model = "team-model"`, `sandbox = "workspace-write"`, "[tools]", "developer_instructions"} {
		if !strings.Contains(string(applied), want) {
			t.Fatalf("profile setting %q lost:\n%s", want, applied)
		}
	}
	if _, err := restoreBreakArmorSnapshot(homeDir, breakArmorClientCodex, "profile"); err != nil {
		t.Fatal(err)
	}
	restoredConfig, _ := os.ReadFile(paths.ConfigPath)
	restoredPrompt, _ := os.ReadFile(paths.PromptPath)
	if !bytes.Equal(restoredConfig, originalConfig) || !bytes.Equal(restoredPrompt, originalPrompt) {
		t.Fatalf("profile restore differs\nconfig:\n%s\nprompt:\n%s", restoredConfig, restoredPrompt)
	}
}

func TestCodexGlobalBreakArmorPreservesOneClickConfigAndFieldRestore(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	configPath := cfg.ClientConfigPaths[clientCodex]
	original := "developer_instructions = \"team developer rules\"\nmodel_instructions_file = \"team.md\"\nmodel = \"route-before\"\nmodel_provider = \"vision-relay\"\n\n[model_providers.vision-relay]\nbase_url = \"http://127.0.0.1:8787/v1\"\n"
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v5", Mode: "global", InjectionMode: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Broken || !status.GlobalBroken {
		t.Fatalf("global status = %#v", status)
	}
	applied, _ := os.ReadFile(configPath)
	for _, want := range []string{`model = "route-before"`, `model_provider = "vision-relay"`, "[model_providers.vision-relay]", breakArmorCodexBlockBegin} {
		if !strings.Contains(string(applied), want) {
			t.Fatalf("global apply lost %q:\n%s", want, applied)
		}
	}
	if _, err := writeCodexConfig(testCodexOneClickContext(homeDir, configPath, "route-after")); err != nil {
		t.Fatal(err)
	}
	afterOneClick, _ := os.ReadFile(configPath)
	if !strings.Contains(string(afterOneClick), breakArmorCodexBlockBegin) || !strings.Contains(string(afterOneClick), `model = "route-after"`) {
		t.Fatalf("one-click and global break armor did not coexist:\n%s", afterOneClick)
	}
	if err := restoreBreakArmorCodexGlobal(homeDir); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreBreakArmorSnapshot(homeDir, breakArmorClientCodex, "global"); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(configPath)
	for _, want := range []string{`developer_instructions = "team developer rules"`, `model_instructions_file = "team.md"`, `model = "route-after"`} {
		if !strings.Contains(string(restored), want) {
			t.Fatalf("global restore lost %q:\n%s", want, restored)
		}
	}
	if strings.Contains(string(restored), breakArmorCodexBlockBegin) {
		t.Fatalf("managed global block remains:\n%s", restored)
	}
}

func TestBreakArmorClientsAreIndependent(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	if _, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "claude", Template: "v5"}); err != nil {
		t.Fatal(err)
	}
	claude, _ := breakArmorStatus(cfg, homeDir, "claude")
	openCode, _ := breakArmorStatus(cfg, homeDir, "opencode")
	codex, _ := breakArmorStatus(cfg, homeDir, "codex")
	if !claude.Broken || openCode.Broken || codex.Broken {
		t.Fatalf("independent states: claude=%t opencode=%t codex=%t", claude.Broken, openCode.Broken, codex.Broken)
	}
	if _, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "opencode", Template: "v35"}); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreBreakArmorSnapshot(homeDir, breakArmorClientClaude, "workspace"); err != nil {
		t.Fatal(err)
	}
	claude, _ = breakArmorStatus(cfg, homeDir, "claude")
	openCode, _ = breakArmorStatus(cfg, homeDir, "opencode")
	if claude.Broken || !openCode.Broken {
		t.Fatalf("restoring Claude changed OpenCode: claude=%t opencode=%t", claude.Broken, openCode.Broken)
	}
}

func TestRepeatedBreakArmorApplyKeepsOriginalRestorePoint(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	promptPath := filepath.Join(homeDir, ".opencode-ctf-workspace", "AGENTS.md")
	original := []byte("# Original OpenCode rules\n")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "opencode", Template: "v5"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "opencode", Template: "v35"}); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreBreakArmorSnapshot(homeDir, breakArmorClientOpenCode, "workspace"); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(promptPath)
	if !bytes.Equal(restored, original) {
		t.Fatalf("repeated apply replaced original restore point: %s", restored)
	}
}

func TestBreakArmorPreviewIsReadOnlyAndExplainsIsolation(t *testing.T) {
	if _, err := breakArmorPrompt("custom", "  "); err == nil {
		t.Fatal("empty custom prompt should be rejected")
	}
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	preview, err := breakArmorPreviewFor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "custom", CustomPrompt: "# Internal test", Mode: "profile"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.SelectedTemplate != breakArmorTemplateCustom || !strings.Contains(preview.Diff, "Internal test") {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	if !strings.Contains(preview.ConfigPreview, "客户端一键配置：不修改") {
		t.Fatalf("preview does not explain isolation: %s", preview.ConfigPreview)
	}
	if _, err := os.Stat(preview.PromptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote prompt: %v", err)
	}
	if _, err := os.Stat(preview.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote profile config: %v", err)
	}
	if _, err := os.Stat(cfg.ClientConfigPaths[clientCodex]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote one-click config: %v", err)
	}
}

func TestCodexBreakArmorPathsFollowConfiguredConfigDirectory(t *testing.T) {
	homeDir := t.TempDir()
	configRoot := filepath.Join(homeDir, "custom-codex-home")
	cfg := breakArmorTestConfig(homeDir)
	cfg.ClientConfigPaths[clientCodex] = filepath.Join(configRoot, "config.toml")

	profile, err := breakArmorClientPathsForMode(cfg, homeDir, breakArmorClientCodex, "profile")
	if err != nil {
		t.Fatal(err)
	}
	if profile.ConfigPath != filepath.Join(configRoot, "ctf.config.toml") || profile.PromptPath != filepath.Join(configRoot, "prompts", "vision-relay-ctf.md") {
		t.Fatalf("profile paths do not follow configured Codex root: %#v", profile)
	}
	global, err := breakArmorClientPathsForMode(cfg, homeDir, breakArmorClientCodex, "global")
	if err != nil {
		t.Fatal(err)
	}
	if global.ConfigPath != cfg.ClientConfigPaths[clientCodex] || global.PromptPath != profile.PromptPath {
		t.Fatalf("global paths do not share the configured Codex root: %#v", global)
	}
}

func TestCodexProfileAndGlobalModesAreMutuallyExclusiveWithoutChangingOneClickRoute(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	configPath := cfg.ClientConfigPaths[clientCodex]
	if _, err := writeCodexConfig(testCodexOneClickContext(homeDir, configPath, "route-before")); err != nil {
		t.Fatal(err)
	}

	profileStatus, profilePaths, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v5", Mode: "profile"})
	if err != nil {
		t.Fatal(err)
	}
	if !profileStatus.ProfileBroken || profileStatus.GlobalBroken {
		t.Fatalf("unexpected profile status: %#v", profileStatus)
	}

	globalStatus, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v35", Mode: "global"})
	if err != nil {
		t.Fatal(err)
	}
	if globalStatus.ProfileBroken || !globalStatus.GlobalBroken {
		t.Fatalf("profile/global were not made exclusive: %#v", globalStatus)
	}
	profileRaw, readErr := os.ReadFile(profilePaths.ConfigPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	if strings.Contains(string(profileRaw), breakArmorCodexBlockBegin) {
		t.Fatalf("profile managed block remained after global apply: %s", profileRaw)
	}
	if _, err := writeCodexConfig(testCodexOneClickContext(homeDir, configPath, "route-after")); err != nil {
		t.Fatal(err)
	}

	profileStatus, _, err = applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v5", Mode: "profile"})
	if err != nil {
		t.Fatal(err)
	}
	if !profileStatus.ProfileBroken || profileStatus.GlobalBroken {
		t.Fatalf("global/profile were not made exclusive: %#v", profileStatus)
	}
	oneClickRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(oneClickRaw), `model = "route-after"`) || strings.Contains(string(oneClickRaw), breakArmorCodexBlockBegin) {
		t.Fatalf("mode switching changed or contaminated one-click route: %s", oneClickRaw)
	}
}

func TestCodexGlobalReapplyDiscardsStaleFieldState(t *testing.T) {
	homeDir := t.TempDir()
	cfg := breakArmorTestConfig(homeDir)
	configPath := cfg.ClientConfigPaths[clientCodex]
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("developer_instructions = \"old rules\"\nmodel = \"route\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v5", Mode: "global"}); err != nil {
		t.Fatal(err)
	}
	// Simulate an external restore that removed Vision Relay's managed block.
	if err := os.WriteFile(configPath, []byte("developer_instructions = \"new rules\"\nmodel = \"route\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyBreakArmor(cfg, homeDir, breakArmorRequest{Client: "codex", Template: "v35", Mode: "global"}); err != nil {
		t.Fatal(err)
	}
	if err := restoreBreakArmorCodexGlobal(homeDir); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(restored), `developer_instructions = "new rules"`) || strings.Contains(string(restored), `developer_instructions = "old rules"`) {
		t.Fatalf("stale global state was reused: %s", restored)
	}
}
