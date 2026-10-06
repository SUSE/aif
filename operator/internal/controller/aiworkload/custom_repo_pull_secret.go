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
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/SUSE/aif-operator/internal/cluster"
	"github.com/SUSE/aif-operator/internal/registryurl"
)

// customRepoPullSecretPrefix prefixes the image pull secret built from a
// custom ClusterRepo's own credentials. The ClusterRepo name follows the
// prefix, so pullSecretFactory can rebuild the secret from the name alone.
const customRepoPullSecretPrefix = "aif-custom-pull-"

// Docker Hub's registry API host and the dockerconfigjson key kubelet uses
// for docker.io image references.
const (
	dockerHubRegistryHost = "registry-1.docker.io"
	dockerHubIndexKey     = "https://index.docker.io/v1/"
)

func customRepoPullSecretName(repoName string) string {
	return customRepoPullSecretPrefix + repoName
}

// customRepoNameFromPullSecret returns the ClusterRepo name encoded in a
// custom repo pull secret name, or false if the name is not one.
func customRepoNameFromPullSecret(secretName string) (string, bool) {
	repo, ok := strings.CutPrefix(secretName, customRepoPullSecretPrefix)
	if !ok || repo == "" {
		return "", false
	}
	return repo, true
}

// buildCustomRepoPullSecret builds a dockerconfigjson Secret in namespace that
// authenticates image pulls against a custom OCI repo's registry host with the
// repo's own credentials. Returns (nil, nil) when the repo is not an
// authenticated custom OCI repo or its credentials are unavailable: HTTP and
// git repos carry chart-index or git credentials, not registry credentials.
// Only the repo's own host is included; Settings registries are never added.
func (r *AIWorkloadReconciler) buildCustomRepoPullSecret(ctx context.Context, repoInfo clusterRepoInfo, namespace string) (*corev1.Secret, error) {
	if !repoInfo.Custom || repoInfo.Kind != repoKindOCI || repoInfo.ClientSecret == "" || repoInfo.Name == "" {
		return nil, nil
	}
	host := registryurl.Host(repoInfo.URL)
	if host == "" {
		return nil, nil
	}
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: repoInfo.ClientSecretNS, Name: repoInfo.ClientSecret}, src); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read credentials for ClusterRepo %q: %w", repoInfo.Name, err)
	}
	username := string(src.Data[corev1.BasicAuthUsernameKey])
	password := string(src.Data[corev1.BasicAuthPasswordKey])
	if username == "" || password == "" {
		return nil, nil
	}
	entry := dockerAuthEntry(username, password)
	auths := map[string]any{host: entry}
	// Docker Hub serves OCI charts from registry-1.docker.io, while kubelet
	// resolves docker.io image references against the Docker Hub index key.
	if host == dockerHubRegistryHost {
		auths[dockerHubIndexKey] = entry
	}
	cfg, err := json.Marshal(map[string]any{"auths": auths})
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: customRepoPullSecretName(repoInfo.Name), Namespace: namespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: cfg},
	}, nil
}

// customRepoInjector delivers a pull secret built from a custom repo's own
// credentials. Chart values are left untouched: the secret reaches pods
// through the ServiceAccount merge, which works for any chart regardless of
// how (or whether) it exposes imagePullSecrets in its values.
type customRepoInjector struct{ r *AIWorkloadReconciler }

func (c *customRepoInjector) Apply(ctx context.Context, cc cluster.Client, targetNamespace string, repoInfo clusterRepoInfo, _ map[string]any, writeLocal bool) ([]string, error) {
	sec, err := c.r.buildCustomRepoPullSecret(ctx, repoInfo, targetNamespace)
	if err != nil || sec == nil {
		return nil, err
	}
	// Downstream-only workloads get the secret (and its namespace) through the
	// per-cluster Fleet Bundle in deliverPullSecrets; nothing is written here.
	if writeLocal {
		if err := c.r.ensureNamespace(ctx, targetNamespace); err != nil {
			return nil, err
		}
		if err := cc.ApplySecret(ctx, sec); err != nil {
			return nil, err
		}
	}
	return []string{sec.Name}, nil
}
