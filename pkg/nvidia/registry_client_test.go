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

	c := newRegistryClient(ts.Client(), ts.URL, "user", "tok", nil)
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

	c := newRegistryClient(ts.Client(), ts.URL, "alice", "s3cr3t", nil)
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

	c := newRegistryClient(ts.Client(), ts.URL, "", "", nil)
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

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t", nil)
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

	c := newRegistryClient(ts.Client(), ts.URL, "u", "wrong", nil)
	_, err := c.ListRepositories(context.Background())
	if !stderrors.Is(err, ErrUnauthorized) {
		t.Errorf("ListRepositories err = %v, want ErrUnauthorized", err)
	}
}

func TestRegistryClient_ListRepositories_Returns500AsUnexpected(t *testing.T) {
	ts := newTestServer(t)
	ts.catalogPages[""] = testPage{body: `oops`, code: http.StatusInternalServerError}

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t", nil)
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

	c := newRegistryClient(http.DefaultClient, closedURL, "u", "t", nil)
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

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t", nil)
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

	c := newRegistryClient(ts.Client(), ts.URL, "u", "t", nil)
	_, err := c.ListTags(context.Background(), "nonexistent")
	if !stderrors.Is(err, ErrUnexpectedResponse) {
		t.Errorf("ListTags err = %v, want ErrUnexpectedResponse for 404", err)
	}
}

// --- OCI Bearer-token exchange (RFC: distribution/spec/auth/token) ---

// newBearerStubs starts two cooperating httptest servers: a "realm"
// (token-issuer) that returns a JSON Bearer token, and a "registry"
// that 401s the first request with a Www-Authenticate: Bearer challenge
// pointing at the realm, then 200s the retry when the Bearer header is
// present.
type bearerStubs struct {
	realm    *httptest.Server
	registry *httptest.Server
	// recorded
	realmAuthHeader   string
	registryAuthCalls []string // Authorization header on each registry call
	tokenIssued       string
}

func newBearerStubs(t *testing.T, registryBody string) *bearerStubs {
	t.Helper()
	stubs := &bearerStubs{tokenIssued: "test-bearer-token-xyz"}

	// realm: returns a Bearer token to anyone who authenticates with Basic.
	stubs.realm = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stubs.realmAuthHeader = r.Header.Get("Authorization")
		// Reject if no Basic auth provided
		if !strings.HasPrefix(stubs.realmAuthHeader, "Basic ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + stubs.tokenIssued + `"}`))
	}))
	t.Cleanup(stubs.realm.Close)

	// registry: 401 with Bearer challenge on first request; 200 on retry
	// (when the Bearer header matches the issued token).
	stubs.registry = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		stubs.registryAuthCalls = append(stubs.registryAuthCalls, auth)
		if auth == "Bearer "+stubs.tokenIssued {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(registryBody))
			return
		}
		// First request OR wrong token → 401 with challenge.
		challenge := `Bearer realm="` + stubs.realm.URL + `",service="test-service",scope="registry:catalog:*"`
		w.Header().Set("Www-Authenticate", challenge)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(stubs.registry.Close)

	return stubs
}

func TestRegistryClient_BearerExchange_RetriesWithToken(t *testing.T) {
	stubs := newBearerStubs(t, `{"repositories":["ai/charts/nvidia/nim-llm"]}`)

	c := newRegistryClient(stubs.registry.Client(), stubs.registry.URL, "alice", "s3cr3t", nil)
	got, err := c.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories: unexpected error: %v", err)
	}
	want := []string{"ai/charts/nvidia/nim-llm"}
	if !equalSlice(got, want) {
		t.Errorf("ListRepositories = %v, want %v", got, want)
	}
	// Confirm the realm was hit with Basic auth (alice:s3cr3t).
	if stubs.realmAuthHeader != "Basic YWxpY2U6czNjcjN0" {
		t.Errorf("realm Authorization = %q, want Basic YWxpY2U6czNjcjN0", stubs.realmAuthHeader)
	}
	// Confirm the registry was called twice: first without Bearer, then with.
	if len(stubs.registryAuthCalls) != 2 {
		t.Fatalf("expected 2 registry calls (challenge + retry), got %d: %v",
			len(stubs.registryAuthCalls), stubs.registryAuthCalls)
	}
	if stubs.registryAuthCalls[1] != "Bearer "+stubs.tokenIssued {
		t.Errorf("retry call Authorization = %q, want Bearer %s",
			stubs.registryAuthCalls[1], stubs.tokenIssued)
	}
}

func TestRegistryClient_BearerExchange_NoCredentialsDoesNotRetry(t *testing.T) {
	stubs := newBearerStubs(t, `{"repositories":[]}`)

	// No credentials → no realm exchange possible → first 401 must return
	// ErrUnauthorized without a retry loop.
	c := newRegistryClient(stubs.registry.Client(), stubs.registry.URL, "", "", nil)
	_, err := c.ListRepositories(context.Background())
	if !stderrors.Is(err, ErrUnauthorized) {
		t.Errorf("ListRepositories err = %v, want ErrUnauthorized", err)
	}
	if len(stubs.registryAuthCalls) != 1 {
		t.Errorf("expected exactly 1 registry call (no retry without creds), got %d",
			len(stubs.registryAuthCalls))
	}
}

func TestRegistryClient_BearerExchange_RealmRejects_ReturnsUnauthorized(t *testing.T) {
	// Realm that always 401s — simulates wrong creds.
	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad creds", http.StatusUnauthorized)
	}))
	t.Cleanup(realm.Close)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Www-Authenticate",
			`Bearer realm="`+realm.URL+`",service="test",scope="registry:catalog:*"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	c := newRegistryClient(registry.Client(), registry.URL, "alice", "wrong", nil)
	_, err := c.ListRepositories(context.Background())
	if !stderrors.Is(err, ErrUnauthorized) {
		t.Errorf("ListRepositories err = %v, want ErrUnauthorized", err)
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
