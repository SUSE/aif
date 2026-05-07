package source_collection_test

import (
	"context"
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
	// 1. Spin up a fake Application Collection API serving three SUSE-
	//    certified HELM_CHART applications.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"slug_name":      "milvus",
					"title":          "Milvus",
					"publisher_name": "Zilliz",
					"helm":           map[string]string{"repository_url": "oci://dp.apps.rancher.io/charts", "chart_name": "milvus"},
					"latest_version": map[string]string{"version": "2.4.0"},
				},
				{
					"slug_name":      "ollama",
					"title":          "Ollama",
					"publisher_name": "Ollama Inc",
					"helm":           map[string]string{"repository_url": "oci://dp.apps.rancher.io/charts", "chart_name": "ollama"},
					"latest_version": map[string]string{"version": "0.4.1"},
				},
				{
					"slug_name":      "vllm",
					"title":          "vLLM",
					"publisher_name": "vLLM Project",
					"helm":           map[string]string{"repository_url": "oci://dp.apps.rancher.io/charts", "chart_name": "vllm"},
					"latest_version": map[string]string{"version": "0.6.0"},
				},
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 2. Construct Client and configure it with the stub's address.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // suppress retry logs for clean Example output
	c := source_collection.NewClient(logger)
	c.UpdateSettings(source_collection.EngineSettings{
		APIURL:   ts.URL,
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
		fmt.Printf("%-10s  publisher=%-15s  chart=%s\n", a.ID, a.Publisher, a.ChartRef)
	}

	// Output:
	// milvus      publisher=Zilliz           chart=oci://dp.apps.rancher.io/charts/milvus:2.4.0
	// ollama      publisher=Ollama Inc       chart=oci://dp.apps.rancher.io/charts/ollama:0.4.1
	// vllm        publisher=vLLM Project     chart=oci://dp.apps.rancher.io/charts/vllm:0.6.0
}
