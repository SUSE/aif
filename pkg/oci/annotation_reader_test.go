package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChartAnnotations_MergesManifestAndChartYaml(t *testing.T) {
	chartYaml := "apiVersion: v2\nname: example\nannotations:\n  ai.suse.com/role: reference-blueprint\n  ai.suse.com/use-case: rag\n"
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "example/Chart.yaml", Mode: 0o644, Size: int64(len(chartYaml))})
	_, _ = tw.Write([]byte(chartYaml))
	_ = tw.Close()
	_ = gz.Close()
	layerBytes := tarBuf.Bytes()
	layerSum := sha256.Sum256(layerBytes)
	layerDigest := "sha256:" + hex.EncodeToString(layerSum[:])
	manifest := fmt.Sprintf(
		`{"schemaVersion":2,"annotations":{"org.opencontainers.image.created":"2026-05-27T00:00:00Z"},"layers":[{"mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip","digest":%q,"size":%d}]}`,
		layerDigest, len(layerBytes))
	manifestSum := sha256.Sum256([]byte(manifest))
	manifestDigest := "sha256:" + hex.EncodeToString(manifestSum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/ai/charts/example/foo/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", manifestDigest)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.WriteString(w, manifest)
	})
	mux.HandleFunc("/v2/ai/charts/example/foo/blobs/"+layerDigest, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(layerBytes)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	w := NewWalker(silentLogger()).(*walker)
	w.UpdateSettings(EngineSettings{Endpoint: ts.URL})

	ar := NewAnnotationReader(silentLogger(), w)
	got, err := ar.ChartAnnotations(context.Background(), "ai/charts/example/foo", "1.0.0")
	if err != nil {
		t.Fatalf("ChartAnnotations: %v", err)
	}
	if got["ai.suse.com/role"] != "reference-blueprint" {
		t.Errorf("role: got %q, want reference-blueprint", got["ai.suse.com/role"])
	}
	if got["org.opencontainers.image.created"] != "2026-05-27T00:00:00Z" {
		t.Errorf("created annotation lost: %q", got["org.opencontainers.image.created"])
	}
}
