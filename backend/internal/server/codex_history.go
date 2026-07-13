package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const codexHistoryManifestVersion = 1

type codexHistoryRequest struct {
	Action string `json:"action"`
}

type codexHistoryManifest struct {
	Version    int                          `json:"version"`
	CreatedAt  time.Time                    `json:"created_at"`
	RestoredAt *time.Time                   `json:"restored_at,omitempty"`
	CodexDir   string                       `json:"codex_dir"`
	Sessions   []codexHistorySessionBackup  `json:"sessions,omitempty"`
	Databases  []codexHistoryDatabaseBackup `json:"databases,omitempty"`
}

type codexHistorySessionBackup struct {
	Path       string   `json:"path"`
	BackupPath string   `json:"backup_path"`
	SessionIDs []string `json:"session_ids"`
}

type codexHistoryDatabaseBackup struct {
	Path       string   `json:"path"`
	BackupPath string   `json:"backup_path"`
	ThreadIDs  []string `json:"thread_ids"`
}

type codexHistoryResult struct {
	HasBackup     bool   `json:"has_backup"`
	ConfigUpdated bool   `json:"config_updated,omitempty"`
	ManifestPath  string `json:"manifest_path,omitempty"`
	Sessions      int    `json:"sessions"`
	Threads       int    `json:"threads"`
	Files         int    `json:"files"`
	Databases     int    `json:"databases"`
}

func (a *app) handleCodexHistory(w http.ResponseWriter, r *http.Request) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		result, err := codexHistoryStatus(homeDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case http.MethodPost:
		var req codexHistoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		var result codexHistoryResult
		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "prepare":
			result.ConfigUpdated, err = prepareCodexUnifiedOfficialConfig(homeDir)
		case "unprepare":
			result.ConfigUpdated, err = restoreCodexOfficialProviderFromUnifiedHistory(homeDir)
		case "migrate":
			result, err = migrateCodexHistory(homeDir)
		case "restore":
			result, err = restoreCodexHistory(homeDir)
		default:
			err = errors.New("action must be prepare, unprepare, migrate, or restore")
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func prepareCodexUnifiedOfficialConfig(homeDir string) (bool, error) {
	path := filepath.Join(codexConfigDir(homeDir), "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if codexLinesContainRelayRootConfig(lines) || rootValueFromLines(lines, "model_provider") != "openai" {
		return false, nil
	}
	lines = removeTomlSection(lines, "model_providers.custom")
	inRoot := true
	providerUpdated := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
		}
		if inRoot && strings.HasPrefix(trimmed, "model_provider =") {
			lines[index] = `model_provider = "custom"`
			providerUpdated = true
			break
		}
	}
	if !providerUpdated {
		return false, nil
	}
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	content += "\n\n" + strings.Join(codexUnifiedOpenAIProviderBlock(), "\n") + "\n"
	if err := writeConfigFile(path, []byte(content)); err != nil {
		return false, err
	}
	return true, nil
}

func restoreCodexOfficialProviderFromUnifiedHistory(homeDir string) (bool, error) {
	path := filepath.Join(codexConfigDir(homeDir), "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if rootValueFromLines(lines, "model_provider") != "custom" || !codexHistoryLinesContainMarker(lines) {
		return false, nil
	}
	lines = removeTomlSection(lines, "model_providers.custom")
	inRoot := true
	providerUpdated := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == codexUnifiedHistoryMarker {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inRoot = false
		}
		if inRoot && strings.HasPrefix(trimmed, "model_provider =") {
			line = `model_provider = "openai"`
			providerUpdated = true
		}
		out = append(out, line)
	}
	if !providerUpdated {
		return false, nil
	}
	content := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if content != "" {
		content += "\n"
	}
	if err := writeConfigFile(path, []byte(content)); err != nil {
		return false, err
	}
	return true, nil
}

func codexHistoryLinesContainMarker(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == codexUnifiedHistoryMarker {
			return true
		}
	}
	return false
}

func codexHistoryStatus(homeDir string) (codexHistoryResult, error) {
	manifestPath, manifest, err := latestPendingCodexHistoryManifest(homeDir)
	if err != nil {
		return codexHistoryResult{}, err
	}
	if manifestPath == "" {
		return codexHistoryResult{}, nil
	}
	return codexHistoryResultFromManifest(manifestPath, manifest), nil
}

func migrateCodexHistory(homeDir string) (codexHistoryResult, error) {
	if result, err := codexHistoryStatus(homeDir); err != nil {
		return codexHistoryResult{}, err
	} else if result.HasBackup {
		return result, nil
	}
	codexDir := codexConfigDir(homeDir)
	backupDir := filepath.Join(codexHistoryBackupRoot(codexDir), time.Now().Format("20060102-150405.000000000"))
	manifest := codexHistoryManifest{
		Version:   codexHistoryManifestVersion,
		CreatedAt: time.Now(),
		CodexDir:  codexDir,
	}
	manifestPath := filepath.Join(backupDir, "manifest.json")
	if err := writeCodexHistoryManifest(manifestPath, manifest); err != nil {
		return codexHistoryResult{}, err
	}
	for _, rootName := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(codexDir, rootName)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
				return nil
			}
			backup, changed, err := migrateCodexSessionFile(codexDir, backupDir, path)
			if changed {
				manifest.Sessions = append(manifest.Sessions, backup)
				if manifestErr := writeCodexHistoryManifest(manifestPath, manifest); manifestErr != nil {
					return manifestErr
				}
			}
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return codexHistoryResult{}, err
		}
	}
	for index, dbPath := range codexStateDBPaths(codexDir) {
		backup, changed, err := migrateCodexStateDB(backupDir, dbPath, index)
		if changed {
			manifest.Databases = append(manifest.Databases, backup)
			if manifestErr := writeCodexHistoryManifest(manifestPath, manifest); manifestErr != nil {
				return codexHistoryResult{}, manifestErr
			}
		}
		if err != nil {
			return codexHistoryResult{}, err
		}
	}
	if len(manifest.Sessions) == 0 && len(manifest.Databases) == 0 {
		_ = os.RemoveAll(backupDir)
		return codexHistoryResult{}, nil
	}
	return codexHistoryResultFromManifest(manifestPath, manifest), nil
}

func restoreCodexHistory(homeDir string) (codexHistoryResult, error) {
	manifestPath, manifest, err := latestPendingCodexHistoryManifest(homeDir)
	if err != nil {
		return codexHistoryResult{}, err
	}
	if manifestPath == "" {
		return codexHistoryResult{}, nil
	}
	for _, session := range manifest.Sessions {
		if err := restoreCodexSessionFile(session.Path, session.SessionIDs); err != nil {
			return codexHistoryResult{}, err
		}
	}
	for _, database := range manifest.Databases {
		if err := restoreCodexStateDB(database.Path, database.ThreadIDs); err != nil {
			return codexHistoryResult{}, err
		}
	}
	restoredAt := time.Now()
	manifest.RestoredAt = &restoredAt
	if err := writeCodexHistoryManifest(manifestPath, manifest); err != nil {
		return codexHistoryResult{}, err
	}
	result := codexHistoryResultFromManifest(manifestPath, manifest)
	result.HasBackup = false
	return result, nil
}

func migrateCodexSessionFile(codexDir, backupDir, path string) (codexHistorySessionBackup, bool, error) {
	before, err := os.Stat(path)
	if err != nil {
		return codexHistorySessionBackup{}, false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return codexHistorySessionBackup{}, false, err
	}
	updated, ids := rewriteCodexSessionProviders(raw, "openai", "custom", nil)
	if len(ids) == 0 {
		return codexHistorySessionBackup{}, false, nil
	}
	after, err := os.Stat(path)
	if err != nil {
		return codexHistorySessionBackup{}, false, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return codexHistorySessionBackup{}, false, fmt.Errorf("Codex session changed while migrating: %s", path)
	}
	relative, err := filepath.Rel(codexDir, path)
	if err != nil {
		return codexHistorySessionBackup{}, false, err
	}
	backupPath := filepath.Join(backupDir, "sessions", relative)
	if err := writeRawFile(backupPath, raw); err != nil {
		return codexHistorySessionBackup{}, false, err
	}
	backup := codexHistorySessionBackup{Path: path, BackupPath: backupPath, SessionIDs: ids}
	if err := replaceCodexHistoryFile(path, updated); err != nil {
		return backup, true, err
	}
	return backup, true, nil
}

func restoreCodexSessionFile(path string, sessionIDs []string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	allowed := stringSet(sessionIDs)
	updated, ids := rewriteCodexSessionProviders(raw, "custom", "openai", allowed)
	if len(ids) == 0 {
		return nil
	}
	return replaceCodexHistoryFile(path, updated)
}

func rewriteCodexSessionProviders(raw []byte, from, to string, allowed map[string]struct{}) ([]byte, []string) {
	lines := bytes.Split(raw, []byte("\n"))
	ids := make([]string, 0)
	for index, line := range lines {
		trimmed := bytes.TrimSuffix(line, []byte("\r"))
		if len(bytes.TrimSpace(trimmed)) == 0 {
			continue
		}
		var event map[string]any
		if json.Unmarshal(trimmed, &event) != nil || firstString(event["type"]) != "session_meta" {
			continue
		}
		payload, _ := event["payload"].(map[string]any)
		id := firstString(payload["id"])
		if id == "" || firstString(payload["model_provider"]) != from {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[id]; !ok {
				continue
			}
		}
		payload["model_provider"] = to
		encoded, err := json.Marshal(event)
		if err != nil {
			continue
		}
		if bytes.HasSuffix(line, []byte("\r")) {
			encoded = append(encoded, '\r')
		}
		lines[index] = encoded
		ids = append(ids, id)
	}
	return bytes.Join(lines, []byte("\n")), uniqueStrings(ids)
}

func migrateCodexStateDB(backupDir, dbPath string, index int) (codexHistoryDatabaseBackup, bool, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return codexHistoryDatabaseBackup{}, false, nil
		}
		return codexHistoryDatabaseBackup{}, false, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return codexHistoryDatabaseBackup{}, false, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id FROM threads WHERE model_provider = 'openai'`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return codexHistoryDatabaseBackup{}, false, nil
		}
		return codexHistoryDatabaseBackup{}, false, err
	}
	threadIDs := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return codexHistoryDatabaseBackup{}, false, err
		}
		if id != "" {
			threadIDs = append(threadIDs, id)
		}
	}
	if err := rows.Close(); err != nil {
		return codexHistoryDatabaseBackup{}, false, err
	}
	if len(threadIDs) == 0 {
		return codexHistoryDatabaseBackup{}, false, nil
	}
	backupPath := filepath.Join(backupDir, "databases", fmt.Sprintf("%02d-%s", index, filepath.Base(dbPath)))
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return codexHistoryDatabaseBackup{}, false, err
	}
	if _, err := db.Exec(`VACUUM INTO ?`, backupPath); err != nil {
		return codexHistoryDatabaseBackup{}, false, fmt.Errorf("backup Codex state database: %w", err)
	}
	backup := codexHistoryDatabaseBackup{Path: dbPath, BackupPath: backupPath, ThreadIDs: uniqueStrings(threadIDs)}
	if _, err := db.Exec(`UPDATE threads SET model_provider = 'custom' WHERE model_provider = 'openai'`); err != nil {
		return backup, true, err
	}
	return backup, true, nil
}

func restoreCodexStateDB(dbPath string, threadIDs []string) error {
	if len(threadIDs) == 0 {
		return nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	for start := 0; start < len(threadIDs); start += 400 {
		end := start + 400
		if end > len(threadIDs) {
			end = len(threadIDs)
		}
		batch := threadIDs[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		query := `UPDATE threads SET model_provider = 'openai' WHERE model_provider = 'custom' AND id IN (` + placeholders + `)`
		if _, err := db.Exec(query, args...); err != nil {
			return err
		}
	}
	return nil
}

func codexStateDBPaths(codexDir string) []string {
	paths := make([]string, 0, 3)
	configPath := filepath.Join(codexDir, "config.toml")
	if raw, err := os.ReadFile(configPath); err == nil {
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		if sqliteHome := strings.TrimSpace(rootValueFromLines(lines, "sqlite_home")); sqliteHome != "" {
			paths = append(paths, codexSQLitePath(sqliteHome, codexDir))
		}
	}
	if sqliteHome := strings.TrimSpace(os.Getenv("CODEX_SQLITE_HOME")); sqliteHome != "" {
		paths = append(paths, codexSQLitePath(os.ExpandEnv(sqliteHome), codexDir))
	}
	paths = append(paths, filepath.Join(codexDir, "state_5.sqlite"))
	return uniqueStrings(paths)
}

func codexSQLitePath(path, codexDir string) string {
	path = filepath.Clean(os.ExpandEnv(path))
	if !filepath.IsAbs(path) {
		path = filepath.Join(codexDir, path)
	}
	if strings.EqualFold(filepath.Ext(path), ".sqlite") || strings.EqualFold(filepath.Ext(path), ".db") {
		return path
	}
	return filepath.Join(path, "state_5.sqlite")
}

func codexHistoryBackupRoot(codexDir string) string {
	return filepath.Join(codexDir, "vision-relay-history-backups", "unified")
}

func latestPendingCodexHistoryManifest(homeDir string) (string, codexHistoryManifest, error) {
	root := codexHistoryBackupRoot(codexConfigDir(homeDir))
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", codexHistoryManifest{}, nil
		}
		return "", codexHistoryManifest{}, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			paths = append(paths, filepath.Join(root, entry.Name(), "manifest.json"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	for _, path := range paths {
		manifest, err := readCodexHistoryManifest(path)
		if err != nil {
			return "", codexHistoryManifest{}, err
		}
		if manifest.RestoredAt == nil {
			return path, manifest, nil
		}
	}
	return "", codexHistoryManifest{}, nil
}

func readCodexHistoryManifest(path string) (codexHistoryManifest, error) {
	var manifest codexHistoryManifest
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func writeCodexHistoryManifest(path string, manifest codexHistoryManifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeRawFile(path, append(raw, '\n'))
}

func codexHistoryResultFromManifest(path string, manifest codexHistoryManifest) codexHistoryResult {
	result := codexHistoryResult{
		HasBackup:    manifest.RestoredAt == nil,
		ManifestPath: path,
		Files:        len(manifest.Sessions),
		Databases:    len(manifest.Databases),
	}
	for _, session := range manifest.Sessions {
		result.Sessions += len(session.SessionIDs)
	}
	for _, database := range manifest.Databases {
		result.Threads += len(database.ThreadIDs)
	}
	return result
}

func writeRawFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func replaceCodexHistoryFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vision-relay-history-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Rename(tmpPath, path)
	}
	oldPath := path + ".vision-relay-swap"
	_ = os.Remove(oldPath)
	if err := os.Rename(path, oldPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(oldPath, path)
		return err
	}
	return os.Remove(oldPath)
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
