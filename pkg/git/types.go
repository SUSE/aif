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
	// pointer is non-nil for an authenticated push. When all are nil,
	// the engine clones and pushes anonymously — appropriate for
	// `file://` transports (used in tests) and public HTTPS repos
	// against servers that allow unauthenticated push. ErrNotConfigured
	// is raised only when RepoURL itself is empty, not when Auth is
	// zero.
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

// TokenAuth carries a bearer / PAT token. The engine wraps it as
// HTTP BasicAuth with the literal username "token", which works for
// Gitea / Gerrit / generic OAuth-style endpoints.
//
// GitHub Personal Access Tokens are NOT supported in P4-3: GitHub
// requires the username "x-access-token" for PAT-as-password auth,
// and TokenAuth has no field to override that. Workaround for P4-3
// users: configure BasicAuth{Username: "x-access-token", Password:
// <pat>} instead. Promoting username to a TokenAuth field is tracked
// for a later phase.
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
	// KnownHostsData holds OpenSSH-format `known_hosts` data — one entry
	// per line (`hostname[,hostname...] keytype base64key [comment]`),
	// the same shape `ssh-keyscan` emits and `~/.ssh/known_hosts`
	// stores. NOT PEM-encoded despite the legacy name in the initial
	// draft (renamed during P4-3 review).
	//
	// When empty, ssh.InsecureIgnoreHostKey is used. When populated,
	// the engine parses it via golang.org/x/crypto/ssh/knownhosts and
	// the resulting callback enforces the supplied set; a malformed
	// payload surfaces as ErrAuth at Push time. Production deployments
	// should populate this.
	KnownHostsData []byte
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
