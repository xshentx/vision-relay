package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func defaultDBPath() string {
	exe, err := os.Executable()
	if err == nil && exe != "" {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if dir := filepath.Dir(exe); dir != "" && dir != "." {
			return filepath.Join(dir, "codex-proxy.db")
		}
	}
	return "codex-proxy.db"
}

func legacyUserConfigDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "codex-proxy.db"
	}
	return filepath.Join(dir, "codex-proxy", "codex-proxy.db")
}

func migrateLegacyDBIfNeeded(dst *sql.DB, dstPath string) (config, bool, error) {
	var cfg config
	legacyPath := legacyUserConfigDBPath()
	if sameFilePath(dstPath, legacyPath) {
		return cfg, false, nil
	}
	if _, err := os.Stat(legacyPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, false, nil
		}
		return cfg, false, err
	}
	legacyDB, err := openAppDB(legacyPath)
	if err != nil {
		return cfg, false, err
	}
	defer legacyDB.Close()
	loaded, ok, err := loadConfigFromDB(legacyDB)
	if err != nil {
		return cfg, false, err
	}
	if ok {
		if err := saveConfigToDB(dst, loaded); err != nil {
			return cfg, false, err
		}
		cfg = loaded
	}
	logs, err := listRequestLogsDB(legacyDB, maxLogs)
	if err == nil {
		for i := len(logs) - 1; i >= 0; i-- {
			_ = insertRequestLogDB(dst, logs[i])
		}
	}
	return cfg, ok, nil
}

func sameFilePath(a, b string) bool {
	aa, err := filepath.Abs(filepath.Clean(a))
	if err == nil {
		a = aa
	}
	bb, err := filepath.Abs(filepath.Clean(b))
	if err == nil {
		b = bb
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func openAppDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := initDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS request_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	at TEXT NOT NULL,
	method TEXT NOT NULL,
	path TEXT NOT NULL,
	protocol TEXT NOT NULL,
	model TEXT NOT NULL,
	client_name TEXT NOT NULL,
	client_key_preview TEXT NOT NULL,
	status INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	first_token_ms INTEGER NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL,
	output_tokens INTEGER NOT NULL,
	total_tokens INTEGER NOT NULL,
	cache_hit_tokens INTEGER NOT NULL,
	cache_write_tokens INTEGER NOT NULL,
	error TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_request_logs_at ON request_logs(at DESC);
`)
	if err != nil {
		return err
	}
	return ensureRequestLogColumns(db)
}

func ensureRequestLogColumns(db *sql.DB) error {
	hasFirstToken := false
	rows, err := db.Query(`PRAGMA table_info(request_logs)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "first_token_ms" {
			hasFirstToken = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasFirstToken {
		_, err := db.Exec(`ALTER TABLE request_logs ADD COLUMN first_token_ms INTEGER NOT NULL DEFAULT 0`)
		return err
	}
	return nil
}

func loadConfigFromDB(db *sql.DB) (config, bool, error) {
	var cfg config
	var raw string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = 'config'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, false, err
	}
	return cfg, true, nil
}

func saveConfigToDB(db *sql.DB, cfg config) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
INSERT INTO settings(key, value, updated_at)
VALUES('config', ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, string(b), time.Now().Format(time.RFC3339Nano))
	return err
}

func insertRequestLogDB(db *sql.DB, log requestLog) error {
	_, err := db.Exec(`
INSERT INTO request_logs(
	at, method, path, protocol, model, client_name, client_key_preview, status, duration_ms, first_token_ms,
	input_tokens, output_tokens, total_tokens, cache_hit_tokens, cache_write_tokens, error
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, log.At.Format(time.RFC3339Nano), log.Method, log.Path, log.Protocol, log.Model, log.ClientName, log.ClientKeyPreview,
		log.Status, log.DurationMS, log.FirstTokenMS, log.InputTokens, log.OutputTokens, log.TotalTokens, log.CacheHitTokens, log.CacheWriteTokens, log.Error)
	return err
}

func listRequestLogsDB(db *sql.DB, limit int) ([]requestLog, error) {
	return listRequestLogsPageDB(db, limit, 0)
}

func listRequestLogsPageDB(db *sql.DB, limit, offset int) ([]requestLog, error) {
	rows, err := db.Query(`
SELECT id, at, method, path, protocol, model, client_name, client_key_preview, status, duration_ms, first_token_ms,
       input_tokens, output_tokens, total_tokens, cache_hit_tokens, cache_write_tokens, error
FROM request_logs
ORDER BY id DESC
LIMIT ? OFFSET ?
`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]requestLog, 0)
	for rows.Next() {
		var log requestLog
		var at string
		if err := rows.Scan(&log.ID, &at, &log.Method, &log.Path, &log.Protocol, &log.Model, &log.ClientName, &log.ClientKeyPreview,
			&log.Status, &log.DurationMS, &log.FirstTokenMS, &log.InputTokens, &log.OutputTokens, &log.TotalTokens, &log.CacheHitTokens, &log.CacheWriteTokens, &log.Error); err != nil {
			return nil, err
		}
		log.At, _ = time.Parse(time.RFC3339Nano, at)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func countRequestLogsDB(db *sql.DB) (int, error) {
	var total int
	err := db.QueryRow(`SELECT COUNT(*) FROM request_logs`).Scan(&total)
	return total, err
}

func clearRequestLogsDB(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM request_logs`)
	return err
}
