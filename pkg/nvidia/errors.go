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

	// ErrNIMNotFound is returned by Discovery.Get when the requested NIM ID
	// is not in the cache. May indicate (a) a stale cache, (b) the model
	// has been removed from SUSE Registry, or (c) the caller used the
	// wrong ID. Distinguish via errors.Is, never via string-matching.
	ErrNIMNotFound = errors.New("nvidia: NIM not found in cache")

	// ErrChartNotFound indicates the chart's OCI manifest returned 404.
	ErrChartNotFound = errors.New("nvidia: chart not found")

	// ErrInvalidRequest is returned by Deployer.GenerateValues when a
	// required field on GenerateRequest is missing or invalid — currently
	// empty Entry.Chart, empty Entry.Version, or an Entry.Type that is not
	// TypeLLM/TypeVLM. P4-5 translates to HTTP 400.
	ErrInvalidRequest = errors.New("nvidia: invalid GenerateRequest")

	// ErrInvalidReplicas is returned by Deployer.GenerateValues when
	// GenerateRequest.Replicas is zero or negative.
	ErrInvalidReplicas = errors.New("nvidia: invalid replica count")

	// ErrInvalidGPUCount is returned by Deployer.GenerateValues when an explicit
	// GenerateRequest.GPUs value is zero or negative. NIMs are GPU-bound; zero
	// is misconfiguration, not a CPU fallback.
	ErrInvalidGPUCount = errors.New("nvidia: invalid GPU count")

	// ErrMissingGPUCount is returned by Deployer.GenerateValues when
	// GenerateRequest.GPUs is nil and Entry.DefaultGPUs is 0. Engineer must
	// specify; we won't guess.
	ErrMissingGPUCount = errors.New("nvidia: GPU count required")
)
