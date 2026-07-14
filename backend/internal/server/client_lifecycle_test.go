package server

import (
	"errors"
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
	for _, client := range clientRouteOrder {
		if !cfg.ClientAutoRestart[client] {
			t.Fatalf("%s should auto restart by default: %#v", client, cfg.ClientAutoRestart)
		}
		if cfg.ClientAutoStart[client] {
			t.Fatalf("%s should stay closed by default: %#v", client, cfg.ClientAutoStart)
		}
	}
	merged := mergeConfig(defaultConfig(), config{Addr: "127.0.0.1:8787"})
	for _, client := range clientRouteOrder {
		if !merged.ClientAutoRestart[client] || merged.ClientAutoStart[client] {
			t.Fatalf("legacy config defaults were not preserved for %s: restart=%#v start=%#v", client, merged.ClientAutoRestart, merged.ClientAutoStart)
		}
	}
}
