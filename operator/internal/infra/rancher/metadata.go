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
	"time"

	"golang.org/x/exp/maps"

	"github.com/SUSE/aif-operator/internal/infra/helm"
	logging "github.com/SUSE/aif-operator/internal/logging"
)

const (
	KeyDisplayName       = "catalog.cattle.io/display-name"
	KeyRancherVersion    = "catalog.cattle.io/rancher-version"
	KeyUIExtensionsRange = "catalog.cattle.io/ui-extensions-version"
)

func buildExtensionMetadata(
	ctx context.Context,
	indexCache *helm.IndexCache,
	repoURL string,
	extensionName string,
	version string,
	userMeta map[string]string,
) (map[string]string, error) {

	log := logging.FromContext(ctx, "rancher.metadata").
		WithValues(
			logging.KeyExtension, extensionName,
			logging.KeyVersion, version,
		)

	logging.Debug(log).Info("Resolving extension metadata from Helm index")

	index, cached, err := getOrFetchIndex(ctx, indexCache, repoURL)
	if err != nil {
		log.Error(err, "Failed to load Helm index")
		return nil, err
	}

	annotations, err := helm.FindAnnotations(index, extensionName, version)
	// A lookup miss (chart or version not found) on a *cached* index is worth one
	// refetch: the cached index may predate a just-published upgrade, or the server
	// may have briefly served an incomplete index that we cached — a fresh fetch
	// recovers both, instead of serving the stale cache for the rest of its TTL.
	// FindAnnotations only reports these in-memory misses; an unreachable registry
	// fails earlier in getOrFetchIndex.
	//
	// Scope: this recovers a stale/incomplete cache on the *next reconcile that runs*.
	// It does not make a persistent miss self-converge — a resolution failure ends the
	// reconcile with Phase=Failed and no RequeueAfter, so the next attempt comes from a
	// spec change or the informer resync, not a fixed short interval. The common
	// upgrade ordering (the server already serves the new version at reconcile time)
	// converges in that single pass; a version published only *after* the reconcile is
	// not picked up until something re-triggers one. Making that self-driving would
	// need a bounded requeue on the failure paths, deliberately left out of scope here.
	if err != nil && cached {
		logging.Debug(log).Info("Requested chart/version not in cached index; refetching",
			"repoURL", repoURL)
		indexCache.Delete(helm.IndexCacheKey{RepoURL: repoURL})

		index, _, err = getOrFetchIndex(ctx, indexCache, repoURL)
		if err != nil {
			log.Error(err, "Failed to reload Helm index")
			return nil, err
		}
		annotations, err = helm.FindAnnotations(index, extensionName, version)
	}
	if err != nil {
		log.Error(err, "Failed to find chart annotations in index.yaml")
		return nil, err
	}

	indexMeta := filterSupportedMetadata(annotations)

	logging.Trace(log).Info(
		"Metadata extracted from index.yaml",
		"metadata", indexMeta,
	)

	final := mergeMetadata(indexMeta, userMeta, extensionName)

	logging.Debug(log).Info(
		"Final UIPlugin metadata resolved",
		"displayName", final[KeyDisplayName],
		"uiExtensionsVersion", final[KeyUIExtensionsRange],
	)

	// Return a clone to avoid accidental mutation
	return maps.Clone(final), nil
}

// getOrFetchIndex returns the repo index and whether it came from the cache.
// The cached flag lets callers decide whether a failed lookup is worth a
// cache-invalidating refetch (a freshly-fetched index won't be helped by one).
func getOrFetchIndex(
	ctx context.Context,
	cache *helm.IndexCache,
	repoURL string,
) (*helm.IndexFile, bool, error) {

	key := helm.IndexCacheKey{RepoURL: repoURL}

	if entry, ok := cache.Get(key); ok {
		return entry.Index, true, nil
	}

	indexURL := fmt.Sprintf("%s/index.yaml", repoURL)

	index, err := helm.FetchIndex(indexURL)
	if err != nil {
		return nil, false, err
	}

	cache.Set(key, &helm.IndexCacheEntry{
		Index:     index,
		FetchedAt: time.Now(),
	})

	return index, false, nil
}

func filterSupportedMetadata(
	annotations map[string]string,
) map[string]string {

	meta := map[string]string{}

	for _, key := range []string{
		KeyDisplayName,
		KeyRancherVersion,
		KeyUIExtensionsRange,
	} {
		if val, ok := annotations[key]; ok {
			meta[key] = val
		}
	}

	return meta
}

func mergeMetadata(
	indexMeta map[string]string,
	userMeta map[string]string,
	extensionName string,
) map[string]string {

	meta := maps.Clone(indexMeta)

	// User overrides always win
	for k, v := range userMeta {
		meta[k] = v
	}

	// Safe default
	if _, ok := meta[KeyDisplayName]; !ok {
		meta[KeyDisplayName] = extensionName
	}

	return meta
}
