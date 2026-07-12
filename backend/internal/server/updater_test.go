package server

import "testing"

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
