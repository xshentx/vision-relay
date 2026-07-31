package server

import (
	"container/list"
	"database/sql"
	"errors"
	"log"
	"sync"
	"time"
)

type visionCacheEntry struct {
	Key        string
	Text       string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

type visionCacheStore struct {
	mu         sync.Mutex
	items      map[string]*list.Element
	lru        *list.List
	maxEntries int
	ttl        time.Duration
}

func newVisionCacheStore(maxEntries, ttlHours int) *visionCacheStore {
	return &visionCacheStore{
		items:      make(map[string]*list.Element),
		lru:        list.New(),
		maxEntries: normalizeVisionCacheMaxEntries(maxEntries),
		ttl:        time.Duration(normalizeVisionCacheTTLHours(ttlHours)) * time.Hour,
	}
}

func (c *visionCacheStore) configure(maxEntries, ttlHours int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxEntries = normalizeVisionCacheMaxEntries(maxEntries)
	newTTL := time.Duration(normalizeVisionCacheTTLHours(ttlHours)) * time.Hour
	if c.ttl != newTTL {
		c.ttl = newTTL
		now := time.Now().UTC()
		for element := c.lru.Back(); element != nil; {
			previous := element.Prev()
			entry := element.Value.(*visionCacheEntry)
			entry.ExpiresAt = entry.CreatedAt.Add(newTTL)
			if !entry.ExpiresAt.After(now) {
				c.removeElementLocked(element)
			}
			element = previous
		}
	}
	c.trimLocked()
}

func (c *visionCacheStore) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return "", false
	}
	entry := element.Value.(*visionCacheEntry)
	if !entry.ExpiresAt.After(now) {
		c.removeElementLocked(element)
		return "", false
	}
	entry.LastUsedAt = now
	c.lru.MoveToFront(element)
	return entry.Text, true
}

func (c *visionCacheStore) put(entry visionCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[entry.Key]; ok {
		stored := element.Value.(*visionCacheEntry)
		*stored = entry
		c.lru.MoveToFront(element)
	} else {
		copyEntry := entry
		element := c.lru.PushFront(&copyEntry)
		c.items[entry.Key] = element
	}
	c.trimLocked()
}

func (c *visionCacheStore) remove(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		if element, ok := c.items[key]; ok {
			c.removeElementLocked(element)
		}
	}
}

func (c *visionCacheStore) clear() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := len(c.items)
	c.items = make(map[string]*list.Element)
	c.lru.Init()
	return count
}

func (c *visionCacheStore) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *visionCacheStore) trimLocked() {
	for len(c.items) > c.maxEntries {
		c.removeElementLocked(c.lru.Back())
	}
}

func (c *visionCacheStore) removeElementLocked(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(*visionCacheEntry)
	delete(c.items, entry.Key)
	c.lru.Remove(element)
}

func visionCacheSettings(cfg config) (int, int) {
	return normalizeVisionCacheMaxEntries(cfg.VisionCacheMaxEntries), normalizeVisionCacheTTLHours(cfg.VisionCacheTTLHours)
}

func (a *app) ensureVisionCacheStore(cfg config) *visionCacheStore {
	maxEntries, ttlHours := visionCacheSettings(cfg)
	a.mu.Lock()
	if a.visionCache == nil {
		a.visionCache = newVisionCacheStore(maxEntries, ttlHours)
	}
	store := a.visionCache
	a.mu.Unlock()
	store.configure(maxEntries, ttlHours)
	return store
}

func (a *app) initializeVisionCache() error {
	cfg := a.currentConfig()
	maxEntries, _ := visionCacheSettings(cfg)
	store := a.ensureVisionCacheStore(cfg)
	if a.db == nil {
		return nil
	}
	if err := a.pruneVisionCache(cfg); err != nil {
		return err
	}
	entries, err := loadRecentVisionCacheDB(a.db, maxEntries, time.Now().UTC())
	if err != nil {
		return err
	}
	// The query is newest-first. Insert oldest-first so the front remains the
	// most recently used item after the in-memory LRU is warmed.
	for i := len(entries) - 1; i >= 0; i-- {
		store.put(entries[i])
	}
	return nil
}

func (a *app) configureVisionCache(cfg config, resetTTL bool) error {
	a.ensureVisionCacheStore(cfg)
	if resetTTL && a.db != nil {
		_, ttlHours := visionCacheSettings(cfg)
		if err := recalculateVisionCacheExpiryDB(a.db, ttlHours); err != nil {
			return err
		}
	}
	return a.pruneVisionCache(cfg)
}

func (a *app) pruneVisionCache(cfg config) error {
	store := a.ensureVisionCacheStore(cfg)
	if a.db == nil {
		return nil
	}
	maxEntries, _ := visionCacheSettings(cfg)
	removed, err := pruneVisionCacheDB(a.db, maxEntries, time.Now().UTC())
	if err != nil {
		return err
	}
	store.remove(removed)
	return nil
}

func (a *app) cachedVisionText(key string) (string, bool) {
	cfg := a.currentConfig()
	store := a.ensureVisionCacheStore(cfg)
	now := time.Now().UTC()
	if text, ok := store.get(key, now); ok {
		if a.db != nil {
			if _, err := a.db.Exec(`UPDATE vision_cache SET last_used_at = ? WHERE cache_key = ?`, formatCacheTime(now), key); err != nil {
				log.Printf("vision cache last-used update warning: %v", err)
			}
		}
		return text, true
	}
	if a.db == nil {
		return "", false
	}
	entry, ok, err := getVisionCacheDB(a.db, key, now)
	if err != nil {
		log.Printf("vision cache read warning: %v", err)
		return "", false
	}
	if !ok {
		return "", false
	}
	store.put(entry)
	return entry.Text, true
}

func (a *app) storeVisionText(key, text string) {
	cfg := a.currentConfig()
	store := a.ensureVisionCacheStore(cfg)
	_, ttlHours := visionCacheSettings(cfg)
	now := time.Now().UTC()
	entry := visionCacheEntry{
		Key:        key,
		Text:       text,
		CreatedAt:  now,
		LastUsedAt: now,
		ExpiresAt:  now.Add(time.Duration(ttlHours) * time.Hour),
	}
	store.put(entry)
	if a.db == nil {
		return
	}
	if err := upsertVisionCacheDB(a.db, entry); err != nil {
		log.Printf("vision cache write warning: %v", err)
		return
	}
	if err := a.pruneVisionCache(cfg); err != nil {
		log.Printf("vision cache cleanup warning: %v", err)
	}
}

func (a *app) clearVisionCache() (int64, error) {
	cfg := a.currentConfig()
	store := a.ensureVisionCacheStore(cfg)
	memoryCount := store.clear()
	if a.db == nil {
		return int64(memoryCount), nil
	}
	result, err := a.db.Exec(`DELETE FROM vision_cache`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (a *app) visionCacheEntryCount() (int64, error) {
	cfg := a.currentConfig()
	store := a.ensureVisionCacheStore(cfg)
	if err := a.pruneVisionCache(cfg); err != nil {
		return 0, err
	}
	if a.db == nil {
		return int64(store.len()), nil
	}
	var count int64
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM vision_cache`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func upsertVisionCacheDB(db *sql.DB, entry visionCacheEntry) error {
	_, err := db.Exec(`
INSERT INTO vision_cache (cache_key, result_text, created_at, last_used_at, expires_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET
	result_text = excluded.result_text,
	created_at = excluded.created_at,
	last_used_at = excluded.last_used_at,
	expires_at = excluded.expires_at
`, entry.Key, entry.Text, formatCacheTime(entry.CreatedAt), formatCacheTime(entry.LastUsedAt), formatCacheTime(entry.ExpiresAt))
	return err
}

func getVisionCacheDB(db *sql.DB, key string, now time.Time) (visionCacheEntry, bool, error) {
	var entry visionCacheEntry
	var createdAt, lastUsedAt, expiresAt string
	err := db.QueryRow(`
SELECT cache_key, result_text, created_at, last_used_at, expires_at
FROM vision_cache
WHERE cache_key = ?
`, key).Scan(&entry.Key, &entry.Text, &createdAt, &lastUsedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return entry, false, nil
	}
	if err != nil {
		return entry, false, err
	}
	entry.CreatedAt, err = parseCacheTime(createdAt)
	if err == nil {
		entry.LastUsedAt, err = parseCacheTime(lastUsedAt)
	}
	if err == nil {
		entry.ExpiresAt, err = parseCacheTime(expiresAt)
	}
	if err != nil || !entry.ExpiresAt.After(now) {
		_, deleteErr := db.Exec(`DELETE FROM vision_cache WHERE cache_key = ?`, key)
		if deleteErr != nil {
			return visionCacheEntry{}, false, deleteErr
		}
		return visionCacheEntry{}, false, nil
	}
	entry.LastUsedAt = now
	if _, err := db.Exec(`UPDATE vision_cache SET last_used_at = ? WHERE cache_key = ?`, formatCacheTime(now), key); err != nil {
		return visionCacheEntry{}, false, err
	}
	return entry, true, nil
}

func loadRecentVisionCacheDB(db *sql.DB, limit int, now time.Time) ([]visionCacheEntry, error) {
	rows, err := db.Query(`
SELECT cache_key, result_text, created_at, last_used_at, expires_at
FROM vision_cache
WHERE expires_at > ?
ORDER BY last_used_at DESC
LIMIT ?
`, formatCacheTime(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]visionCacheEntry, 0, limit)
	invalidKeys := make([]string, 0)
	for rows.Next() {
		var entry visionCacheEntry
		var createdAt, lastUsedAt, expiresAt string
		if err := rows.Scan(&entry.Key, &entry.Text, &createdAt, &lastUsedAt, &expiresAt); err != nil {
			return nil, err
		}
		entry.CreatedAt, err = parseCacheTime(createdAt)
		if err == nil {
			entry.LastUsedAt, err = parseCacheTime(lastUsedAt)
		}
		if err == nil {
			entry.ExpiresAt, err = parseCacheTime(expiresAt)
		}
		if err != nil || !entry.ExpiresAt.After(now) {
			invalidKeys = append(invalidKeys, entry.Key)
			continue
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, key := range invalidKeys {
		_, _ = db.Exec(`DELETE FROM vision_cache WHERE cache_key = ?`, key)
	}
	return entries, nil
}

func recalculateVisionCacheExpiryDB(db *sql.DB, ttlHours int) error {
	rows, err := db.Query(`SELECT cache_key, created_at FROM vision_cache`)
	if err != nil {
		return err
	}
	type expiryUpdate struct {
		key       string
		expiresAt time.Time
	}
	updates := make([]expiryUpdate, 0)
	invalidKeys := make([]string, 0)
	ttl := time.Duration(normalizeVisionCacheTTLHours(ttlHours)) * time.Hour
	for rows.Next() {
		var key, createdAt string
		if err := rows.Scan(&key, &createdAt); err != nil {
			rows.Close()
			return err
		}
		created, err := parseCacheTime(createdAt)
		if err != nil {
			invalidKeys = append(invalidKeys, key)
			continue
		}
		updates = append(updates, expiryUpdate{key: key, expiresAt: created.Add(ttl)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, update := range updates {
		if _, err := tx.Exec(`UPDATE vision_cache SET expires_at = ? WHERE cache_key = ?`, formatCacheTime(update.expiresAt), update.key); err != nil {
			return err
		}
	}
	for _, key := range invalidKeys {
		if _, err := tx.Exec(`DELETE FROM vision_cache WHERE cache_key = ?`, key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func pruneVisionCacheDB(db *sql.DB, maxEntries int, now time.Time) ([]string, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	removedSet := make(map[string]struct{})
	rows, err := tx.Query(`SELECT cache_key FROM vision_cache WHERE expires_at <= ?`, formatCacheTime(now))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		removedSet[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM vision_cache WHERE expires_at <= ?`, formatCacheTime(now)); err != nil {
		return nil, err
	}

	rows, err = tx.Query(`
SELECT cache_key
FROM vision_cache
ORDER BY last_used_at DESC
LIMIT -1 OFFSET ?
`, normalizeVisionCacheMaxEntries(maxEntries))
	if err != nil {
		return nil, err
	}
	overLimit := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		overLimit = append(overLimit, key)
		removedSet[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, key := range overLimit {
		if _, err := tx.Exec(`DELETE FROM vision_cache WHERE cache_key = ?`, key); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(removedSet))
	for key := range removedSet {
		removed = append(removed, key)
	}
	return removed, nil
}

func formatCacheTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseCacheTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
