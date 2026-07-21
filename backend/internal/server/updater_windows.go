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

const updateProcessCreationFlags = windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP

func startUpdateHelper(downloaded, target string, pid int, restartArgs []string) error {
	encoded, _ := json.Marshal(restartArgs)
	args := []string{
		"--apply-update=" + target,
		"--wait-pid=" + strconv.Itoa(pid),
		"--restart-args=" + base64.RawURLEncoding.EncodeToString(encoded),
	}
	return startDetachedUpdateProcess(downloaded, args, os.Environ(), currentWorkingDirectory())
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

func withUpdateHelperEnvironment(env []string, source string) []string {
	const key = "VISION_RELAY_UPDATE_HELPER"
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, key) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, key+"="+source)
}

// RunUpdateHelperIfRequested applies a downloaded update before normal flag parsing.
func RunUpdateHelperIfRequested() bool {
	values := map[string]string{}
	for _, arg := range os.Args[1:] {
		for _, key := range []string{"--apply-update=", "--wait-pid=", "--restart-args="} {
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
	if err := applyUpdate(target, pid, restartArgs); err != nil {
		_ = os.WriteFile(target+".update-error.txt", []byte(err.Error()), 0600)
	}
	return true
}

func applyUpdate(target string, pid int, restartArgs []string) error {
	if err := waitForUpdateTargetExit(pid); err != nil {
		return err
	}
	source, err := os.Executable()
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
	restored := false
	defer func() {
		if !restored {
			return
		}
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
	}()
	if err := copyExecutable(source, target); err != nil {
		restored = true
		return fmt.Errorf("写入新版本失败: %w", err)
	}
	if err := startDetachedUpdateProcess(
		target,
		restartArgs,
		withUpdateHelperEnvironment(os.Environ(), source),
		currentWorkingDirectory(),
	); err != nil {
		restored = true
		return fmt.Errorf("重启新版本失败: %w", err)
	}
	return nil
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

func cleanupUpdateHelper() {
	path := os.Getenv("VISION_RELAY_UPDATE_HELPER")
	if path == "" {
		return
	}
	_ = os.Unsetenv("VISION_RELAY_UPDATE_HELPER")
	go func() {
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			if os.Remove(path) == nil {
				return
			}
		}
	}()
}
