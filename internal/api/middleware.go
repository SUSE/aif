package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

// Middleware is a function that wraps an http.HandlerFunc with additional behavior.
type Middleware func(http.HandlerFunc) http.HandlerFunc

// contextKey is an unexported type for context keys in this package.
type contextKey string

const (
	requestIDKey contextKey = "request_id"
	loggerKey    contextKey = "logger"
)

// RequestIDFromContext returns the request ID stored in the context, or "" if not set.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// ContextWithLogger returns a new context with the given logger stored.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext returns the logger stored in the context, or slog.Default() if not set.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	l, ok := ctx.Value(loggerKey).(*slog.Logger)
	if !ok || l == nil {
		return slog.Default()
	}
	return l
}

// Chain composes middlewares left-to-right: Chain(A, B, C) produces A(B(C(handler))).
// A is the outermost middleware.
func Chain(middlewares ...Middleware) Middleware {
	return func(handler http.HandlerFunc) http.HandlerFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			handler = middlewares[i](handler)
		}
		return handler
	}
}

// CORSMiddleware returns a middleware that sets CORS headers when allowedOrigin is non-empty.
// OPTIONS requests are always handled with 204 No Content without calling the next handler.
// When allowedOrigin is empty, no CORS headers are set but OPTIONS still returns 204.
func CORSMiddleware(allowedOrigin string) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if allowedOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Impersonate-User, Impersonate-Group")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next(w, r)
		}
	}
}

// RequestIDMiddleware returns a middleware that ensures each request has a unique ID.
// If the incoming request has an X-Request-ID header, it is reused. Otherwise, a new
// UUID v4 is generated. The request ID is stored in the context and set as a response header.
func RequestIDMiddleware() Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = uuid.New().String()
			}

			w.Header().Set("X-Request-ID", id)

			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next(w, r.WithContext(ctx))
		}
	}
}

// LoggingMiddleware returns a middleware that creates a child logger with component=api
// and request_id from context, stores it in context, and logs method, path, status, and
// duration_ms at Info level after the handler runs.
func LoggingMiddleware(logger *slog.Logger) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			reqID := RequestIDFromContext(r.Context())
			childLogger := logger.With(
				"component", "api",
				"request_id", reqID,
			)

			ctx := ContextWithLogger(r.Context(), childLogger)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			next(rec, r.WithContext(ctx))

			duration := time.Since(start)
			childLogger.Info("request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", duration.Milliseconds(),
			)
		}
	}
}

// Package-level variables for Prometheus metrics.
var (
	apiRequestDuration *prometheus.HistogramVec
	metricsOnce        sync.Once
)

// initMetrics registers the Prometheus histogram for API request durations.
// It is safe to call multiple times; registration happens only once via sync.Once.
func initMetrics() {
	metricsOnce.Do(func() {
		apiRequestDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "aif_api_request_duration_seconds",
				Help:    "Duration of API requests in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"path", "method", "status"},
		)
		prometheus.MustRegister(apiRequestDuration)
	})
}

// MetricsMiddleware returns a middleware that records request duration in a Prometheus
// histogram with labels for path, method, and status code.
func MetricsMiddleware() Middleware {
	initMetrics()
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			next(rec, r)

			duration := time.Since(start).Seconds()
			apiRequestDuration.WithLabelValues(
				r.URL.Path,
				r.Method,
				fmt.Sprintf("%d", rec.status),
			).Observe(duration)
		}
	}
}

// statusRecorder wraps http.ResponseWriter to capture the HTTP status code.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader captures the status code on the first call and delegates to the
// underlying ResponseWriter.
func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}
