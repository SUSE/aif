package nvidia_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	"github.com/SUSE/aif/pkg/nvidia"
)

// Example_discovery demonstrates the end-to-end Discovery flow against an
// in-process OCI Distribution v2 stub. It also doubles as the contract
// `make verify-nim-mock` runs to prove the package works without a live
// registry.
//
// Spec hooks: ARCHITECTURE.md §13.1 (mirror path convention + chart→type
// heuristic) and §6.2 (Discovery interface).
func Example_discovery() {
	// 1. Spin up a fake OCI Distribution v2 registry serving two NVIDIA
	//    charts (one LLM, one VLM) plus one unrelated chart that should be
	//    filtered out by the prefix.
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"repositories":[
			"ai/charts/nvidia/nim-llm",
			"ai/charts/nvidia/nim-vlm",
			"other/something"
		]}`)
	})
	tags := map[string]string{
		"ai/charts/nvidia/nim-llm": `{"name":"ai/charts/nvidia/nim-llm","tags":["1.0.0","1.1.0"]}`,
		"ai/charts/nvidia/nim-vlm": `{"name":"ai/charts/nvidia/nim-vlm","tags":["2.0.0"]}`,
	}
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/"), "/tags/list")
		body, ok := tags[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 2. Construct Discovery and configure it with the stub's address.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // suppress log output for clean Example output
	d := nvidia.NewDiscovery(logger)
	d.UpdateSettings(nvidia.EngineSettings{
		RegistryEndpoint: ts.URL,
		Username:         "demo-user",
		Token:            "demo-token",
	})

	// 3. Refresh + Index.
	ctx := context.Background()
	if err := d.Refresh(ctx); err != nil {
		fmt.Println("Refresh error:", err)
		return
	}
	entries, _ := d.Index(ctx)

	// 4. Print result deterministically (Index returns sorted by ID).
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	for _, e := range entries {
		fmt.Printf("%-15s  type=%-3s  chart=%s\n", e.ID, e.Type, e.Chart)
	}

	// Output:
	// nim-llm:1.0.0    type=llm  chart=nim-llm
	// nim-llm:1.1.0    type=llm  chart=nim-llm
	// nim-vlm:2.0.0    type=vlm  chart=nim-vlm
}
