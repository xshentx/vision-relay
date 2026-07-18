//go:build windows

package server

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRootClientProcesses(t *testing.T) {
	tests := []struct {
		name      string
		processes []clientProcess
		want      []clientProcess
	}{
		{
			name: "one process tree",
			processes: []clientProcess{
				{PID: 100, ParentPID: 10},
				{PID: 101, ParentPID: 100},
				{PID: 102, ParentPID: 100},
				{PID: 103, ParentPID: 101},
			},
			want: []clientProcess{{PID: 100, ParentPID: 10}},
		},
		{
			name: "two independent process trees",
			processes: []clientProcess{
				{PID: 200, ParentPID: 20},
				{PID: 201, ParentPID: 200},
				{PID: 300, ParentPID: 30},
				{PID: 301, ParentPID: 300},
			},
			want: []clientProcess{
				{PID: 200, ParentPID: 20},
				{PID: 300, ParentPID: 30},
			},
		},
		{
			name: "unmatched parent is a root",
			processes: []clientProcess{
				{PID: 400, ParentPID: 999},
				{PID: 401, ParentPID: 400},
			},
			want: []clientProcess{{PID: 400, ParentPID: 999}},
		},
		{
			name: "cycle falls back to all processes",
			processes: []clientProcess{
				{PID: 500, ParentPID: 501},
				{PID: 501, ParentPID: 500},
			},
			want: []clientProcess{
				{PID: 500, ParentPID: 501},
				{PID: 501, ParentPID: 500},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rootClientProcesses(tt.processes); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rootClientProcesses() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestWindowsProcessCommandLine(t *testing.T) {
	commandLine, err := windowsProcessCommandLine(uint32(os.Getpid()))
	if err != nil {
		t.Fatalf("windowsProcessCommandLine() error = %v", err)
	}
	if strings.TrimSpace(commandLine) == "" {
		t.Fatal("windowsProcessCommandLine() returned an empty command line")
	}
}

func TestClientWrapperCommandLineMarkers(t *testing.T) {
	dir := t.TempDir()
	wrapperPath := filepath.Join(dir, "claude.cmd")
	content := `@ECHO off
"%dp0%\node.exe" "%dp0%\node_modules\@anthropic-ai\claude-code\cli.js" %*
`
	if err := os.WriteFile(wrapperPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	markers := clientWrapperCommandLineMarkers(wrapperPath)
	wrapperMarker := normalizeCommandLineText(wrapperPath)
	scriptMarker := normalizeCommandLineText(filepath.Join(dir, "node_modules", "@anthropic-ai", "claude-code", "cli.js"))
	if !reflect.DeepEqual(markers, []string{wrapperMarker, scriptMarker}) {
		t.Fatalf("clientWrapperCommandLineMarkers() = %#v, want %#v", markers, []string{wrapperMarker, scriptMarker})
	}
	if !commandLineContainsMarker(`"C:\Program Files\nodejs\node.exe" "`+scriptMarker+`"`, markers) {
		t.Fatal("node command line should match the wrapper target script")
	}
	if commandLineContainsMarker(`"C:\Program Files\nodejs\node.exe" C:\work\unrelated.js`, markers) {
		t.Fatal("unrelated node command line should not match")
	}
}

func TestClientProcessImageNamesForWrappers(t *testing.T) {
	names := clientProcessImageNames(clientCodex, `C:\Users\test\AppData\Roaming\npm\codex.cmd`)
	want := []string{"ChatGPT.exe", "Codex.exe", "cmd.exe", "node.exe"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("clientProcessImageNames() = %#v, want %#v", names, want)
	}
	if !clientProcessRequiresCommandLine("node.exe", `C:\Users\test\AppData\Roaming\npm\codex.cmd`) {
		t.Fatal("generic node host should require a command-line marker")
	}
	if clientProcessRequiresCommandLine("Codex.exe", `C:\Users\test\AppData\Roaming\npm\codex.cmd`) {
		t.Fatal("native Codex process should match by its dedicated image name")
	}
}

func TestDetectClaudeProgramPathUsesDesktopInsteadOfCLI(t *testing.T) {
	localAppData := t.TempDir()
	desktopPath := filepath.Join(localAppData, "AnthropicClaude", "claude.exe")
	if err := os.MkdirAll(filepath.Dir(desktopPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktopPath, []byte("desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	cliDir := t.TempDir()
	cliPath := filepath.Join(cliDir, "claude.cmd")
	if err := os.WriteFile(cliPath, []byte("@echo off"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("PATH", cliDir)

	if got := detectClientProgramPath(clientClaudeCode, t.TempDir()); got != desktopPath {
		t.Fatalf("detected Claude program = %q, want desktop %q", got, desktopPath)
	}
	if got := detectClientProgramPath(clientClaudeCLI, t.TempDir()); got != cliPath {
		t.Fatalf("detected Claude CLI = %q, want %q", got, cliPath)
	}
	if err := os.Remove(desktopPath); err != nil {
		t.Fatal(err)
	}
	if got := detectClientProgramPath(clientClaudeCode, t.TempDir()); got != "" {
		t.Fatalf("Claude CLI must not be used as the desktop program, got %q", got)
	}
}

func TestClaudePathDetectionRevisionReplacesLegacyCLI(t *testing.T) {
	localAppData := t.TempDir()
	desktopPath := filepath.Join(localAppData, "AnthropicClaude", "claude.exe")
	if err := os.MkdirAll(filepath.Dir(desktopPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktopPath, []byte("desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	cliPath := filepath.Join(t.TempDir(), "custom-claude.exe")
	if err := os.WriteFile(cliPath, []byte("native cli"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", localAppData)

	cfg := defaultConfig()
	cfg.ClientPathsDetected = true
	cfg.ClientPathDetectionVersion = claudeDesktopCLISplitPathDetectionVersion - 1
	cfg.ClientProgramPaths = map[string]string{clientClaudeCode: cliPath}
	cfg = detectClientPaths(cfg, t.TempDir(), false)

	if got := cfg.ClientProgramPaths[clientClaudeCode]; got != desktopPath {
		t.Fatalf("migrated Claude program = %q, want desktop %q", got, desktopPath)
	}
	if got := cfg.ClientProgramPaths[clientClaudeCLI]; got != cliPath {
		t.Fatalf("migrated Claude CLI = %q, want preserved %q", got, cliPath)
	}
}

func TestClientProcessPathMatchesClaudeSquirrelLauncher(t *testing.T) {
	launcher := filepath.Join(`C:\Users\test\AppData\Local\AnthropicClaude`, "claude.exe")
	versioned := filepath.Join(`C:\Users\test\AppData\Local\AnthropicClaude`, "app-1.2.3", "claude.exe")
	if !clientProcessPathMatches(clientClaudeCode, launcher, versioned) {
		t.Fatal("Claude Squirrel launcher should match its versioned desktop process")
	}
	if clientProcessPathMatches(clientClaudeCode, launcher, `C:\Tools\claude.exe`) {
		t.Fatal("unrelated Claude CLI executable must not match Claude Desktop")
	}
	if clientProcessPathMatches(clientOpenCode, launcher, versioned) {
		t.Fatal("Claude path equivalence must not apply to another client")
	}
}

func TestDetectCodexCLIProgramPathFromPATH(t *testing.T) {
	dir := t.TempDir()
	cliPath := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(cliPath, []byte("@echo off"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got := detectClientProgramPath(clientCodexCLI, t.TempDir()); got != cliPath {
		t.Fatalf("detected Codex CLI = %q, want %q", got, cliPath)
	}
}

func TestCLIWrapperProcessNamesExcludeDesktopClients(t *testing.T) {
	if got := clientProcessImageNames(clientCodexCLI, `C:\npm\codex.cmd`); !reflect.DeepEqual(got, []string{"cmd.exe", "node.exe"}) {
		t.Fatalf("Codex CLI process names = %#v", got)
	}
	if got := clientProcessImageNames(clientClaudeCLI, `C:\npm\claude.cmd`); !reflect.DeepEqual(got, []string{"cmd.exe", "node.exe"}) {
		t.Fatalf("Claude CLI process names = %#v", got)
	}
}
