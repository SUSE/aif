package git

import (
	"errors"
	"strings"
	"testing"

	sshauth "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Regression coverage for the bug where SSHAuth.KnownHostsData was
// silently dropped: a populated KnownHostsData left HostKeyCallback nil,
// which made go-git fall back to ~/.ssh/known_hosts on the operator
// pod's filesystem. The fix wires parseKnownHostsCallback into
// buildAuthMethod and surfaces parse errors as ErrAuth.

// testPrivateKey is a non-secret ed25519 private key generated solely
// for unit testing the ssh.ParsePrivateKey -> buildAuthMethod happy
// path. It is not used to authenticate against any real host.
const testPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACCfDjAWIwy6nqbJlzqVtCgmIVXp6jJjab1yIauyMFaV+wAAAJg6oV/XOqFf
1wAAAAtzc2gtZWQyNTUxOQAAACCfDjAWIwy6nqbJlzqVtCgmIVXp6jJjab1yIauyMFaV+w
AAAEDwR3mDLpSjJq+22MndzGmPeRkLSke39FFTEM1nThv0xp8OMBYjDLqepsmXOpW0KCYh
VenqMmNpvXIhq7IwVpX7AAAAEGFpZi10ZXN0QGV4YW1wbGUBAgMEBQ==
-----END OPENSSH PRIVATE KEY-----
`

func TestBuildAuthMethod_SSH_KnownHostsPopulated_ParsesCallback(t *testing.T) {
	// One real-looking known_hosts line. Using ssh-ed25519 because the
	// key shape is fixed-length and unambiguous; the actual key bytes
	// don't matter — the parser only validates structure.
	knownHosts := []byte("github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n")
	am, err := buildAuthMethod(GitAuth{SSH: &SSHAuth{
		PrivateKeyPEM: []byte(testPrivateKey),
		KnownHostsData: knownHosts,
	}})
	if err != nil {
		t.Fatalf("buildAuthMethod: %v", err)
	}
	pk, ok := am.(*sshauth.PublicKeys)
	if !ok {
		t.Fatalf("want *sshauth.PublicKeys, got %T", am)
	}
	if pk.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback is nil — KnownHostsData was silently dropped (regression of the P4-3 review #1 bug)")
	}
}

func TestBuildAuthMethod_SSH_KnownHostsGarbage_ReturnsErrAuth(t *testing.T) {
	_, err := buildAuthMethod(GitAuth{SSH: &SSHAuth{
		PrivateKeyPEM: []byte(testPrivateKey),
		KnownHostsData: []byte("this is not a known_hosts line\n"),
	}})
	if err == nil {
		t.Fatal("want error for malformed known_hosts, got nil")
	}
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("want errors.Is(err, ErrAuth), got %v", err)
	}
}

func TestBuildAuthMethod_SSH_KnownHostsEmpty_UsesInsecureCallback(t *testing.T) {
	am, err := buildAuthMethod(GitAuth{SSH: &SSHAuth{
		PrivateKeyPEM: []byte(testPrivateKey),
		// KnownHostsData left nil — documented insecure default.
	}})
	if err != nil {
		t.Fatalf("buildAuthMethod: %v", err)
	}
	pk, ok := am.(*sshauth.PublicKeys)
	if !ok {
		t.Fatalf("want *sshauth.PublicKeys, got %T", am)
	}
	if pk.HostKeyCallback == nil {
		t.Fatal("empty KnownHostsData should yield InsecureIgnoreHostKey callback, not nil")
	}
}

func TestParseKnownHostsCallback_EmptyData(t *testing.T) {
	_, err := parseKnownHostsCallback(nil)
	if err == nil {
		t.Fatal("want error for nil data, got nil")
	}
	_, err = parseKnownHostsCallback([]byte("   \n\t\n"))
	if err == nil {
		t.Fatal("want error for whitespace-only data, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("want error mentioning 'empty', got %v", err)
	}
}
