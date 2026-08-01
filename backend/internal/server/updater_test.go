package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVersionNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.1.2", "v1.1.2", false},
		{"v1.1.1", "v1.1.2", false},
		{"v2.0.0", "dev", true},
		{"1.3.0", "v1.2.9-4-gabcdef", true},
	}
	for _, tt := range tests {
		if got := versionNewer(tt.latest, tt.current); got != tt.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestAutoCheckUpdatesDefaultsAndMerges(t *testing.T) {
	defaults := defaultConfig()
	if defaults.AutoCheckUpdates == nil || !*defaults.AutoCheckUpdates {
		t.Fatal("automatic update checks should be enabled by default")
	}

	disabled := false
	merged := mergeConfig(defaults, config{AutoCheckUpdates: &disabled})
	if merged.AutoCheckUpdates == nil || *merged.AutoCheckUpdates {
		t.Fatal("explicitly disabled automatic update checks were not preserved")
	}

	legacy := mergeConfig(defaultConfig(), config{})
	if legacy.AutoCheckUpdates == nil || !*legacy.AutoCheckUpdates {
		t.Fatal("legacy config should keep automatic update checks enabled")
	}
}

func TestFetchLatestReleaseUsesFeedWithoutAnonymousAPICall(t *testing.T) {
	var apiCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases.atom":
			w.Header().Set("Content-Type", "application/atom+xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <updated>2026-07-25T18:45:27Z</updated>
    <link rel="alternate" type="text/html" href="https://github.com/xshentx/vision-relay/releases/tag/v2.2.2"/>
    <title>Vision Relay v2.2.2</title>
    <content type="html">&lt;h2&gt;Changes&lt;/h2&gt;&lt;ul&gt;&lt;li&gt;Fixes &amp;amp; improvements&lt;/li&gt;&lt;/ul&gt;</content>
  </entry>
</feed>`)
		case "/api":
			apiCalls++
			http.Error(w, "rate limited", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldAPI, oldFeed := latestReleaseAPI, latestReleaseFeed
	latestReleaseAPI, latestReleaseFeed = server.URL+"/api", server.URL+"/releases.atom"
	defer func() { latestReleaseAPI, latestReleaseFeed = oldAPI, oldFeed }()
	t.Setenv("VISION_RELAY_GITHUB_TOKEN", "")

	release, err := (&app{httpClient: server.Client()}).fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if apiCalls != 0 {
		t.Fatalf("anonymous REST API calls = %d, want 0", apiCalls)
	}
	if release.TagName != "v2.2.2" || release.Name != "Vision Relay v2.2.2" {
		t.Fatalf("unexpected release: %#v", release)
	}
	if strings.Contains(release.Body, "<") || !strings.Contains(release.Body, "Fixes & improvements") {
		t.Fatalf("release notes were not converted to plain text: %q", release.Body)
	}
	asset, ok := selectReleaseAsset(release.Assets, "windows", "amd64")
	if !ok || asset.Name != "vision-relay.exe" || !strings.Contains(asset.BrowserDownloadURL, "/v2.2.2/") {
		t.Fatalf("unexpected synthesized Windows asset: %#v, %v", asset, ok)
	}
	if _, ok := findAsset(release.Assets, "vision-relay.exe.sha256"); !ok {
		t.Fatal("synthesized release is missing the Windows checksum asset")
	}
	if _, ok := findAsset(release.Assets, "vision-relay.exe.sig"); !ok {
		t.Fatal("synthesized release is missing the Windows signature asset")
	}
}

func TestFetchLatestReleaseFallsBackToFeedAfterAuthenticatedRateLimit(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			authorization = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"API rate limit exceeded"}`)
		case "/releases.atom":
			_, _ = io.WriteString(w, `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom"><entry>
<updated>2026-07-25T18:45:27Z</updated>
<link rel="alternate" href="https://github.com/xshentx/vision-relay/releases/tag/v2.2.2"/>
<title>Vision Relay v2.2.2</title><content type="html">Notes</content>
</entry></feed>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldAPI, oldFeed := latestReleaseAPI, latestReleaseFeed
	latestReleaseAPI, latestReleaseFeed = server.URL+"/api", server.URL+"/releases.atom"
	defer func() { latestReleaseAPI, latestReleaseFeed = oldAPI, oldFeed }()
	t.Setenv("VISION_RELAY_GITHUB_TOKEN", "test-token")

	release, err := (&app{httpClient: server.Client()}).fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want bearer token", authorization)
	}
	if release.TagName != "v2.2.2" {
		t.Fatalf("fallback release tag = %q", release.TagName)
	}
}

func findAsset(assets []githubAsset, name string) (githubAsset, bool) {
	for _, asset := range assets {
		if strings.EqualFold(asset.Name, name) {
			return asset, true
		}
	}
	return githubAsset{}, false
}

func TestSelectWindowsAssetPrefersCanonicalName(t *testing.T) {
	assets := []githubAsset{
		{Name: "vision-relay-windows-amd64.exe", BrowserDownloadURL: "fallback"},
		{Name: "vision-relay.exe", BrowserDownloadURL: "canonical"},
	}
	got, ok := selectWindowsAsset(assets)
	if !ok || got.BrowserDownloadURL != "canonical" {
		t.Fatalf("selectWindowsAsset() = %#v, %v", got, ok)
	}
}

func TestDownloadUpdateReportsProgress(t *testing.T) {
	payload := append([]byte("MZ"), bytes.Repeat([]byte{0x5a}, 256*1024)...)
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previousPublicKey := UpdatePublicKey
	UpdatePublicKey = base64.StdEncoding.EncodeToString(publicKey)
	t.Cleanup(func() { UpdatePublicKey = previousPublicKey })
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:]))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checksum" {
			_, _ = w.Write([]byte(checksum + "  vision-relay.exe\n"))
			return
		}
		if r.URL.Path == "/signature" {
			_, _ = w.Write([]byte(signature + "\n"))
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	asset := githubAsset{Name: "vision-relay.exe", BrowserDownloadURL: server.URL, Size: int64(len(payload))}
	checksumAsset := githubAsset{Name: "vision-relay.exe.sha256", BrowserDownloadURL: server.URL + "/checksum"}
	signatureAsset := githubAsset{Name: "vision-relay.exe.sig", BrowserDownloadURL: server.URL + "/signature"}
	info := updateInfo{AssetSize: asset.Size, asset: asset, release: githubRelease{Assets: []githubAsset{asset, checksumAsset, signatureAsset}}}
	a := &app{httpClient: server.Client()}
	var reports []updateProgress
	destinationDir := t.TempDir()
	path, err := a.downloadUpdate(context.Background(), info, destinationDir, func(state string, downloaded, total int64) {
		reports = append(reports, updateProgress{State: state, DownloadedBytes: downloaded, TotalBytes: total})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if filepath.Dir(path) != destinationDir {
		t.Fatalf("download directory = %q, want %q", filepath.Dir(path), destinationDir)
	}
	if filepath.Base(path) != "vision-relay.update" {
		t.Fatalf("download filename = %q, want stable non-executable staging name", filepath.Base(path))
	}
	if len(reports) < 3 {
		t.Fatalf("progress report count = %d, want at least 3", len(reports))
	}
	last := reports[len(reports)-1]
	if last.State != "verifying" || last.DownloadedBytes != int64(len(payload)) || last.TotalBytes != int64(len(payload)) {
		t.Fatalf("unexpected final progress: %#v", last)
	}
}

func TestDownloadUpdateRequiresChecksum(t *testing.T) {
	payload := append([]byte("MZ"), bytes.Repeat([]byte{0x5a}, 1024)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	asset := githubAsset{Name: "vision-relay.exe", BrowserDownloadURL: server.URL, Size: int64(len(payload))}
	info := updateInfo{AssetSize: asset.Size, asset: asset, release: githubRelease{Assets: []githubAsset{asset}}}
	a := &app{httpClient: server.Client()}
	path, err := a.downloadUpdate(context.Background(), info, t.TempDir(), nil)
	if err == nil {
		_ = os.Remove(path)
		t.Fatal("downloadUpdate accepted a release without a checksum")
	}
	if !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("missing-checksum error = %q", err)
	}
}

func TestUpdateProgressEndpointAndDuplicateGuard(t *testing.T) {
	a := &app{}
	if !a.beginUpdate() {
		t.Fatal("first update task should start")
	}
	if a.beginUpdate() {
		t.Fatal("duplicate update task should be rejected")
	}

	recorder := httptest.NewRecorder()
	a.handleUpdateProgress(recorder, httptest.NewRequest(http.MethodGet, "/api/update/progress", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("progress status = %d, want 200", recorder.Code)
	}
	var progress updateProgress
	if err := json.Unmarshal(recorder.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.State != "checking" || progress.Message == "" {
		t.Fatalf("unexpected progress payload: %#v", progress)
	}

	recorder = httptest.NewRecorder()
	a.handleUpdateProgress(recorder, httptest.NewRequest(http.MethodPost, "/api/update/progress", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST progress status = %d, want 405", recorder.Code)
	}
}

func TestSelectDarwinAssetMatchesArchitecture(t *testing.T) {
	assets := []githubAsset{
		{Name: "vision-relay-darwin-amd64.zip", BrowserDownloadURL: "intel"},
		{Name: "vision-relay-darwin-arm64.zip", BrowserDownloadURL: "apple-silicon"},
		{Name: "vision-relay-darwin-universal.zip", BrowserDownloadURL: "universal"},
	}
	got, ok := selectReleaseAsset(assets, "darwin", "arm64")
	if !ok || got.BrowserDownloadURL != "apple-silicon" {
		t.Fatalf("arm64 Darwin asset = %#v, %v", got, ok)
	}
	got, ok = selectReleaseAsset(assets, "darwin", "amd64")
	if !ok || got.BrowserDownloadURL != "intel" {
		t.Fatalf("amd64 Darwin asset = %#v, %v", got, ok)
	}
	if _, ok := selectReleaseAsset(assets, "linux", "arm64"); ok {
		t.Fatal("unsupported Linux target unexpectedly selected a release asset")
	}
}
