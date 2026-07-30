package rancher

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckAuth_Classification(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		wantErr    bool
		wantUnauth bool
	}{
		{"ok", http.StatusOK, false, false},
		{"unauthorized", http.StatusUnauthorized, true, true},
		{"forbidden", http.StatusForbidden, true, true},
		{"server error", http.StatusInternalServerError, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(tc.statusCode)
			}))
			defer srv.Close()

			c, err := NewCatalogClient(srv.URL, "tok-123", nil, false)
			if err != nil {
				t.Fatalf("NewCatalogClient: %v", err)
			}
			err = c.CheckAuth(context.Background())
			if tc.wantErr != (err != nil) {
				t.Fatalf("CheckAuth err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantUnauth != errors.Is(err, ErrUnauthorized) {
				t.Fatalf("errors.Is(ErrUnauthorized)=%v want %v (err=%v)", errors.Is(err, ErrUnauthorized), tc.wantUnauth, err)
			}
			if gotPath != "/v1/catalog.cattle.io.clusterrepos" {
				t.Errorf("path = %q", gotPath)
			}
			if gotAuth != "Bearer tok-123" {
				t.Errorf("auth = %q", gotAuth)
			}
		})
	}
}

func TestFetchChart_BuildsRequestAndReturnsBody(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("chart-tgz-bytes"))
	}))
	defer srv.Close()

	c, err := NewCatalogClient(srv.URL, "tok-123", nil, false)
	if err != nil {
		t.Fatalf("NewCatalogClient: %v", err)
	}
	body, err := c.FetchChart(context.Background(), "rancher-charts", "rancher-ai-agent", "109.0.1")
	if err != nil {
		t.Fatalf("FetchChart: %v", err)
	}
	if string(body) != "chart-tgz-bytes" {
		t.Fatalf("body = %q", string(body))
	}
	if gotPath != "/v1/catalog.cattle.io.clusterrepos/rancher-charts" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("auth = %q", gotAuth)
	}
	// query must carry link=chart + chartName + version
	for _, want := range []string{"link=chart", "chartName=rancher-ai-agent", "version=109.0.1"} {
		if !contains(gotQuery, want) {
			t.Fatalf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestFetchChart_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c, err := NewCatalogClient(srv.URL, "", nil, false)
	if err != nil {
		t.Fatalf("NewCatalogClient: %v", err)
	}
	if _, err := c.FetchChart(context.Background(), "repo", "chart", "1.0.0"); err == nil {
		t.Fatal("expected error on non-200")
	}
}

// FetchChart is the path a git-backed component actually takes, so it — not just
// CheckAuth — has to produce ErrUnauthorized on a rejected token. The AIWorkload
// controller keys its RancherTokenRejected condition off errors.Is(ErrUnauthorized);
// without the sentinel here an expired token surfaced as the generic
// ComponentReconcileFailed, which tells the user nothing about re-authorizing.
// The controller-side test fakes the sentinel, so only this test covers the
// producer.
func TestFetchChart_Classification(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		wantUnauth bool
	}{
		{"unauthorized", http.StatusUnauthorized, true},
		{"forbidden", http.StatusForbidden, true},
		{"not found", http.StatusNotFound, false},
		{"server error", http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(`{"type":"error","status":"401","message":"Unauthorized 401: must authenticate"}`))
			}))
			defer srv.Close()

			c, err := NewCatalogClient(srv.URL, "tok-123", nil, false)
			if err != nil {
				t.Fatalf("NewCatalogClient: %v", err)
			}
			_, err = c.FetchChart(context.Background(), "rancher-charts", "rancher-backup-crd", "108.0.6+up9.0.6")
			if err == nil {
				t.Fatal("expected an error on non-200")
			}
			if got := errors.Is(err, ErrUnauthorized); got != tc.wantUnauth {
				t.Fatalf("errors.Is(ErrUnauthorized) = %v, want %v (err=%v)", got, tc.wantUnauth, err)
			}
			// The chart coordinates stay in the message either way — they are what
			// makes the condition actionable.
			for _, want := range []string{"rancher-backup-crd", "108.0.6+up9.0.6", "rancher-charts"} {
				if !contains(err.Error(), want) {
					t.Errorf("error %q dropped %q", err.Error(), want)
				}
			}
		})
	}
}

// An ingress or service mesh can answer with a large HTML error page. The body
// is only bounded by maxChartDownloadBytes (64 MiB), so quoting it whole put
// megabytes into a log line on every backoff tick — and, now that the generic
// component-failure path sets a condition, into an AIWorkload status condition
// the CRD caps at 32768 bytes.
func TestFetchChart_Non200ErrorBodyIsBounded(t *testing.T) {
	const bodySize = 4 << 20 // 4 MiB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("A", bodySize)))
	}))
	defer srv.Close()

	c, err := NewCatalogClient(srv.URL, "", nil, false)
	if err != nil {
		t.Fatalf("NewCatalogClient: %v", err)
	}
	_, err = c.FetchChart(context.Background(), "repo", "chart", "1.0.0")
	if err == nil {
		t.Fatal("expected error on non-200")
	}
	// Generous ceiling: the excerpt cap plus the surrounding message, nowhere
	// near the 4 MiB body.
	if len(err.Error()) > maxErrorBodyBytes+512 {
		t.Fatalf("error message is %d bytes; want it bounded near the %d-byte excerpt cap",
			len(err.Error()), maxErrorBodyBytes)
	}
	// The status must survive the truncation — it is the diagnostic that matters.
	if !contains(err.Error(), "502") {
		t.Errorf("error dropped the HTTP status: %q", err.Error())
	}
}

func TestNewCatalogClient_BadCA(t *testing.T) {
	if _, err := NewCatalogClient("https://rancher", "tok", []byte("not-a-pem"), false); err == nil {
		t.Fatal("expected error for invalid CA PEM")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
