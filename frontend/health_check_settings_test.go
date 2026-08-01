package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

func TestProviderHealthCheckSettingsAreRemoved(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	assets := string(indexRaw) + "\n" + string(scriptRaw)
	for _, obsolete := range []string{
		"\u4f9b\u5e94\u5546\u5065\u5eb7\u68c0\u67e5",
		"providerHealthCheckEnabled",
		"providerHealthCheckIntervalMinutes",
		"provider_health_check_enabled",
		"provider_health_check_interval_seconds",
	} {
		if strings.Contains(assets, obsolete) {
			t.Fatalf("obsolete health-check setting %q is still embedded", obsolete)
		}
	}
}
