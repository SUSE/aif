package source_collection

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type appcoOCIStub struct {
	chart     string
	version   string
	chartYaml string
	getHits   int32
	headHits  int32
}

func (s *appcoOCIStub) layerBytes() []byte {
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

func (s *appcoOCIStub) layerDigest() string {
	sum := sha256.Sum256(s.layerBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *appcoOCIStub) manifestBytes() []byte {
	return []byte(fmt.Sprintf(`{
		"schemaVersion": 2,
		"layers": [
			{ "mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": %q, "size": %d }
		]
	}`, s.layerDigest(), len(s.layerBytes())))
}

func (s *appcoOCIStub) manifestDigest() string {
	sum := sha256.Sum256(s.manifestBytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *appcoOCIStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/manifests/"+s.version) && r.Method == http.MethodHead:
		atomic.AddInt32(&s.headHits, 1)
		w.Header().Set("Docker-Content-Digest", s.manifestDigest())
		w.WriteHeader(http.StatusOK)
	case strings.HasSuffix(r.URL.Path, "/manifests/"+s.version) && r.Method == http.MethodGet:
		atomic.AddInt32(&s.getHits, 1)
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write(s.manifestBytes())
	case strings.Contains(r.URL.Path, "/blobs/"+s.layerDigest()):
		_, _ = w.Write(s.layerBytes())
	default:
		http.NotFound(w, r)
	}
}

func newTestAppcoReader(ts *httptest.Server) *apiClient {
	c := &apiClient{
		httpClient: ts.Client(),
		limiter:    rate.NewLimiter(rate.Inf, 0),
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		annCache:   map[string]annotationCacheEntry{},
		settings:   EngineSettings{OCIHost: ts.URL},
	}
	return c
}

func TestAppcoAnnotationReader_HappyPathAndCacheHit(t *testing.T) {
	stub := &appcoOCIStub{
		chart:   "milvus",
		version: "2.4.0",
		chartYaml: `apiVersion: v2
name: milvus
annotations:
  ai.suse.com/role: reference-blueprint
`,
	}
	ts := httptest.NewServer(stub)
	defer ts.Close()

	c := newTestAppcoReader(ts)
	got, err := c.ChartAnnotations(context.Background(), "charts", "milvus", "2.4.0")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got["ai.suse.com/role"] != "reference-blueprint" {
		t.Fatalf("got %v", got)
	}

	getsAfterFirst := atomic.LoadInt32(&stub.getHits)
	_, err = c.ChartAnnotations(context.Background(), "charts", "milvus", "2.4.0")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if atomic.LoadInt32(&stub.getHits) != getsAfterFirst {
		t.Fatalf("expected cache hit; GET went from %d to %d",
			getsAfterFirst, atomic.LoadInt32(&stub.getHits))
	}
}

func TestAppcoAnnotationReader_404_ReturnsErrChartNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	c := newTestAppcoReader(ts)
	_, err := c.ChartAnnotations(context.Background(), "charts", "missing", "9.9.9")
	if !errors.Is(err, ErrChartNotFound) {
		t.Fatalf("got %v, want ErrChartNotFound", err)
	}
}

func TestAppcoAnnotationReader_NotConfigured(t *testing.T) {
	c := &apiClient{
		httpClient: &http.Client{Timeout: time.Second},
		limiter:    rate.NewLimiter(rate.Inf, 0),
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		annCache:   map[string]annotationCacheEntry{},
	}
	_, err := c.ChartAnnotations(context.Background(), "charts", "milvus", "2.4.0")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}
