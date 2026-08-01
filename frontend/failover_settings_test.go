package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

func TestProviderFailoverControlsAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, expected := range []string{
		"\u4f9b\u5e94\u5546\u6545\u969c\u8f6c\u79fb",
		`id="providerFailoverEnabled"`,
		"P1\u3001P2",
		"\u70b9\u51fb\u201c\u52a0\u5165\u201d\u7684\u4f9b\u5e94\u5546",
		"\u4ec5\u5728\u672c\u5730 API \u63a5\u53e3\u5f00\u542f\u65f6\u53ef\u7528",
		"\u5173\u95ed\u672c\u5730\u670d\u52a1\u540e\u4f9b\u5e94\u5546\u6545\u969c\u8f6c\u79fb\u548c\u89c6\u89c9\u6a21\u578b\u5c06\u4e0d\u53ef\u7528",
	} {
		if !strings.Contains(index, expected) {
			t.Fatalf("failover setting %q is missing", expected)
		}
	}

	styleRaw, err := fs.ReadFile(FS, "assets/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(styleRaw), ".switch-control input:disabled + span") {
		t.Fatal("disabled switch styling is missing")
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{
		"provider_failover_enabled",
		"provider_failover_profiles",
		"normalizeProviderFailoverProfiles",
		`data-action="failover"`,
		"providerFailoverBadge",
		"providerFailoverControlsEnabled",
		`return settingsLocalAPIEnabled?.checked !== false && programSettings.providerFailoverEnabled === true && providerFailoverEnabled?.checked !== false;`,
		`providerFailoverEnabled.disabled = disabled;`,
		`if (disabled) providerFailoverEnabled.checked = false;`,
		`providerFailoverEnabled?.addEventListener("change"`,
		"syncProviderFailoverOrderToProfiles",
		`kind === "text" && providerFailoverControlsEnabled()`,
		`providerFailoverControlsEnabled() ? providerFailoverBadge(profile) : ""`,
		"const previousProgramSettings = programSettings",
		"programSettings = previousProgramSettings",
		"\u8bbe\u7f6e\u5df2\u4fdd\u5b58\uff0c\u4f46\u5237\u65b0\u9875\u9762\u72b6\u6001\u5931\u8d25",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("failover UI logic %q is missing", expected)
		}
	}
	persisted := strings.Index(script, `await persistConfig("");`)
	markedSaved := strings.Index(script, "settingsSaved = true;")
	refreshed := strings.Index(script, "await refreshVisionCacheStats();")
	if persisted < 0 || markedSaved < persisted || refreshed < markedSaved {
		t.Fatalf("settings save markers are in the wrong order: persist=%d saved=%d refresh=%d", persisted, markedSaved, refreshed)
	}
}
