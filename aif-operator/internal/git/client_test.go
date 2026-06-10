package git_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	igit "github.com/SUSE/aif-operator/internal/git"
)

// fakeReader satisfies git.SecretReader without touching Kubernetes.
type fakeReader struct{}

func (fakeReader) ReadSecretKey(_ context.Context, _, _, _ string) (string, error) {
	return "token", nil
}

// newTestRemote creates a bare git repo with one initial commit on branch "main".
// Returns the file:// URL of the bare repo.
func newTestRemote(t *testing.T) string {
	t.Helper()

	remoteDir := t.TempDir()
	_, err := gogit.PlainInit(remoteDir, true)
	require.NoError(t, err)

	// Seed with an initial commit via a temporary local clone.
	localDir := t.TempDir()
	local, err := gogit.PlainInit(localDir, false)
	require.NoError(t, err)

	_, err = local.CreateRemote(&gogitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"file://" + remoteDir},
	})
	require.NoError(t, err)

	wt, err := local.Worktree()
	require.NoError(t, err)

	f, err := wt.Filesystem.Create("README.md")
	require.NoError(t, err)
	_, _ = f.Write([]byte("init"))
	f.Close()

	_, err = wt.Add("README.md")
	require.NoError(t, err)

	_, err = wt.Commit("init", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()},
	})
	require.NoError(t, err)

	err = local.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gogitconfig.RefSpec{"refs/heads/master:refs/heads/main"},
	})
	require.NoError(t, err)

	return "file://" + remoteDir
}

// readFileFromRemote clones the remote and reads filePath. Returns error if file not found.
func readFileFromRemote(t *testing.T, remoteURL, filePath string) (string, error) {
	t.Helper()
	cloneDir := t.TempDir()
	repo, err := gogit.PlainClone(cloneDir, false, &gogit.CloneOptions{
		URL:           remoteURL,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	require.NoError(t, err)
	fh, err := wt.Filesystem.Open(filePath)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	data, err := io.ReadAll(fh)
	require.NoError(t, err)
	return string(data), nil
}

func newClient(t *testing.T, repoURL string) *igit.Client {
	t.Helper()
	s := &aiplatformv1alpha1.Settings{}
	s.Spec.Fleet.RepoURL = repoURL
	s.Spec.Fleet.Branch = "main"
	c, err := igit.NewFromSettings(context.Background(), s, "ns", fakeReader{})
	require.NoError(t, err)
	return c
}

func TestWriteFile(t *testing.T) {
	remote := newTestRemote(t)
	c := newClient(t, remote)

	hash, err := c.WriteFile(context.Background(),
		filepath.Join("workloads", "test.yaml"),
		"content: true\n",
		"chore: add test")
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	got, err := readFileFromRemote(t, remote, filepath.Join("workloads", "test.yaml"))
	require.NoError(t, err)
	require.Equal(t, "content: true\n", got)
}

func TestWriteFile_UnchangedContentIsNoOp(t *testing.T) {
	remote := newTestRemote(t)
	c := newClient(t, remote)

	firstHash, err := c.WriteFile(context.Background(),
		filepath.Join("workloads", "test.yaml"),
		"content: true\n",
		"chore: add test")
	require.NoError(t, err)
	require.NotEmpty(t, firstHash)

	secondHash, err := c.WriteFile(context.Background(),
		filepath.Join("workloads", "test.yaml"),
		"content: true\n",
		"chore: add test")
	require.NoError(t, err)
	require.Empty(t, secondHash)

	got, err := readFileFromRemote(t, remote, filepath.Join("workloads", "test.yaml"))
	require.NoError(t, err)
	require.Equal(t, "content: true\n", got)
}

func TestDeleteFile_Exists(t *testing.T) {
	remote := newTestRemote(t)
	c := newClient(t, remote)

	_, err := c.WriteFile(context.Background(), "workloads/del.yaml", "x: 1\n", "add")
	require.NoError(t, err)

	hash, err := c.DeleteFile(context.Background(), "workloads/del.yaml", "remove")
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	_, err = readFileFromRemote(t, remote, "workloads/del.yaml")
	require.Error(t, err) // file gone
}

func TestDeleteFile_NotExists(t *testing.T) {
	remote := newTestRemote(t)
	c := newClient(t, remote)

	hash, err := c.DeleteFile(context.Background(), "workloads/missing.yaml", "remove")
	require.NoError(t, err)
	require.Empty(t, hash) // no-op — file never existed
}

func TestNewFromSettings_NoRepoURL(t *testing.T) {
	s := &aiplatformv1alpha1.Settings{}
	_, err := igit.NewFromSettings(context.Background(), s, "ns", fakeReader{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "repoURL")
}
