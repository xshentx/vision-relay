package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

func TestClientConfigureActionsAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, buttonID := range []string{"configureOpenCode", "configureCodex", "configureClaudeCode"} {
		if !strings.Contains(index, `id="`+buttonID+`"`) {
			t.Fatalf("client configure button %q is missing", buttonID)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, clientID := range []string{`client: "opencode"`, `client: "codex"`, `client: "claude-code"`} {
		if !strings.Contains(script, clientID) {
			t.Fatalf("client configure action %q is missing", clientID)
		}
	}
	if !strings.Contains(script, `fetch("/api/client/configure"`) || !strings.Contains(script, `body: JSON.stringify({client})`) {
		t.Fatal("client configure actions are not wired to the shared API request")
	}
}
