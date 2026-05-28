package oci_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	"github.com/SUSE/aif/pkg/oci"
)

// ExampleWalker_EnumerateCharts exercises the Walker against an in-process
// OCI Distribution v2 stub. Doubles as the contract `make verify-oci-mock`
// runs to prove the walker works without a live registry.
func ExampleWalker_EnumerateCharts() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"repositories":[
			"ai/charts/nvidia/nim-llm",
			"ai/charts/example/foo",
			"ai/charts/example/bar",
			"other/skip"
		]}`)
	})
	tags := map[string]string{
		"ai/charts/example/foo": `{"name":"ai/charts/example/foo","tags":["1.0.0","sha256-2da536d9d3e093af219f235df2dbdac6d948f4536f11f69342f78ee6c2f7d911.sig"]}`,
		"ai/charts/example/bar": `{"name":"ai/charts/example/bar","tags":["2.0.0","2.1.0"]}`,
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := oci.NewWalker(logger)
	w.UpdateSettings(oci.EngineSettings{Endpoint: ts.URL})

	got, err := w.EnumerateCharts(context.Background(), "ai/charts/", []string{"nvidia"})
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].Repository != got[j].Repository {
			return got[i].Repository < got[j].Repository
		}
		return got[i].Tag < got[j].Tag
	})
	for _, c := range got {
		fmt.Printf("%s:%s\n", c.Repository, c.Tag)
	}
	// Output:
	// ai/charts/example/bar:2.0.0
	// ai/charts/example/bar:2.1.0
	// ai/charts/example/foo:1.0.0
}
