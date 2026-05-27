package source_collection_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"

	"github.com/SUSE/aif/pkg/source_collection"
)

// Example_clientList demonstrates the end-to-end List flow against an
// in-process Application Collection HTTP API stub. Doubles as the
// contract `make verify-appco-mock` runs to prove the package works
// without hitting the live api.apps.rancher.io.
//
// Spec hooks: ARCHITECTURE.md §13.2 (Application Collection HTTP API
// shape) and §6.2 (source_collection.Client interface).
func Example_clientList() {
	// 1. Spin up a fake Application Collection API serving three
	//    applications. List endpoint returns minimal items; detail
	//    endpoint supplies labels (→ Categories); /v1/artifacts
	//    supplies (version, revision) → (LatestVersion, ChartTag).
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"slug_name": "milvus", "name": "Milvus"},
				{"slug_name": "ollama", "name": "Ollama"},
				{"slug_name": "vllm", "name": "vLLM"},
			},
		})
	})
	mux.HandleFunc("/v1/applications/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		slug := r.URL.Path[len("/v1/applications/"):]
		details := map[string]map[string]any{
			"milvus": {
				"slug_name": "milvus",
				"labels":    []string{"category:vector-db"},
				"branches":  []map[string]any{{"baseline": "2.4.0", "is_lts": false}},
			},
			"ollama": {
				"slug_name": "ollama",
				"labels":    []string{"category:llm"},
				"branches":  []map[string]any{{"baseline": "0.4.1", "is_lts": false}},
			},
			"vllm": {
				"slug_name": "vllm",
				"labels":    []string{"category:llm"},
				"branches":  []map[string]any{{"baseline": "0.6.0", "is_lts": false}},
			},
		}
		_ = json.NewEncoder(w).Encode(details[slug])
	})
	mux.HandleFunc("/v1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		slug := r.URL.Query().Get("component_slug_name")
		// Map each example slug to a deterministic chart tag. Add entries
		// here whenever the list mock above gains a new app.
		tags := map[string]struct{ version, revision string }{
			"milvus": {"2.4.0", "1"},
			"ollama": {"0.4.1", "1"},
			"vllm":   {"0.6.0", "1"},
		}
		v, ok := tags[slug]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"items":[{"name":"%s:%s-%s","version":%q,"revision":%q,"packaging_format":"HELM_CHART","application_version":"ignored"}],
			"page":1,"page_size":1,"total_size":1,"total_pages":1
		}`, slug, v.version, v.revision, v.version, v.revision)))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 2. Construct Client and configure it with the stub's address.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // suppress retry logs for clean Example output
	c, _ := source_collection.NewClient(logger)
	c.UpdateSettings(source_collection.EngineSettings{
		APIURL:   ts.URL,
		OCIHost:  "dp.apps.rancher.io",
		Username: "demo-user",
		Token:    "demo-token",
	})

	// 3. List the catalog.
	apps, err := c.List(context.Background())
	if err != nil {
		fmt.Println("List error:", err)
		return
	}

	// 4. Print result deterministically (List preserves API order; we sort
	//    by ID to make the Output independent of upstream ordering).
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	for _, a := range apps {
		category := ""
		if len(a.Categories) > 0 {
			category = a.Categories[0]
		}
		fmt.Printf("%-10s  version=%-6s  category=%-10s  chart=%s\n", a.ID, a.LatestVersion, category, a.ChartRef)
	}

	// Output:
	// milvus      version=2.4.0   category=vector-db   chart=oci://dp.apps.rancher.io/charts/milvus:2.4.0-1
	// ollama      version=0.4.1   category=llm         chart=oci://dp.apps.rancher.io/charts/ollama:0.4.1-1
	// vllm        version=0.6.0   category=llm         chart=oci://dp.apps.rancher.io/charts/vllm:0.6.0-1
}

// Example_chartAnnotations exercises the AnnotationReader against an
// in-process Helm OCI stub. Doubles as the contract `make verify-appco-mock`
// runs to prove digest-cached annotation fetching works without a live
// registry.
func Example_chartAnnotations() {
	chartYaml := "apiVersion: v2\nname: my-chart\nannotations:\n  ai.suse.com/role: reference-blueprint\n  ai.suse.com/use-case: rag\n"
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "my-chart/Chart.yaml", Mode: 0o644, Size: int64(len(chartYaml))}
	_ = tw.WriteHeader(hdr)
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
	mux.HandleFunc("/v2/charts/my-chart/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", manifestDigest)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.WriteString(w, manifest)
	})
	mux.HandleFunc("/v2/charts/my-chart/blobs/"+layerDigest, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(layerBytes)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, ar := source_collection.NewClient(logger)
	c.UpdateSettings(source_collection.EngineSettings{OCIHost: ts.URL})

	ann, err := ar.ChartAnnotations(context.Background(), "charts", "my-chart", "1.0.0")
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println("role:", ann["ai.suse.com/role"])
	fmt.Println("use-case:", ann["ai.suse.com/use-case"])

	// Output:
	// role: reference-blueprint
	// use-case: rag
}
