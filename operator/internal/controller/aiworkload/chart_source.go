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

package aiworkload

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"net/http"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/catalog"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
)

// maxChartIndexBytes caps a repository's index.yaml. Decoding it takes about
// 15x its size in heap, so a larger cap risks OOM-killing the operator.
const maxChartIndexBytes = 2 << 20

// chartArchivePuller downloads one chart version's archive straight from an
// http/oci repository. A field on the reconciler so tests can substitute it.
type chartArchivePuller func(ctx context.Context, req infrahelm.ChartArchiveRequest) ([]byte, error)

// chartPuller returns the configured puller, defaulting to the Helm SDK pull
// (a hand-built reconciler in a test may leave the field unset).
func (r *AIWorkloadReconciler) chartPuller() chartArchivePuller {
	if r.ChartPuller != nil {
		return r.ChartPuller
	}
	return infrahelm.PullChartArchive
}

// fetchChartArchive returns the chart .tgz for a component that is rendered as
// an embedded-chart Bundle. Git-backed repos go through the Rancher catalog (the
// only way to reach them, needing the Rancher API token); http/oci repos are
// downloaded directly with the ClusterRepo's own credentials and TLS settings,
// the same ones the HelmOp path hands to Fleet.
func (r *AIWorkloadReconciler) fetchChartArchive(ctx context.Context, c aiplatformv1alpha1.BlueprintComponent, repoInfo clusterRepoInfo) ([]byte, error) {
	if repoInfo.Kind == repoKindGit {
		if r.CatalogClient == nil || r.CatalogClient.Get() == nil {
			return nil, fmt.Errorf("%w: git-backed ClusterRepo %q needs a Rancher API token", errCatalogClientNotConfigured, c.ChartRepo)
		}
		return fetchGitChart(ctx, r.CatalogClient.Get(), c)
	}

	req := infrahelm.ChartArchiveRequest{
		RepoURL:       repoInfo.URL,
		ChartName:     c.ChartName,
		Version:       c.ChartVersion,
		PlainHTTP:     repoInfo.InsecurePlainHTTP,
		MaxBytes:      maxChartArchiveBytes,
		MaxIndexBytes: maxChartIndexBytes,
	}
	if repoInfo.Kind == repoKindOCI {
		req.RepoURL = ociChartRef(repoInfo.URL, c.ChartName)
	}
	var secretCA []byte
	if repoInfo.ClientSecret != "" {
		user, pass, ca, err := r.readRepoAuthSecret(ctx, repoInfo.ClientSecretNS, repoInfo.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("read clientSecret %q for repo %q: %w", repoInfo.ClientSecret, c.ChartRepo, err)
		}
		if pass != "" {
			req.Auth = &infrahelm.RegistryAuth{Username: user, Password: pass}
		}
		secretCA = ca
	}
	tlsCfg, err := clusterRepoTLSConfig(repoInfo, secretCA)
	if err != nil {
		return nil, fmt.Errorf("repo %q: %w", c.ChartRepo, err)
	}
	req.TLS = tlsCfg

	tgz, err := r.chartPuller()(ctx, req)
	if err != nil {
		if stderrors.Is(err, infrahelm.ErrArchiveTooLarge) {
			// Terminal, like any other size limit: an http/oci chart is only
			// embedded because of its overlay.
			return nil, chartTooLarge(c, true, fmt.Sprintf("its archive is over the %d MiB limit", maxChartArchiveBytes>>20), "")
		}
		if stderrors.Is(err, infrahelm.ErrIndexTooLarge) {
			return nil, chartTooLarge(c, true, fmt.Sprintf("repository %q has an index.yaml over the %d MiB limit", c.ChartRepo, maxChartIndexBytes>>20), "")
		}
		err = fmt.Errorf("fetch chart %s@%s from repo %q: %w", c.ChartName, c.ChartVersion, c.ChartRepo, err)
		if hint := chartAuthHint(err, c.ChartRepo, repoInfo); hint != "" {
			return nil, fmt.Errorf("%s: %w", hint, err)
		}
		return nil, err
	}
	return tgz, nil
}

// chartAuthHint explains a 401/403 chart download in terms of the
// repository's credentials, or returns "" for any other failure.
func chartAuthHint(err error, repo string, repoInfo clusterRepoInfo) string {
	var se *infrahelm.HTTPStatusError
	if !stderrors.As(err, &se) || (se.Code != http.StatusUnauthorized && se.Code != http.StatusForbidden) {
		return ""
	}
	if repoInfo.ClientSecret == "" {
		return fmt.Sprintf("repository %q requires credentials but none are configured for it in Settings", repo)
	}
	if se.Code == http.StatusUnauthorized {
		return fmt.Sprintf("the credentials configured for repository %q were rejected (HTTP 401): check them in Settings", repo)
	}
	hint := fmt.Sprintf("the credentials configured for repository %q authenticate but are not allowed to download this chart (HTTP 403): the account behind them probably lacks the required entitlement", repo)
	if catalog.IsNGCURL(repoInfo.URL) {
		hint += "; for NVIDIA gated repositories (NIM, cuOpt, NVIDIA Run:ai) use an NGC API key from an organization with that entitlement"
	}
	return hint
}

// clusterRepoTLSConfig maps a ClusterRepo's TLS settings onto a client config:
// caBundle and the clientSecret's cacerts (where the managed registry repos
// keep their CA, which Fleet honours too) are trusted in addition to the
// system roots, insecureSkipTLSVerify disables verification (and so wins over
// both, as in Rancher). nil means "system defaults".
func clusterRepoTLSConfig(repoInfo clusterRepoInfo, secretCA []byte) (*tls.Config, error) {
	if len(repoInfo.CABundle) == 0 && len(secretCA) == 0 && !repoInfo.InsecureSkipTLSVerify {
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(repoInfo.CABundle) > 0 || len(secretCA) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if len(repoInfo.CABundle) > 0 && !pool.AppendCertsFromPEM(repoInfo.CABundle) {
			return nil, fmt.Errorf("spec.caBundle contains no PEM certificates")
		}
		if len(secretCA) > 0 && !pool.AppendCertsFromPEM(secretCA) {
			return nil, fmt.Errorf("clientSecret key cacerts contains no PEM certificates")
		}
		cfg.RootCAs = pool
	}
	if repoInfo.InsecureSkipTLSVerify {
		cfg.InsecureSkipVerify = true //nolint:gosec // explicitly requested by the ClusterRepo's insecureSkipTLSVerify
	}
	return cfg, nil
}
