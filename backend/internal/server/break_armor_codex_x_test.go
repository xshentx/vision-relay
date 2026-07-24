package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type breakArmorRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn breakArmorRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func breakArmorHTTPResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestBundledBreakArmorCodexXTemplatesMatchPinnedUpstream(t *testing.T) {
	wantHashes := map[string]string{
		"gpt5.5-unrestricted.md":      "9dd61978522d4f3fd6247348ae5e44be57774173861c34e5778401be44562a16",
		"gpt5.4-unrestricted.md":      "f68aea9afd065eefd9486e6fb14b7f93ef94aaab3634063bd65d115e43e06388",
		"gpt5.5-jeli.md":              "9d57577869557e1a18b1df412be7c4bfd85ba64db5fa70ed863397a84b497094",
		"gpt-5.6-sol-unrestricted.md": "5867af4e6d039fb331e2368ec13499b01c8e93d189e072631f31a226108becf7",
		"海鸥3.0破甲.md":                  "0f41b90e995ec4bffea7d093b424df98ba2c1f142c9540b7a662558e8d13bafc",
	}
	items := bundledBreakArmorCodexXTemplates(breakArmorClientCodex)
	if len(items) != len(wantHashes) {
		t.Fatalf("got %d bundled templates, want %d", len(items), len(wantHashes))
	}
	for _, item := range items {
		wantHash, ok := wantHashes[item.Name]
		if !ok {
			t.Fatalf("unexpected bundled template: %#v", item)
		}
		raw, err := breakArmorCodexXBundledFS.ReadFile("break_armor_codex_x_templates/" + item.Name)
		if err != nil {
			t.Fatal(err)
		}
		normalized := []byte(strings.ReplaceAll(string(raw), "\r\n", "\n"))
		if got := fmt.Sprintf("%x", sha256.Sum256(normalized)); got != wantHash {
			t.Fatalf("%s hash=%s want=%s", item.Name, got, wantHash)
		}
		if item.ID != codexXTemplateID(item.Name) || item.Client != breakArmorClientCodex || item.Description == "" || item.Prompt == "" || !item.Builtin || !item.Bundled || !item.ReadOnly || item.Source != breakArmorCodexXSource || item.SourceRevision != breakArmorCodexXBundledRevision {
			t.Fatalf("unexpected bundled metadata: %#v", item)
		}
	}
}

func TestFetchBreakArmorCodexXTemplatesValidatesAndMapsCatalog(t *testing.T) {
	promptBody := "  # prompt\n\nrun and verify  "
	catalog := fmt.Sprintf(`[
		{"name":"gpt-5.6-sol-unrestricted.md","path":"examples/gpt-5.6-sol-unrestricted.md","type":"file","sha":%q,"download_url":"https://raw.githubusercontent.com/yynxxxxx/Codex-X/main/examples/gpt-5.6-sol-unrestricted.md"},
		{"name":"README.md","path":"examples/README.md","type":"file","sha":"skip","download_url":"https://example.com/README.md"}
	]`, gitBlobSHA([]byte(promptBody)))
	client := &http.Client{Transport: breakArmorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case breakArmorCodexXCatalogURL:
			return breakArmorHTTPResponse(http.StatusOK, catalog), nil
		case "https://raw.githubusercontent.com/yynxxxxx/Codex-X/main/examples/gpt-5.6-sol-unrestricted.md":
			return breakArmorHTTPResponse(http.StatusOK, promptBody), nil
		default:
			t.Fatalf("unexpected URL: %s", req.URL)
			return nil, nil
		}
	})}
	items, err := fetchBreakArmorCodexXTemplates(context.Background(), client, breakArmorClientCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d templates, want 1", len(items))
	}
	item := items[0]
	if item.ID != "codex-x:gpt-5.6-sol-unrestricted.md" || item.Name != "gpt-5.6-sol-unrestricted.md" {
		t.Fatalf("unexpected template identity: %#v", item)
	}
	if item.Client != breakArmorClientCodex || item.Description == "" || item.Prompt != "# prompt\n\nrun and verify" || !item.ReadOnly || item.Source != breakArmorCodexXSource || item.SourceRevision != gitBlobSHA([]byte(promptBody)) {
		t.Fatalf("unexpected mapped template: %#v", item)
	}
}

func TestFetchBreakArmorCodexXTemplatesRejectsDuplicateCanonicalIDBeforeDownload(t *testing.T) {
	catalog := `[
		{"name":"gpt5.5-unrestricted.md","path":"examples/gpt5.5-unrestricted.md","type":"file","sha":"first","download_url":"https://raw.githubusercontent.com/yynxxxxx/Codex-X/main/examples/gpt5.5-unrestricted.md"},
		{"name":"gpt5.5-unrestricted.md","path":"examples/gpt5.5-unrestricted.md","type":"file","sha":"second","download_url":"https://raw.githubusercontent.com/yynxxxxx/Codex-X/main/examples/gpt5.5-unrestricted.md"}
	]`
	rawCalls := 0
	client := &http.Client{Transport: breakArmorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == breakArmorCodexXCatalogURL {
			return breakArmorHTTPResponse(http.StatusOK, catalog), nil
		}
		rawCalls++
		return breakArmorHTTPResponse(http.StatusOK, "must not download"), nil
	})}
	_, err := fetchBreakArmorCodexXTemplates(context.Background(), client, breakArmorClientCodex)
	if err == nil || !strings.Contains(err.Error(), "重复 ID") {
		t.Fatalf("expected duplicate-ID error, got %v", err)
	}
	if rawCalls != 0 {
		t.Fatalf("downloaded %d templates before detecting duplicate IDs", rawCalls)
	}
}

func TestFetchBreakArmorCodexXTemplatesRejectsBlobSHAMismatch(t *testing.T) {
	catalog := `[{"name":"gpt5.5-unrestricted.md","path":"examples/gpt5.5-unrestricted.md","type":"file","sha":"0000000000000000000000000000000000000000","download_url":"https://raw.githubusercontent.com/yynxxxxx/Codex-X/main/examples/gpt5.5-unrestricted.md"}]`
	client := &http.Client{Transport: breakArmorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == breakArmorCodexXCatalogURL {
			return breakArmorHTTPResponse(http.StatusOK, catalog), nil
		}
		return breakArmorHTTPResponse(http.StatusOK, "different content"), nil
	})}
	_, err := fetchBreakArmorCodexXTemplates(context.Background(), client, breakArmorClientCodex)
	if err == nil || !strings.Contains(err.Error(), "内容与目录版本不一致") {
		t.Fatalf("expected blob-SHA mismatch error, got %v", err)
	}
}

func TestFetchBreakArmorCodexXTemplatesRejectsUntrustedDownloadHost(t *testing.T) {
	catalog := `[{"name":"gpt5.5-unrestricted.md","path":"examples/gpt5.5-unrestricted.md","type":"file","sha":"abc","download_url":"https://example.com/gpt5.5-unrestricted.md"}]`
	client := &http.Client{Transport: breakArmorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return breakArmorHTTPResponse(http.StatusOK, catalog), nil
	})}
	_, err := fetchBreakArmorCodexXTemplates(context.Background(), client, breakArmorClientCodex)
	if err == nil || !strings.Contains(err.Error(), "不受信任") {
		t.Fatalf("expected untrusted-host error, got %v", err)
	}
}

func TestFetchBreakArmorCodexXTemplatesRejectsWrongRepositoryPath(t *testing.T) {
	catalog := `[{"name":"gpt5.5-unrestricted.md","path":"examples/gpt5.5-unrestricted.md","type":"file","sha":"abc","download_url":"https://raw.githubusercontent.com/yynxxxxx/Codex-X/main/README.md"}]`
	client := &http.Client{Transport: breakArmorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return breakArmorHTTPResponse(http.StatusOK, catalog), nil
	})}
	_, err := fetchBreakArmorCodexXTemplates(context.Background(), client, breakArmorClientCodex)
	if err == nil || !strings.Contains(err.Error(), "超出参考目录") {
		t.Fatalf("expected out-of-directory error, got %v", err)
	}
}

func TestFetchBreakArmorHTTPBodyRejectsRedirect(t *testing.T) {
	client := &http.Client{Transport: breakArmorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Header:     http.Header{"Location": []string{"https://example.com/prompt.md"}},
			Request:    req,
		}, nil
	})}
	_, err := fetchBreakArmorHTTPBody(context.Background(), client, breakArmorCodexXCatalogURL, 1024)
	if err == nil || !strings.Contains(err.Error(), "返回 302") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}

func TestReplaceBreakArmorCodexXTemplatesIsAtomicPerClient(t *testing.T) {
	home := t.TempDir()
	store := breakArmorTemplateStore{Version: 1, Templates: []breakArmorSavedTemplate{
		{ID: "mine", Client: breakArmorClientCodex, Name: "Mine", Prompt: "keep me"},
		{ID: "codex-x:old.md", Client: breakArmorClientCodex, Name: "Old", Prompt: "replace me", Source: breakArmorCodexXSource, ReadOnly: true},
		{ID: "codex-x:claude.md", Client: breakArmorClientClaude, Name: "Claude", Prompt: "keep other client", Source: breakArmorCodexXSource, ReadOnly: true},
	}}
	if err := saveBreakArmorTemplates(home, store); err != nil {
		t.Fatal(err)
	}
	updated := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	items := []breakArmorSavedTemplate{{ID: "codex-x:new.md", Name: "New", Prompt: "new prompt", UpdatedAt: updated}}
	changed, err := replaceBreakArmorCodexXTemplates(home, breakArmorClientCodex, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].ID != "codex-x:new.md" {
		t.Fatalf("unexpected changed templates: %#v", changed)
	}
	loaded, err := loadBreakArmorTemplates(home)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]breakArmorSavedTemplate{}
	for _, item := range loaded.Templates {
		byID[item.ID] = item
	}
	if _, ok := byID["codex-x:old.md"]; ok {
		t.Fatal("stale Codex-X template was not replaced")
	}
	if byID["mine"].Prompt != "keep me" || byID["codex-x:claude.md"].Prompt != "keep other client" {
		t.Fatalf("unrelated templates changed: %#v", loaded.Templates)
	}
	item := byID["codex-x:new.md"]
	if item.Client != breakArmorClientCodex || item.Source != breakArmorCodexXSource || !item.ReadOnly || item.Builtin {
		t.Fatalf("new template metadata not normalized: %#v", item)
	}
}

func TestReplaceBreakArmorCodexXTemplatesSkipsUnchangedBundledContent(t *testing.T) {
	home := t.TempDir()
	bundled := bundledBreakArmorCodexXTemplates(breakArmorClientCodex)[0]
	remote := bundled
	remote.Builtin = false
	remote.Bundled = false
	remote.SourceRevision = "remote-blob"
	remote.UpdatedAt = time.Now().UTC()

	changed, err := replaceBreakArmorCodexXTemplates(home, breakArmorClientCodex, []breakArmorSavedTemplate{remote})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("unchanged bundled content reported as updated: %#v", changed)
	}

	store, err := loadBreakArmorTemplates(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Templates) != 0 {
		t.Fatalf("unchanged bundled content was persisted: %#v", store.Templates)
	}
}

func TestReplaceBreakArmorCodexXTemplatesCountsOnlyContentChanges(t *testing.T) {
	home := t.TempDir()
	bundled := bundledBreakArmorCodexXTemplates(breakArmorClientCodex)[0]
	firstUpdatedAt := time.Date(2026, 7, 25, 1, 0, 0, 0, time.UTC)
	remote := bundled
	remote.Prompt = bundled.Prompt + "\nremote update"
	remote.SourceRevision = "remote-blob"
	remote.UpdatedAt = firstUpdatedAt

	changed, err := replaceBreakArmorCodexXTemplates(home, breakArmorClientCodex, []breakArmorSavedTemplate{remote})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("first content change count=%d want=1", len(changed))
	}
	remote.UpdatedAt = firstUpdatedAt.Add(time.Hour)
	changed, err = replaceBreakArmorCodexXTemplates(home, breakArmorClientCodex, []breakArmorSavedTemplate{remote})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("unchanged cached content reported as updated: %#v", changed)
	}
	store, err := loadBreakArmorTemplates(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Templates) != 1 || !store.Templates[0].UpdatedAt.Equal(firstUpdatedAt) {
		t.Fatalf("unchanged cache was rewritten with a new timestamp: %#v", store.Templates)
	}

	reverted := bundled
	reverted.SourceRevision = "reverted-blob"
	reverted.UpdatedAt = firstUpdatedAt.Add(2 * time.Hour)
	changed, err = replaceBreakArmorCodexXTemplates(home, breakArmorClientCodex, []breakArmorSavedTemplate{reverted})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("revert to bundled content count=%d want=1", len(changed))
	}
	store, err = loadBreakArmorTemplates(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Templates) != 0 {
		t.Fatalf("cache matching bundled content was not removed: %#v", store.Templates)
	}
}

func TestListBreakArmorTemplatesOverlaysGitHubUpdateWithoutDuplicates(t *testing.T) {
	home := t.TempDir()
	updatedID := codexXTemplateID("gpt5.5-unrestricted.md")
	store := breakArmorTemplateStore{Version: 1, Templates: []breakArmorSavedTemplate{
		{ID: updatedID, Client: breakArmorClientCodex, Name: "legacy-name-without-extension", Prompt: "github update", Source: breakArmorCodexXSource, SourceRevision: "new-sha", ReadOnly: true, UpdatedAt: time.Now()},
		{ID: "mine", Client: breakArmorClientCodex, Name: "Mine", Prompt: "custom"},
	}}
	if err := saveBreakArmorTemplates(home, store); err != nil {
		t.Fatal(err)
	}
	items, err := listBreakArmorTemplates(home, breakArmorClientCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 8 {
		t.Fatalf("got %d templates, want 7 built-ins plus 1 custom", len(items))
	}
	matches := 0
	for _, item := range items {
		if item.ID != updatedID {
			continue
		}
		matches++
		if item.Name != "gpt5.5-unrestricted.md" || item.Prompt != "github update" || item.Description == "" || !item.Builtin || item.Bundled || !item.ReadOnly || item.SourceRevision != "new-sha" {
			t.Fatalf("GitHub update did not overlay bundled template: %#v", item)
		}
	}
	if matches != 1 {
		t.Fatalf("updated template appeared %d times, want once", matches)
	}
}

func TestReadOnlyBreakArmorTemplateIDs(t *testing.T) {
	for _, id := range []string{"v5", "v35", "codex-x:gpt5.5.md"} {
		if !isReadOnlyBreakArmorTemplateID(id) {
			t.Fatalf("%s should be read-only", id)
		}
	}
	if isReadOnlyBreakArmorTemplateID("tpl-custom") {
		t.Fatal("custom template should remain editable")
	}
}
