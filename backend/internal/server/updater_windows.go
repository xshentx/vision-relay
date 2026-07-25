//go:build windows

package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	updateProcessCreationFlags  = windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
	updateStartupSurvivalWindow = 1500 * time.Millisecond
	// Keep these environment names for cleanup compatibility with releases that
	// used a temporary helper executable. They now carry the previous executable
	// and the verified download; neither file is executed from its staging path.
	updateHelperEnvironment  = "VISION_RELAY_UPDATE_HELPER"
	updatePayloadEnvironment = "VISION_RELAY_UPDATE_PAYLOAD"
	updateWaitPIDEnvironment = "VISION_RELAY_UPDATE_WAIT_PID"
)

var legacyUpdateCleanupName = regexp.MustCompile(`(?i)^\.vision-relay-(?:helper-[0-9]+\.exe|payload-[0-9]+\.download)$`)

type detachedUpdateProcess struct {
	process *os.Process
	exited  <-chan error
}

// installUpdate performs the replacement in the running application. Windows
// permits a running image to be renamed, so a copied helper executable is not
// necessary. This avoids creating and launching a hidden, randomly named EXE,
// a pattern that endpoint-protection products commonly classify as a dropper.
func installUpdate(source, target string, restartArgs []string) (returnErr error) {
	var err error
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("更新载荷不可用: %w", err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return errors.New("更新载荷不是普通文件")
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	if strings.EqualFold(source, target) {
		return errors.New("更新源文件与目标文件相同")
	}

	backup := target + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		return fmt.Errorf("备份旧版本失败: %w", err)
	}
	defer func() {
		if returnErr == nil {
			return
		}
		if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			returnErr = errors.Join(returnErr, fmt.Errorf("删除不完整的新版本失败: %w", removeErr))
		}
		if restoreErr := os.Rename(backup, target); restoreErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("恢复旧版本文件失败: %w", restoreErr))
		}
	}()

	if err := copyExecutable(source, target); err != nil {
		return fmt.Errorf("写入新版本失败: %w", err)
	}
	restartEnv := withUpdateCleanupEnvironment(os.Environ(), backup, source)
	restartEnv = append(restartEnv, updateWaitPIDEnvironment+"="+strconv.Itoa(os.Getpid()))
	process, err := startDetachedUpdateProcess(
		target,
		restartArgs,
		restartEnv,
		currentWorkingDirectory(),
	)
	if err != nil {
		return fmt.Errorf("重启新版本失败: %w", err)
	}
	if err := process.requireSurvival(updateStartupSurvivalWindow); err != nil {
		return fmt.Errorf("新版本启动检查失败: %w", err)
	}
	return nil
}

// startDetachedUpdateProcess keeps the restarted application independent from
// the process that launched it. This matters when Vision Relay is itself in a
// kill-on-close Job Object.
func startDetachedUpdateProcess(path string, args, env []string, dir string) (*detachedUpdateProcess, error) {
	flags := []uint32{
		updateProcessCreationFlags | windows.CREATE_BREAKAWAY_FROM_JOB,
		updateProcessCreationFlags,
	}
	var startErrors []error
	for _, creationFlags := range flags {
		cmd := exec.Command(path, args...)
		cmd.Dir = dir
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: creationFlags,
		}
		if err := cmd.Start(); err != nil {
			startErrors = append(startErrors, err)
			continue
		}
		exited := make(chan error, 1)
		go func() {
			exited <- cmd.Wait()
			close(exited)
		}()
		return &detachedUpdateProcess{process: cmd.Process, exited: exited}, nil
	}
	return nil, errors.Join(startErrors...)
}

func (p *detachedUpdateProcess) requireSurvival(minimum time.Duration) error {
	if p == nil || p.process == nil || p.exited == nil {
		return errors.New("新版本进程句柄不可用")
	}
	timer := time.NewTimer(minimum)
	defer timer.Stop()
	select {
	case err := <-p.exited:
		if err != nil {
			return fmt.Errorf("新版本进程 %d 提前退出: %w", p.process.Pid, err)
		}
		return fmt.Errorf("新版本进程 %d 在启动检查期间提前退出", p.process.Pid)
	case <-timer.C:
		return nil
	}
}

func currentWorkingDirectory() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func withUpdateCleanupEnvironment(env []string, previous, payload string) []string {
	result := make([]string, 0, len(env)+2)
	for _, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(name, updateHelperEnvironment) || strings.EqualFold(name, updatePayloadEnvironment) || strings.EqualFold(name, updateWaitPIDEnvironment)) {
			continue
		}
		result = append(result, entry)
	}
	if previous != "" {
		result = append(result, updateHelperEnvironment+"="+previous)
	}
	if payload != "" && !strings.EqualFold(payload, previous) {
		result = append(result, updatePayloadEnvironment+"="+payload)
	}
	return result
}

func waitForUpdateParent() error {
	rawPID := os.Getenv(updateWaitPIDEnvironment)
	_ = os.Unsetenv(updateWaitPIDEnvironment)
	if rawPID == "" {
		return nil
	}
	pid, err := strconv.Atoi(rawPID)
	if err != nil || pid <= 0 {
		return errors.New("更新重启的父进程 PID 无效")
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// The parent can disappear between CreateProcess and OpenProcess. Give
		// Windows a moment to release the executable and single-instance mutex.
		time.Sleep(300 * time.Millisecond)
		return nil
	}
	defer windows.CloseHandle(process)
	result, err := windows.WaitForSingleObject(process, 60_000)
	if err != nil {
		return fmt.Errorf("等待旧版本退出失败: %w", err)
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("等待旧版本退出超时 (wait result 0x%x)", result)
	}
	return nil
}
func copyExecutable(source, target string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	syncErr := dst.Sync()
	closeErr := dst.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(target)
		return errors.Join(copyErr, syncErr, closeErr)
	}
	return nil
}

func scheduleUpdateFileCleanup(paths ...string) {
	seen := map[string]struct{}{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) {
			continue
		}
		ptr, err := windows.UTF16PtrFromString(path)
		if err == nil {
			_ = windows.MoveFileEx(ptr, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
		}
	}
}

func validatedUpdateCleanupPaths(currentExecutable string, candidates ...string) []string {
	currentExecutable, err := filepath.Abs(currentExecutable)
	if err != nil {
		return nil
	}
	executableDir := filepath.Dir(currentExecutable)
	allowedNames := map[string]struct{}{
		strings.ToLower(currentExecutable + ".old"):                          {},
		strings.ToLower(filepath.Join(executableDir, "vision-relay.update")): {},
	}
	seen := make(map[string]struct{}, len(candidates))
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		candidate, err = filepath.Abs(candidate)
		if err != nil || !strings.EqualFold(filepath.Dir(candidate), executableDir) {
			continue
		}
		key := strings.ToLower(candidate)
		if _, allowed := allowedNames[key]; !allowed && !legacyUpdateCleanupName.MatchString(filepath.Base(candidate)) {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		info, err := os.Lstat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		pathPointer, err := windows.UTF16PtrFromString(candidate)
		if err != nil {
			continue
		}
		attributes, err := windows.GetFileAttributes(pathPointer)
		if err != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func cleanupUpdateFiles() {
	candidates := []string{os.Getenv(updateHelperEnvironment), os.Getenv(updatePayloadEnvironment)}
	_ = os.Unsetenv(updateHelperEnvironment)
	_ = os.Unsetenv(updatePayloadEnvironment)
	currentExecutable, err := os.Executable()
	if err != nil {
		return
	}
	paths := validatedUpdateCleanupPaths(currentExecutable, candidates...)
	go func() {
		pending := paths
		for i := 0; i < 20 && len(pending) > 0; i++ {
			time.Sleep(500 * time.Millisecond)
			next := pending[:0]
			for _, path := range pending {
				if path != "" && os.Remove(path) != nil {
					next = append(next, path)
				}
			}
			pending = next
		}
		if len(pending) > 0 {
			scheduleUpdateFileCleanup(pending...)
		}
	}()
}
