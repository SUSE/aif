package nvidia

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer is a minimal OCI Distribution v2 stub. Each endpoint takes a
// canned JSON body and an optional Link header for pagination. Records the
// inbound Authorization header so tests can assert on credentials.
//
// Routes:
//   GET /v2/_catalog            → catalog body (paginated via ?n=&last=)
//   GET /v2/<repo>/tags/list    → tags body for that repo
type testServer struct {
	*httptest.Server
	authHeader string

	// Catalog responses keyed by query string ("" = first page).
	catalogPages map[string]testPage

	// Tags responses keyed by repo name.
	tagsPages map[string]map[string]testPage
}

type testPage struct {
	body string
	next string // value for Link rel="next"; empty = no more pages
	code int    // HTTP status; 0 means 200
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{
		catalogPages: map[string]testPage{},
		tagsPages:    map[string]map[string]testPage{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		ts.authHeader = r.Header.Get("Authorization")
		page, ok := ts.catalogPages[r.URL.RawQuery]
		if !ok {
			http.Error(w, "no canned response", http.StatusInternalServerError)
			return
		}
		respondPage(w, page)
	})
	// Tags endpoint: /v2/<repo>/tags/list
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		ts.authHeader = r.Header.Get("Authorization")
		const suffix = "/tags/list"
		path := strings.TrimPrefix(r.URL.Path, "/v2/")
		if !strings.HasSuffix(path, suffix) {
			http.NotFound(w, r)
			return
		}
		repo := strings.TrimSuffix(path, suffix)
		repoPages, ok := ts.tagsPages[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		page, ok := repoPages[r.URL.RawQuery]
		if !ok {
			http.Error(w, "no canned response", http.StatusInternalServerError)
			return
		}
		respondPage(w, page)
	})
	ts.Server = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func respondPage(w http.ResponseWriter, p testPage) {
	if p.next != "" {
		w.Header().Set("Link", `</v2/_catalog?`+p.next+`>; rel="next"`)
	}
	w.Header().Set("Content-Type", "application/json")
	if p.code != 0 {
		w.WriteHeader(p.code)
	}
	_, _ = w.Write([]byte(p.body))
}

// --- ListRepositories ---

func TestRegistryClient_ListRepositories_SinglePage(t *testing.T) {
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{body: `{"repositories":["ai/charts/nvidia/nim-llm","ai/charts/nvidia/nim-vlm","other/foo"]}`}

	c := newRegistryClient(ts.Client(), ts.URL, "user", "tok")
	got, err := c.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories: unexpected error: %v", err)
	}
	want := []string{"ai/charts/nvidia/nim-llm", "ai/charts/nvidia/nim-vlm", "other/foo"}
	if !equalSlice(got, want) {
		t.Errorf("ListRepositories = %v, want %v", got, want)
	}
}

func TestRegistryClient_ListRepositories_SendsBasicAuth(t *testing.T) {
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{body: `{"repositories":[]}`}

	c := newRegistryClient(ts.Client(), ts.URL, "alice", "s3cr3t")
	if _, err := c.ListRepositories(context.Background()); err != nil {
		t.Fatalf("ListRepositories: unexpected error: %v", err)
	}
	// "alice:s3cr3t" base64-encoded = "YWxpY2U6czNjcjN0"
	want := "Basic YWxpY2U6czNjcjN0"
	if ts.authHeader != want {
		t.Errorf("Authorization header = %q, want %q", ts.authHeader, want)
	}
}

func TestRegistryClient_ListRepositories_NoCredentialsOmitsAuth(t *testing.T) {
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{body: `{"repositories":[]}`}

	c := newRegistryClient(ts.Client(), ts.URL, "", "")
	if _, err := c.ListRepositories(context.Background()); err != nil {
		t.Fatalf("ListRepositories: unexpected error: %v", err)
	}
	if ts.authHeader != "" {
		t.Errorf("Authorization header = %q, want empty when no credentials supplied", ts.authHeader)
	}
}

func TestRegistryClient_ListRepositories_FollowsPagination(t *testing.T) {
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{
		body: `{"repositories":["repo1","repo2"]}`,
		next: "n=2&last=repo2",
	}
	ts.catalogPages["n=2&last=repo2"] = testPage{
		body: `{"repositories":["repo3"]}`,
		// no Link → terminal page
	}

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t")
	got, err := c.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories: unexpected error: %v", err)
	}
	want := []string{"repo1", "repo2", "repo3"}
	if !equalSlice(got, want) {
		t.Errorf("ListRepositories = %v, want %v", got, want)
	}
}

func TestRegistryClient_ListRepositories_Returns401AsUnauthorized(t *testing.T) {
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{body: `unauthorized`, code: http.StatusUnauthorized}

	c := newRegistryClient(ts.Client(), ts.URL, "u", "wrong")
	_, err := c.ListRepositories(context.Background())
	if !stderrors.Is(err, ErrUnauthorized) {
		t.Errorf("ListRepositories err = %v, want ErrUnauthorized", err)
	}
}

func TestRegistryClient_ListRepositories_Returns500AsUnexpected(t *testing.T) {
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{body: `oops`, code: http.StatusInternalServerError}

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t")
	_, err := c.ListRepositories(context.Background())
	if !stderrors.Is(err, ErrUnexpectedResponse) {
		t.Errorf("ListRepositories err = %v, want ErrUnexpectedResponse", err)
	}
}

func TestRegistryClient_ListRepositories_NetworkErrorIsUnreachable(t *testing.T) {
	// Closed server → connection refused. Use a server that's already shut down.
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := ts.URL
	ts.Close()

	c := newRegistryClient(http.DefaultClient, closedURL, "u", "t")
	_, err := c.ListRepositories(context.Background())
	if !stderrors.Is(err, ErrUnreachable) {
		t.Errorf("ListRepositories err = %v, want ErrUnreachable", err)
	}
}

// --- ListTags ---

func TestRegistryClient_ListTags_SinglePage(t *testing.T) {
	ts := newTestServer(t)
	ts.tagsPages["ai/charts/nvidia/nim-llm"] = map[string]testPage{
		"": {body: `{"name":"ai/charts/nvidia/nim-llm","tags":["1.0.0","1.1.0","1.2.0"]}`},
	}

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t")
	got, err := c.ListTags(context.Background(), "ai/charts/nvidia/nim-llm")
	if err != nil {
		t.Fatalf("ListTags: unexpected error: %v", err)
	}
	want := []string{"1.0.0", "1.1.0", "1.2.0"}
	if !equalSlice(got, want) {
		t.Errorf("ListTags = %v, want %v", got, want)
	}
}

func TestRegistryClient_ListTags_404IsUnexpected(t *testing.T) {
	ts := newTestServer(t)
	// no tagsPages entry → 404

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t")
	_, err := c.ListTags(context.Background(), "nonexistent")
	if !stderrors.Is(err, ErrUnexpectedResponse) {
		t.Errorf("ListTags err = %v, want ErrUnexpectedResponse for 404", err)
	}
}

// equalSlice is a tiny helper to keep tests free of reflect noise.
func equalSlice[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
