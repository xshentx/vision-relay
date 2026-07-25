//go:build windows

package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	updateProcessCreationFlags = windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
	updateHelperEnvironment    = "VISION_RELAY_UPDATE_HELPER"
	updatePayloadEnvironment   = "VISION_RELAY_UPDATE_PAYLOAD"
)

func startUpdateHelper(downloaded, target string, pid int, restartArgs []string) error {
	helper, err := createUpdateHelper(target)
	if err != nil {
		return fmt.Errorf("创建更新助手失败: %w", err)
	}
	encoded, _ := json.Marshal(restartArgs)
	args := []string{
		"--apply-update=" + target,
		"--update-source=" + downloaded,
		"--wait-pid=" + strconv.Itoa(pid),
		"--restart-args=" + base64.RawURLEncoding.EncodeToString(encoded),
	}
	if err := startDetachedUpdateProcess(helper, args, os.Environ(), currentWorkingDirectory()); err != nil {
		_ = os.Remove(helper)
		return err
	}
	return nil
}

// createUpdateHelper copies the already-running, trusted executable to a
// separate path. The downloaded release is treated only as replacement data,
// so CreateProcess never depends on a freshly downloaded executable surviving
// endpoint-protection scanning.
func createUpdateHelper(target string) (string, error) {
	current, err := os.Executable()
	if err != nil {
		return "", err
	}
	source, err := os.Open(current)
	if err != nil {
		return "", err
	}
	defer source.Close()

	file, err := os.CreateTemp(filepath.Dir(target), ".vision-relay-helper-*.exe")
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(file, source); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

// startDetachedUpdateProcess keeps the updater and the restarted application
// independent from the process that launched them. This matters when Vision
// Relay was itself started by a process that owns a kill-on-close Job Object:
// a normal child process can otherwise be terminated together with the old
// application before the update restart becomes visible.
func startDetachedUpdateProcess(path string, args, env []string, dir string) error {
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
		// Start is intentionally not followed by Wait: both the updater and the
		// new application must outlive their launcher. Release closes our copy
		// of the process handle and avoids leaking one until garbage collection.
		_ = cmd.Process.Release()
		return nil
	}
	return errors.Join(startErrors...)
}

func currentWorkingDirectory() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func withUpdateCleanupEnvironment(env []string, helper, payload string) []string {
	result := make([]string, 0, len(env)+2)
	for _, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(name, updateHelperEnvironment) || strings.EqualFold(name, updatePayloadEnvironment)) {
			continue
		}
		result = append(result, entry)
	}
	if helper != "" {
		result = append(result, updateHelperEnvironment+"="+helper)
	}
	if payload != "" && !strings.EqualFold(payload, helper) {
		result = append(result, updatePayloadEnvironment+"="+payload)
	}
	return result
}

// RunUpdateHelperIfRequested applies a downloaded update before normal flag parsing.
func RunUpdateHelperIfRequested() bool {
	values := map[string]string{}
	for _, arg := range os.Args[1:] {
		for _, key := range []string{"--apply-update=", "--update-source=", "--wait-pid=", "--restart-args="} {
			if strings.HasPrefix(arg, key) {
				values[strings.TrimSuffix(key, "=")] = strings.TrimPrefix(arg, key)
			}
		}
	}
	target := values["--apply-update"]
	if target == "" {
		return false
	}
	pid, _ := strconv.Atoi(values["--wait-pid"])
	var restartArgs []string
	if raw, err := base64.RawURLEncoding.DecodeString(values["--restart-args"]); err == nil {
		_ = json.Unmarshal(raw, &restartArgs)
	}
	helper, _ := os.Executable()
	source := values["--update-source"]
	if source == "" {
		// Backward compatibility with helpers from releases that executed the
		// downloaded binary directly.
		source = helper
	}
	if err := waitForUpdateTargetExit(pid); err != nil {
		// The old process may still own the single-instance mutex. Starting a
		// fallback copy here could immediately exit as a duplicate and leave no
		// running application when the old process eventually terminates.
		_ = os.WriteFile(target+".update-error.txt", []byte(err.Error()), 0600)
		scheduleUpdateFileCleanup(helper, source)
		return true
	}
	if err := applyUpdate(source, target, restartArgs); err != nil {
		failure := err
		if restartErr := restartAfterFailedUpdate(target, restartArgs, helper, source); restartErr != nil {
			failure = errors.Join(failure, fmt.Errorf("恢复启动旧版本失败: %w", restartErr))
		}
		_ = os.WriteFile(target+".update-error.txt", []byte(failure.Error()), 0600)
		scheduleUpdateFileCleanup(helper, source)
	}
	return true
}

func applyUpdate(source, target string, restartArgs []string) (returnErr error) {
	var err error
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("更新载荷不可用: %w", err)
	}
	helper, err := os.Executable()
	if err != nil {
		return err
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
	if err := startDetachedUpdateProcess(
		target,
		restartArgs,
		withUpdateCleanupEnvironment(os.Environ(), helper, source),
		currentWorkingDirectory(),
	); err != nil {
		return fmt.Errorf("重启新版本失败: %w", err)
	}
	return nil
}

func restartAfterFailedUpdate(target string, restartArgs []string, helper, payload string) error {
	if _, err := os.Stat(target); err != nil {
		return err
	}
	return startDetachedUpdateProcess(
		target,
		restartArgs,
		withUpdateCleanupEnvironment(os.Environ(), helper, payload),
		currentWorkingDirectory(),
	)
}

func waitForUpdateTargetExit(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// The process can disappear between launching the helper and opening its
		// handle. A short delay also gives Windows time to release executable and
		// single-instance kernel handles before replacement.
		time.Sleep(1200 * time.Millisecond)
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
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return copyErr
	}
	return closeErr
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
		if os.Remove(path) == nil {
			continue
		}
		ptr, err := windows.UTF16PtrFromString(path)
		if err == nil {
			_ = windows.MoveFileEx(ptr, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
		}
	}
}

func cleanupUpdateHelper() {
	paths := []string{os.Getenv(updateHelperEnvironment), os.Getenv(updatePayloadEnvironment)}
	_ = os.Unsetenv(updateHelperEnvironment)
	_ = os.Unsetenv(updatePayloadEnvironment)
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
