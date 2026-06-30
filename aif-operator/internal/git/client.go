package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// SecretReader reads a secret key value from a Kubernetes Secret.
type SecretReader interface {
	ReadSecretKey(ctx context.Context, namespace, name, key string) (string, error)
}

// Client performs in-memory git operations against a remote repository.
type Client struct {
	repoURL string
	branch  string
	auth    *gogithttp.BasicAuth
}

// NewFromSettings constructs a Client from the Fleet section of a Settings CR.
// namespace is where credential Secrets live.
func NewFromSettings(ctx context.Context, s *aiplatformv1alpha1.Settings, namespace string, reader SecretReader) (*Client, error) {
	if s.Spec.Fleet.RepoURL == "" {
		return nil, fmt.Errorf("fleet.repoURL not configured in Settings")
	}
	branch := s.Spec.Fleet.Branch
	if branch == "" {
		branch = "main"
	}
	var auth *gogithttp.BasicAuth
	if s.Spec.Fleet.CredSecretRef != nil {
		ref := s.Spec.Fleet.CredSecretRef
		password, err := reader.ReadSecretKey(ctx, namespace, ref.Name, ref.Key)
		if err != nil {
			return nil, fmt.Errorf("read git credential: %w", err)
		}
		auth = &gogithttp.BasicAuth{Username: "token", Password: password}
	}
	return &Client{repoURL: s.Spec.Fleet.RepoURL, branch: branch, auth: auth}, nil
}

// WriteFile clones the repo, writes content at path, commits with commitMsg, and pushes.
func (c *Client) WriteFile(ctx context.Context, path, content, commitMsg string) (string, error) {
	repo, wt, err := c.clone(ctx)
	if err != nil {
		return "", err
	}
	if unchanged, err := fileContentMatches(wt.Filesystem, path, content); err != nil {
		return "", err
	} else if unchanged {
		return "", nil
	}
	// Create intermediate directories if needed.
	dir := filepath.Dir(path)
	if dir != "." {
		if err := wt.Filesystem.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("mkdir: %w", err)
		}
	}
	f, err := wt.Filesystem.Create(path)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write([]byte(content)); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	if _, err := wt.Add(path); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	return c.commitAndPush(ctx, repo, wt, commitMsg)
}

func fileContentMatches(fs billy.Filesystem, path, expected string) (bool, error) {
	f, err := fs.Open(path)
	if err != nil {
		return false, nil
	}
	defer f.Close()

	current, err := io.ReadAll(f)
	if err != nil {
		return false, fmt.Errorf("read existing file: %w", err)
	}

	return string(current) == expected, nil
}

// DeleteFile clones the repo, removes path if it exists, commits, and pushes.
// Returns ("", nil) if the file does not exist.
func (c *Client) DeleteFile(ctx context.Context, path, commitMsg string) (string, error) {
	repo, wt, err := c.clone(ctx)
	if err != nil {
		return "", err
	}
	if _, err := wt.Filesystem.Stat(path); err != nil {
		return "", nil
	}
	if _, err := wt.Remove(path); err != nil {
		return "", fmt.Errorf("git rm: %w", err)
	}
	return c.commitAndPush(ctx, repo, wt, commitMsg)
}

func (c *Client) clone(ctx context.Context) (*gogit.Repository, *gogit.Worktree, error) {
	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), memfs.New(), &gogit.CloneOptions{
		URL:           c.repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(c.branch),
		SingleBranch:  true,
		Depth:         1,
		Auth:          c.auth,
	})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		// A freshly created GitOps repo has no commits yet, so go-git cannot
		// clone it. Initialize an in-memory repo on the target branch wired to
		// the remote; the first WriteFile commit then creates the branch on push.
		return c.initEmpty()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("clone: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, fmt.Errorf("worktree: %w", err)
	}
	return repo, wt, nil
}

// initEmpty builds an in-memory repository positioned on c.branch with the
// remote configured, for when the remote exists but has no commits.
func (c *Client) initEmpty() (*gogit.Repository, *gogit.Worktree, error) {
	repo, err := gogit.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return nil, nil, fmt.Errorf("init empty repo: %w", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: gogit.DefaultRemoteName,
		URLs: []string{c.repoURL},
	}); err != nil {
		return nil, nil, fmt.Errorf("create remote: %w", err)
	}
	// Point HEAD at the target branch so the initial commit lands there even
	// though it does not exist yet (orphan/initial commit).
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(c.branch))
	if err := repo.Storer.SetReference(headRef); err != nil {
		return nil, nil, fmt.Errorf("set HEAD to %s: %w", c.branch, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, fmt.Errorf("worktree: %w", err)
	}
	return repo, wt, nil
}

func (c *Client) commitAndPush(ctx context.Context, repo *gogit.Repository, wt *gogit.Worktree, msg string) (string, error) {
	hash, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "SUSE AI Factory",
			Email: "noreply@suse.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	// Push the branch explicitly so this also creates the branch on a
	// previously-empty remote (where there is no tracking config to default to).
	branchRef := plumbing.NewBranchReferenceName(c.branch)
	if err := repo.PushContext(ctx, &gogit.PushOptions{
		RemoteName: gogit.DefaultRemoteName,
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("%s:%s", branchRef, branchRef))},
		Auth:       c.auth,
	}); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return "", fmt.Errorf("push: %w", err)
	}
	return hash.String(), nil
}
