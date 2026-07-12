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

func startUpdateHelper(downloaded, target string, pid int, restartArgs []string) error {
	encoded, _ := json.Marshal(restartArgs)
	cmd := exec.Command(downloaded,
		"--apply-update="+target,
		"--wait-pid="+strconv.Itoa(pid),
		"--restart-args="+base64.RawURLEncoding.EncodeToString(encoded),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
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
	if pid > 0 {
		if process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid)); err == nil {
			_, _ = windows.WaitForSingleObject(process, 60_000)
			windows.CloseHandle(process)
		} else {
			time.Sleep(1200 * time.Millisecond)
		}
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
	cmd := exec.Command(target, restartArgs...)
	cmd.Env = append(os.Environ(), "VISION_RELAY_UPDATE_HELPER="+source)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		restored = true
		return fmt.Errorf("重启新版本失败: %w", err)
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
