package helm_oci

import (
	"encoding/json"
	"fmt"
)

// chartContentMediaType is the OCI mediaType the Helm OCI artifact spec
// uses for the tar.gz layer carrying the packaged chart.
const chartContentMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

// ociManifest is the subset of the OCI v1 image manifest we care about.
// Other fields (schemaVersion, config, mediaType, annotations) are
// intentionally ignored — we only need the chart-content layer's digest.
type ociManifest struct {
	Layers      []ociLayer        `json:"layers"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ociLayer struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
}

// FindChartLayerDigest parses an OCI manifest and returns the digest of
// the layer carrying the Helm chart content (tar.gz). Returns
// ErrChartLayerMissing if no such layer exists, ErrManifestMalformed if
// the JSON is unparseable.
func FindChartLayerDigest(manifest []byte) (string, error) {
	var m ociManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return "", fmt.Errorf("%w: %w", ErrManifestMalformed, err)
	}
	for _, layer := range m.Layers {
		if layer.MediaType == chartContentMediaType {
			return layer.Digest, nil
		}
	}
	return "", ErrChartLayerMissing
}

// ExtractManifestAnnotations parses an OCI manifest and returns its
// top-level annotations map. Returns (nil, nil) when the manifest has
// no annotations. These are distinct from Chart.yaml annotations —
// they are set by `helm push` and include standard OCI fields like
// org.opencontainers.image.created.
func ExtractManifestAnnotations(manifest []byte) (map[string]string, error) {
	var m ociManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrManifestMalformed, err)
	}
	return m.Annotations, nil
}
