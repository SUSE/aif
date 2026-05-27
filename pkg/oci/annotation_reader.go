package oci

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SUSE/aif/pkg/helm_oci"
)

// annotationCacheEntry mirrors pkg/nvidia's previous version: cache one
// annotation map per (repo, tag), invalidated by manifest digest.
type annotationCacheEntry struct {
	digest      string
	annotations map[string]string
}

type annotationReader struct {
	logger *slog.Logger
	w      *walker // concrete to access fetchBytes/headDigest helpers
	mu     sync.RWMutex
	cache  map[string]annotationCacheEntry // key: repo+":"+tag
}

// NewAnnotationReader binds an AnnotationReader to a Walker. The Walker
// must be a *walker produced by NewWalker (the interface is exported
// for use by tests that wrap with fakes — the production reader needs
// the concrete helpers).
func NewAnnotationReader(logger *slog.Logger, w Walker) AnnotationReader {
	concrete, _ := w.(*walker)
	return &annotationReader{
		logger: logger,
		w:      concrete,
		cache:  make(map[string]annotationCacheEntry),
	}
}

func (r *annotationReader) ChartAnnotations(ctx context.Context, repo, tag string) (map[string]string, error) {
	if r.w == nil {
		return nil, ErrNotConfigured
	}
	manifestPath := "/v2/" + repo + "/manifests/" + tag
	digest, err := r.w.headDigest(ctx, manifestPath)
	if err != nil {
		return nil, err
	}

	cacheKey := repo + ":" + tag
	r.mu.RLock()
	entry, ok := r.cache[cacheKey]
	r.mu.RUnlock()
	if ok && entry.digest == digest {
		return entry.annotations, nil
	}

	manifest, err := r.w.fetchBytes(ctx, manifestPath)
	if err != nil {
		return nil, err
	}
	manifestAnns, err := helm_oci.ExtractManifestAnnotations(manifest)
	if err != nil {
		return nil, fmt.Errorf("oci: %w", err)
	}
	layerDigest, err := helm_oci.FindChartLayerDigest(manifest)
	if err != nil {
		return nil, fmt.Errorf("oci: %w", err)
	}
	body, err := r.w.fetchBytes(ctx, "/v2/"+repo+"/blobs/"+layerDigest)
	if err != nil {
		return nil, err
	}
	chartAnns, err := helm_oci.ExtractChartYamlAnnotations(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oci: %w", err)
	}

	if len(manifestAnns) == 0 && len(chartAnns) == 0 {
		r.mu.Lock()
		r.cache[cacheKey] = annotationCacheEntry{digest: digest}
		r.mu.Unlock()
		return nil, nil
	}

	merged := make(map[string]string, len(manifestAnns)+len(chartAnns))
	for k, v := range manifestAnns {
		merged[k] = v
	}
	for k, v := range chartAnns {
		merged[k] = v
	}
	r.mu.Lock()
	r.cache[cacheKey] = annotationCacheEntry{digest: digest, annotations: merged}
	r.mu.Unlock()
	return merged, nil
}
