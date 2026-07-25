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
	Payload    string   `json:"payload"`
}

func TestWithUpdateCleanupEnvironmentReplacesExistingValues(t *testing.T) {
	env := []string{
		"PATH=C:\\Windows",
		"vision_relay_update_helper=stale-helper.exe",
		"Vision_Relay_Update_Payload=stale-payload.download",
		"OTHER=value",
	}
	helper := `C:\\Temp\\fresh-helper.exe`
	payload := `C:\\Temp\\fresh-payload.download`
	got := withUpdateCleanupEnvironment(env, helper, payload)

	want := map[string]string{
		updateHelperEnvironment:  helper,
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

func TestCreateUpdateHelperCopiesCurrentExecutableBesideTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vision-relay.exe")
	helper, err := createUpdateHelper(target)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(helper)
	if filepath.Dir(helper) != dir {
		t.Fatalf("helper directory = %q, want %q", filepath.Dir(helper), dir)
	}
	if !strings.HasPrefix(filepath.Base(helper), ".vision-relay-helper-") || !strings.EqualFold(filepath.Ext(helper), ".exe") {
		t.Fatalf("unexpected helper name %q", helper)
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	currentInfo, err := os.Stat(current)
	if err != nil {
		t.Fatal(err)
	}
	helperInfo, err := os.Stat(helper)
	if err != nil {
		t.Fatal(err)
	}
	if helperInfo.Size() != currentInfo.Size() {
		t.Fatalf("helper size = %d, want %d", helperInfo.Size(), currentInfo.Size())
	}
}

func TestApplyUpdateReplacesAndRestartsTarget(t *testing.T) {
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
	source := filepath.Join(dir, ".vision-relay-payload-test.download")
	if err := copyExecutable(current, source); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "restart-child.json")
	t.Setenv(updateRestartTestMarkerEnv, marker)

	if err := applyUpdate(source, target, []string{"-test.run=^TestUpdateRestartChild$"}); err != nil {
		t.Fatal(err)
	}
	result := waitForUpdateRestartTestResult(t, marker)
	if !strings.EqualFold(result.Helper, current) {
		t.Fatalf("restart helper = %q, want %q", result.Helper, current)
	}
	if !strings.EqualFold(result.Payload, source) {
		t.Fatalf("restart payload = %q, want %q", result.Payload, source)
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

func TestFailedUpdateRestoresAndRestartsOldTarget(t *testing.T) {
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

	if err := applyUpdate(invalidSource, target, nil); err == nil {
		t.Fatal("applyUpdate succeeded with a directory payload")
	}
	restoredInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("old target was not restored: %v", err)
	}
	if restoredInfo.Size() != targetInfo.Size() {
		t.Fatalf("restored target size = %d, want %d", restoredInfo.Size(), targetInfo.Size())
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf("backup residue still exists after restore: %v", err)
	}

	marker := filepath.Join(dir, "failed-update-restart.json")
	t.Setenv(updateRestartTestMarkerEnv, marker)
	restartArgs := []string{"-test.run=^TestUpdateRestartChild$"}
	if err := restartAfterFailedUpdate(target, restartArgs, current, invalidSource); err != nil {
		t.Fatal(err)
	}
	result := waitForUpdateRestartTestResult(t, marker)
	if !strings.EqualFold(result.Helper, current) {
		t.Fatalf("restart helper = %q, want %q", result.Helper, current)
	}
	if !strings.EqualFold(result.Payload, invalidSource) {
		t.Fatalf("restart payload = %q, want %q", result.Payload, invalidSource)
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
		Helper:     os.Getenv(updateHelperEnvironment),
		Payload:    os.Getenv(updatePayloadEnvironment),
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
