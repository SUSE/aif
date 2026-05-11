// pkg/helm/errors.go
package helm

import "errors"

// ErrMissingImageRepository is returned by MergeValues when the merged
// values map has no image.repository key (or it is empty). Per §6.6
// "Required after merge".
var ErrMissingImageRepository = errors.New("helm: image.repository missing after merge")

// ErrReleaseNotFound is returned by Status (and surfaced by callers branching
// on errors.Is) when the requested Helm release does not exist. Wraps the
// Helm SDK's driver.ErrReleaseNotFound at the package boundary so consumers
// never grep error strings.
var ErrReleaseNotFound = errors.New("helm: release not found")

// ErrPullFailed is returned when a chart cannot be pulled from its OCI repo.
// Use errors.Is(err, ErrPullFailed) to branch; the wrapped error carries the
// underlying cause for logs.
var ErrPullFailed = errors.New("helm: chart pull failed")
