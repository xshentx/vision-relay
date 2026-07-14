package server

import (
	"fmt"
	"path/filepath"
	"strings"
)

type clientProgramController interface {
	IsRunning(client, programPath string) (bool, error)
	Stop(client, programPath string) (bool, error)
	Start(client, programPath, workDir string) error
}

type systemClientProgramController struct{}

func (systemClientProgramController) IsRunning(client, programPath string) (bool, error) {
	return platformClientProgramRunning(client, programPath)
}

func (systemClientProgramController) Stop(client, programPath string) (bool, error) {
	return platformStopClientProgram(client, programPath)
}

func (systemClientProgramController) Start(client, programPath, workDir string) error {
	return platformStartClientProgram(programPath, workDir)
}

type clientProgramActionResult struct {
	ProgramPath     string `json:"program_path,omitempty"`
	WasRunning      bool   `json:"was_running"`
	AutoRestart     bool   `json:"auto_restart"`
	AutoStart       bool   `json:"auto_start"`
	Stopped         bool   `json:"stopped"`
	Started         bool   `json:"started"`
	Restarted       bool   `json:"restarted"`
	RestartRequired bool   `json:"restart_required"`
	Action          string `json:"program_action"`
	Warning         string `json:"program_warning,omitempty"`
}

func (a *app) configuredProgramController() clientProgramController {
	if a.clientProgramController != nil {
		return a.clientProgramController
	}
	return systemClientProgramController{}
}

func configuredClientProgramPath(cfg config, client, homeDir string) string {
	client = normalizeClientID(client)
	configuredPath := strings.TrimSpace(cfg.ClientProgramPaths[client])
	if configuredPath != "" {
		configuredPath = resolveClientPath(configuredPath, homeDir)
	}
	if existingPath := firstExistingFile(configuredPath); existingPath != "" {
		return existingPath
	}
	if detectedPath := detectClientProgramPath(client, homeDir); detectedPath != "" {
		return detectedPath
	}
	return configuredPath
}

func applyClientProgramBehavior(controller clientProgramController, client, programPath, workDir string, autoRestart, autoStart bool) clientProgramActionResult {
	result := clientProgramActionResult{
		ProgramPath: strings.TrimSpace(programPath),
		AutoRestart: autoRestart,
		AutoStart:   autoStart,
		Action:      "none",
	}
	if controller == nil {
		result.Warning = "客户端程序控制器不可用"
		return result
	}

	running, err := controller.IsRunning(client, result.ProgramPath)
	if err != nil {
		result.Warning = fmt.Sprintf("检测客户端运行状态失败：%v", err)
		result.Action = "error"
		return result
	}
	result.WasRunning = running

	if !running {
		if !autoStart {
			result.Action = "not-running"
			return result
		}
		if result.ProgramPath == "" {
			result.Action = "error"
			result.Warning = "未检测到客户端程序位置，无法自动启动"
			return result
		}
		if err := controller.Start(client, result.ProgramPath, workDir); err != nil {
			result.Action = "error"
			result.Warning = fmt.Sprintf("自动启动客户端失败：%v", err)
			return result
		}
		result.Started = true
		result.Action = "started"
		return result
	}

	if !autoRestart {
		result.RestartRequired = true
		result.Action = "restart-required"
		return result
	}

	stopped, err := controller.Stop(client, result.ProgramPath)
	if err != nil {
		result.RestartRequired = true
		result.Action = "restart-required"
		result.Warning = fmt.Sprintf("自动停止客户端失败：%v", err)
		return result
	}
	result.Stopped = stopped
	if result.ProgramPath == "" {
		result.RestartRequired = true
		result.Action = "restart-required"
		result.Warning = "客户端已停止，但未检测到程序位置，请手动启动"
		return result
	}
	if err := controller.Start(client, result.ProgramPath, workDir); err != nil {
		result.RestartRequired = true
		result.Action = "restart-required"
		result.Warning = fmt.Sprintf("客户端已停止，但自动重新启动失败：%v", err)
		return result
	}
	result.Started = true
	result.Restarted = true
	result.Action = "restarted"
	return result
}

func normalizeClientWorkDir(workDir, programPath string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir != "" {
		if info, err := filepath.Abs(workDir); err == nil {
			return info
		}
		return workDir
	}
	if programPath = strings.TrimSpace(programPath); programPath != "" {
		return filepath.Dir(programPath)
	}
	return ""
}
