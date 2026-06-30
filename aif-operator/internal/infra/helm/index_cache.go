package helm

import (
	"sync"
	"time"
)

type IndexCacheKey struct {
	RepoURL string
}

type IndexCacheEntry struct {
	Index     *IndexFile
	FetchedAt time.Time
}

type IndexCache struct {
	mu    sync.Mutex
	items map[IndexCacheKey]*IndexCacheEntry
}

func NewIndexCache() *IndexCache {
	return &IndexCache{
		items: make(map[IndexCacheKey]*IndexCacheEntry),
	}
}

const indexCacheTTL = 5 * time.Minute

func (c *IndexCache) Get(key IndexCacheKey) (*IndexCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if time.Since(entry.FetchedAt) > indexCacheTTL {
		delete(c.items, key)
		return nil, false
	}
	return entry, true
}

func (c *IndexCache) Set(key IndexCacheKey, entry *IndexCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = entry
}
