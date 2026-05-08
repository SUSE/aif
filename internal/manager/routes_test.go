package manager

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SUSE/aif/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testHandler struct {
	registered bool
}

func (h *testHandler) Register(mux *http.ServeMux) {
	h.registered = true
	mux.HandleFunc("GET /api/v1/test-resource", func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, http.StatusOK, map[string]string{"resource": "test"})
	})
}

func TestRoutes_MiddlewareStack(t *testing.T) {
	mux := http.NewServeMux()
	logger := slog.Default()
	handler := Register(mux, logger, "https://rancher.example.com")

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/version")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("X-Request-ID"))
	assert.Equal(t, "https://rancher.example.com", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestRoutes_HealthEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	logger := slog.Default()
	handler := Register(mux, logger, "https://rancher.example.com")

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("X-Request-ID"))

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestRoutes_VersionEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	logger := slog.Default()
	handler := Register(mux, logger, "")

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/version")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "0.1.0", body["version"])
	assert.Equal(t, "aif-operator", body["service"])
}

func TestRoutes_HandlerRegistration(t *testing.T) {
	mux := http.NewServeMux()
	logger := slog.Default()
	h := &testHandler{}
	handler := Register(mux, logger, "", h)

	assert.True(t, h.registered)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/test-resource")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
