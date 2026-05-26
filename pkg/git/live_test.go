//go:build live

package git_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SUSE/aif/pkg/git"
)

// TestLive_Push exercises the engine against a real remote. Skips
// cleanly when env vars are unset (so `make verify-git-live` in CI
// is a no-op without credentials).
//
// Required:
//
//	AIF_GIT_LIVE_REPO       — e.g. https://github.com/user/test-repo.git
//	AIF_GIT_LIVE_BRANCH     — e.g. "main"
//	AIF_GIT_LIVE_AUTH_MODE  — "token" | "basic" | "ssh"
//
// Mode-specific:
//
//	token: AIF_GIT_LIVE_TOKEN
//	basic: AIF_GIT_LIVE_USERNAME + AIF_GIT_LIVE_PASSWORD
//	ssh:   AIF_GIT_LIVE_SSH_KEY_PATH
func TestLive_Push(t *testing.T) {
	repo := os.Getenv("AIF_GIT_LIVE_REPO")
	if repo == "" {
		t.Skip("AIF_GIT_LIVE_REPO not set; skipping live test")
	}
	branch := os.Getenv("AIF_GIT_LIVE_BRANCH")
	if branch == "" {
		branch = "main"
	}

	auth, err := buildLiveAuth()
	if err != nil {
		t.Fatalf("auth setup: %v", err)
	}

	e := git.NewEngine(slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.UpdateSettings(git.EngineSettings{
		RepoURL: repo,
		Branch:  branch,
		Auth:    auth,
	})

	ts := time.Now().UTC().Format("20060102-150405")
	sub := "aif-live-test/" + ts
	req := git.PushRequest{
		Subtrees: []git.ManifestSubtree{{
			Path:  sub,
			Files: map[string][]byte{"hello.yaml": []byte("# aif live test " + ts + "\n")},
		}},
		CommitMessage: "aif: live test " + ts,
		AuthorName:    "AIF Live Test",
		AuthorEmail:   "aif-live-test@suse.com",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := e.Push(ctx, req)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.CommitSHA == "" {
		t.Fatal("expected commit on first live push; got NoOp")
	}
	t.Logf("live push committed: %s subtree=%s", res.CommitSHA, sub)
}

func buildLiveAuth() (git.GitAuth, error) {
	switch strings.ToLower(os.Getenv("AIF_GIT_LIVE_AUTH_MODE")) {
	case "token":
		return git.GitAuth{Token: &git.TokenAuth{Token: os.Getenv("AIF_GIT_LIVE_TOKEN")}}, nil
	case "basic":
		return git.GitAuth{Basic: &git.BasicAuth{
			Username: os.Getenv("AIF_GIT_LIVE_USERNAME"),
			Password: os.Getenv("AIF_GIT_LIVE_PASSWORD"),
		}}, nil
	case "ssh":
		path := os.Getenv("AIF_GIT_LIVE_SSH_KEY_PATH")
		key, err := os.ReadFile(path)
		if err != nil {
			return git.GitAuth{}, err
		}
		return git.GitAuth{SSH: &git.SSHAuth{PrivateKeyPEM: key}}, nil
	default:
		return git.GitAuth{}, nil // anonymous; only works for file:// remotes
	}
}
