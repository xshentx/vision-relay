package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func openTestVisionCacheDB(t *testing.T) (*app, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vision-relay.db")
	db, err := openAppDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := defaultConfig()
	return &app{cfg: cfg, db: db}, path
}

func TestInitDBCreatesVisionCacheTable(t *testing.T) {
	a, _ := openTestVisionCacheDB(t)
	var name string
	if err := a.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'vision_cache'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "vision_cache" {
		t.Fatalf("table = %q", name)
	}
}

func TestVisionCachePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vision-relay.db")
	db, err := openAppDB(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	first := &app{cfg: cfg, db: db}
	first.storeVisionText("image-key", "persisted description")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = openAppDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	second := &app{cfg: cfg, db: db}
	if err := second.initializeVisionCache(); err != nil {
		t.Fatal(err)
	}
	text, ok := second.cachedVisionText("image-key")
	if !ok || text != "persisted description" {
		t.Fatalf("cache after restart = %q, %v", text, ok)
	}
}

func TestVisionMemoryCacheUsesLRU(t *testing.T) {
	cfg := defaultConfig()
	cfg.VisionCacheMaxEntries = 2
	a := &app{cfg: cfg}
	a.storeVisionText("a", "A")
	a.storeVisionText("b", "B")
	if _, ok := a.cachedVisionText("a"); !ok {
		t.Fatal("expected a to be cached")
	}
	a.storeVisionText("c", "C")
	if _, ok := a.cachedVisionText("b"); ok {
		t.Fatal("least recently used entry b was not evicted")
	}
	if _, ok := a.cachedVisionText("a"); !ok {
		t.Fatal("recently used entry a was evicted")
	}
	if _, ok := a.cachedVisionText("c"); !ok {
		t.Fatal("new entry c was not cached")
	}
}

func TestVisionSQLiteCachePrunesLeastRecentlyUsed(t *testing.T) {
	a, _ := openTestVisionCacheDB(t)
	a.cfg.VisionCacheMaxEntries = 2
	a.storeVisionText("a", "A")
	time.Sleep(time.Millisecond)
	a.storeVisionText("b", "B")
	time.Sleep(time.Millisecond)
	if _, ok := a.cachedVisionText("a"); !ok {
		t.Fatal("expected a to be cached")
	}
	time.Sleep(time.Millisecond)
	a.storeVisionText("c", "C")

	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM vision_cache`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("cache row count = %d, want 2", count)
	}
	var evicted int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM vision_cache WHERE cache_key = 'b'`).Scan(&evicted); err != nil {
		t.Fatal(err)
	}
	if evicted != 0 {
		t.Fatal("SQLite did not evict the least recently used entry")
	}
}

func TestVisionCacheExpiredEntryIsRemoved(t *testing.T) {
	a, _ := openTestVisionCacheDB(t)
	now := time.Now().UTC()
	entry := visionCacheEntry{
		Key:        "expired",
		Text:       "old",
		CreatedAt:  now.Add(-2 * time.Hour),
		LastUsedAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(-time.Minute),
	}
	if err := upsertVisionCacheDB(a.db, entry); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.cachedVisionText("expired"); ok {
		t.Fatal("expired entry was returned")
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM vision_cache WHERE cache_key = 'expired'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("expired entry was not deleted")
	}
}

func TestVisionCacheTTLChangeAppliesToExistingEntries(t *testing.T) {
	a, _ := openTestVisionCacheDB(t)
	now := time.Now().UTC()
	entry := visionCacheEntry{
		Key:        "old-entry",
		Text:       "old",
		CreatedAt:  now.Add(-2 * time.Hour),
		LastUsedAt: now,
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
	}
	if err := upsertVisionCacheDB(a.db, entry); err != nil {
		t.Fatal(err)
	}
	if err := recalculateVisionCacheExpiryDB(a.db, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := pruneVisionCacheDB(a.db, defaultVisionCacheMaxEntries, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM vision_cache WHERE cache_key = 'old-entry'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("shorter TTL did not expire an existing entry")
	}
}

func TestVisionCacheHandlerReportsAndClearsCache(t *testing.T) {
	a, _ := openTestVisionCacheDB(t)
	a.storeVisionText("one", "description")

	getRecorder := httptest.NewRecorder()
	a.handleVisionCache(getRecorder, httptest.NewRequest(http.MethodGet, "/api/vision-cache", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}
	var stats struct {
		Entries    int  `json:"entries"`
		Persistent bool `json:"persistent"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 || !stats.Persistent {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	deleteRecorder := httptest.NewRecorder()
	a.handleVisionCache(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/vision-cache", nil))
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if _, ok := a.cachedVisionText("one"); ok {
		t.Fatal("memory cache was not cleared")
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM vision_cache`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("SQLite cache row count = %d", count)
	}
}

func TestVisionCacheConfigDefaultsAndLimits(t *testing.T) {
	cfg := mergeConfig(defaultConfig(), config{})
	if cfg.VisionCacheTTLHours != defaultVisionCacheTTLHours {
		t.Fatalf("TTL default = %d", cfg.VisionCacheTTLHours)
	}
	if cfg.VisionCacheMaxEntries != defaultVisionCacheMaxEntries {
		t.Fatalf("max entries default = %d", cfg.VisionCacheMaxEntries)
	}
	if got := normalizeVisionCacheTTLHours(maxVisionCacheTTLHours + 1); got != maxVisionCacheTTLHours {
		t.Fatalf("TTL limit = %d", got)
	}
	if got := normalizeVisionCacheMaxEntries(maxVisionCacheEntries + 1); got != maxVisionCacheEntries {
		t.Fatalf("entry limit = %d", got)
	}
}
