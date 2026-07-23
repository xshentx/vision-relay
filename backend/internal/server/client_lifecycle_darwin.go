//go:build darwin

package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func platformClientProgramRunning(client, programPath string) (bool, error) {
	processes, err := matchingDarwinClientProcesses(client, programPath)
	return len(processes) > 0, err
}

func platformStopClientProgram(client, programPath string) (bool, error) {
	processes, err := matchingDarwinClientProcesses(client, programPath)
	if err != nil || len(processes) == 0 {
		return false, err
	}

	if bundle := darwinAppBundlePath(programPath); bundle != "" {
		// Give desktop applications a chance to flush their state before falling
		// back to POSIX signals. AppleScript is intentionally best-effort because
		// accessibility automation can be disabled by the user.
		_ = quitDarwinApplication(filepath.Base(strings.TrimSuffix(bundle, ".app")))
		if stopped, waitErr := waitForDarwinClientProcessesStopped(client, programPath, 3*time.Second); stopped || waitErr != nil {
			return stopped, waitErr
		}
		// Refresh after the graceful-quit wait. Do not signal stale PIDs if the
		// application exited and the operating system reused an identifier.
		processes, err = matchingDarwinClientProcesses(client, programPath)
		if err != nil {
			return false, err
		}
		if len(processes) == 0 {
			return true, nil
		}
	}

	for _, pid := range processes {
		_ = signalDarwinProcess(pid, syscall.SIGTERM)
	}
	if stopped, waitErr := waitForDarwinClientProcessesStopped(client, programPath, 3*time.Second); stopped || waitErr != nil {
		return stopped, waitErr
	}

	remaining, err := matchingDarwinClientProcesses(client, programPath)
	if err != nil {
		return false, err
	}
	for _, pid := range remaining {
		_ = signalDarwinProcess(pid, syscall.SIGKILL)
	}
	if stopped, waitErr := waitForDarwinClientProcessesStopped(client, programPath, 2*time.Second); stopped || waitErr != nil {
		return stopped, waitErr
	}
	remaining, err = matchingDarwinClientProcesses(client, programPath)
	if err != nil {
		return false, err
	}
	pids := make([]string, 0, len(remaining))
	for _, pid := range remaining {
		pids = append(pids, strconv.Itoa(pid))
	}
	return false, fmt.Errorf("仍有客户端进程未退出（PID %s），可能与 Vision Relay 的运行权限不同", strings.Join(pids, ", "))
}

func platformStartClientProgram(programPath, workDir string) error {
	programPath = strings.TrimSpace(programPath)
	if programPath == "" {
		return errors.New("未配置客户端程序位置")
	}
	info, err := os.Stat(programPath)
	if err != nil {
		return err
	}
	bundle := darwinAppBundlePath(programPath)
	if info.IsDir() && bundle == "" {
		return errors.New("客户端程序位置必须是可执行文件或 macOS 应用程序")
	}

	if bundle != "" {
		cmd := exec.Command("open", bundle)
		if workDir = normalizeClientWorkDir(workDir, programPath); workDir != "" && !info.IsDir() {
			cmd.Dir = workDir
		}
		cmd.Env = os.Environ()
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("通过 Launch Services 启动客户端失败: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}

	cmd := exec.Command(programPath)
	if workDir = normalizeClientWorkDir(workDir, programPath); workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func matchingDarwinClientProcesses(client, programPath string) ([]int, error) {
	pids := map[int]bool{}
	for _, marker := range darwinClientProcessMarkers(programPath) {
		matches, err := darwinPgrep("-f", darwinExactCommandMarkerPattern(marker))
		if err != nil {
			return nil, err
		}
		for _, pid := range matches {
			pids[pid] = true
		}
	}
	for _, name := range darwinClientProcessNames(client, programPath) {
		matches, err := darwinPgrep("-x", name)
		if err != nil {
			return nil, err
		}
		for _, pid := range matches {
			pids[pid] = true
		}
	}
	delete(pids, os.Getpid())
	result := make([]int, 0, len(pids))
	for pid := range pids {
		result = append(result, pid)
	}
	sort.Ints(result)
	return result, nil
}

func darwinPgrep(mode, value string) ([]int, error) {
	output, err := exec.Command("pgrep", mode, value).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	fields := strings.Fields(string(output))
	result := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, parseErr := strconv.Atoi(field)
		if parseErr == nil && pid > 0 {
			result = append(result, pid)
		}
	}
	return result, nil
}

func quitDarwinApplication(name string) error {
	name = strings.ReplaceAll(name, `\\`, `\\\\`)
	name = strings.ReplaceAll(name, `"`, `\\"`)
	script := `tell application "` + name + `" to quit`
	return exec.Command("osascript", "-e", script).Run()
}

func signalDarwinProcess(pid int, signal syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(signal)
}

func waitForDarwinClientProcessesStopped(client, programPath string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		processes, err := matchingDarwinClientProcesses(client, programPath)
		if err != nil {
			return false, err
		}
		if len(processes) == 0 {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}
