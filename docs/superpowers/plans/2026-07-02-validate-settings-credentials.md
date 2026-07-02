# Validate Settings Credentials Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an on-demand per-section "Test" action on the AI Factory Settings page that verifies each configured registry credential (SUSE, AppCo, NGC) and the Fleet GitOps git credential actually authenticate.

**Architecture:** A new operator endpoint `POST /api/v1/settings/validate-credentials` resolves each credential's Secret refs (from the request body for test-before-save, falling back to the saved Settings CR) and performs a live auth check — a hand-rolled Docker Registry v2 handshake for registries, a `git ls-remote` for GitOps. The UI adds a `validateCredentials()` client and a per-section Test button with an inline result badge.

**Tech Stack:** Go (controller-runtime HTTP handlers, `net/http`, `go-git`), Vue 3 (Options API, `@rancher/shell` `AsyncButton`), no new dependencies.

## Global Constraints

- Prefix all Go commands with `GOTOOLCHAIN=auto` (system Go 1.24 vs go.mod newer). Run Go commands from `operator/`.
- No new Go module dependencies — registry probe is hand-rolled `net/http`; git check reuses `go-git` (already vendored).
- No new UI dependencies.
- Commit messages are plain — **no `Co-Authored-By` trailer**.
- API group is `ai-factory.suse.com/v1alpha1`; API types imported as `aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"`.
- Response `status` values are exactly `ok` | `failed` | `error` | `skipped`. `failed` = endpoint reached but creds rejected; `error` = endpoint unreachable. Never put secret values in any message.
- Target identifiers are exactly `applicationCollection`, `suseRegistry`, `nvidia`, `gitops`.
- UI code in `ui/pkg/aif-ui/` follows the existing Options-API style of `pages/Settings.vue`.

---

## File Structure

- **Create** `operator/internal/credcheck/credcheck.go` — Docker Registry v2 auth probe (`ProbeRegistry`).
- **Create** `operator/internal/credcheck/credcheck_test.go` — probe tests against `httptest` TLS servers.
- **Modify** `operator/internal/git/client.go` — add `CheckAuth` method.
- **Modify** `operator/internal/git/client_test.go` — add `CheckAuth` tests.
- **Modify** `operator/internal/api/settings.go` — add `validate-credentials` route, request/response types, resolution + classification logic.
- **Modify** `operator/internal/api/settings_test.go` — add handler tests using function seams.
- **Modify** `ui/pkg/aif-ui/utils/operator-api.ts` — add `validateCredentials` + types.
- **Modify** `ui/pkg/aif-ui/pages/Settings.vue` — per-section Test buttons + inline results.
- **Modify** `ui/pkg/aif-ui/l10n/en-us.json` — add `suseai.pages.settings.test.*` strings.

---

## Task 1: Registry v2 auth probe (`credcheck` package)

**Files:**
- Create: `operator/internal/credcheck/credcheck.go`
- Test: `operator/internal/credcheck/credcheck_test.go`

**Interfaces:**
- Consumes: nothing (leaf package, stdlib only).
- Produces:
  - `credcheck.Status` (string) with consts `StatusOK = "ok"`, `StatusFailed = "failed"`, `StatusError = "error"`.
  - `credcheck.Result{ Status Status; Message string }`.
  - `credcheck.ProbeRegistry(ctx context.Context, host, username, password string) Result`.
  - Unexported seam `probe(ctx, client *http.Client, scheme, host, username, password string) Result` used by tests to point at an `httptest` TLS server.

- [ ] **Step 1: Write the failing tests**

Create `operator/internal/credcheck/credcheck_test.go`:

```go
package credcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	return strings.TrimPrefix(rawURL, "https://")
}

// 200 straight from /v2/ with basic auth => ok.
func TestProbe_BasicAuthOK(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u == "user" && p == "pass" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	res := probe(context.Background(), srv.Client(), "https", hostOf(t, srv.URL), "user", "pass")
	if res.Status != StatusOK {
		t.Fatalf("status=%q msg=%q want ok", res.Status, res.Message)
	}
}

// 401 -> bearer token flow -> 200 => ok.
func TestProbe_BearerTokenOK(t *testing.T) {
	var srvURL string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			if r.Header.Get("Authorization") == "Bearer good-token" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srvURL+`/token",service="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/token":
			u, p, _ := r.BasicAuth()
			if u != "user" || p != "pass" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"good-token"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	res := probe(context.Background(), srv.Client(), "https", hostOf(t, srv.URL), "user", "pass")
	if res.Status != StatusOK {
		t.Fatalf("status=%q msg=%q want ok", res.Status, res.Message)
	}
}

// Bad basic-auth creds with no bearer challenge => failed.
func TestProbe_BadCredsFailed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	res := probe(context.Background(), srv.Client(), "https", hostOf(t, srv.URL), "user", "wrong")
	if res.Status != StatusFailed {
		t.Fatalf("status=%q want failed", res.Status)
	}
}

// 500 => error.
func TestProbe_ServerErrorIsError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := probe(context.Background(), srv.Client(), "https", hostOf(t, srv.URL), "user", "pass")
	if res.Status != StatusError {
		t.Fatalf("status=%q want error", res.Status)
	}
}

// Unreachable host => error (dial failure, deterministic, no timing dependency).
func TestProbe_UnreachableIsError(t *testing.T) {
	res := probe(context.Background(), http.DefaultClient, "https", "127.0.0.1:1", "user", "pass")
	if res.Status != StatusError {
		t.Fatalf("status=%q want error", res.Status)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd operator && GOTOOLCHAIN=auto go test ./internal/credcheck/...`
Expected: FAIL — `undefined: probe`, `undefined: StatusOK`, etc.

- [ ] **Step 3: Write the implementation**

Create `operator/internal/credcheck/credcheck.go`:

```go
// Package credcheck performs live authentication probes against container
// registries using the Docker Registry v2 protocol.
package credcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Status is the outcome of a credential probe.
type Status string

const (
	// StatusOK means the credentials authenticated successfully.
	StatusOK Status = "ok"
	// StatusFailed means the endpoint was reached but rejected the credentials.
	StatusFailed Status = "failed"
	// StatusError means the endpoint could not be reached (DNS/dial/timeout/etc).
	StatusError Status = "error"
)

// Result is the outcome of a probe. Message never contains secret values.
type Result struct {
	Status  Status
	Message string
}

const probeTimeout = 10 * time.Second

// ProbeRegistry checks that (username, password) authenticate against the
// registry at host, following the Docker Registry v2 bearer-token handshake.
func ProbeRegistry(ctx context.Context, host, username, password string) Result {
	return probe(ctx, http.DefaultClient, "https", host, username, password)
}

func probe(ctx context.Context, client *http.Client, scheme, host, username, password string) Result {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	base := scheme + "://" + host + "/v2/"

	resp, err := doGet(ctx, client, base, username, password, "")
	if err != nil {
		return Result{Status: StatusError, Message: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return Result{Status: StatusOK, Message: "authenticated"}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		if strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
			token, terr := fetchBearerToken(ctx, client, challenge, username, password)
			if terr != nil {
				return Result{Status: StatusFailed, Message: terr.Error()}
			}
			resp2, err2 := doGet(ctx, client, base, username, password, token)
			if err2 != nil {
				return Result{Status: StatusError, Message: err2.Error()}
			}
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				return Result{Status: StatusOK, Message: "authenticated"}
			}
			return Result{Status: StatusFailed, Message: statusMessage(resp2.StatusCode)}
		}
		return Result{Status: StatusFailed, Message: "401 unauthorized"}
	}
	return Result{Status: StatusError, Message: "unexpected " + statusMessage(resp.StatusCode)}
}

func doGet(ctx context.Context, client *http.Client, url, user, pass, bearer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	} else if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	return client.Do(req)
}

func fetchBearerToken(ctx context.Context, client *http.Client, challenge, user, pass string) (string, error) {
	params := parseChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("no realm in bearer challenge")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if svc := params["service"]; svc != "" {
		q.Set("service", svc)
	}
	if scope := params["scope"]; scope != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %s", statusMessage(resp.StatusCode))
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.Token != "" {
		return tok.Token, nil
	}
	if tok.AccessToken != "" {
		return tok.AccessToken, nil
	}
	return "", fmt.Errorf("no token in response")
}

// parseChallenge parses a `Bearer realm="...",service="...",scope="..."` header.
func parseChallenge(h string) map[string]string {
	out := map[string]string{}
	h = strings.TrimSpace(h)
	if i := strings.IndexByte(h, ' '); i >= 0 {
		h = h[i+1:] // strip the "Bearer" scheme
	}
	for _, part := range strings.Split(h, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		out[key] = val
	}
	return out
}

func statusMessage(code int) string {
	return fmt.Sprintf("%d %s", code, http.StatusText(code))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd operator && GOTOOLCHAIN=auto go test ./internal/credcheck/...`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add operator/internal/credcheck/
git commit -m "feat(operator): add registry v2 credential probe"
```

---

## Task 2: Git auth check (`CheckAuth` on git.Client)

**Files:**
- Modify: `operator/internal/git/client.go`
- Test: `operator/internal/git/client_test.go`

**Interfaces:**
- Consumes: existing `git.Client` (built via `git.NewFromSettings`).
- Produces: method `func (c *Client) CheckAuth(ctx context.Context) error` — nil when the repo is reachable and creds authenticate (an empty remote counts as reachable); a `transport.ErrAuthenticationRequired`/`ErrAuthorizationFailed` when creds are rejected; other errors when unreachable.

- [ ] **Step 1: Write the failing tests**

Append to `operator/internal/git/client_test.go`:

```go
func TestCheckAuth_ReachableOK(t *testing.T) {
	remote := newTestRemote(t)
	c := newClient(t, remote)

	err := c.CheckAuth(context.Background())
	require.NoError(t, err)
}

func TestCheckAuth_EmptyRemoteOK(t *testing.T) {
	remote := newEmptyTestRemote(t)
	c := newClient(t, remote)

	err := c.CheckAuth(context.Background())
	require.NoError(t, err)
}

func TestCheckAuth_UnreachableErrors(t *testing.T) {
	c := newClient(t, "file:///nonexistent/repo/path.git")

	err := c.CheckAuth(context.Background())
	require.Error(t, err)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd operator && GOTOOLCHAIN=auto go test ./internal/git/ -run TestCheckAuth`
Expected: FAIL — `c.CheckAuth undefined`.

- [ ] **Step 3: Write the implementation**

Add to `operator/internal/git/client.go` (after `NewFromSettings`). All required imports (`gogit`, `config`, `memory`, `transport`) are already present in the file:

```go
// CheckAuth verifies the configured repository is reachable and the credentials
// authenticate, without cloning or writing. It is the equivalent of
// `git ls-remote`. An empty remote (no commits yet) counts as reachable.
func (c *Client) CheckAuth(ctx context.Context) error {
	remote := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: gogit.DefaultRemoteName,
		URLs: []string{c.repoURL},
	})
	_, err := remote.ListContext(ctx, &gogit.ListOptions{Auth: c.auth})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd operator && GOTOOLCHAIN=auto go test ./internal/git/ -run TestCheckAuth`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add operator/internal/git/
git commit -m "feat(operator): add git CheckAuth ls-remote check"
```

---

## Task 3: `validate-credentials` endpoint

**Files:**
- Modify: `operator/internal/api/settings.go`
- Test: `operator/internal/api/settings_test.go`

**Interfaces:**
- Consumes: `credcheck.ProbeRegistry` (Task 1), `(*git.Client).CheckAuth` (Task 2), existing `readSecretKey`, `settingsSecretReader`, `registryurl.Host`, `git.NewFromSettings`.
- Produces: route `POST /api/v1/settings/validate-credentials`. Request `{ targets?: string[], overrides?: map[target]{userSecretRef?,tokenSecretRef?,credSecretRef?,repoURL?,branch?} }`. Response `{ results: [{ target, status, host?, message, latencyMs? }] }`. Overridable seams `probeRegistryFn` and `gitCheckAuthFn` for tests.

- [ ] **Step 1: Write the failing tests**

Append to `operator/internal/api/settings_test.go`:

```go
func TestValidateCredentials_RegistryOKFromSaved(t *testing.T) {
	const ns = "aif-operator"
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "su-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("p")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			SUSERegistry: aiplatformv1alpha1.SUSERegistrySettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "su-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "su-token", Key: "token"},
			},
		},
	}
	c := newSettingsFakeClient(t, cr, userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	// Stub the probe: assert it receives the resolved creds + default host.
	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	probeRegistryFn = func(_ context.Context, host, user, pass string) credcheck.Result {
		if host != "registry.suse.com" || user != "u" || pass != "p" {
			return credcheck.Result{Status: credcheck.StatusFailed, Message: "unexpected inputs"}
		}
		return credcheck.Result{Status: credcheck.StatusOK, Message: "authenticated"}
	}

	body := `{"targets":["suseRegistry"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", rec.Code, rec.Body)
	}
	var resp validateCredsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Target != "suseRegistry" || resp.Results[0].Status != statusOK {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
	if resp.Results[0].Host != "registry.suse.com" {
		t.Errorf("host=%q want registry.suse.com", resp.Results[0].Host)
	}
}

func TestValidateCredentials_SkippedWhenUnconfigured(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	body := `{"targets":["nvidia"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].Status != statusSkipped {
		t.Fatalf("want skipped, got %+v", resp.Results)
	}
}

func TestValidateCredentials_OverrideRefsBeforeSave(t *testing.T) {
	const ns = "aif-operator"
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ov-user", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("ou")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ov-token", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("op")},
	}
	// Settings CR has NO nvidia refs; the override supplies them.
	c := newSettingsFakeClient(t, sampleCR(), userSecret, tokenSecret)
	h := newSettingsHandler(c, ns)

	orig := probeRegistryFn
	defer func() { probeRegistryFn = orig }()
	got := struct{ user, pass, host string }{}
	probeRegistryFn = func(_ context.Context, host, user, pass string) credcheck.Result {
		got.user, got.pass, got.host = user, pass, host
		return credcheck.Result{Status: credcheck.StatusOK, Message: "authenticated"}
	}

	body := `{"targets":["nvidia"],"overrides":{"nvidia":{"userSecretRef":{"name":"ov-user","key":"username"},"tokenSecretRef":{"name":"ov-token","key":"token"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got.user != "ou" || got.pass != "op" || got.host != "nvcr.io" {
		t.Fatalf("probe got %+v want ou/op/nvcr.io", got)
	}
}

func TestValidateCredentials_GitAuthClassification(t *testing.T) {
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{RepoURL: "https://git.example.com/repo.git", Branch: "main"},
		},
	}
	c := newSettingsFakeClient(t, cr)
	h := newSettingsHandler(c, ns)

	orig := gitCheckAuthFn
	defer func() { gitCheckAuthFn = orig }()
	gitCheckAuthFn = func(_ *git.Client, _ context.Context) error {
		return transport.ErrAuthenticationRequired
	}

	body := `{"targets":["gitops"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp validateCredsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].Status != statusFailed {
		t.Fatalf("want failed (auth), got %+v", resp.Results)
	}
}

func TestValidateCredentials_InvalidJSON400(t *testing.T) {
	c := newSettingsFakeClient(t, sampleCR())
	h := newSettingsHandler(c, "aif-operator")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/validate-credentials", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", rec.Code)
	}
}
```

Add these imports to the `settings_test.go` import block:

```go
	"github.com/SUSE/aif-operator/internal/credcheck"
	"github.com/SUSE/aif-operator/internal/git"
	"github.com/go-git/go-git/v5/plumbing/transport"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd operator && GOTOOLCHAIN=auto go test ./internal/api/ -run TestValidateCredentials`
Expected: FAIL — `undefined: probeRegistryFn`, `undefined: validateCredsResponse`, etc.

- [ ] **Step 3: Add the route**

In `operator/internal/api/settings.go`, add to `Register`:

```go
	mux.HandleFunc("POST /api/v1/settings/validate-credentials", h.validateCredentials)
```

- [ ] **Step 4: Add imports**

Extend the `settings.go` import block with (`errors`, `io`, `time`, credcheck, transport — `git` and `registryurl` are already imported):

```go
	"errors"
	"io"
	"time"

	"github.com/SUSE/aif-operator/internal/credcheck"
	"github.com/go-git/go-git/v5/plumbing/transport"
```

- [ ] **Step 5: Add types, seams, and handler**

Append to `operator/internal/api/settings.go`:

```go
// Function seams so tests can stub the live network checks.
var (
	probeRegistryFn = credcheck.ProbeRegistry
	gitCheckAuthFn  = (*git.Client).CheckAuth
)

const (
	statusOK      = "ok"
	statusFailed  = "failed"
	statusError   = "error"
	statusSkipped = "skipped"
)

var allValidateTargets = []string{"applicationCollection", "suseRegistry", "nvidia", "gitops"}

type validateOverride struct {
	UserSecretRef  *aiplatformv1alpha1.SecretKeyRef `json:"userSecretRef,omitempty"`
	TokenSecretRef *aiplatformv1alpha1.SecretKeyRef `json:"tokenSecretRef,omitempty"`
	CredSecretRef  *aiplatformv1alpha1.SecretKeyRef `json:"credSecretRef,omitempty"`
	RepoURL        string                           `json:"repoURL,omitempty"`
	Branch         string                           `json:"branch,omitempty"`
}

type validateCredsRequest struct {
	Targets   []string                    `json:"targets,omitempty"`
	Overrides map[string]validateOverride `json:"overrides,omitempty"`
}

type validateResult struct {
	Target    string `json:"target"`
	Status    string `json:"status"`
	Host      string `json:"host,omitempty"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

type validateCredsResponse struct {
	Results []validateResult `json:"results"`
}

func (h *SettingsHandler) validateCredentials(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req validateCredsRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	var s aiplatformv1alpha1.Settings
	_ = h.client.Get(r.Context(), types.NamespacedName{Namespace: h.namespace, Name: settingsName}, &s)

	targets := req.Targets
	if len(targets) == 0 {
		targets = allValidateTargets
	}

	resp := validateCredsResponse{}
	for _, target := range targets {
		ov := req.Overrides[target]
		switch target {
		case "gitops":
			resp.Results = append(resp.Results, h.validateGit(r.Context(), &s, ov))
		case "applicationCollection", "suseRegistry", "nvidia":
			resp.Results = append(resp.Results, h.validateRegistry(r.Context(), target, &s, ov))
		default:
			resp.Results = append(resp.Results, validateResult{
				Target: target, Status: statusSkipped, Message: "unknown target",
			})
		}
	}
	writeJSON(w, http.StatusOK, &resp)
}

func (h *SettingsHandler) validateRegistry(ctx context.Context, target string, s *aiplatformv1alpha1.Settings, ov validateOverride) validateResult {
	res := validateResult{Target: target, Host: h.registryHost(target, s)}

	userRef, tokenRef := ov.UserSecretRef, ov.TokenSecretRef
	if userRef == nil && tokenRef == nil {
		userRef, tokenRef = savedRegistryRefs(target, s)
	}
	if userRef == nil || tokenRef == nil {
		res.Status = statusSkipped
		res.Message = "not configured"
		return res
	}

	user, err := h.readSecretKey(ctx, userRef)
	if err != nil {
		res.Status = statusFailed
		res.Message = err.Error()
		return res
	}
	pass, err := h.readSecretKey(ctx, tokenRef)
	if err != nil {
		res.Status = statusFailed
		res.Message = err.Error()
		return res
	}

	start := time.Now()
	probe := probeRegistryFn(ctx, res.Host, user, pass)
	res.LatencyMs = time.Since(start).Milliseconds()
	res.Status = string(probe.Status)
	res.Message = probe.Message
	return res
}

func (h *SettingsHandler) validateGit(ctx context.Context, s *aiplatformv1alpha1.Settings, ov validateOverride) validateResult {
	res := validateResult{Target: "gitops"}

	repoURL, branch, credRef := ov.RepoURL, ov.Branch, ov.CredSecretRef
	if repoURL == "" {
		repoURL = s.Spec.Fleet.RepoURL
		branch = s.Spec.Fleet.Branch
		credRef = s.Spec.Fleet.CredSecretRef
	}
	if repoURL == "" {
		res.Status = statusSkipped
		res.Message = "not configured"
		return res
	}

	tmp := &aiplatformv1alpha1.Settings{}
	tmp.Spec.Fleet.RepoURL = repoURL
	tmp.Spec.Fleet.Branch = branch
	tmp.Spec.Fleet.CredSecretRef = credRef

	gc, err := git.NewFromSettings(ctx, tmp, h.namespace, settingsSecretReader{h.client})
	if err != nil {
		res.Status = statusError
		res.Message = err.Error()
		return res
	}

	switch err := gitCheckAuthFn(gc, ctx); {
	case err == nil:
		res.Status = statusOK
		res.Message = "repository reachable"
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed):
		res.Status = statusFailed
		res.Message = err.Error()
	default:
		res.Status = statusError
		res.Message = err.Error()
	}
	return res
}

func savedRegistryRefs(target string, s *aiplatformv1alpha1.Settings) (*aiplatformv1alpha1.SecretKeyRef, *aiplatformv1alpha1.SecretKeyRef) {
	switch target {
	case "applicationCollection":
		return s.Spec.ApplicationCollection.UserSecretRef, s.Spec.ApplicationCollection.TokenSecretRef
	case "suseRegistry":
		return s.Spec.SUSERegistry.UserSecretRef, s.Spec.SUSERegistry.TokenSecretRef
	case "nvidia":
		return s.Spec.Nvidia.UserSecretRef, s.Spec.Nvidia.TokenSecretRef
	}
	return nil, nil
}

func (h *SettingsHandler) registryHost(target string, s *aiplatformv1alpha1.Settings) string {
	switch target {
	case "applicationCollection":
		if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.ApplicationCollection != "" {
			return registryurl.Host(s.Spec.RegistryEndpoints.ApplicationCollection)
		}
		return defaultAppCollectionHost
	case "suseRegistry":
		if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.SUSERegistry != "" {
			return registryurl.Host(s.Spec.RegistryEndpoints.SUSERegistry)
		}
		return defaultSUSERegistryHost
	case "nvidia":
		return defaultNvidiaHost
	}
	return ""
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd operator && GOTOOLCHAIN=auto go test ./internal/api/ -run TestValidateCredentials`
Expected: PASS (5 tests).

- [ ] **Step 7: Run the full operator suite + vet**

Run: `cd operator && GOTOOLCHAIN=auto go vet ./... && GOTOOLCHAIN=auto go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add operator/internal/api/settings.go operator/internal/api/settings_test.go
git commit -m "feat(operator): add validate-credentials endpoint"
```

---

## Task 4: UI operator-api client

**Files:**
- Modify: `ui/pkg/aif-ui/utils/operator-api.ts`

**Interfaces:**
- Consumes: existing `operatorFetch` from `./operator-config`.
- Produces: `validateCredentials(body: ValidateRequest, timeoutMs?): Promise<ValidateResponse>` and exported types `ValidateOverride`, `ValidateRequest`, `ValidateResult`, `ValidateResponse`.

- [ ] **Step 1: Add the client function and types**

Append to `ui/pkg/aif-ui/utils/operator-api.ts`:

```ts
export interface ValidateOverride {
  userSecretRef?:  { name: string; key: string } | null;
  tokenSecretRef?: { name: string; key: string } | null;
  credSecretRef?:  { name: string; key: string } | null;
  repoURL?:        string;
  branch?:         string;
}

export interface ValidateRequest {
  targets?:   string[];
  overrides?: Record<string, ValidateOverride>;
}

export interface ValidateResult {
  target:     string;
  status:     'ok' | 'failed' | 'error' | 'skipped';
  host?:      string;
  message:    string;
  latencyMs?: number;
}

export interface ValidateResponse {
  results: ValidateResult[];
}

export function validateCredentials(body: ValidateRequest, timeoutMs = 20000): Promise<ValidateResponse> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  return operatorFetch('/api/v1/settings/validate-credentials', {
    method: 'POST',
    body:   JSON.stringify(body),
    signal: controller.signal,
  }).finally(() => clearTimeout(timer));
}
```

- [ ] **Step 2: Verify it typechecks**

Run: `cd ui && yarn lint pkg/aif-ui/utils/operator-api.ts` (or the repo's configured lint/typecheck command — check `ui/package.json` scripts; if a `type-check` script exists prefer it).
Expected: no errors on the changed file.

- [ ] **Step 3: Commit**

```bash
git add ui/pkg/aif-ui/utils/operator-api.ts
git commit -m "feat(ui): add validateCredentials operator client"
```

---

## Task 5: Settings.vue per-section Test buttons

**Files:**
- Modify: `ui/pkg/aif-ui/pages/Settings.vue`
- Modify: `ui/pkg/aif-ui/l10n/en-us.json`

**Interfaces:**
- Consumes: `validateCredentials` + `ValidateResult` from Task 4.
- Produces: a `runTest(target, override, buttonDone)` method, a `testResults` data map keyed by target, a `testResultText(target)` helper, and Test buttons in the appCollection / suseRegistry / nvidia / fleet sections.

- [ ] **Step 1: Add i18n strings**

In `ui/pkg/aif-ui/l10n/en-us.json`, add a `test` object inside `suseai.pages.settings` (sibling of `title`, `apply`, `notConfigured`, `sections`):

```json
"test": {
  "button": "Test",
  "ok": "Authenticated",
  "failed": "Authentication failed",
  "error": "Could not reach endpoint",
  "skipped": "Not configured"
}
```

- [ ] **Step 2: Import the client**

In the `<script>` of `Settings.vue`, add to the imports near the other util imports:

```js
import { validateCredentials } from '../utils/operator-api';
```

- [ ] **Step 3: Add data + methods**

Add `testResults` to the object returned by `data()` (alongside `expanded`):

```js
      testResults: {
        applicationCollection: null,
        suseRegistry:          null,
        nvidia:                null,
        gitops:                null,
      },
```

Add these methods to the `methods` object:

```js
    async runTest(target, override, buttonDone) {
      try {
        const resp = await validateCredentials({ targets: [target], overrides: { [target]: override } });
        const res = (resp.results || []).find((r) => r.target === target) || null;
        this.testResults[target] = res;
        buttonDone(res?.status === 'ok');
      } catch (e) {
        this.testResults[target] = { target, status: 'error', message: e?.message || String(e) };
        buttonDone(false);
      }
    },

    testResultText(target) {
      const r = this.testResults[target];
      if (!r) return '';
      const label = this.t(`suseai.pages.settings.test.${ r.status }`);
      return r.message ? `${ label }: ${ r.message }` : label;
    },

    testResultClass(target) {
      const r = this.testResults[target];
      if (!r) return '';
      return r.status === 'ok' ? 'text-success' : (r.status === 'skipped' ? 'text-muted' : 'text-error');
    },
```

- [ ] **Step 4: Add the Test buttons to each section**

In each credential section's content `<div>` (the one guarded by `v-if="expanded.<section>"`), add the following block just before its closing `</div>`. **AppCo section** (`spec.applicationCollection`):

```html
          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                @click="cb => runTest('applicationCollection', { userSecretRef: spec.applicationCollection.userSecretRef, tokenSecretRef: spec.applicationCollection.tokenSecretRef }, cb)"
              />
              <span
                v-if="testResults.applicationCollection"
                :class="testResultClass('applicationCollection')"
                class="ml-10"
              >{{ testResultText('applicationCollection') }}</span>
            </div>
          </div>
```

**SUSE Registry section** (`spec.suseRegistry`):

```html
          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                @click="cb => runTest('suseRegistry', { userSecretRef: spec.suseRegistry.userSecretRef, tokenSecretRef: spec.suseRegistry.tokenSecretRef }, cb)"
              />
              <span
                v-if="testResults.suseRegistry"
                :class="testResultClass('suseRegistry')"
                class="ml-10"
              >{{ testResultText('suseRegistry') }}</span>
            </div>
          </div>
```

**NVIDIA section** (`spec.nvidia`):

```html
          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                @click="cb => runTest('nvidia', { userSecretRef: spec.nvidia.userSecretRef, tokenSecretRef: spec.nvidia.tokenSecretRef }, cb)"
              />
              <span
                v-if="testResults.nvidia"
                :class="testResultClass('nvidia')"
                class="ml-10"
              >{{ testResultText('nvidia') }}</span>
            </div>
          </div>
```

**Fleet / GitOps section** (`spec.fleet`):

```html
          <div class="row mt-10">
            <div class="col span-12">
              <AsyncButton
                mode="edit"
                :action-label="t('suseai.pages.settings.test.button')"
                @click="cb => runTest('gitops', { repoURL: spec.fleet.repoURL, branch: spec.fleet.branch, credSecretRef: spec.fleet.credSecretRef }, cb)"
              />
              <span
                v-if="testResults.gitops"
                :class="testResultClass('gitops')"
                class="ml-10"
              >{{ testResultText('gitops') }}</span>
            </div>
          </div>
```

- [ ] **Step 5: Verify build/lint**

Run: `cd ui && yarn lint pkg/aif-ui/pages/Settings.vue`
Expected: no errors on the changed file. (If the repo has a full build script such as `yarn build-pkg aif-ui`, run it to confirm the template compiles.)

- [ ] **Step 6: Manual verification**

Refer to the memory note "Live UI dev shell setup" for running the extension against a Rancher container. Open **Settings**, expand a section, click **Test**, and confirm: a configured-and-valid credential shows a green "Authenticated" with the host; a wrong credential shows a red "Authentication failed"; an unconfigured section shows a muted "Not configured". Confirm test-before-save works: change a secret selection without saving, click Test, and the new selection is what gets probed.

- [ ] **Step 7: Commit**

```bash
git add ui/pkg/aif-ui/pages/Settings.vue ui/pkg/aif-ui/l10n/en-us.json
git commit -m "feat(ui): add per-section credential Test buttons"
```

---

## Self-Review

**Spec coverage:**
- Endpoint `POST /api/v1/settings/validate-credentials` → Task 3. ✓
- Auth-probe depth (registry v2 handshake) → Task 1. ✓
- Git `ls-remote` check → Task 2, wired in Task 3. ✓
- Test-before-save via `overrides` with saved fallback → Task 3 (`validateRegistry`/`validateGit`), exercised by `TestValidateCredentials_OverrideRefsBeforeSave`. ✓
- Status enum `ok`/`failed`/`error`/`skipped` with failed-vs-error split → Tasks 1 & 3. ✓
- Host resolution reusing `getRegistryCredentials` rules (RegistryEndpoints for AppCo/SUSE, nvcr.io for NGC) → Task 3 `registryHost`. ✓
- Per-section Test button, inline result, no global button → Task 5. ✓
- Never returns/echoes secret values → messages only carry statuses/errors; no code path writes the password. ✓
- No new dependencies; `GOTOOLCHAIN=auto` → Global Constraints, used in every Go run step. ✓
- Air-gap NGC limitation → documented in the spec; the error-vs-failed split (Tasks 1/3) is the mitigation. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; no "similar to Task N" (all four UI button blocks written out). ✓

**Type consistency:** `credcheck.Result{Status, Message}` and `Status` consts (Task 1) are consumed verbatim in Task 3 (`probe.Status`, `probe.Message`). `(*git.Client).CheckAuth` signature (Task 2) matches the `gitCheckAuthFn` seam and its stub `func(*git.Client, context.Context) error` (Task 3). API `status` strings match the UI `ValidateResult.status` union (Tasks 3/4) and the i18n keys `test.ok/failed/error/skipped` (Task 5). Target ids `applicationCollection`/`suseRegistry`/`nvidia`/`gitops` are identical across handler, client, and Vue. ✓
