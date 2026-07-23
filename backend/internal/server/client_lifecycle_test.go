package server

import (
	"errors"
	"path/filepath"
	"regexp"
	"testing"
)

type recordingClientProgramController struct {
	running      bool
	isRunningErr error
	stopErr      error
	startErr     error
	stopCalls    int
	startCalls   int
	lastClient   string
	lastPath     string
	lastWorkDir  string
}

func (c *recordingClientProgramController) IsRunning(client, programPath string) (bool, error) {
	c.lastClient = client
	c.lastPath = programPath
	return c.running, c.isRunningErr
}

func (c *recordingClientProgramController) Stop(client, programPath string) (bool, error) {
	c.stopCalls++
	c.lastClient = client
	c.lastPath = programPath
	if c.stopErr != nil {
		return false, c.stopErr
	}
	c.running = false
	return true, nil
}

func (c *recordingClientProgramController) Start(client, programPath, workDir string) error {
	c.startCalls++
	c.lastClient = client
	c.lastPath = programPath
	c.lastWorkDir = workDir
	if c.startErr != nil {
		return c.startErr
	}
	c.running = true
	return nil
}

func TestApplyClientProgramBehavior(t *testing.T) {
	tests := []struct {
		name                string
		running             bool
		autoRestart         bool
		autoStart           bool
		wantAction          string
		wantStopCalls       int
		wantStartCalls      int
		wantRestarted       bool
		wantStarted         bool
		wantRestartRequired bool
	}{
		{name: "restart running client", running: true, autoRestart: true, wantAction: "restarted", wantStopCalls: 1, wantStartCalls: 1, wantRestarted: true, wantStarted: true},
		{name: "keep stopped client closed", autoRestart: true, wantAction: "not-running"},
		{name: "start stopped client when enabled", autoRestart: true, autoStart: true, wantAction: "started", wantStartCalls: 1, wantStarted: true},
		{name: "require manual restart when disabled", running: true, wantAction: "restart-required", wantRestartRequired: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &recordingClientProgramController{running: tt.running}
			result := applyClientProgramBehavior(controller, clientCodex, "C:/Apps/Codex/ChatGPT.exe", "C:/work", tt.autoRestart, tt.autoStart)
			if result.Action != tt.wantAction || controller.stopCalls != tt.wantStopCalls || controller.startCalls != tt.wantStartCalls || result.Restarted != tt.wantRestarted || result.Started != tt.wantStarted || result.RestartRequired != tt.wantRestartRequired {
				t.Fatalf("unexpected result: %#v, controller: %#v", result, controller)
			}
		})
	}
}

func TestApplyClientProgramBehaviorReportsRestartFailure(t *testing.T) {
	controller := &recordingClientProgramController{running: true, startErr: errors.New("launch failed")}
	result := applyClientProgramBehavior(controller, clientCodex, "C:/Apps/Codex/ChatGPT.exe", "", true, false)
	if !result.Stopped || result.Restarted || !result.RestartRequired || result.Warning == "" {
		t.Fatalf("restart failure should request manual recovery: %#v", result)
	}
}

func TestClientBehaviorDefaultsAndLegacyMerge(t *testing.T) {
	cfg := defaultConfig()
	for _, client := range clientProgramOrder {
		if !cfg.ClientAutoRestart[client] {
			t.Fatalf("%s should auto restart by default: %#v", client, cfg.ClientAutoRestart)
		}
		if cfg.ClientAutoStart[client] {
			t.Fatalf("%s should stay closed by default: %#v", client, cfg.ClientAutoStart)
		}
	}
	merged := mergeConfig(defaultConfig(), config{Addr: "127.0.0.1:8787"})
	for _, client := range clientProgramOrder {
		if !merged.ClientAutoRestart[client] || merged.ClientAutoStart[client] {
			t.Fatalf("legacy config defaults were not preserved for %s: restart=%#v start=%#v", client, merged.ClientAutoRestart, merged.ClientAutoStart)
		}
	}
}

func TestClientProgramTargetsIncludeDesktopAndCLI(t *testing.T) {
	tests := map[string][]string{
		clientCodex:      {clientCodex, clientCodexCLI},
		clientClaudeCode: {clientClaudeCode, clientClaudeCLI},
		clientOpenCode:   {clientOpenCode},
		clientOpenClaw:   {clientOpenClaw},
	}
	for client, want := range tests {
		got := clientProgramTargets(client)
		if len(got) != len(want) {
			t.Fatalf("clientProgramTargets(%q) = %#v, want %#v", client, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("clientProgramTargets(%q) = %#v, want %#v", client, got, want)
			}
		}
	}
}

func TestDarwinAppBundleAndProcessMatchingHelpers(t *testing.T) {
	programPath := filepath.Join(string(filepath.Separator), "Applications", "Claude.app", "Contents", "MacOS", "Claude")
	wantBundle := filepath.Join(string(filepath.Separator), "Applications", "Claude.app")
	if got := darwinAppBundlePath(programPath); got != wantBundle {
		t.Fatalf("darwinAppBundlePath(%q) = %q, want %q", programPath, got, wantBundle)
	}
	if got := darwinAppBundlePath(filepath.Join(string(filepath.Separator), "usr", "local", "bin", "claude")); got != "" {
		t.Fatalf("CLI path was treated as an app bundle: %q", got)
	}

	names := darwinClientProcessNames(clientClaudeCode, programPath)
	if len(names) != 1 || names[0] != "Claude" {
		t.Fatalf("Claude desktop process names = %#v, want [Claude]", names)
	}
	cliNames := darwinClientProcessNames(clientClaudeCLI, filepath.Join(string(filepath.Separator), "opt", "homebrew", "bin", "claude"))
	if len(cliNames) != 1 || cliNames[0] != "claude" {
		t.Fatalf("Claude CLI process names = %#v, want [claude]", cliNames)
	}

	pattern := regexp.MustCompile(darwinExactCommandMarkerPattern(programPath))
	if !pattern.MatchString(`"` + programPath + `" --disable-gpu`) {
		t.Fatalf("process marker pattern did not match quoted app executable: %s", pattern)
	}
	if pattern.MatchString(programPath + "Helper --type=renderer") {
		t.Fatalf("process marker pattern matched a different helper executable: %s", pattern)
	}
	if markers := darwinClientProcessMarkers(programPath); len(markers) != 0 {
		t.Fatalf("app bundle should be matched by process name, got markers %#v", markers)
	}
	if markers := darwinClientProcessMarkers(filepath.Join(string(filepath.Separator), "usr", "local", "bin", "claude")); len(markers) == 0 {
		t.Fatal("CLI executable should retain command-line path matching")
	}
}
