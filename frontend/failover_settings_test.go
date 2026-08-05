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
		"\u4f9b\u5e94\u5546\u5217\u8868\u4e2d\u7684\u201c\u4f7f\u7528\u201d\u6309\u94ae\u4f1a\u53d8\u6210\u201c\u52a0\u5165/\u9000\u51fa\u201d",
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
	style := string(styleRaw)
	if !strings.Contains(style, ".switch-control input:disabled + span") {
		t.Fatal("disabled switch styling is missing")
	}
	joinedRow := cssRuleBody(t, style, ".profile-row.failover-joined")
	for _, expected := range []string{"border-color: #86efac;", "background: #f0fdf4;"} {
		if !strings.Contains(joinedRow, expected) {
			t.Fatalf("joined row styling %q is missing from its CSS rule", expected)
		}
	}
	hoveredJoinedRow := cssRuleBody(t, style, ".profile-row.failover-joined:hover")
	for _, expected := range []string{"border-color: #4ade80;", "background: #ecfdf5;"} {
		if !strings.Contains(hoveredJoinedRow, expected) {
			t.Fatalf("joined row hover styling %q is missing from its CSS rule", expected)
		}
	}
	failoverBadge := cssRuleBody(t, style, ".profile-main .provider-failover-badge")
	for _, expected := range []string{"border: 1px solid #86efac;", "background: #f0fdf4;", "color: #15803d;"} {
		if !strings.Contains(failoverBadge, expected) {
			t.Fatalf("failover badge styling %q is missing from its CSS rule", expected)
		}
	}
	joinedButton := cssRuleBody(t, style, ".profile-failover.is-joined")
	for _, expected := range []string{"border-color: #86efac;", "background: #f0fdf4;", "color: #15803d;"} {
		if !strings.Contains(joinedButton, expected) {
			t.Fatalf("joined failover button styling %q is missing from its CSS rule", expected)
		}
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
		"const primaryActionLabel = failoverEnabled ? (failoverJoined ? \"\u9000\u51fa\" : \"\u52a0\u5165\") : \"\u4f7f\u7528\";",
		`const primaryActionClass = failoverEnabled`,
		`const rowStateClass = failoverEnabled`,
		`? (failoverJoined ? " failover-joined" : "")`,
		`row.className = ` + "`profile-row${rowStateClass}`" + `;`,
		`data-action="switch" title="${primaryActionTitle}"${primaryActionDisabled}>${primaryActionLabel}</button>`,
		`toggleProviderFailoverProfile(profile)`,
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

func cssRuleBody(t *testing.T, css, selector string) string {
	t.Helper()
	start := strings.Index(css, selector+" {")
	if start < 0 {
		t.Fatalf("CSS rule %q is missing", selector)
	}
	bodyStart := start + len(selector) + len(" {")
	bodyEnd := strings.Index(css[bodyStart:], "}")
	if bodyEnd < 0 {
		t.Fatalf("CSS rule %q is not closed", selector)
	}
	return css[bodyStart : bodyStart+bodyEnd]
}
