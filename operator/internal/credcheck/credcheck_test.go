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
