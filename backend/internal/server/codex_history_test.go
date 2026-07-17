package server

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCodexConfigCanReplaceAndRestoreOfficialAuth(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	officialAuth := `{"OPENAI_API_KEY":null,"tokens":{"access_token":"official-token"}}`
	authPath := filepath.Join(codexDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(officialAuth), 0o600); err != nil {
		t.Fatal(err)
	}
	preserve := false
	ctx := clientConfigContext{
		HomeDir:              homeDir,
		Origin:               "http://127.0.0.1:8787",
		Key:                  "sk-client-key",
		DirectUpstream:       true,
		Model:                "glm-5",
		PreserveOfficialAuth: &preserve,
	}
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(codexAuthBackupPath(homeDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != officialAuth {
		t.Fatalf("official auth backup mismatch: %s", backup)
	}
	managedRaw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	var managed map[string]any
	if err := json.Unmarshal(managedRaw, &managed); err != nil {
		t.Fatal(err)
	}
	if managed["vision_relay_managed"] != true || managed["OPENAI_API_KEY"] != "sk-client-key" {
		t.Fatalf("unexpected managed auth: %#v", managed)
	}
	configRaw, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configRaw), "experimental_bearer_token") {
		t.Fatalf("managed auth mode should read the key from auth.json:\n%s", configRaw)
	}

	ctx.DirectUpstream = false
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != officialAuth {
		t.Fatalf("official auth was not restored for local no-auth mode: %s", restored)
	}
	configRaw, err = os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configRaw), "requires_openai_auth = false") || strings.Contains(string(configRaw), "experimental_bearer_token") {
		t.Fatalf("local Codex config should disable authentication:\n%s", configRaw)
	}

	ctx.DirectUpstream = true
	preserve = true
	if _, err := writeCodexConfig(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err = os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != officialAuth {
		t.Fatalf("official auth was not restored: %s", restored)
	}
	configRaw, err = os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configRaw), `experimental_bearer_token = "sk-client-key"`) {
		t.Fatalf("preserved auth mode should use the client-named token:\n%s", configRaw)
	}
}

func TestRestoreCodexAccountCanUseUnifiedCustomProvider(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	relayConfig := strings.Join([]string{
		`model = "glm-5"`,
		`model_provider = "custom"`,
		``,
		`[model_providers.custom]`,
		`name = "Vision Relay"`,
		`base_url = "http://127.0.0.1:8787/v1"`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(relayConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	officialConfig := strings.Join([]string{
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		`model_reasoning_effort = "high"`,
	}, "\n")
	if err := os.WriteFile(codexAccountBackupPath(homeDir), []byte(officialConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := restoreCodexAccountConfigWithOptions(homeDir, "", true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	for _, want := range []string{`model = "gpt-5.5"`, `model_provider = "custom"`, `[model_providers.custom]`, `name = "OpenAI"`, `supports_websockets = true`} {
		if !strings.Contains(after, want) {
			t.Fatalf("unified official config missing %s:\n%s", want, after)
		}
	}
	if strings.Contains(after, "127.0.0.1") || strings.Contains(after, "experimental_bearer_token") {
		t.Fatalf("unified official provider must not retain relay routing:\n%s", after)
	}
}

func TestPrepareCodexUnifiedOfficialConfigOnlyChangesOfficialProvider(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(codexDir, "config.toml")
	officialConfig := strings.Join([]string{
		`model = "gpt-5.5"`,
		`model_provider = "openai"`,
		`forced_login_method = "chatgpt"`,
		``,
		`[projects.'C:\\work']`,
		`trust_level = "trusted"`,
	}, "\n")
	if err := os.WriteFile(configPath, []byte(officialConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := prepareCodexUnifiedOfficialConfig(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("official config was not updated")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	after := string(raw)
	for _, want := range []string{`model_provider = "custom"`, `forced_login_method = "chatgpt"`, `[projects.'C:\\work']`, `[model_providers.custom]`, `name = "OpenAI"`} {
		if !strings.Contains(after, want) {
			t.Fatalf("prepared config missing %s:\n%s", want, after)
		}
	}
	if err := saveCodexAccountConfigBackup(homeDir, configPath); err != nil {
		t.Fatal(err)
	}
	backupRaw, err := os.ReadFile(codexAccountBackupPath(homeDir))
	if err != nil {
		t.Fatal(err)
	}
	backup := string(backupRaw)
	if !strings.Contains(backup, `model = "gpt-5.5"`) || !strings.Contains(backup, `model_provider = "openai"`) || strings.Contains(backup, `[model_providers.custom]`) {
		t.Fatalf("unified official backup was not normalized:\n%s", backup)
	}
	restored, err := restoreCodexOfficialProviderFromUnifiedHistory(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !restored {
		t.Fatal("unified official provider was not restored")
	}
	raw, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	after = string(raw)
	if !strings.Contains(after, `model_provider = "openai"`) || strings.Contains(after, codexUnifiedHistoryMarker) || strings.Contains(after, `[model_providers.custom]`) {
		t.Fatalf("official provider was not restored cleanly:\n%s", after)
	}
	if !strings.Contains(after, `forced_login_method = "chatgpt"`) || !strings.Contains(after, `[projects.'C:\\work']`) {
		t.Fatalf("restoring provider removed unrelated config:\n%s", after)
	}

	thirdPartyConfig := strings.Join([]string{
		`# Added by Vision Relay. Edit from the Client Access page.`,
		`model_provider = "custom"`,
		``,
		`[model_providers.custom]`,
		`name = "Vision Relay"`,
		`base_url = "http://127.0.0.1:8787/v1"`,
	}, "\n")
	if err := os.WriteFile(configPath, []byte(thirdPartyConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err = prepareCodexUnifiedOfficialConfig(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("third-party config must not be replaced with official routing")
	}
	raw, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != thirdPartyConfig {
		t.Fatalf("third-party config changed unexpectedly:\n%s", raw)
	}
	restored, err = restoreCodexOfficialProviderFromUnifiedHistory(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if restored {
		t.Fatal("third-party config must not be restored as official")
	}
}

func TestCodexHistoryMigrationRestoresOnlyRecordedIDs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	codexDir := filepath.Join(homeDir, ".codex")
	sessionPath := filepath.Join(codexDir, "sessions", "2026", "07", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	officialLine := `{"type":"session_meta","payload":{"id":"official-session","model_provider":"openai"}}`
	customLine := `{"type":"session_meta","payload":{"id":"existing-custom","model_provider":"custom"}}`
	if err := os.WriteFile(sessionPath, []byte(officialLine+"\n"+customLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(codexDir, "state_5.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, model_provider TEXT NOT NULL)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads(id, model_provider) VALUES ('official-thread', 'openai'), ('existing-custom-thread', 'custom')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := migrateCodexHistory(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasBackup || result.Sessions != 1 || result.Threads != 1 {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	if _, err := os.Stat(result.ManifestPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	afterMigration, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(afterMigration), `"model_provider":"openai"`) {
		t.Fatalf("official session was not migrated: %s", afterMigration)
	}
	newCustomLine := `{"type":"session_meta","payload":{"id":"new-custom","model_provider":"custom"}}`
	if file, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	} else {
		if _, err := file.WriteString(newCustomLine + "\n"); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads(id, model_provider) VALUES ('new-custom-thread', 'custom')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	restored, err := restoreCodexHistory(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if restored.HasBackup || restored.Sessions != 1 || restored.Threads != 1 {
		t.Fatalf("unexpected restore result: %#v", restored)
	}
	afterRestore, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	providers := codexSessionProviders(t, afterRestore)
	if providers["official-session"] != "openai" || providers["existing-custom"] != "custom" || providers["new-custom"] != "custom" {
		t.Fatalf("sessions were not precisely restored: %#v", providers)
	}
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for id, want := range map[string]string{
		"official-thread":        "openai",
		"existing-custom-thread": "custom",
		"new-custom-thread":      "custom",
	} {
		var got string
		if err := db.QueryRow(`SELECT model_provider FROM threads WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("thread %s provider = %s, want %s", id, got, want)
		}
	}
}

func codexSessionProviders(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	providers := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		payload, _ := event["payload"].(map[string]any)
		providers[firstString(payload["id"])] = firstString(payload["model_provider"])
	}
	return providers
}
