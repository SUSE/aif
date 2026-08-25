package aiworkload

import "testing"

func TestGitManifestHashChangesWithContent(t *testing.T) {
	a := gitManifestHash("version: 1.0.0\nforceSyncGeneration: 0\n")
	b := gitManifestHash("version: 1.0.0\nforceSyncGeneration: 1\n")
	c := gitManifestHash("version: 1.0.0\nforceSyncGeneration: 0\n")
	if a == b {
		t.Fatal("hash must change when forceSyncGeneration changes")
	}
	if a != c {
		t.Fatal("hash must be stable for identical content")
	}
}
