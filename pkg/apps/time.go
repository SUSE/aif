package apps

import (
	"log/slog"
	"time"
)

// parseTimePtr parses an RFC 3339 timestamp string into *time.Time.
// Returns nil for empty input. Logs a warning and returns nil for
// malformed input so an upstream schema change doesn't go unnoticed.
func parseTimePtr(logger *slog.Logger, s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		if logger != nil {
			logger.Warn("unparseable timestamp; treating as nil", "value", s, "error", err)
		}
		return nil
	}
	return &t
}
