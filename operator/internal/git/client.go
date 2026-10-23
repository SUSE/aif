/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package git

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
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
	"github.com/SUSE/aif-operator/internal/credentials"
)

// SecretReader reads the data stored in a Kubernetes Secret.
type SecretReader interface {
	ReadSecret(ctx context.Context, namespace, name string) (map[string][]byte, error)
}

// Client performs in-memory git operations against a remote repository.
type Client struct {
	repoURL  string
	branch   string
	auth     *gogithttp.BasicAuth
	caBundle []byte
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
		switch s.Spec.Fleet.AuthType {
		case "", "token", "basic":
			// token and basic are deprecated compatibility aliases. Git HTTPS
			// credentials use HTTP Basic in every case.
		default:
			return nil, fmt.Errorf("unsupported fleet.authType %q; HTTPS Git credentials use username plus password or personal access token", s.Spec.Fleet.AuthType)
		}
		secretData, err := reader.ReadSecret(ctx, namespace, ref.Name)
		if err != nil {
			return nil, fmt.Errorf("read git credential Secret %s: %w", ref.Name, err)
		}
		password, found := secretData[ref.Key]
		if !found {
			return nil, fmt.Errorf("git credential Secret %s does not contain key %q", ref.Name, ref.Key)
		}
		if len(password) == 0 {
			return nil, fmt.Errorf("git credential must not be empty")
		}
		username := credentials.ResolveGitHTTPSUsername(s.Spec.Fleet.Username, secretData)
		auth = &gogithttp.BasicAuth{Username: username, Password: string(password)}
	} else if s.Spec.Fleet.AuthType != "" || s.Spec.Fleet.Username != "" {
		return nil, fmt.Errorf("fleet.authType or fleet.username requires fleet.credSecretRef")
	}
	var caBundle []byte
	if s.Spec.Fleet.CABundleSecretRef != nil {
		ref := s.Spec.Fleet.CABundleSecretRef
		secretData, err := reader.ReadSecret(ctx, namespace, ref.Name)
		if err != nil {
			return nil, fmt.Errorf("read git CA bundle: %w", err)
		}
		value, found := secretData[ref.Key]
		if !found {
			return nil, fmt.Errorf("git CA Secret %s does not contain key %q", ref.Name, ref.Key)
		}
		caBundle = value
		pool := x509.NewCertPool()
		if len(caBundle) == 0 || !pool.AppendCertsFromPEM(caBundle) {
			return nil, fmt.Errorf("read git CA bundle: secret %s key %s does not contain a PEM certificate", ref.Name, ref.Key)
		}
	}
	return &Client{repoURL: s.Spec.Fleet.RepoURL, branch: branch, auth: auth, caBundle: caBundle}, nil
}

// NewForRepo constructs a Client for an arbitrary git-backed repository,
// independent of the Settings GitOps repo. branch defaults to "main".
func NewForRepo(repoURL, branch string, auth *gogithttp.BasicAuth, caBundle []byte) *Client {
	if branch == "" {
		branch = "main"
	}
	return &Client{repoURL: repoURL, branch: branch, auth: auth, caBundle: caBundle}
}

// CheckAuth verifies the configured repository is reachable and the credentials
// authenticate, without cloning or writing. It is the equivalent of
// `git ls-remote`. An empty remote (no commits yet) counts as reachable.
func (c *Client) CheckAuth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	remote := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: gogit.DefaultRemoteName,
		URLs: []string{c.repoURL},
	})
	_, err := remote.ListContext(ctx, &gogit.ListOptions{Auth: c.auth, CABundle: c.caBundle})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return nil
	}
	return err
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

// ReadDir clones the repo (in-memory, shallow) and returns the regular files
// under dirPath, keyed by their path relative to dirPath. Total content is
// capped at 1 MiB to match the Fleet bundle payload limit.
func (c *Client) ReadDir(ctx context.Context, dirPath string) (map[string]string, error) {
	_, wt, err := c.clone(ctx)
	if err != nil {
		return nil, err
	}
	const capBytes = 1 << 20
	out := map[string]string{}
	var total int64
	if _, err := wt.Filesystem.ReadDir(dirPath); err != nil {
		return nil, fmt.Errorf("read dir %q: %w", dirPath, err)
	}
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := wt.Filesystem.ReadDir(filepath.Join(dirPath, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			r := filepath.ToSlash(filepath.Join(rel, e.Name()))
			// Guard against path traversal in overlay entries, mirroring
			// aiworkload/gitchart.go's rejectUnsafePath. Duplicated here
			// (rather than imported) to avoid a git -> aiworkload import
			// cycle; kept intentionally tiny.
			if r == ".." || strings.HasPrefix(r, "../") || path.IsAbs(r) {
				return fmt.Errorf("path %q escapes the overlay root", r)
			}
			// Skip symlinks: embedding a symlink as its target's content is
			// surprising, and a link out of the repo must never be followed.
			if e.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if e.IsDir() {
				if err := walk(r); err != nil {
					return err
				}
				continue
			}
			f, err := wt.Filesystem.Open(filepath.Join(dirPath, r))
			if err != nil {
				return err
			}
			b, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				return err
			}
			total += int64(len(b))
			if total > capBytes {
				return fmt.Errorf("overlay under %q exceeds the %d-byte limit", dirPath, capBytes)
			}
			out[r] = string(b)
		}
		return nil
	}
	if err := walk("."); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) clone(ctx context.Context) (*gogit.Repository, *gogit.Worktree, error) {
	repo, err := gogit.CloneContext(ctx, memory.NewStorage(), memfs.New(), &gogit.CloneOptions{
		URL:           c.repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(c.branch),
		SingleBranch:  true,
		Depth:         1,
		Auth:          c.auth,
		CABundle:      c.caBundle,
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
		CABundle:   c.caBundle,
	}); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return "", fmt.Errorf("push: %w", err)
	}
	return hash.String(), nil
}
