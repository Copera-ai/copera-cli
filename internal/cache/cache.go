// Package cache provides content and table schema caches backed by a Store interface.
package cache

import (
	"encoding/json"
	"time"
)

// Cache stores doc content with a TTL via a Store backend.
type Cache struct {
	store Store
	ttl   time.Duration
}

type entry struct {
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

// New creates a Cache backed by disk. dir is the base cache directory;
// defaults to os.TempDir()/copera-cli-{version}/docs. ttl defaults to 1h.
func New(dir string, ttl time.Duration) *Cache {
	if ttl == 0 {
		ttl = time.Hour
	}
	return &Cache{store: NewDiskStore(DocsDir(dir)), ttl: ttl}
}

// NewWithStore creates a Cache with a custom Store (e.g. MemStore for tests).
func NewWithStore(store Store, ttl time.Duration) *Cache {
	if ttl == 0 {
		ttl = time.Hour
	}
	return &Cache{store: store, ttl: ttl}
}

// Get returns the cached value and true if present and not expired.
func (c *Cache) Get(key string) (string, bool) {
	data, err := c.store.Read(key + ".json")
	if err != nil {
		return "", false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return "", false
	}
	if time.Now().After(e.ExpiresAt) {
		return "", false
	}
	return e.Value, true
}

// Set stores a value under key with the configured TTL.
func (c *Cache) Set(key, value string) {
	data, _ := json.Marshal(entry{Value: value, ExpiresAt: time.Now().Add(c.ttl)})
	_ = c.store.Write(key+".json", data)
}

// Delete removes a cache entry (e.g. after a successful update).
func (c *Cache) Delete(key string) {
	_ = c.store.Delete(key + ".json")
}
