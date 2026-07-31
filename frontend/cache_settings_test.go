package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

func TestVisionCacheSettingsAreEmbedded(t *testing.T) {
	indexRaw, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexRaw)
	for _, id := range []string{"visionCacheTTLDays", "visionCacheMaxEntries", "visionCacheState", "clearVisionCache"} {
		if !strings.Contains(index, `id="`+id+`"`) {
			t.Fatalf("vision cache setting %q is missing", id)
		}
	}

	scriptRaw, err := fs.ReadFile(FS, "assets/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, expected := range []string{"vision_cache_ttl_hours", "vision_cache_max_entries", `fetch("/api/vision-cache"`} {
		if !strings.Contains(script, expected) {
			t.Fatalf("vision cache UI logic %q is missing", expected)
		}
	}
}
