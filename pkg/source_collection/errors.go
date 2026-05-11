package source_collection

import "errors"

// Sentinel errors for upstream API failure classification.
// P5-8 switches on these to decide fallback vs propagate.
// Consumers use errors.Is — never strings.Contains.

// ErrAuthFailed is returned on HTTP 401/403.
var ErrAuthFailed = errors.New("source_collection: authentication failed")

// ErrCatalogMalformed is returned on HTTP 200 with empty or invalid JSON.
var ErrCatalogMalformed = errors.New("source_collection: malformed catalog response")

// ErrUpstreamUnavailable is returned on HTTP 5xx or network errors.
var ErrUpstreamUnavailable = errors.New("source_collection: upstream unavailable")

// ErrVersionNotFound is returned when a requested chart version does not exist.
var ErrVersionNotFound = errors.New("source_collection: version not found")

// ErrNotConfigured is returned when APIURL has not been set via UpdateSettings.
var ErrNotConfigured = errors.New("source_collection: client not configured (APIURL is required)")

// ErrChartNotFound indicates the chart's OCI manifest returned 404.
var ErrChartNotFound = errors.New("source_collection: chart not found")
