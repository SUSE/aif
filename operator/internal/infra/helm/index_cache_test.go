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
	"testing"
	"time"
)

func TestIndexCache_Delete(t *testing.T) {
	c := NewIndexCache()
	key := IndexCacheKey{RepoURL: "http://example.test/repo"}
	c.Set(key, &IndexCacheEntry{Index: &IndexFile{}, FetchedAt: time.Now()})

	if _, ok := c.Get(key); !ok {
		t.Fatalf("expected entry present after Set")
	}

	c.Delete(key)
	if _, ok := c.Get(key); ok {
		t.Fatalf("expected entry gone after Delete")
	}

	// Deleting a missing key must be a no-op, not a panic.
	c.Delete(IndexCacheKey{RepoURL: "http://example.test/absent"})
}
