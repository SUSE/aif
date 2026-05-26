package git_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SUSE/aif/pkg/git"
)

func TestFakeEngine_RecordsPush(t *testing.T) {
	f := &git.FakeEngine{PushResult: git.PushResult{CommitSHA: "abc"}}
	req := git.PushRequest{CommitMessage: "test"}
	res, err := f.Push(context.Background(), req)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.CommitSHA != "abc" {
		t.Fatalf("got SHA %q, want abc", res.CommitSHA)
	}
	if len(f.Pushes) != 1 || f.Pushes[0].CommitMessage != "test" {
		t.Fatalf("Pushes not recorded: %+v", f.Pushes)
	}
}

func TestFakeEngine_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	f := &git.FakeEngine{PushErr: sentinel}
	_, err := f.Push(context.Background(), git.PushRequest{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel; got %v", err)
	}
}

func TestFakeEngine_RecordsSettings(t *testing.T) {
	f := &git.FakeEngine{}
	f.UpdateSettings(git.EngineSettings{RepoURL: "https://x.test/r.git"})
	if f.Settings.RepoURL != "https://x.test/r.git" {
		t.Fatalf("settings not recorded: %+v", f.Settings)
	}
}
