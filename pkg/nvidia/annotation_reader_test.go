package nvidia

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// helmOCIStub serves a minimal Helm OCI registry rooted at the
// nvidiaChartPrefix subtree (so callers can drive ChartAnnotations with
// the bare chart name). Provides HEAD/GET on a manifest returning a
// one-layer manifest, plus GET on the chart-content blob returning a
// tar.gz with Chart.yaml.
type helmOCIStub struct {
	chart     string // bare chart name (e.g. "my-chart"); full repo path is nvidiaChartPrefix+chart
	version   string
	chartYaml string
	headHits  int32
	getHits   int32
}

func (s *helmOCIStub) repoPath() string {
	return nvidiaChartPrefix + s.chart
}

func (s *helmOCIStub) layerBytes() []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte(s.chartYaml)
	hdr := &tar.Header{Name: s.chart + "/Chart.yaml", Mode: 0o644, Size: int64(len(body))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func (s *helmOCIStub) layerDigest() string {
	sum := sha256.Sum256(s.layerBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *helmOCIStub) manifestBytes() []byte {
	return []byte(fmt.Sprintf(`{
		"schemaVersion": 2,
		"layers": [
			{ "mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": %q, "size": %d }
		]
	}`, s.layerDigest(), len(s.layerBytes())))
}

func (s *helmOCIStub) manifestDigest() string {
	sum := sha256.Sum256(s.manifestBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *helmOCIStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	manifestPath := "/v2/" + s.repoPath() + "/manifests/" + s.version
	blobPath := "/v2/" + s.repoPath() + "/blobs/" + s.layerDigest()
	switch {
	case r.URL.Path == manifestPath && r.Method == http.MethodHead:
		atomic.AddInt32(&s.headHits, 1)
		w.Header().Set("Docker-Content-Digest", s.manifestDigest())
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == manifestPath && r.Method == http.MethodGet:
		atomic.AddInt32(&s.getHits, 1)
		w.Header().Set("Docker-Content-Digest", s.manifestDigest())
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write(s.manifestBytes())
	case r.URL.Path == blobPath:
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(s.layerBytes())
	default:
		http.NotFound(w, r)
	}
}

// readerWith builds a Discovery+AnnotationReader pair through the public
// surface, pointed at ts via UpdateSettings.
func readerWith(t *testing.T, ts *httptest.Server) AnnotationReader {
	t.Helper()
	d, ar := NewDiscovery(silentLogger())
	d.UpdateSettings(EngineSettings{RegistryEndpoint: ts.URL})
	return ar
}

func TestAnnotationReader_HappyPathAndCacheHit(t *testing.T) {
	stub := &helmOCIStub{
		chart:   "my-chart",
		version: "1.0.0",
		chartYaml: `apiVersion: v2
name: my-chart
annotations:
  ai.suse.com/role: reference-blueprint
`,
	}
	ts := httptest.NewServer(stub)
	defer ts.Close()

	ar := readerWith(t, ts)
	got, err := ar.ChartAnnotations(context.Background(), "my-chart", "1.0.0")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got["ai.suse.com/role"] != "reference-blueprint" {
		t.Fatalf("first call: got %v, want role=reference-blueprint", got)
	}

	// Second call — same digest → cache hit, no second GET.
	getsAfterFirst := atomic.LoadInt32(&stub.getHits)
	got2, err := ar.ChartAnnotations(context.Background(), "my-chart", "1.0.0")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got2["ai.suse.com/role"] != "reference-blueprint" {
		t.Fatalf("second call: got %v", got2)
	}
	if atomic.LoadInt32(&stub.getHits) != getsAfterFirst {
		t.Fatalf("expected cache hit; GET count went from %d to %d",
			getsAfterFirst, atomic.LoadInt32(&stub.getHits))
	}
}

func TestAnnotationReader_404_ReturnsErrChartNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ar := readerWith(t, ts)
	_, err := ar.ChartAnnotations(context.Background(), "missing", "9.9.9")
	if !errors.Is(err, ErrChartNotFound) {
		t.Fatalf("got %v, want ErrChartNotFound", err)
	}
}

// helmOCIStubWithManifestAnns extends helmOCIStub to include manifest-level annotations.
type helmOCIStubWithManifestAnns struct {
	helmOCIStub
	manifestAnnotations map[string]string
}

func (s *helmOCIStubWithManifestAnns) manifestBytes() []byte {
	// Deterministic key order so manifestDigest() is stable across calls.
	keys := make([]string, 0, len(s.manifestAnnotations))
	for k := range s.manifestAnnotations {
		keys = append(keys, k)
	}
	// Simple insertion-sort to avoid pulling sort here; small map.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	anns := "{"
	for i, k := range keys {
		if i > 0 {
			anns += ","
		}
		anns += fmt.Sprintf("%q:%q", k, s.manifestAnnotations[k])
	}
	anns += "}"
	return []byte(fmt.Sprintf(`{
		"schemaVersion": 2,
		"layers": [
			{ "mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": %q, "size": %d }
		],
		"annotations": %s
	}`, s.layerDigest(), len(s.layerBytes()), anns))
}

func (s *helmOCIStubWithManifestAnns) manifestDigest() string {
	sum := sha256.Sum256(s.manifestBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *helmOCIStubWithManifestAnns) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	manifestPath := "/v2/" + s.repoPath() + "/manifests/" + s.version
	blobPath := "/v2/" + s.repoPath() + "/blobs/" + s.layerDigest()
	switch {
	case r.URL.Path == manifestPath && r.Method == http.MethodHead:
		w.Header().Set("Docker-Content-Digest", s.manifestDigest())
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == manifestPath && r.Method == http.MethodGet:
		w.Header().Set("Docker-Content-Digest", s.manifestDigest())
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write(s.manifestBytes())
	case strings.HasPrefix(r.URL.Path, blobPath):
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(s.layerBytes())
	default:
		http.NotFound(w, r)
	}
}

func TestAnnotationReader_MergesManifestAndChartYamlAnnotations(t *testing.T) {
	stub := &helmOCIStubWithManifestAnns{
		helmOCIStub: helmOCIStub{
			chart:   "nim-llm",
			version: "1.0.0",
			chartYaml: `apiVersion: v2
name: nim-llm
annotations:
  ai.suse.com/role: reference-blueprint
`,
		},
		manifestAnnotations: map[string]string{
			"org.opencontainers.image.created": "2026-03-04T10:05:02Z",
			"org.opencontainers.image.title":   "nim-llm",
		},
	}
	ts := httptest.NewServer(stub)
	defer ts.Close()

	ar := readerWith(t, ts)
	got, err := ar.ChartAnnotations(context.Background(), "nim-llm", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["org.opencontainers.image.created"] != "2026-03-04T10:05:02Z" {
		t.Errorf("manifest annotation missing: got %q", got["org.opencontainers.image.created"])
	}
	if got["ai.suse.com/role"] != "reference-blueprint" {
		t.Errorf("chart.yaml annotation missing: got %q", got["ai.suse.com/role"])
	}
	if got["org.opencontainers.image.title"] != "nim-llm" {
		t.Errorf("manifest title missing: got %q", got["org.opencontainers.image.title"])
	}
}

func TestAnnotationReader_ChartYamlOverridesManifest(t *testing.T) {
	stub := &helmOCIStubWithManifestAnns{
		helmOCIStub: helmOCIStub{
			chart:   "nim-llm",
			version: "1.0.0",
			chartYaml: `apiVersion: v2
name: nim-llm
annotations:
  license: Apache-2.0
`,
		},
		manifestAnnotations: map[string]string{
			"license": "MIT",
		},
	}
	ts := httptest.NewServer(stub)
	defer ts.Close()

	ar := readerWith(t, ts)
	got, err := ar.ChartAnnotations(context.Background(), "nim-llm", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["license"] != "Apache-2.0" {
		t.Errorf("Chart.yaml should override manifest: got %q, want %q", got["license"], "Apache-2.0")
	}
}

func TestAnnotationReader_ManifestAnnotationsOnly(t *testing.T) {
	stub := &helmOCIStubWithManifestAnns{
		helmOCIStub: helmOCIStub{
			chart:     "nim-llm",
			version:   "1.0.0",
			chartYaml: "apiVersion: v2\nname: nim-llm\n",
		},
		manifestAnnotations: map[string]string{
			"org.opencontainers.image.created": "2026-03-04T10:05:02Z",
		},
	}
	ts := httptest.NewServer(stub)
	defer ts.Close()

	ar := readerWith(t, ts)
	got, err := ar.ChartAnnotations(context.Background(), "nim-llm", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["org.opencontainers.image.created"] != "2026-03-04T10:05:02Z" {
		t.Errorf("expected manifest annotation, got %v", got)
	}
}

func TestAnnotationReader_NoAnnotationsBlock_ReturnsNilNil(t *testing.T) {
	stub := &helmOCIStub{
		chart:     "plain-chart",
		version:   "1.0.0",
		chartYaml: "apiVersion: v2\nname: plain-chart\n",
	}
	ts := httptest.NewServer(stub)
	defer ts.Close()

	ar := readerWith(t, ts)
	got, err := ar.ChartAnnotations(context.Background(), "plain-chart", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil annotations, got %v", got)
	}
}
