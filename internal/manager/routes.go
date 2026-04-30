package manager

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Register sets up HTTP routes on the provided mux.
func Register(mux *http.ServeMux, logger *slog.Logger, allowedOrigin string) {
	// CORS middleware
	corsHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if allowedOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next(w, r)
		}
	}

	// Health endpoint (separate from controller-runtime health)
	mux.HandleFunc("/healthz", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	// API version endpoint
	mux.HandleFunc("/api/v1/version", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"version": "0.1.0",
			"service": "aif-operator",
		})
	}))

	logger.Info("HTTP routes registered")
}
