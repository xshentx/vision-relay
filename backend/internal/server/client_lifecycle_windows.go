//go:build windows

package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func platformClientProgramRunning(client, programPath string) (bool, error) {
	processes, err := matchingClientProcesses(client, programPath)
	return len(processes) > 0, err
}

func platformStopClientProgram(client, programPath string) (bool, error) {
	processes, err := matchingClientProcesses(client, programPath)
	if err != nil || len(processes) == 0 {
		return false, err
	}

	roots := rootClientProcesses(processes)
	for _, process := range roots {
		_ = taskkillClientProcessTree(process.PID, false)
	}
	if stopped, waitErr := waitForClientProcessesStopped(client, programPath, 1500*time.Millisecond); stopped || waitErr != nil {
		return stopped, waitErr
	}

	// Electron clients such as OpenCode create several helper processes. Kill the
	// root process trees instead of treating every renderer/helper as a separate
	// failure; child PIDs can disappear while the stop operation is in flight.
	for _, process := range roots {
		_ = taskkillClientProcessTree(process.PID, true)
	}
	if stopped, waitErr := waitForClientProcessesStopped(client, programPath, 3*time.Second); stopped || waitErr != nil {
		return stopped, waitErr
	}

	remaining, err := matchingClientProcesses(client, programPath)
	if err != nil {
		return false, err
	}
	for _, process := range remaining {
		handle, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, process.PID)
		if openErr != nil {
			continue
		}
		_ = windows.TerminateProcess(handle, 0)
		_, _ = windows.WaitForSingleObject(handle, 1000)
		windows.CloseHandle(handle)
	}
	if stopped, waitErr := waitForClientProcessesStopped(client, programPath, 2*time.Second); stopped || waitErr != nil {
		return stopped, waitErr
	}

	remaining, err = matchingClientProcesses(client, programPath)
	if err != nil {
		return false, err
	}
	pids := make([]string, 0, len(remaining))
	for _, process := range remaining {
		pids = append(pids, fmt.Sprintf("%d", process.PID))
	}
	return false, fmt.Errorf("仍有客户端进程未退出（PID %s），可能与 Vision Relay 的运行权限不同", strings.Join(pids, ", "))
}

func taskkillClientProcessTree(pid uint32, force bool) error {
	args := []string{"/PID", fmt.Sprintf("%d", pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	cmd := exec.Command("taskkill.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.Run()
}

func waitForClientProcessesStopped(client, programPath string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		processes, err := matchingClientProcesses(client, programPath)
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

func platformStartClientProgram(programPath, workDir string) error {
	programPath = strings.TrimSpace(programPath)
	if programPath == "" {
		return errors.New("未配置客户端程序位置")
	}
	info, err := os.Stat(programPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("客户端程序位置不能是目录")
	}

	workDir = normalizeClientWorkDir(workDir, programPath)
	ext := strings.ToLower(filepath.Ext(programPath))
	var cmd *exec.Cmd
	switch ext {
	case ".cmd", ".bat":
		cmd = exec.Command("cmd.exe", "/d", "/c", "start", "", programPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	case ".ps1":
		cmd = exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", programPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	default:
		cmd = exec.Command(programPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	}
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = os.Environ()
	if err := cmd.Start(); err == nil {
		return cmd.Process.Release()
	} else if ext != ".exe" {
		return err
	}

	// Packaged desktop apps can reject a direct CreateProcess call even when
	// their executable path is readable. Explorer launches them through Shell.
	fallback := exec.Command("explorer.exe", programPath)
	fallback.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if workDir != "" {
		fallback.Dir = workDir
	}
	if err := fallback.Start(); err != nil {
		return err
	}
	return fallback.Process.Release()
}

type clientProcess struct {
	PID       uint32
	ParentPID uint32
}

func rootClientProcesses(processes []clientProcess) []clientProcess {
	matched := make(map[uint32]bool, len(processes))
	for _, process := range processes {
		matched[process.PID] = true
	}
	roots := make([]clientProcess, 0, len(processes))
	for _, process := range processes {
		if !matched[process.ParentPID] {
			roots = append(roots, process)
		}
	}
	if len(roots) == 0 {
		return append([]clientProcess(nil), processes...)
	}
	return roots
}

func matchingClientProcesses(client, programPath string) ([]clientProcess, error) {
	names := clientProcessImageNames(client, programPath)
	if len(names) == 0 {
		return nil, nil
	}
	commandLineMarkers := clientWrapperCommandLineMarkers(programPath)
	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		nameSet[strings.ToLower(name)] = true
	}

	targetPath := ""
	if strings.EqualFold(filepath.Ext(programPath), ".exe") {
		if abs, err := filepath.Abs(programPath); err == nil {
			targetPath = filepath.Clean(abs)
		}
	}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return nil, nil
		}
		return nil, err
	}
	var matches []clientProcess
	for {
		imageName := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		if nameSet[imageName] {
			matched := targetPath == "" && !clientProcessRequiresCommandLine(imageName, programPath)
			if targetPath != "" {
				path, pathErr := windowsProcessPath(entry.ProcessID)
				matched = pathErr == nil && strings.EqualFold(filepath.Clean(path), targetPath)
			} else if clientProcessRequiresCommandLine(imageName, programPath) {
				commandLine, commandLineErr := windowsProcessCommandLine(entry.ProcessID)
				matched = commandLineErr == nil && commandLineContainsMarker(commandLine, commandLineMarkers)
			}
			if matched {
				matches = append(matches, clientProcess{PID: entry.ProcessID, ParentPID: entry.ParentProcessID})
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, err
		}
	}
	return matches, nil
}

func windowsProcessPath(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func windowsProcessCommandLine(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	var size uint32
	_ = windows.NtQueryInformationProcess(handle, windows.ProcessCommandLineInformation, nil, 0, &size)
	if size < uint32(unsafe.Sizeof(windows.NTUnicodeString{})) {
		return "", errors.New("process command line is unavailable")
	}
	for attempt := 0; attempt < 2; attempt++ {
		buffer := make([]byte, size)
		returned := size
		err = windows.NtQueryInformationProcess(handle, windows.ProcessCommandLineInformation, unsafe.Pointer(&buffer[0]), size, &returned)
		if err != nil {
			if returned > size {
				size = returned
				continue
			}
			return "", err
		}
		value := (*windows.NTUnicodeString)(unsafe.Pointer(&buffer[0]))
		if value.Length == 0 || value.Buffer == nil {
			return "", nil
		}
		start := uintptr(unsafe.Pointer(&buffer[0]))
		end := start + uintptr(len(buffer))
		textStart := uintptr(unsafe.Pointer(value.Buffer))
		textEnd := textStart + uintptr(value.Length)
		if textStart < start || textEnd < textStart || textEnd > end || value.Length%2 != 0 {
			return "", errors.New("process command line buffer is invalid")
		}
		return windows.UTF16ToString(unsafe.Slice(value.Buffer, int(value.Length/2))), nil
	}
	return "", errors.New("process command line changed while reading")
}

func clientWrapperCommandLineMarkers(programPath string) []string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(programPath)))
	if ext != ".cmd" && ext != ".bat" && ext != ".ps1" {
		return nil
	}
	abs, err := filepath.Abs(programPath)
	if err != nil {
		abs = filepath.Clean(programPath)
	}
	markers := []string{normalizeCommandLineText(abs)}
	content, err := os.ReadFile(programPath)
	if err != nil || len(content) > 1024*1024 {
		return markers
	}
	baseDir := filepath.Dir(abs)
	text := string(content)
	lowerText := strings.ToLower(text)
	variables := []string{"%dp0%", "%~dp0", "${basedir}", "$basedir", "${psscriptroot}", "$psscriptroot"}
	for _, variable := range variables {
		for offset := 0; ; {
			index := strings.Index(lowerText[offset:], variable)
			if index < 0 {
				break
			}
			index += offset + len(variable)
			tail := text[index:]
			tail = strings.TrimLeft(tail, `\/`)
			if target := wrapperRelativeTarget(tail); target != "" {
				markers = append(markers, normalizeCommandLineText(filepath.Join(baseDir, filepath.FromSlash(strings.ReplaceAll(target, `\`, "/")))))
			}
			offset = index
		}
	}
	return uniqueCommandLineMarkers(markers)
}

func wrapperRelativeTarget(value string) string {
	end := len(value)
	if index := strings.IndexAny(value, "\"'\r\n"); index >= 0 {
		end = index
	}
	value = strings.TrimSpace(value[:end])
	lower := strings.ToLower(value)
	targetEnd := -1
	for _, ext := range []string{".mjs", ".cjs", ".js"} {
		if index := strings.Index(lower, ext); index >= 0 {
			candidateEnd := index + len(ext)
			if targetEnd < 0 || candidateEnd < targetEnd {
				targetEnd = candidateEnd
			}
		}
	}
	if targetEnd < 0 {
		return ""
	}
	return value[:targetEnd]
}

func clientProcessRequiresCommandLine(imageName, programPath string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(programPath)))
	if ext != ".cmd" && ext != ".bat" && ext != ".ps1" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(imageName)) {
	case "cmd.exe", "powershell.exe", "pwsh.exe", "node.exe":
		return true
	default:
		return false
	}
}

func commandLineContainsMarker(commandLine string, markers []string) bool {
	commandLine = normalizeCommandLineText(commandLine)
	for _, marker := range markers {
		if marker != "" && strings.Contains(commandLine, marker) {
			return true
		}
	}
	return false
}

func normalizeCommandLineText(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "/", `\`)
	value = strings.ReplaceAll(value, `\\?\`, "")
	return strings.ToLower(value)
}

func uniqueCommandLineMarkers(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func clientProcessImageNames(client, programPath string) []string {
	var names []string
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(programPath)))
	if base := filepath.Base(strings.TrimSpace(programPath)); ext == ".exe" {
		names = append(names, base)
	}
	switch normalizeClientID(client) {
	case clientCodex:
		names = append(names, "ChatGPT.exe", "Codex.exe")
	case clientOpenCode:
		names = append(names, "OpenCode.exe", "opencode.exe")
	case clientClaudeCode:
		names = append(names, "claude.exe")
	case clientOpenClaw:
		names = append(names, "openclaw.exe")
	}
	switch ext {
	case ".cmd", ".bat":
		names = append(names, "cmd.exe", "node.exe")
	case ".ps1":
		names = append(names, "powershell.exe", "pwsh.exe", "node.exe")
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(names))
	for _, name := range names {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, name)
	}
	return result
}
