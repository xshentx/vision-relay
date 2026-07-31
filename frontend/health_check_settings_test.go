package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

func TestProviderHealthCheckSettingsAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, expected := range []string{
		"\u4f9b\u5e94\u5546\u5065\u5eb7\u68c0\u67e5",
		`id="providerHealthCheckEnabled"`,
		`id="providerHealthCheckIntervalMinutes"`,
		"\u666e\u901a\u5ba2\u6237\u7aef\u8bf7\u6c42\u548c\u624b\u52a8\u6a21\u578b\u6d4b\u8bd5\u4e5f\u4f1a\u5237\u65b0\u5065\u5eb7\u72b6\u6001",
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("health check setting %q is missing", expected)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		"provider_health_check_enabled",
		"provider_health_check_interval_seconds",
		"providerHealthCheckIntervalMinutes * 60",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("health check UI logic %q is missing", expected)
		}
	}
}
