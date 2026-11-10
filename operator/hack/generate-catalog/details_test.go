package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/SUSE/aif-operator/internal/catalog"
)

func TestNGCDetailsPath(t *testing.T) {
	tests := []struct {
		name          string
		repositoryURL string
		slug          string
		wantPath      string
		wantOK        bool
	}{
		{"org only", nvidiaHelmRepo, "k8s-nim-operator",
			"/v2/org/nvidia/helm-charts/k8s-nim-operator", true},
		{"org and team, trailing slash", "https://helm.ngc.nvidia.com/nim/mit/", "boltz2-nim",
			"/v2/org/nim/team/mit/helm-charts/boltz2-nim", true},
		{"non-NGC host", "https://charts.example.com/x", "a", "", false},
		{"no org", "https://helm.ngc.nvidia.com", "a", "", false},
		{"too many path segments", "https://helm.ngc.nvidia.com/a/b/c", "x", "", false},
		{"empty slug", nvidiaHelmRepo, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotOK := ngcDetailsPath(tt.repositoryURL, tt.slug)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotOK && gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

func TestParseDetails(t *testing.T) {
	body := []byte(`{"artifact":{"description":"## T","latestVersionId":"3.1.3",` +
		`"updatedDate":"2026-10-26T18:58:25.424Z"}}`)
	got, err := parseDetails(body)
	if err != nil {
		t.Fatalf("parseDetails: %v", err)
	}
	want := chartDetails{Overview: "## T", LatestVersion: "3.1.3", UpdatedDate: "2026-10-26T18:58:25.424Z"}
	if got != want {
		t.Errorf("parseDetails = %+v, want %+v", got, want)
	}

	if _, err := parseDetails([]byte(`{"requestStatus":{}}`)); err == nil {
		t.Error("expected error for missing artifact")
	}
}

func TestFetchAllDetails(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}
		switch r.URL.Path {
		case "/v2/org/nvidia/helm-charts/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"artifact":{"description":"## OK","latestVersionId":"1.0.0",` +
				`"updatedDate":"2026-01-01T00:00:00.000Z"}}`))
		case "/v2/org/nvidia/helm-charts/gone":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	items := []catalog.Item{
		{SlugName: "ok", RepositoryURL: nvidiaHelmRepo},
		{SlugName: "gone", RepositoryURL: nvidiaHelmRepo},
		{SlugName: "ext", RepositoryURL: "https://charts.example.com"},
	}

	got := fetchAllDetails(context.Background(), srv.Client(), srv.URL, "test-key", items)

	wantKey := chartKey(nvidiaHelmRepo, "ok")
	if padded := chartKey(" "+nvidiaHelmRepo+"/ ", "ok"); padded != wantKey {
		t.Errorf("chartKey with padded URL = %q, want %q", padded, wantKey)
	}
	if len(got) != 1 {
		t.Fatalf("len(result) = %d, want 1: %+v", len(got), got)
	}
	if _, ok := got[wantKey]; !ok {
		t.Errorf("result missing key %q: %+v", wantKey, got)
	}
	if n := atomic.LoadInt32(&requests); n != 2 {
		t.Errorf("server saw %d requests, want 2", n)
	}
}

func TestFetchAllDetails_RequestsEachChartOnce(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = w.Write([]byte(`{"artifact":{"description":"## OK"}}`))
	}))
	defer srv.Close()

	// The same chart listed several times, including with a padded URL, as
	// happens when a chart appears both in the search results and the catalog.
	items := []catalog.Item{
		{SlugName: "ok", RepositoryURL: nvidiaHelmRepo},
		{SlugName: "ok", RepositoryURL: nvidiaHelmRepo},
		{SlugName: "ok", RepositoryURL: " " + nvidiaHelmRepo + "/ "},
	}

	got := fetchAllDetails(context.Background(), srv.Client(), srv.URL, "test-key", items)

	if n := atomic.LoadInt32(&requests); n != 1 {
		t.Errorf("server saw %d requests, want 1", n)
	}
	if len(got) != 1 {
		t.Errorf("len(result) = %d, want 1: %+v", len(got), got)
	}
}
