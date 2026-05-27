package suse_registry_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/SUSE/aif/pkg/oci"
	"github.com/SUSE/aif/pkg/suse_registry"
)

// ExampleProvider_List exercises the Provider end-to-end against an in-process
// OCI Distribution v2 stub. Doubles as `make verify-suse-registry-mock`.
func ExampleProvider_List() {
	chartYaml := "apiVersion: v2\nname: example-chart\nannotations:\n  ai.suse.com/display-name: Example Chart\n"
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "example-chart/Chart.yaml", Mode: 0o644, Size: int64(len(chartYaml))})
	_, _ = tw.Write([]byte(chartYaml))
	_ = tw.Close()
	_ = gz.Close()
	layerBytes := tarBuf.Bytes()
	layerSum := sha256.Sum256(layerBytes)
	layerDigest := "sha256:" + hex.EncodeToString(layerSum[:])
	manifest := fmt.Sprintf(`{"schemaVersion":2,"layers":[{"mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip","digest":%q,"size":%d}]}`, layerDigest, len(layerBytes))
	manifestSum := sha256.Sum256([]byte(manifest))
	manifestDigest := "sha256:" + hex.EncodeToString(manifestSum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"repositories":[
			"ai/charts/nvidia/nim-llm",
			"ai/charts/example-chart"
		]}`)
	})
	mux.HandleFunc("/v2/ai/charts/example-chart/tags/list", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"name":"ai/charts/example-chart","tags":["1.0.0"]}`)
	})
	mux.HandleFunc("/v2/ai/charts/example-chart/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", manifestDigest)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.WriteString(w, manifest)
	})
	mux.HandleFunc("/v2/ai/charts/example-chart/blobs/"+layerDigest, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(layerBytes)
	})
	// nvidia/nim-llm tag list is intentionally never registered — the walker
	// must NOT request it because the exclude-first-segment filter skips it.
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	walker := oci.NewWalker(logger)
	annR := oci.NewAnnotationReader(logger, walker)
	p := suse_registry.NewProvider(logger, walker, annR)
	p.UpdateSettings(suse_registry.EngineSettings{RegistryEndpoint: ts.URL})

	ctx := context.Background()
	if err := p.Refresh(ctx); err != nil {
		fmt.Println("err:", err)
		return
	}
	entries, _ := p.List(ctx)
	for _, e := range entries {
		fmt.Printf("%s display=%s\n", e.ID, e.DisplayName)
	}
	// Output:
	// example-chart:1.0.0 display=Example Chart
}
