//go:build !windows && !darwin

package server

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

func platformClientProgramRunning(_, _ string) (bool, error) {
	return false, nil
}

func platformStopClientProgram(_, _ string) (bool, error) {
	return false, nil
}

func platformStartClientProgram(programPath, workDir string) error {
	programPath = strings.TrimSpace(programPath)
	if programPath == "" {
		return errors.New("未配置客户端程序位置")
	}
	cmd := exec.Command(programPath)
	if workDir = normalizeClientWorkDir(workDir, programPath); workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
