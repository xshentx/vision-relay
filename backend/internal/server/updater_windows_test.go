//go:build windows

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const updateRestartTestMarkerEnv = "VISION_RELAY_UPDATE_RESTART_TEST_MARKER"

type updateRestartTestResult struct {
	Args       []string `json:"args"`
	WorkingDir string   `json:"working_dir"`
	Helper     string   `json:"helper"`
}

func TestWithUpdateHelperEnvironmentReplacesExistingValue(t *testing.T) {
	env := []string{
		"PATH=C:\\Windows",
		"vision_relay_update_helper=stale.exe",
		"OTHER=value",
	}
	got := withUpdateHelperEnvironment(env, `C:\\Temp\\fresh.exe`)

	var helperEntries []string
	for _, entry := range got {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "VISION_RELAY_UPDATE_HELPER") {
			helperEntries = append(helperEntries, entry)
		}
	}
	if len(helperEntries) != 1 || helperEntries[0] != `VISION_RELAY_UPDATE_HELPER=C:\\Temp\\fresh.exe` {
		t.Fatalf("helper environment entries = %#v", helperEntries)
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

	if err := startDetachedUpdateProcess(os.Args[0], args, env, workingDir); err != nil {
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

func TestApplyUpdateReplacesAndRestartsTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vision-relay-update-target.exe")
	oldContents := []byte("old version")
	if err := os.WriteFile(target, oldContents, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "restart-child.json")
	t.Setenv(updateRestartTestMarkerEnv, marker)

	if err := applyUpdate(target, 0, []string{"-test.run=^TestUpdateRestartChild$"}); err != nil {
		t.Fatal(err)
	}
	result := waitForUpdateRestartTestResult(t, marker)
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(result.Helper, source) {
		t.Fatalf("restart helper = %q, want %q", result.Helper, source)
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
		Helper:     os.Getenv("VISION_RELAY_UPDATE_HELPER"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, payload, 0o600); err != nil {
		t.Fatal(err)
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
