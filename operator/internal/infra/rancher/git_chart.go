package rancher

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// GitChartFetcher reads an indexed chart directly from an administrator-managed
// ClusterRepo. It never reads Settings.rancherCatalog or calls Steve. Repository
// credentials are scoped to this fetch, including after credential rotation.
type GitChartFetcher struct {
	reader client.Reader
}

func NewGitChartFetcher(reader client.Reader) *GitChartFetcher {
	return &GitChartFetcher{reader: reader}
}

const (
	gitChartTimeout          = 3 * time.Minute
	maxGitIndexBytes         = 16 << 20
	maxGitChartBytes         = 4 << 20
	maxGitChartUnpackedBytes = 32 << 20
)

// FetchChart pins the read to the revision used by the reconciler's bundle
// fingerprint. A moving branch must never produce content from another revision.
func (f *GitChartFetcher) FetchChart(ctx context.Context, repoName, chartName, version, revision string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitChartTimeout)
	defer cancel()
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := f.reader.Get(ctx, client.ObjectKey{Name: repoName}, cr); err != nil {
		return nil, fmt.Errorf("read ClusterRepo %q: %w", repoName, err)
	}
	repository, _, _ := unstructured.NestedString(cr.Object, "spec", "gitRepo")
	if !plumbing.IsHash(revision) || chartName == "" || version == "" {
		return nil, fmt.Errorf("ClusterRepo %q must have an indexed Git commit and an exact chart name/version", repoName)
	}
	endpoint, err := transport.NewEndpoint(repository)
	if err != nil || (endpoint.Protocol != "https" && endpoint.Protocol != "ssh") {
		return nil, fmt.Errorf("ClusterRepo %q must use HTTPS or SSH Git", repoName)
	}
	// Credentials belong in clientSecret, never in a URL that may reach logs.
	if endpoint.Password != "" || (endpoint.Protocol == "https" && endpoint.User != "") {
		return nil, fmt.Errorf("ClusterRepo %q: use clientSecret instead of credentials in the Git URL", repoName)
	}
	dir, err := os.MkdirTemp("", "aif-git-chart-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	credentials, err := f.credentials(ctx, cr, endpoint, dir)
	if err != nil {
		return nil, err
	}
	r, err := gogit.PlainInit(path.Join(dir, "repository"), true)
	if err != nil {
		return nil, err
	}
	if _, err = r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{repository}}); err != nil {
		return nil, err
	}
	// A bare, shallow fetch avoids checking out arbitrary repository paths and
	// keeps packfiles on temporary disk instead of retaining a clone in memory.
	// No submodules or external asset URLs are followed.
	err = r.FetchContext(ctx, &gogit.FetchOptions{
		RemoteName: "origin", Depth: 1, Tags: gogit.NoTags,
		RefSpecs: []config.RefSpec{config.RefSpec("+" + revision + ":refs/aif/chart")},
		Auth:     credentials.auth, CABundle: credentials.caBundle, ClientCert: credentials.clientCert, ClientKey: credentials.clientKey,
	})
	if err == gogit.ErrExactSHA1NotSupported {
		// Some private servers only advertise branch fetches. Fetch that branch,
		// then still require the indexed commit below. If it moved meanwhile,
		// fail and let Rancher's index catch up rather than deploying new bytes
		// under the old fingerprint.
		branch, _, _ := unstructured.NestedString(cr.Object, "spec", "gitBranch")
		ref := plumbing.HEAD
		if branch != "" {
			ref = plumbing.NewBranchReferenceName(strings.TrimPrefix(branch, "refs/heads/"))
		}
		if err := ref.Validate(); err != nil {
			return nil, fmt.Errorf("ClusterRepo %q has an invalid Git branch", repoName)
		}
		err = r.FetchContext(ctx, &gogit.FetchOptions{
			RemoteName: "origin", Depth: 1, Tags: gogit.NoTags,
			RefSpecs: []config.RefSpec{config.RefSpec("+" + ref.String() + ":refs/aif/chart")},
			Auth:     credentials.auth, CABundle: credentials.caBundle, ClientCert: credentials.clientCert, ClientKey: credentials.clientKey,
		})
	}
	if err != nil && err != gogit.NoErrAlreadyUpToDate {
		// Transport errors may contain credential-bearing URLs supplied by a
		// remote. Keep these out of status/logs; identify the configured source.
		return nil, fmt.Errorf("fetch ClusterRepo %q at %s failed; check repository access, CA/SSH trust, connectivity, and whether the indexed commit is still available", repoName, revision)
	}
	commit, err := r.CommitObject(plumbing.NewHash(revision))
	if err != nil {
		return nil, fmt.Errorf("indexed commit unavailable in ClusterRepo %q", repoName)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	return chartFromGitTree(tree, chartName, version)
}

type gitCredentials struct {
	auth                            transport.AuthMethod
	caBundle, clientCert, clientKey []byte
}

func (f *GitChartFetcher) credentials(ctx context.Context, cr *unstructured.Unstructured, endpoint *transport.Endpoint, dir string) (gitCredentials, error) {
	name, _, _ := unstructured.NestedString(cr.Object, "spec", "clientSecret", "name")
	ns, _, _ := unstructured.NestedString(cr.Object, "spec", "clientSecret", "namespace")
	if ns == "" {
		ns = "cattle-system"
	}
	data := map[string][]byte{}
	if name != "" {
		var secret corev1.Secret
		if err := f.reader.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &secret); err != nil {
			return gitCredentials{}, fmt.Errorf("read Git credential Secret %s/%s: %w", ns, name, err)
		}
		data = secret.Data
	}
	encodedCA, _, _ := unstructured.NestedString(cr.Object, "spec", "caBundle")
	var ca []byte
	if encodedCA != "" {
		var err error
		ca, err = base64.StdEncoding.DecodeString(encodedCA)
		if err != nil {
			return gitCredentials{}, fmt.Errorf("ClusterRepo caBundle must be base64-encoded PEM")
		}
	} else {
		ca = data["cacerts"]
	}
	if len(ca) > 0 && !x509.NewCertPool().AppendCertsFromPEM(ca) {
		// Rancher also accepts base64 DER for Git ClusterRepos. Normalize it
		// for go-git's PEM trust bundle so existing private repositories work.
		certs, err := x509.ParseCertificates(ca)
		if err != nil {
			return gitCredentials{}, fmt.Errorf("Git CA bundle contains no valid PEM or DER certificates")
		}
		ca = nil
		for _, cert := range certs {
			ca = append(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
		}
	}
	if insecure, _, _ := unstructured.NestedBool(cr.Object, "spec", "insecureSkipTLSVerify"); insecure && endpoint.Protocol == "https" {
		return gitCredentials{}, fmt.Errorf("Git chart downloads require TLS verification; configure ClusterRepo caBundle")
	}
	if endpoint.Protocol == "ssh" {
		if len(data[corev1.SSHAuthPrivateKey]) == 0 || len(data["known_hosts"]) == 0 {
			return gitCredentials{}, fmt.Errorf("SSH Git clientSecret requires ssh-privatekey and known_hosts")
		}
		user := endpoint.User
		if user == "" {
			user = "git"
		}
		keys, err := gitssh.NewPublicKeys(user, data[corev1.SSHAuthPrivateKey], "")
		if err != nil {
			return gitCredentials{}, fmt.Errorf("invalid Git SSH private key")
		}
		knownFile := path.Join(dir, "known_hosts")
		if err := os.WriteFile(knownFile, data["known_hosts"], 0600); err != nil {
			return gitCredentials{}, err
		}
		keys.HostKeyCallback, err = knownhosts.New(knownFile)
		if err != nil {
			return gitCredentials{}, fmt.Errorf("invalid Git known_hosts")
		}
		return gitCredentials{auth: keys, caBundle: ca}, nil
	}
	if len(data[corev1.TLSCertKey]) > 0 || len(data[corev1.TLSPrivateKeyKey]) > 0 {
		if _, err := tls.X509KeyPair(data[corev1.TLSCertKey], data[corev1.TLSPrivateKeyKey]); err != nil {
			return gitCredentials{}, fmt.Errorf("invalid Git TLS client certificate/key")
		}
		return gitCredentials{caBundle: ca, clientCert: data[corev1.TLSCertKey], clientKey: data[corev1.TLSPrivateKeyKey]}, nil
	}
	if name == "" {
		return gitCredentials{caBundle: ca}, nil
	}
	if len(data[corev1.BasicAuthUsernameKey]) == 0 || len(data[corev1.BasicAuthPasswordKey]) == 0 {
		return gitCredentials{}, fmt.Errorf("HTTPS Git clientSecret requires username and password")
	}
	return gitCredentials{auth: &githttp.BasicAuth{Username: string(data[corev1.BasicAuthUsernameKey]), Password: string(data[corev1.BasicAuthPasswordKey])}, caBundle: ca}, nil
}

func gitTreeFile(tree *object.Tree, name string, limit int64) ([]byte, error) {
	entry, err := tree.FindEntry(name)
	if err != nil {
		return nil, err
	}
	if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable {
		return nil, fmt.Errorf("Git chart path %q must be a regular file", name)
	}
	f, err := tree.File(name)
	if err != nil {
		return nil, err
	}
	if f.Size > limit {
		return nil, fmt.Errorf("%w: file %q exceeds %d bytes", ErrChartTooLarge, name, limit)
	}
	r, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: file %q exceeds %d bytes", ErrChartTooLarge, name, limit)
	}
	return data, err
}

func chartFromGitTree(tree *object.Tree, name, version string) ([]byte, error) {
	index, metadata, err := gitChartIndex(tree)
	if err != nil {
		return nil, fmt.Errorf("read Git chart index: %w", err)
	}
	if index == nil {
		return gitDirectoryChart(tree, metadata, name, version)
	}
	for _, entry := range index.Entries[name] {
		if entry == nil || entry.Metadata == nil || entry.Version != version {
			continue
		}
		for _, address := range entry.URLs {
			// Rancher's Git catalog also uses file:// for repository-relative
			// paths. Resolve only inside this Git tree, never the local filesystem.
			u, err := url.Parse(strings.TrimPrefix(address, "file://"))
			if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
				continue
			}
			file := path.Clean(u.Path)
			if path.IsAbs(file) || file == ".." || strings.HasPrefix(file, "../") || strings.Contains(file, "\\") {
				continue
			}
			if file == "." {
				return packageGitChart(tree, file, name, version)
			}
			if source, err := tree.FindEntry(file); err == nil && source.Mode == filemode.Dir {
				return packageGitChart(tree, file, name, version)
			}
			archive, err := gitTreeFile(tree, file, maxGitChartBytes)
			if err != nil {
				return nil, fmt.Errorf("read chart archive: %w", err)
			}
			if entry.Digest != "" {
				digest := sha256.Sum256(archive)
				if !strings.EqualFold(entry.Digest, hex.EncodeToString(digest[:])) {
					return nil, fmt.Errorf("Git chart archive digest does not match index")
				}
			}
			if err := validateGitChart(archive, name, version); err != nil {
				return nil, err
			}
			return archive, nil
		}
		return nil, fmt.Errorf("chart %s@%s must have a repository-relative archive URL in index.yaml; mirror assets into the Git repository", name, version)
	}
	return nil, fmt.Errorf("chart %s@%s is absent from the indexed Git commit", name, version)
}

func validateGitChart(data []byte, name, version string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid Git chart archive: %w", err)
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxGitChartUnpackedBytes + 1}
	tr := tar.NewReader(limited)
	found := false
	for {
		h, err := tr.Next()
		if limited.N <= 0 {
			return fmt.Errorf("%w: unpacked size limit", ErrChartTooLarge)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid Git chart archive: %w", err)
		}
		p := path.Clean(h.Name)
		if path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "\\") ||
			(p != name && !strings.HasPrefix(p, name+"/")) {
			return fmt.Errorf("Git chart archive contains an unsafe path")
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("Git chart archive contains an unsupported file type")
		}
		if strings.Count(p, "/") == 1 && path.Base(p) == "Chart.yaml" && h.Typeflag == tar.TypeReg {
			if found || h.Size > 1<<20 {
				return fmt.Errorf("Git chart archive has duplicate or oversized Chart.yaml")
			}
			metadata, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return err
			}
			var chart struct{ Name, Version string }
			if err := yaml.Unmarshal(metadata, &chart); err != nil {
				return err
			}
			if chart.Name != name || chart.Version != version {
				return fmt.Errorf("Git chart metadata does not match requested name/version")
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("Git chart archive has no root Chart.yaml")
	}
	return nil
}
