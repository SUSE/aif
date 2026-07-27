package rancher

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
