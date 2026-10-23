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

package git_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"

	igit "github.com/SUSE/aif-operator/internal/git"
)

// newReadDirTestRemote builds a bare git repo on disk (via t.TempDir(), same
// as client_test.go's newTestRemote) with the given files committed on branch
// "main", and returns a repo URL for it.
//
// It deliberately does NOT prefix the path with "file://" the way
// client_test.go's newTestRemote does. Building the URL as "file://" +
// remoteDir breaks on Windows: go-git's transport.NewEndpoint runs that
// string through url.Parse, which misreads the drive-letter colon in
// "file://C:\Users\...\001" as a port ("invalid port ... after host") --
// this is exactly the pre-existing failure this package has on this
// machine (TestWriteFile, TestCheckAuth_ReachableOK, etc. all fail with
// that error; verified independently before writing this test).
//
// Passing the bare OS path instead (no scheme) takes transport.NewEndpoint
// down its parseFile() branch, which this go-git version (v5.19.2) resolves
// via internal/url.MatchesScpLike's explicit Windows-drive-prefix check
// (hasDosDrivePrefix) rather than url.Parse, so it does not hit the bug.
// clone() requires Depth:1 shallow-clone support, which is only implemented
// by go-git's real (disk-backed, git-binary-shelling) file transport --
// go-git's pure in-memory/in-process transport
// (plumbing/transport/server.NewClient, the mechanism go-git's own tests
// use for an in-memory remote) explicitly rejects shallow requests with
// "shallow not supported" (plumbing/transport/server/server.go), so it
// cannot be used here without changing clone()'s Depth:1, which is
// out of scope. This harness was verified to pass on this Windows box.
func newReadDirTestRemote(t *testing.T, files map[string]string) string {
	t.Helper()

	remoteDir := t.TempDir()
	_, err := gogit.PlainInit(remoteDir, true)
	require.NoError(t, err)

	localDir := t.TempDir()
	local, err := gogit.PlainInit(localDir, false)
	require.NoError(t, err)
	_, err = local.CreateRemote(&gogitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteDir},
	})
	require.NoError(t, err)

	wt, err := local.Worktree()
	require.NoError(t, err)
	for p, content := range files {
		if dir := filepath.Dir(p); dir != "." {
			require.NoError(t, wt.Filesystem.MkdirAll(dir, 0o755))
		}
		f, err := wt.Filesystem.Create(p)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
		require.NoError(t, f.Close())
		_, err = wt.Add(p)
		require.NoError(t, err)
	}
	_, err = wt.Commit("seed", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()},
	})
	require.NoError(t, err)

	err = local.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gogitconfig.RefSpec{"refs/heads/master:refs/heads/main"},
	})
	require.NoError(t, err)

	return remoteDir
}

func TestReadDir_ReturnsFilesUnderPath(t *testing.T) {
	remote := newReadDirTestRemote(t, map[string]string{
		"overlay/kustomization.yaml": "resources:\n- deployment.yaml\n",
		"overlay/deployment.yaml":    "kind: Deployment\n",
		"other/ignored.yaml":         "kind: ConfigMap\n",
	})

	c := igit.NewForRepo(remote, "main", nil, nil)
	got, err := c.ReadDir(context.Background(), "overlay")
	require.NoError(t, err)

	require.Equal(t, map[string]string{
		"kustomization.yaml": "resources:\n- deployment.yaml\n",
		"deployment.yaml":    "kind: Deployment\n",
	}, got)
}

func TestReadDir_RejectsOversizeTree(t *testing.T) {
	big := strings.Repeat("a", (1<<20)+1)
	remote := newReadDirTestRemote(t, map[string]string{
		"overlay/big.yaml": big,
	})

	c := igit.NewForRepo(remote, "main", nil, nil)
	_, err := c.ReadDir(context.Background(), "overlay")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

// TestNewForRepo_DefaultsBranch proves the "" -> "main" default is real, not
// just that NewForRepo returns a non-nil Client. The seeded remote's only
// branch is "main"; clone() clones c.branch, so a successful ReadDir here
// only works if the empty branch was actually defaulted to "main". If the
// `if branch == "" { branch = "main" }` line in client.go were removed,
// clone() would try to clone branch "" and this test would fail.
func TestNewForRepo_DefaultsBranch(t *testing.T) {
	remote := newReadDirTestRemote(t, map[string]string{
		"overlay/kustomization.yaml": "resources:\n- deployment.yaml\n",
	})

	c := igit.NewForRepo(remote, "", nil, nil)
	got, err := c.ReadDir(context.Background(), "overlay")
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"kustomization.yaml": "resources:\n- deployment.yaml\n",
	}, got)
}
