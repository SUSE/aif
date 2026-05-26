package git

// EngineSettings is the EngineSettings push target for this package.
// Pushed by SettingsReconciler.applySettingsToEngines via engine_bus.
type EngineSettings struct {
	// RepoURL is the remote URL, e.g. "https://github.com/customer/gitops-fleet.git"
	// or "git@github.com:customer/gitops-fleet.git".
	RepoURL string

	// Branch is the target branch, e.g. "main".
	Branch string

	// Auth is the tagged union of supported auth modes. Exactly one
	// pointer is non-nil. When all are nil, the engine will fail with
	// ErrNotConfigured at Push time.
	Auth GitAuth
}

// GitAuth is a tagged union over the three supported go-git auth modes.
// Exactly one field is non-nil; the engine selects the corresponding
// go-git transport.AuthMethod at Push time.
type GitAuth struct {
	Token *TokenAuth // bearer / personal-access-token
	Basic *BasicAuth
	SSH   *SSHAuth
}

// TokenAuth carries a bearer / PAT token. go-git uses BasicAuth with
// username "token" (or "x-access-token" for GitHub-style providers).
type TokenAuth struct {
	Token string
}

type BasicAuth struct {
	Username string
	Password string
}

type SSHAuth struct {
	PrivateKeyPEM []byte
	User          string // defaults to "git" when empty
	// KnownHostsPEM is optional. When empty, ssh.InsecureIgnoreHostKey is
	// used; otherwise the parsed callback enforces the supplied set.
	// (Insecure default is documented; production deployments should
	// populate this.)
	KnownHostsPEM []byte
}

// PushRequest is what callers hand to Engine.Push.
type PushRequest struct {
	// Subtrees enumerate every directory the engine owns and must
	// rewrite in this push. The engine never touches files outside
	// these subtrees.
	Subtrees []ManifestSubtree

	// CommitMessage / AuthorName / AuthorEmail per ARCHITECTURE.md §6.7.
	CommitMessage string
	AuthorName    string
	AuthorEmail   string
}

// ManifestSubtree is one directory's worth of files to rewrite.
// Format: Path is relative to the repo root; Files is keyed by path
// relative to Path.
type ManifestSubtree struct {
	Path  string            // e.g. "gitops/cluster-a/workload-1"
	Files map[string][]byte // e.g. {"fleet.yaml": ..., "manifests/00-namespace.yaml": ...}
}

// PushResult is the outcome of Engine.Push. NoOp=true means the
// rendered tree was byte-identical to what was already on the remote
// branch; no commit was created and CommitSHA is "".
type PushResult struct {
	CommitSHA string
	NoOp      bool
}
