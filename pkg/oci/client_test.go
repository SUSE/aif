package oci

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestEnumerateCharts_FiltersPrefixAndExcludesSubtrees verifies the
// walker honours both the prefix filter and the first-segment exclude
// list (the core capability suse_registry needs to skip "nvidia/").
func TestEnumerateCharts_FiltersPrefixAndExcludesSubtrees(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"repositories":[
			"ai/charts/nvidia/nim-llm",
			"ai/charts/example/foo",
			"ai/charts/example/bar",
			"other/something"
		]}`)
	})
	tags := map[string]string{
		"ai/charts/example/foo": `{"name":"ai/charts/example/foo","tags":["1.0.0"]}`,
		"ai/charts/example/bar": `{"name":"ai/charts/example/bar","tags":["2.0.0","2.1.0"]}`,
	}
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Path
		repo = repo[len("/v2/") : len(repo)-len("/tags/list")]
		body, ok := tags[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	w := NewWalker(silentLogger())
	w.UpdateSettings(EngineSettings{Endpoint: ts.URL})

	got, err := w.EnumerateCharts(context.Background(), "ai/charts/", []string{"nvidia"})
	if err != nil {
		t.Fatalf("EnumerateCharts: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 coordinates (foo:1.0.0, bar:2.0.0, bar:2.1.0), got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Repository == "ai/charts/nvidia/nim-llm" {
			t.Errorf("nvidia subtree must be excluded; got %+v", c)
		}
	}
}

// TestEnumerateCharts_NotConfigured asserts the no-settings path.
func TestEnumerateCharts_NotConfigured(t *testing.T) {
	w := NewWalker(silentLogger())
	_, err := w.EnumerateCharts(context.Background(), "", nil)
	if err == nil || err.Error() != ErrNotConfigured.Error() {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}
