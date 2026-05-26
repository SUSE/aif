package git

import "errors"

// Sentinel errors. Consumers MUST use errors.Is to classify; no
// strings.Contains on error messages.
var (
	// ErrNotConfigured means EngineSettings.RepoURL was empty at Push
	// time — the engine never received credentials/repo info from
	// SettingsReconciler. The consumer should surface this as a
	// configuration error, not a transient I/O failure.
	ErrNotConfigured = errors.New("git: engine not configured (RepoURL empty)")

	// ErrAuth wraps go-git authentication failures.
	ErrAuth = errors.New("git: authentication failed")

	// ErrPushRejected wraps a non-fast-forward / branch-protection
	// rejection from the remote.
	ErrPushRejected = errors.New("git: push rejected")

	// ErrUnreachable wraps DNS / network / TLS errors talking to the
	// remote.
	ErrUnreachable = errors.New("git: remote unreachable")

	// ErrInvalidRef wraps "branch does not exist" / "invalid ref" errors.
	ErrInvalidRef = errors.New("git: invalid ref")
)
