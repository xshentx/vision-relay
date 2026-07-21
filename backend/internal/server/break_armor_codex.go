package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const breakArmorCodexBlockBegin = "# BEGIN Vision Relay Break Armor"
const breakArmorCodexBlockEnd = "# END Vision Relay Break Armor"

type breakArmorCodexFieldState struct {
	Original    string `json:"original,omitempty"`
	HadOriginal bool   `json:"had_original"`
}

type breakArmorCodexGlobalState struct {
	Version    int                                  `json:"version"`
	ConfigPath string                               `json:"config_path"`
	Fields     map[string]breakArmorCodexFieldState `json:"fields,omitempty"`
	// Legacy fields keep version-1 state files restorable.
	Field       string    `json:"field,omitempty"`
	Original    string    `json:"original,omitempty"`
	HadOriginal bool      `json:"had_original,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func breakArmorCodexGlobalStatePath(home string) string {
	return filepath.Join(home, ".vision-relay", "break-armor", "codex", "global-state.json")
}
func breakArmorCodexConfigBody(paths breakArmorPaths, prompt, injection string) string {
	injection = strings.ToLower(strings.TrimSpace(injection))
	if injection == "append" {
		escaped := strings.ReplaceAll(strings.ReplaceAll(prompt, "\\", "\\\\"), "\"\"\"", "\\\"\\\"\\\"")
		return "developer_instructions = \"\"\"\n" + escaped + "\n\"\"\"\n"
	}
	promptRef := filepath.ToSlash(paths.PromptPath)
	if strings.Contains(promptRef, "/.codex/") {
		promptRef = "~/.codex/" + strings.SplitN(promptRef, "/.codex/", 2)[1]
	}
	return "model_instructions_file = " + strconvQuote(promptRef) + "\n"
}
func strconvQuote(value string) string { raw, _ := json.Marshal(value); return string(raw) }
func writeBreakArmorCodexConfig(home string, paths breakArmorPaths, prompt, injection string) error {
	if paths.Client != breakArmorClientCodex || paths.Mode == "workspace" {
		return nil
	}
	body := breakArmorCodexConfigBody(paths, prompt, injection)
	if paths.Mode == "profile" {
		raw, err := os.ReadFile(paths.ConfigPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		content := removeBreakArmorManagedBlock(string(raw))
		content, _, _ = removeBreakArmorRootField(content, "developer_instructions")
		content, _, _ = removeBreakArmorRootField(content, "model_instructions_file")
		managed := "# Codex CTF profile managed by Vision Relay\n" + breakArmorCodexBlockBegin + "\n" + body + breakArmorCodexBlockEnd + "\n\n"
		content = managed + strings.TrimLeft(content, "\r\n")
		if err := os.MkdirAll(filepath.Dir(paths.ConfigPath), 0o700); err != nil {
			return err
		}
		return os.WriteFile(paths.ConfigPath, []byte(content), 0o600)
	}
	return writeBreakArmorCodexGlobal(home, paths.ConfigPath, body)
}
func rootBreakArmorField(body string) string {
	if strings.HasPrefix(strings.TrimSpace(body), "developer_instructions") {
		return "developer_instructions"
	}
	return "model_instructions_file"
}
func writeBreakArmorCodexGlobal(home, configPath, body string) error {
	raw, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(raw)
	statePath := breakArmorCodexGlobalStatePath(home)
	if !strings.Contains(content, breakArmorCodexBlockBegin) {
		// A state file without its managed block is stale (for example after an
		// external restore). Capture the current one-click-compatible fields as
		// the new restore point instead of reviving obsolete state.
		if removeErr := os.Remove(statePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
	}
	if _, stateErr := os.Stat(statePath); errors.Is(stateErr, os.ErrNotExist) {
		fields := map[string]breakArmorCodexFieldState{}
		for _, field := range []string{"developer_instructions", "model_instructions_file"} {
			original, had := extractBreakArmorRootField(content, field)
			fields[field] = breakArmorCodexFieldState{Original: original, HadOriginal: had}
		}
		state := breakArmorCodexGlobalState{Version: 2, ConfigPath: configPath, Fields: fields, CreatedAt: time.Now()}
		if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
			return err
		}
		stateRaw, _ := json.MarshalIndent(state, "", "  ")
		if err := os.WriteFile(statePath, stateRaw, 0o600); err != nil {
			return err
		}
	} else if stateErr != nil {
		return stateErr
	}
	content = removeBreakArmorManagedBlock(content)
	content, _, _ = removeBreakArmorRootField(content, "developer_instructions")
	content, _, _ = removeBreakArmorRootField(content, "model_instructions_file")
	managed := breakArmorCodexBlockBegin + "\n" + body + breakArmorCodexBlockEnd + "\n\n"
	content = managed + strings.TrimLeft(content, "\r\n")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(content), 0o600)
}
func extractBreakArmorRootField(content, field string) (string, bool) {
	_, raw, found := removeBreakArmorRootField(content, field)
	return raw, found
}
func breakArmorTOMLMultilineDelimiter(line string) (string, bool) {
	equals := strings.IndexByte(line, '=')
	if equals < 0 {
		return "", false
	}
	value := strings.TrimSpace(line[equals+1:])
	for _, delimiter := range []string{`"""`, `'''`} {
		if strings.HasPrefix(value, delimiter) {
			return delimiter, !breakArmorTOMLHasClosingDelimiter(value[len(delimiter):], delimiter)
		}
	}
	return "", false
}

func breakArmorTOMLHasClosingDelimiter(value, delimiter string) bool {
	if delimiter == `'''` {
		return strings.Contains(value, delimiter)
	}
	for offset := 0; ; {
		index := strings.Index(value[offset:], delimiter)
		if index < 0 {
			return false
		}
		index += offset
		backslashes := 0
		for i := index - 1; i >= 0 && value[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return true
		}
		offset = index + len(delimiter)
	}
}

func removeBreakArmorRootField(content, field string) (string, string, bool) {
	lines := strings.SplitAfter(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	section := false
	pattern := regexp.MustCompile("^\\s*" + regexp.QuoteMeta(field) + "\\s*=")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			section = true
		}
		if section {
			break
		}
		if !pattern.MatchString(line) {
			continue
		}
		end := i + 1
		if delimiter, open := breakArmorTOMLMultilineDelimiter(line); open {
			for end < len(lines) {
				if breakArmorTOMLHasClosingDelimiter(lines[end], delimiter) {
					end++
					break
				}
				end++
			}
		}
		raw := strings.Join(lines[i:end], "")
		out := append(append([]string{}, lines[:i]...), lines[end:]...)
		return strings.Join(out, ""), raw, true
	}
	return content, "", false
}
func removeBreakArmorManagedBlock(content string) string {
	for {
		start := strings.Index(content, breakArmorCodexBlockBegin)
		if start < 0 {
			return content
		}
		endRel := strings.Index(content[start:], breakArmorCodexBlockEnd)
		if endRel < 0 {
			return content[:start]
		}
		end := start + endRel + len(breakArmorCodexBlockEnd)
		for end < len(content) && (content[end] == '\r' || content[end] == '\n') {
			end++
		}
		content = content[:start] + content[end:]
	}
}

func breakArmorCodexConfigManaged(configPath string) bool {
	raw, err := os.ReadFile(configPath)
	return err == nil && strings.Contains(string(raw), breakArmorCodexBlockBegin)
}

// The reference implementation makes Codex Profile and global injection
// mutually exclusive. Restoring the inactive mode before applying the selected
// one also prevents a shared prompt file from making both modes active.
func disableOtherBreakArmorCodexModes(cfg config, home, selectedMode string) error {
	profilePaths, err := breakArmorClientPathsForMode(cfg, home, breakArmorClientCodex, "profile")
	if err != nil {
		return err
	}
	globalPaths, err := breakArmorClientPathsForMode(cfg, home, breakArmorClientCodex, "global")
	if err != nil {
		return err
	}
	selectedMode = normalizeBreakArmorMode(breakArmorClientCodex, selectedMode)
	if selectedMode != "profile" && breakArmorCodexConfigManaged(profilePaths.ConfigPath) {
		if _, found, snapshotErr := latestBreakArmorSnapshot(home, breakArmorClientCodex, "profile"); snapshotErr != nil {
			return snapshotErr
		} else if !found {
			return errors.New("Codex Profile is active but its restore snapshot is missing; mode switch stopped to protect the existing configuration")
		}
		if _, err := restoreBreakArmorSnapshot(home, breakArmorClientCodex, "profile"); err != nil {
			return err
		}
	}
	if selectedMode != "global" && breakArmorCodexConfigManaged(globalPaths.ConfigPath) {
		if _, found, snapshotErr := latestBreakArmorSnapshot(home, breakArmorClientCodex, "global"); snapshotErr != nil {
			return snapshotErr
		} else if !found {
			return errors.New("Codex global mode is active but its restore snapshot is missing; mode switch stopped to protect the existing configuration")
		}
		if err := restoreBreakArmorCodexGlobalMode(home); err != nil {
			return err
		}
	}
	return nil
}

func breakArmorCodexProfileBroken(paths breakArmorPaths) bool {
	raw, err := os.ReadFile(paths.ConfigPath)
	return err == nil && strings.Contains(string(raw), breakArmorCodexBlockBegin) && pathIsFile(paths.PromptPath)
}
func breakArmorCodexGlobalBroken(configPath, promptPath string) bool {
	raw, err := os.ReadFile(configPath)
	return err == nil && strings.Contains(string(raw), breakArmorCodexBlockBegin) && pathIsFile(promptPath)
}

type breakArmorCodexGlobalRestorePlan struct {
	StatePath     string
	ConfigPath    string
	Content       []byte
	CurrentConfig breakArmorSnapshotFile
}

func writeBreakArmorFileAtomic(path string, raw []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".break-armor-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if mode == 0 {
		mode = 0o600
	}
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func prepareBreakArmorCodexGlobalRestore(home string) (breakArmorCodexGlobalRestorePlan, error) {
	statePath := breakArmorCodexGlobalStatePath(home)
	raw, err := os.ReadFile(statePath)
	if err != nil {
		return breakArmorCodexGlobalRestorePlan{}, fmt.Errorf("读取 Codex 全局恢复状态失败: %w", err)
	}
	var state breakArmorCodexGlobalState
	if json.Unmarshal(raw, &state) != nil || state.ConfigPath == "" {
		return breakArmorCodexGlobalRestorePlan{}, errors.New("Codex 全局恢复状态已损坏")
	}
	configRaw, err := os.ReadFile(state.ConfigPath)
	if err != nil {
		return breakArmorCodexGlobalRestorePlan{}, err
	}
	currentConfig, err := snapshotBreakArmorFile(state.ConfigPath, "config")
	if err != nil {
		return breakArmorCodexGlobalRestorePlan{}, err
	}
	content := removeBreakArmorManagedBlock(string(configRaw))
	content, _, _ = removeBreakArmorRootField(content, "developer_instructions")
	content, _, _ = removeBreakArmorRootField(content, "model_instructions_file")
	fields := state.Fields
	if len(fields) == 0 && state.Field != "" {
		fields = map[string]breakArmorCodexFieldState{state.Field: {Original: state.Original, HadOriginal: state.HadOriginal}}
	}
	var originals strings.Builder
	for _, field := range []string{"developer_instructions", "model_instructions_file"} {
		if saved, ok := fields[field]; ok && saved.HadOriginal {
			originals.WriteString(saved.Original)
			if !strings.HasSuffix(saved.Original, "\n") {
				originals.WriteByte('\n')
			}
		}
	}
	content = originals.String() + strings.TrimLeft(content, "\r\n")
	return breakArmorCodexGlobalRestorePlan{
		StatePath: statePath, ConfigPath: state.ConfigPath, Content: []byte(content), CurrentConfig: currentConfig,
	}, nil
}

func applyBreakArmorCodexGlobalRestore(plan breakArmorCodexGlobalRestorePlan) error {
	mode := os.FileMode(plan.CurrentConfig.Mode)
	if mode == 0 {
		mode = 0o600
	}
	return writeBreakArmorFileAtomic(plan.ConfigPath, plan.Content, mode)
}

func validateBreakArmorSnapshotManifest(manifest breakArmorSnapshotManifest) error {
	if len(manifest.Files) == 0 {
		return errors.New("破甲快照清单不包含任何文件")
	}
	for _, entry := range manifest.Files {
		if entry.Kind != "prompt" && entry.Kind != "config" && entry.Kind != "" {
			return errors.New("破甲快照清单包含不支持的文件类型")
		}
		if entry.Path == "" {
			return errors.New("破甲快照清单包含空文件路径")
		}
		if entry.Existed {
			if _, err := base64.StdEncoding.DecodeString(entry.Data); err != nil {
				return errors.New("破甲快照清单包含无效的文件数据")
			}
		}
	}
	return nil
}

func rollbackBreakArmorSnapshotFiles(entries []breakArmorSnapshotFile) error {
	var rollbackErr error
	for i := len(entries) - 1; i >= 0; i-- {
		rollbackErr = errors.Join(rollbackErr, restoreBreakArmorSnapshotFile(entries[i]))
	}
	return rollbackErr
}

// restoreBreakArmorCodexGlobalMode treats the prompt snapshot, global config,
// and restore-state marker as one recoverable operation. Any failed step rolls
// the files back and leaves the state marker available for a retry.
func restoreBreakArmorCodexGlobalMode(home string) error {
	plan, err := prepareBreakArmorCodexGlobalRestore(home)
	if err != nil {
		return err
	}
	manifest, found, err := latestBreakArmorSnapshot(home, breakArmorClientCodex, "global")
	if err != nil {
		return err
	}
	if !found {
		return errors.New("找不到 Codex 全局模式恢复快照")
	}
	if err = validateBreakArmorSnapshotManifest(manifest); err != nil {
		return err
	}
	currentFiles := make([]breakArmorSnapshotFile, 0, len(manifest.Files))
	for _, entry := range manifest.Files {
		current, snapshotErr := snapshotBreakArmorFile(entry.Path, entry.Kind)
		if snapshotErr != nil {
			return snapshotErr
		}
		currentFiles = append(currentFiles, current)
	}
	for i, entry := range manifest.Files {
		if err = restoreBreakArmorSnapshotFile(entry); err != nil {
			return errors.Join(err, rollbackBreakArmorSnapshotFiles(currentFiles[:i]))
		}
	}
	if err = applyBreakArmorCodexGlobalRestore(plan); err != nil {
		return errors.Join(err, rollbackBreakArmorSnapshotFiles(currentFiles))
	}
	if err = os.Remove(plan.StatePath); err != nil {
		configRollbackErr := restoreBreakArmorSnapshotFile(plan.CurrentConfig)
		filesRollbackErr := rollbackBreakArmorSnapshotFiles(currentFiles)
		return errors.Join(err, configRollbackErr, filesRollbackErr)
	}
	return nil
}

func restoreBreakArmorCodexGlobal(home string) error {
	plan, err := prepareBreakArmorCodexGlobalRestore(home)
	if err != nil {
		return err
	}
	if err = applyBreakArmorCodexGlobalRestore(plan); err != nil {
		return err
	}
	return os.Remove(plan.StatePath)
}
