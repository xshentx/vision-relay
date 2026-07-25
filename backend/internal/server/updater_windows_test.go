//go:build windows

package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	updateRestartTestMarkerEnv = "VISION_RELAY_UPDATE_RESTART_TEST_MARKER"
	updateRestartTestHoldEnv   = "VISION_RELAY_UPDATE_RESTART_TEST_HOLD"
)

type updateRestartTestResult struct {
	Args       []string `json:"args"`
	WorkingDir string   `json:"working_dir"`
	Previous   string   `json:"previous"`
	Payload    string   `json:"payload"`
	ParentPID  string   `json:"parent_pid"`
}

func TestWithUpdateCleanupEnvironmentReplacesExistingValues(t *testing.T) {
	env := []string{
		"PATH=C:\\Windows",
		"vision_relay_update_helper=stale-helper.exe",
		"Vision_Relay_Update_Payload=stale-payload.download",
		"OTHER=value",
	}
	previous := `C:\\Temp\\vision-relay.exe.old`
	payload := `C:\\Temp\\vision-relay.update`
	got := withUpdateCleanupEnvironment(env, previous, payload)

	want := map[string]string{
		updateHelperEnvironment:  previous,
		updatePayloadEnvironment: payload,
	}
	counts := map[string]int{}
	for _, entry := range got {
		name, value, _ := strings.Cut(entry, "=")
		for key, wantValue := range want {
			if strings.EqualFold(name, key) {
				counts[key]++
				if value != wantValue {
					t.Fatalf("%s = %q, want %q", key, value, wantValue)
				}
			}
		}
	}
	for key := range want {
		if counts[key] != 1 {
			t.Fatalf("%s entry count = %d, want 1; env %#v", key, counts[key], got)
		}
	}
	if !slices.Contains(got, "PATH=C:\\Windows") || !slices.Contains(got, "OTHER=value") {
		t.Fatalf("unrelated environment entries were not preserved: %#v", got)
	}
}

func TestStartDetachedUpdateProcessStartsChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "detached-child.json")
	workingDir := t.TempDir()
	env := append(os.Environ(), updateRestartTestMarkerEnv+"="+marker)
	args := []string{"-test.run=^TestUpdateRestartChild$"}

	if _, err := startDetachedUpdateProcess(os.Args[0], args, env, workingDir); err != nil {
		t.Fatal(err)
	}
	result := waitForUpdateRestartTestResult(t, marker)
	if result.WorkingDir != workingDir {
		t.Fatalf("child working directory = %q, want %q", result.WorkingDir, workingDir)
	}
	if !slices.Contains(result.Args, args[0]) {
		t.Fatalf("child args = %#v, want %q", result.Args, args[0])
	}
}

func TestInstallUpdateReplacesAndRestartsWithoutHelperExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vision-relay-update-target.exe")
	oldContents := []byte("old version")
	if err := os.WriteFile(target, oldContents, 0o755); err != nil {
		t.Fatal(err)
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "vision-relay.update")
	if err := copyExecutable(current, source); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "restart-child.json")
	t.Setenv(updateRestartTestMarkerEnv, marker)
	t.Setenv(updateRestartTestHoldEnv, "1750ms")

	if err := installUpdate(source, target, []string{"-test.run=^TestUpdateRestartChild$"}); err != nil {
		t.Fatal(err)
	}
	result := waitForUpdateRestartTestResult(t, marker)
	if !strings.EqualFold(result.Previous, target+".old") {
		t.Fatalf("restart previous executable = %q, want %q", result.Previous, target+".old")
	}
	if !strings.EqualFold(result.Payload, source) {
		t.Fatalf("restart payload = %q, want %q", result.Payload, source)
	}
	if result.ParentPID != strconv.Itoa(os.Getpid()) {
		t.Fatalf("restart parent PID = %q, want %d", result.ParentPID, os.Getpid())
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".vision-relay-helper-*.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("update unexpectedly created helper executables: %#v", matches)
	}
	backup, err := os.ReadFile(target + ".old")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(oldContents) {
		t.Fatalf("backup = %q, want %q", backup, oldContents)
	}
	updated, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 2)
	_, readErr := updated.Read(header)
	_ = updated.Close()
	if readErr != nil || string(header) != "MZ" {
		t.Fatalf("updated target header = %q, err %v", header, readErr)
	}
	// Let the test child finish before TempDir cleanup tries to remove its image.
	time.Sleep(400 * time.Millisecond)
}

func TestFailedUpdateRestoresOldTarget(t *testing.T) {
	dir := t.TempDir()
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "vision-relay-old-target.exe")
	if err := copyExecutable(current, target); err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	invalidSource := filepath.Join(dir, "payload-directory")
	if err := os.Mkdir(invalidSource, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installUpdate(invalidSource, target, nil); err == nil {
		t.Fatal("installUpdate succeeded with a directory payload")
	}
	restoredInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("old target was not preserved: %v", err)
	}
	if restoredInfo.Size() != targetInfo.Size() {
		t.Fatalf("restored target size = %d, want %d", restoredInfo.Size(), targetInfo.Size())
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf("backup residue exists after rejected payload: %v", err)
	}
}

func TestUpdateProcessEarlyExitRestoresOldTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vision-relay-early-exit.exe")
	oldContents := []byte("known good old version")
	if err := os.WriteFile(target, oldContents, 0o755); err != nil {
		t.Fatal(err)
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "vision-relay.update")
	if err := copyExecutable(current, source); err != nil {
		t.Fatal(err)
	}

	err = installUpdate(source, target, []string{"-test.run=^TestUpdateRestartImmediateExitChild$"})
	if err == nil {
		t.Fatal("installUpdate accepted a child that exited during the startup survival window")
	}
	if !strings.Contains(err.Error(), "\u63d0\u524d\u9000\u51fa") {
		t.Fatalf("early-exit error = %q", err)
	}
	restored, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("old target was not restored: %v", readErr)
	}
	if string(restored) != string(oldContents) {
		t.Fatalf("restored target = %q, want %q", restored, oldContents)
	}
	if _, statErr := os.Stat(target + ".old"); !os.IsNotExist(statErr) {
		t.Fatalf("backup residue exists after early-exit rollback: %v", statErr)
	}
}

func TestValidatedUpdateCleanupPathsUsesStrictWhitelist(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	currentExecutable := filepath.Join(dir, "vision-relay.exe")
	previous := currentExecutable + ".old"
	payload := filepath.Join(dir, "vision-relay.update")
	legacyHelper := filepath.Join(dir, ".vision-relay-helper-123456.exe")
	legacyPayload := filepath.Join(dir, ".vision-relay-payload-123456.download")
	badLegacyName := filepath.Join(dir, ".vision-relay-helper-not-random.exe")
	outside := filepath.Join(outsideDir, "vision-relay.update")
	for _, path := range []string{previous, payload, legacyHelper, legacyPayload, badLegacyName, outside} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := validatedUpdateCleanupPaths(
		currentExecutable,
		previous,
		payload,
		legacyHelper,
		legacyPayload,
		badLegacyName,
		outside,
		previous,
	)
	want := []string{previous, payload, legacyHelper, legacyPayload}
	if len(got) != len(want) {
		t.Fatalf("validated paths = %#v, want %#v", got, want)
	}
	for _, path := range want {
		if !containsPathFold(got, path) {
			t.Fatalf("validated paths %#v do not contain %q", got, path)
		}
	}

	if err := os.Remove(previous); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	if paths := validatedUpdateCleanupPaths(currentExecutable, previous); len(paths) != 0 {
		t.Fatalf("cleanup accepted a directory: %#v", paths)
	}

	if err := os.Remove(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, payload); err == nil {
		if paths := validatedUpdateCleanupPaths(currentExecutable, payload); len(paths) != 0 {
			t.Fatalf("cleanup accepted a reparse point: %#v", paths)
		}
	}
}

func TestUpdateRestartImmediateExitChild(t *testing.T) {}

func containsPathFold(paths []string, want string) bool {
	for _, path := range paths {
		if strings.EqualFold(path, want) {
			return true
		}
	}
	return false
}

func TestWaitForUpdateParent(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateWaitParentChild$")
	cmd.Env = append(os.Environ(), "VISION_RELAY_UPDATE_WAIT_TEST=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(updateWaitPIDEnvironment, strconv.Itoa(cmd.Process.Pid))
	started := time.Now()
	if err := waitForUpdateParent(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
		t.Fatalf("waitForUpdateParent returned too early after %v", elapsed)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if value := os.Getenv(updateWaitPIDEnvironment); value != "" {
		t.Fatalf("wait PID environment was not cleared: %q", value)
	}
}

func TestUpdateWaitParentChild(t *testing.T) {
	if os.Getenv("VISION_RELAY_UPDATE_WAIT_TEST") == "" {
		return
	}
	time.Sleep(300 * time.Millisecond)
}

func TestUpdateRestartChild(t *testing.T) {
	marker := os.Getenv(updateRestartTestMarkerEnv)
	if marker == "" {
		return
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(updateRestartTestResult{
		Args:       os.Args[1:],
		WorkingDir: workingDir,
		Previous:   os.Getenv(updateHelperEnvironment),
		Payload:    os.Getenv(updatePayloadEnvironment),
		ParentPID:  os.Getenv(updateWaitPIDEnvironment),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if hold := os.Getenv(updateRestartTestHoldEnv); hold != "" {
		duration, err := time.ParseDuration(hold)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(duration)
	}
}

func waitForUpdateRestartTestResult(t *testing.T, marker string) updateRestartTestResult {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		payload, err := os.ReadFile(marker)
		if err == nil {
			var result updateRestartTestResult
			if err := json.Unmarshal(payload, &result); err != nil {
				t.Fatal(err)
			}
			return result
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("updated process did not create restart marker %q", marker)
	return updateRestartTestResult{}
}
