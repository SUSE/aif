package manager

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/SUSE/aif/internal/api"
)

// Register sets up HTTP routes on the provided mux and returns an http.Handler
// with CORS, request ID, logging, and metrics middleware applied to all routes.
// Additional route groups are registered via the handlers variadic parameter.
func Register(mux *http.ServeMux, logger *slog.Logger, allowedOrigin string, handlers ...api.Handler) http.Handler {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version": "0.1.0",
			"service": "aif-operator",
		})
	})

	for _, h := range handlers {
		h.Register(mux)
	}

	logger.Info("HTTP routes registered")

	chain := api.Chain(
		api.CORSMiddleware(allowedOrigin),
		api.RequestIDMiddleware(),
		api.LoggingMiddleware(logger),
		api.MetricsMiddleware(),
	)
	return http.HandlerFunc(chain(mux.ServeHTTP))
}
