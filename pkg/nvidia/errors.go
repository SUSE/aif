package nvidia

import "errors"

// Sentinel errors. Callers classify failures with errors.Is — never with
// strings.Contains on the error message (that pattern is on CLAUDE.md's
// Forbidden list).
var (
	// ErrUnreachable is returned when the SUSE Registry endpoint cannot be
	// reached (DNS failure, connection refused, TLS error, timeout).
	ErrUnreachable = errors.New("nvidia: registry unreachable")

	// ErrUnauthorized is returned for HTTP 401 / 403 responses from the
	// registry. Indicates a credentials problem; the caller should surface
	// this as a Settings condition, not retry blindly.
	ErrUnauthorized = errors.New("nvidia: registry unauthorized")

	// ErrUnexpectedResponse is returned for non-2xx, non-401/403 responses
	// from the registry, or for malformed response bodies. Wraps the
	// underlying status / parse error via fmt.Errorf %w.
	ErrUnexpectedResponse = errors.New("nvidia: unexpected registry response")

	// ErrNotConfigured is returned by Discovery methods when UpdateSettings
	// has not yet been called with a non-empty RegistryEndpoint. Indicates
	// the caller is invoking the discovery before settings have been
	// reconciled.
	ErrNotConfigured = errors.New("nvidia: discovery not configured (call UpdateSettings first)")
)
