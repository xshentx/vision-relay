package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	asset := githubAsset{Name: "vision-relay.exe", BrowserDownloadURL: server.URL, Size: int64(len(payload))}
	info := updateInfo{AssetSize: asset.Size, asset: asset, release: githubRelease{Assets: []githubAsset{asset}}}
	a := &app{httpClient: server.Client()}
	var reports []updateProgress
	path, err := a.downloadUpdate(context.Background(), info, func(state string, downloaded, total int64) {
		reports = append(reports, updateProgress{State: state, DownloadedBytes: downloaded, TotalBytes: total})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if len(reports) < 3 {
		t.Fatalf("progress report count = %d, want at least 3", len(reports))
	}
	last := reports[len(reports)-1]
	if last.State != "verifying" || last.DownloadedBytes != int64(len(payload)) || last.TotalBytes != int64(len(payload)) {
		t.Fatalf("unexpected final progress: %#v", last)
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
