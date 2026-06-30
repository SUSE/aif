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

	index, err := getOrFetchIndex(ctx, indexCache, repoURL)
	if err != nil {
		log.Error(err, "Failed to load Helm index")
		return nil, err
	}

	annotations, err := helm.FindAnnotations(index, extensionName, version)
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

func getOrFetchIndex(
	ctx context.Context,
	cache *helm.IndexCache,
	repoURL string,
) (*helm.IndexFile, error) {

	key := helm.IndexCacheKey{RepoURL: repoURL}

	if entry, ok := cache.Get(key); ok {
		return entry.Index, nil
	}

	indexURL := fmt.Sprintf("%s/index.yaml", repoURL)

	index, err := helm.FetchIndex(indexURL)
	if err != nil {
		return nil, err
	}

	cache.Set(key, &helm.IndexCacheEntry{
		Index:     index,
		FetchedAt: time.Now(),
	})

	return index, nil
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
