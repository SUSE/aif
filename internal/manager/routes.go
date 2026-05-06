package manager

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/SUSE/aif/internal/api"
)

// Register sets up HTTP routes on the provided mux. It applies CORS, request ID,
// logging, and metrics middleware to built-in endpoints (/healthz, /api/v1/version).
// Additional route groups are registered via the handlers variadic parameter.
func Register(mux *http.ServeMux, logger *slog.Logger, allowedOrigin string, handlers ...api.Handler) {
	chain := api.Chain(
		api.CORSMiddleware(allowedOrigin),
		api.RequestIDMiddleware(),
		api.LoggingMiddleware(logger),
		api.MetricsMiddleware(),
	)

	mux.HandleFunc("/healthz", chain(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/v1/version", chain(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version": "0.1.0",
			"service": "aif-operator",
		})
	}))

	for _, h := range handlers {
		h.Register(mux)
	}

	logger.Info("HTTP routes registered")
}
