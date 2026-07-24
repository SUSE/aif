/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

// Delete removes a cached index entry. It is a no-op if the key is absent.
// Used to invalidate a stale index (e.g. after an extension upgrade) so the next
// lookup refetches instead of serving the pre-upgrade index for the whole TTL.
func (c *IndexCache) Delete(key IndexCacheKey) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
}
