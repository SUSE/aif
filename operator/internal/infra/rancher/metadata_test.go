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

package rancher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SUSE/aif-operator/internal/infra/helm"
)

func indexYAML(version string) string {
	return fmt.Sprintf(`entries:
  aif-ui:
    - version: %s
      annotations:
        catalog.cattle.io/display-name: SUSE AI Factory
        catalog.cattle.io/ui-extensions-version: '>= 3.0.0 < 4.0.0'
`, version)
}

// After an extension upgrade the served index.yaml advertises the new version,
// but the operator's index cache may still hold the pre-upgrade index (5m TTL).
// buildExtensionMetadata must recover: when the requested version isn't in the
// cached index, invalidate that entry, refetch, and find the new version — rather
// than erroring for the whole TTL window.
func TestBuildExtensionMetadata_RefetchesWhenCachedIndexMissesVersion(t *testing.T) {
	served := indexYAML("1.0.0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, served)
	}))
	defer srv.Close()

	cache := helm.NewIndexCache()
	ctx := context.Background()

	// 1) resolve 1.0.0 → primes the cache with the 1.0.0 index.
	if _, err := buildExtensionMetadata(ctx, cache, srv.URL, "aif-ui", "1.0.0", nil); err != nil {
		t.Fatalf("resolve 1.0.0: %v", err)
	}

	// 2) extension upgraded: the server now serves 2.0.0; the cache still holds 1.0.0.
	served = indexYAML("2.0.0")

	// 3) resolving 2.0.0 must refetch-on-miss and succeed.
	meta, err := buildExtensionMetadata(ctx, cache, srv.URL, "aif-ui", "2.0.0", nil)
	if err != nil {
		t.Fatalf("resolve 2.0.0 after upgrade should refetch and succeed, got: %v", err)
	}
	if meta[KeyDisplayName] != "SUSE AI Factory" {
		t.Fatalf("expected resolved display-name metadata, got: %v", meta)
	}
}

// A steady-state cache hit (requested version present) must NOT trigger a
// refetch — the server is hit once, then served from cache.
func TestBuildExtensionMetadata_CacheHitDoesNotRefetch(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		fmt.Fprint(w, indexYAML("1.0.0"))
	}))
	defer srv.Close()

	cache := helm.NewIndexCache()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := buildExtensionMetadata(ctx, cache, srv.URL, "aif-ui", "1.0.0", nil); err != nil {
			t.Fatalf("resolve 1.0.0 (iter %d): %v", i, err)
		}
	}
	if hits != 1 {
		t.Fatalf("expected exactly 1 index fetch across cache hits, got %d", hits)
	}
}

// A cached index that is missing the requested CHART (e.g. the server briefly served
// an incomplete index that got cached) must also refetch-on-miss and recover — not
// stay stuck on the stale cached index for the whole TTL. This mirrors the version
// case and guards the "chart or version miss refetches" behavior.
func TestBuildExtensionMetadata_RefetchesWhenCachedIndexMissesChart(t *testing.T) {
	served := indexYAML("1.0.0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, served)
	}))
	defer srv.Close()

	cache := helm.NewIndexCache()
	ctx := context.Background()

	// 1) prime the cache with an index that lacks "other-ext".
	if _, err := buildExtensionMetadata(ctx, cache, srv.URL, "aif-ui", "1.0.0", nil); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	// 2) the server now also advertises "other-ext"; the cache still holds the old index.
	served = indexYAML("1.0.0") + `  other-ext:
    - version: 1.0.0
      annotations:
        catalog.cattle.io/display-name: Other Ext
`

	// 3) resolving other-ext must refetch-on-miss (chart absent from cache) and succeed.
	meta, err := buildExtensionMetadata(ctx, cache, srv.URL, "other-ext", "1.0.0", nil)
	if err != nil {
		t.Fatalf("resolve other-ext should refetch and succeed, got: %v", err)
	}
	if meta[KeyDisplayName] != "Other Ext" {
		t.Fatalf("expected resolved display-name for other-ext, got: %v", meta)
	}
}
