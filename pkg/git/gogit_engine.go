package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	httpauth "github.com/go-git/go-git/v5/plumbing/transport/http"
	sshauth "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	cryptossh "golang.org/x/crypto/ssh"
	sshknownhosts "golang.org/x/crypto/ssh/knownhosts"
)

// gogitEngine is the production Engine. Holds:
//   - settings (RepoURL/Branch/Auth) under an RWMutex (UpdateSettings is rare).
//   - mu, the per-engine serializing mutex: only one clone-write-commit-push
//     critical section at a time so concurrent Pushes can't race the remote
//     into a non-fast-forward state.
type gogitEngine struct {
	logger *slog.Logger

	mu sync.Mutex // serializes Push

	settingsMu sync.RWMutex
	settings   EngineSettings
}

// NewEngine constructs the production Engine.
func NewEngine(logger *slog.Logger) Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &gogitEngine{logger: logger}
}

func (e *gogitEngine) UpdateSettings(s EngineSettings) {
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	e.settings = s
}

func (e *gogitEngine) snapshot() EngineSettings {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.settings
}

func (e *gogitEngine) Push(ctx context.Context, req PushRequest) (PushResult, error) {
	s := e.snapshot()
	if s.RepoURL == "" {
		return PushResult{}, ErrNotConfigured
	}

	auth, err := buildAuthMethod(s.Auth)
	if err != nil {
		return PushResult{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	fs := memfs.New()
	storer := memory.NewStorage()

	branchRef := plumbing.NewBranchReferenceName(s.Branch)
	repo, err := gogit.CloneContext(ctx, storer, fs, &gogit.CloneOptions{
		URL:           s.RepoURL,
		Auth:          auth,
		ReferenceName: branchRef,
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		return PushResult{}, classifyTransport(err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return PushResult{}, fmt.Errorf("worktree: %w", err)
	}

	for _, st := range req.Subtrees {
		if err := removeAll(fs, st.Path); err != nil {
			return PushResult{}, fmt.Errorf("clear subtree %q: %w", st.Path, err)
		}
		for rel, content := range st.Files {
			full := path.Join(st.Path, rel)
			if err := writeFile(fs, full, content); err != nil {
				return PushResult{}, fmt.Errorf("write %q: %w", full, err)
			}
		}
	}

	if _, err := wt.Add("."); err != nil {
		return PushResult{}, fmt.Errorf("stage: %w", err)
	}

	status, err := wt.Status()
	if err != nil {
		return PushResult{}, fmt.Errorf("status: %w", err)
	}
	if status.IsClean() {
		return PushResult{NoOp: true}, nil
	}

	sig := &object.Signature{
		Name:  defaultIfEmpty(req.AuthorName, "AIF Operator"),
		Email: defaultIfEmpty(req.AuthorEmail, "aif-operator@suse.com"),
		When:  time.Now(),
	}
	commit, err := wt.Commit(req.CommitMessage, &gogit.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		return PushResult{}, fmt.Errorf("commit: %w", err)
	}

	if err := repo.PushContext(ctx, &gogit.PushOptions{
		Auth: auth,
		RefSpecs: []config.RefSpec{
			config.RefSpec(branchRef.String() + ":" + branchRef.String()),
		},
	}); err != nil {
		return PushResult{}, classifyTransport(err)
	}

	return PushResult{CommitSHA: commit.String()}, nil
}

func buildAuthMethod(a GitAuth) (transport.AuthMethod, error) {
	switch {
	case a.Token != nil:
		return &httpauth.BasicAuth{Username: "token", Password: a.Token.Token}, nil
	case a.Basic != nil:
		return &httpauth.BasicAuth{Username: a.Basic.Username, Password: a.Basic.Password}, nil
	case a.SSH != nil:
		user := a.SSH.User
		if user == "" {
			user = "git"
		}
		signer, err := cryptossh.ParsePrivateKey(a.SSH.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("%w: parse ssh key: %v", ErrAuth, err)
		}
		am := &sshauth.PublicKeys{User: user, Signer: signer}
		if len(a.SSH.KnownHostsData) == 0 {
			am.HostKeyCallback = cryptossh.InsecureIgnoreHostKey() //nolint:gosec // documented insecure default; production sets KnownHostsData
		} else {
			cb, err := parseKnownHostsCallback(a.SSH.KnownHostsData)
			if err != nil {
				return nil, fmt.Errorf("%w: parse known_hosts: %v", ErrAuth, err)
			}
			am.HostKeyCallback = cb
		}
		return am, nil
	}
	return nil, nil // anonymous (e.g., file:// or test transports)
}

// parseKnownHostsCallback materialises a HostKeyCallback from raw
// OpenSSH-format known_hosts bytes. The upstream knownhosts package
// only takes file paths, so we round-trip through a temp file — read
// eagerly during New(), then unlink immediately. The callback retains
// the parsed entries in memory; the file is no longer needed.
//
// Returns ErrAuth-friendly errors (the caller wraps with ErrAuth);
// returns an explicit error on empty/garbage input so the field can't
// be silently dropped.
func parseKnownHostsCallback(data []byte) (cryptossh.HostKeyCallback, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("empty known_hosts payload")
	}
	f, err := os.CreateTemp("", "aif-known-hosts-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	name := f.Name()
	// Best-effort unlink even if subsequent steps fail; the goroutine
	// owns the only reference to this path.
	defer func() { _ = os.Remove(name) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close temp file: %w", err)
	}
	cb, err := sshknownhosts.New(name)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts: %w", err)
	}
	return cb, nil
}

func classifyTransport(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, transport.ErrAuthenticationRequired),
		errors.Is(err, transport.ErrAuthorizationFailed),
		errors.Is(err, transport.ErrInvalidAuthMethod):
		return fmt.Errorf("%w: %v", ErrAuth, err)
	case errors.Is(err, transport.ErrEmptyRemoteRepository):
		return fmt.Errorf("%w: %v", ErrInvalidRef, err)
	case errors.Is(err, plumbing.ErrReferenceNotFound):
		return fmt.Errorf("%w: %v", ErrInvalidRef, err)
	case errors.Is(err, gogit.ErrNonFastForwardUpdate),
		errors.Is(err, gogit.ErrForceNeeded):
		return fmt.Errorf("%w: %v", ErrPushRejected, err)
	}
	var dnsErr *net.DNSError
	var urlErr *url.Error
	if errors.As(err, &dnsErr) || errors.As(err, &urlErr) {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return err
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// removeAll removes dir (recursively) from the in-memory billy filesystem.
// memfs has no RemoveAll, so walk it ourselves. Missing dir = nothing to do.
func removeAll(fs billy.Filesystem, dir string) error {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		full := path.Join(dir, e.Name())
		if e.IsDir() {
			if err := removeAll(fs, full); err != nil {
				return err
			}
			continue
		}
		if err := fs.Remove(full); err != nil {
			return err
		}
	}
	return fs.Remove(dir)
}

func writeFile(fs billy.Filesystem, full string, content []byte) error {
	f, err := fs.Create(full)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, bytes.NewReader(content)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
