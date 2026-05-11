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

// helmOCIStub serves a minimal Helm OCI registry: HEAD/GET on a manifest
// returning a one-layer manifest, plus GET on the chart-content blob
// returning a tar.gz with Chart.yaml.
type helmOCIStub struct {
	chart     string
	version   string
	chartYaml string
	headHits  int32
	getHits   int32
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
	switch {
	case strings.HasSuffix(r.URL.Path, "/manifests/"+s.version) && r.Method == http.MethodHead:
		atomic.AddInt32(&s.headHits, 1)
		w.Header().Set("Docker-Content-Digest", s.manifestDigest())
		w.WriteHeader(http.StatusOK)
	case strings.HasSuffix(r.URL.Path, "/manifests/"+s.version) && r.Method == http.MethodGet:
		atomic.AddInt32(&s.getHits, 1)
		w.Header().Set("Docker-Content-Digest", s.manifestDigest())
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write(s.manifestBytes())
	case strings.Contains(r.URL.Path, "/blobs/"+s.layerDigest()):
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(s.layerBytes())
	default:
		http.NotFound(w, r)
	}
}

// readerWith builds an annotation-ready discoveryImpl wired to ts.
func readerWith(ts *httptest.Server) *discoveryImpl {
	d := &discoveryImpl{
		httpClient: ts.Client(),
		annCache:   map[string]annotationCacheEntry{},
	}
	d.client = newRegistryClient(ts.Client(), ts.URL, "", "", nil)
	d.settings.RegistryEndpoint = ts.URL
	return d
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

	d := readerWith(ts)
	got, err := d.ChartAnnotations(context.Background(), "my-chart", "1.0.0")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got["ai.suse.com/role"] != "reference-blueprint" {
		t.Fatalf("first call: got %v, want role=reference-blueprint", got)
	}

	// Second call — same digest → cache hit, no second GET.
	getsAfterFirst := atomic.LoadInt32(&stub.getHits)
	got2, err := d.ChartAnnotations(context.Background(), "my-chart", "1.0.0")
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

	d := readerWith(ts)
	_, err := d.ChartAnnotations(context.Background(), "missing", "9.9.9")
	if !errors.Is(err, ErrChartNotFound) {
		t.Fatalf("got %v, want ErrChartNotFound", err)
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

	d := readerWith(ts)
	got, err := d.ChartAnnotations(context.Background(), "plain-chart", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil annotations, got %v", got)
	}
}
