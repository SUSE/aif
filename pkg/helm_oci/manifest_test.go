package helm_oci

import (
	"errors"
	"testing"
)

func TestFindChartLayerDigest_HappyPath(t *testing.T) {
	manifest := []byte(`{
		"schemaVersion": 2,
		"config": { "mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": "sha256:cfg" },
		"layers": [
			{ "mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": "sha256:abc123", "size": 1024 }
		]
	}`)
	got, err := FindChartLayerDigest(manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sha256:abc123" {
		t.Fatalf("got %q, want %q", got, "sha256:abc123")
	}
}

func TestFindChartLayerDigest_MultipleLayers_PicksChartContent(t *testing.T) {
	manifest := []byte(`{
		"schemaVersion": 2,
		"layers": [
			{ "mediaType": "application/vnd.cncf.helm.chart.provenance.v1.prov", "digest": "sha256:prov" },
			{ "mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": "sha256:chart" },
			{ "mediaType": "application/vnd.cncf.helm.chart.provenance.v1.prov", "digest": "sha256:prov2" }
		]
	}`)
	got, err := FindChartLayerDigest(manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sha256:chart" {
		t.Fatalf("got %q, want %q", got, "sha256:chart")
	}
}

func TestFindChartLayerDigest_NoChartLayer_ReturnsErrChartLayerMissing(t *testing.T) {
	manifest := []byte(`{
		"schemaVersion": 2,
		"layers": [
			{ "mediaType": "application/vnd.cncf.helm.chart.provenance.v1.prov", "digest": "sha256:prov" }
		]
	}`)
	_, err := FindChartLayerDigest(manifest)
	if !errors.Is(err, ErrChartLayerMissing) {
		t.Fatalf("got %v, want ErrChartLayerMissing", err)
	}
}

func TestFindChartLayerDigest_MalformedJSON_ReturnsErrManifestMalformed(t *testing.T) {
	_, err := FindChartLayerDigest([]byte("{not json"))
	if !errors.Is(err, ErrManifestMalformed) {
		t.Fatalf("got %v, want ErrManifestMalformed", err)
	}
}

func TestFindChartLayerDigest_EmptyLayers_ReturnsErrChartLayerMissing(t *testing.T) {
	manifest := []byte(`{ "schemaVersion": 2, "layers": [] }`)
	_, err := FindChartLayerDigest(manifest)
	if !errors.Is(err, ErrChartLayerMissing) {
		t.Fatalf("got %v, want ErrChartLayerMissing", err)
	}
}

func TestExtractManifestAnnotations_HappyPath(t *testing.T) {
	manifest := []byte(`{
		"schemaVersion": 2,
		"layers": [],
		"annotations": {
			"org.opencontainers.image.created": "2026-03-04T10:05:02Z",
			"org.opencontainers.image.title": "litellm"
		}
	}`)
	got, err := ExtractManifestAnnotations(manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["org.opencontainers.image.created"] != "2026-03-04T10:05:02Z" {
		t.Errorf("created = %q, want %q", got["org.opencontainers.image.created"], "2026-03-04T10:05:02Z")
	}
	if got["org.opencontainers.image.title"] != "litellm" {
		t.Errorf("title = %q, want %q", got["org.opencontainers.image.title"], "litellm")
	}
}

func TestExtractManifestAnnotations_NoAnnotations(t *testing.T) {
	manifest := []byte(`{ "schemaVersion": 2, "layers": [] }`)
	got, err := ExtractManifestAnnotations(manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestExtractManifestAnnotations_MalformedJSON(t *testing.T) {
	_, err := ExtractManifestAnnotations([]byte("{not json"))
	if !errors.Is(err, ErrManifestMalformed) {
		t.Fatalf("got %v, want ErrManifestMalformed", err)
	}
}
