package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCORSMiddleware_AllowedOrigin(t *testing.T) {
	handler := CORSMiddleware("https://rancher.local")(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bundles", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "https://rancher.local", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, PATCH, DELETE, OPTIONS", resp.Header.Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type, Authorization, Impersonate-User, Impersonate-Group", resp.Header.Get("Access-Control-Allow-Headers"))
}

func TestCORSMiddleware_NoOrigin(t *testing.T) {
	handler := CORSMiddleware("")(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bundles", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Methods"))
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Headers"))
}

func TestCORSMiddleware_WrongOrigin(t *testing.T) {
	handler := CORSMiddleware("https://rancher.local")(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bundles", nil)
	req.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// CORS headers are still set with the configured allowed origin (server-side; browser enforces).
	assert.Equal(t, "https://rancher.local", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	handlerCalled := false
	handler := CORSMiddleware("https://rancher.local")(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/bundles", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.False(t, handlerCalled, "handler should not be called on OPTIONS preflight")
	assert.Equal(t, "https://rancher.local", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST, PUT, PATCH, DELETE, OPTIONS", resp.Header.Get("Access-Control-Allow-Methods"))
}

func TestCORSMiddleware_Preflight_NoOrigin(t *testing.T) {
	handlerCalled := false
	handler := CORSMiddleware("")(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/bundles", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.False(t, handlerCalled, "handler should not be called on OPTIONS preflight even with empty origin")
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	handler := RequestIDMiddleware()(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	requestID := resp.Header.Get("X-Request-ID")
	assert.NotEmpty(t, requestID)
	assert.Len(t, requestID, 36, "UUID v4 should be 36 characters")
}

func TestRequestIDMiddleware_ReusesExisting(t *testing.T) {
	handler := RequestIDMiddleware()(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.Header.Set("X-Request-ID", "existing-trace-id-123")
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, "existing-trace-id-123", resp.Header.Get("X-Request-ID"))
}

func TestRequestIDMiddleware_InContext(t *testing.T) {
	var ctxRequestID string

	handler := RequestIDMiddleware()(func(w http.ResponseWriter, r *http.Request) {
		ctxRequestID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.NotEmpty(t, ctxRequestID)
	assert.Equal(t, resp.Header.Get("X-Request-ID"), ctxRequestID)
}

func TestLoggingMiddleware_LogsRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	handler := Chain(
		RequestIDMiddleware(),
		LoggingMiddleware(logger),
	)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "GET")
	assert.Contains(t, logOutput, "/api/v1/version")
	assert.Contains(t, logOutput, "200")
	assert.Contains(t, logOutput, "component=api")
}

func TestLoggingMiddleware_LoggerInContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	var ctxLogger *slog.Logger

	handler := Chain(
		RequestIDMiddleware(),
		LoggingMiddleware(logger),
	)(func(w http.ResponseWriter, r *http.Request) {
		ctxLogger = LoggerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	assert.NotNil(t, ctxLogger)
}

func TestMetricsMiddleware_RecordsHistogram(t *testing.T) {
	// Reset metrics state for this test.
	resetMetrics()
	initMetrics()

	handler := MetricsMiddleware()(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bundles", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	// Collect the histogram metric and verify it has 1 sample.
	ch := make(chan prometheus.Metric, 10)
	apiRequestDuration.Collect(ch)
	close(ch)

	var found bool
	for m := range ch {
		metric := &dto.Metric{}
		require.NoError(t, m.Write(metric))
		if metric.Histogram != nil && metric.Histogram.GetSampleCount() == 1 {
			found = true
			break
		}
	}
	assert.True(t, found, "expected histogram with 1 sample")
}

func TestChain_Order(t *testing.T) {
	var order []string

	makeMiddleware := func(name string) Middleware {
		return func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+"-before")
				next(w, r)
				order = append(order, name+"-after")
			}
		}
	}

	handler := Chain(
		makeMiddleware("A"),
		makeMiddleware("B"),
		makeMiddleware("C"),
	)(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	expected := []string{"A-before", "B-before", "C-before", "handler", "C-after", "B-after", "A-after"}
	assert.Equal(t, expected, order)
}

func TestLoggerFromContext_Default(t *testing.T) {
	logger := LoggerFromContext(context.Background())
	assert.NotNil(t, logger, "LoggerFromContext should return a non-nil logger for empty context")
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	id := RequestIDFromContext(context.Background())
	assert.Equal(t, "", id, "RequestIDFromContext should return empty string for empty context")
}

func TestStatusRecorder_DefaultStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	// Write body without explicit WriteHeader call.
	_, err := rec.Write([]byte("hello"))
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.status)
}

func TestStatusRecorder_ExplicitStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	rec.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, rec.status)
	assert.True(t, rec.wroteHeader)

	// Second WriteHeader should be ignored (in our recorder).
	rec.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusNotFound, rec.status, "status should not change after first WriteHeader")
}

// resetMetrics resets the package-level metrics state so tests are isolated.
func resetMetrics() {
	if apiRequestDuration != nil {
		prometheus.Unregister(apiRequestDuration)
	}
	apiRequestDuration = nil
	metricsOnce = sync.Once{}
}
