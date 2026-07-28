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

package credentials

import (
	"context"
	"fmt"
	urlpkg "net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// RegistryCredentials is an in-memory username/password pair resolved for a
// chart pull. It is never persisted, logged, or written to CR status.
type RegistryCredentials struct {
	Username string
	Password string
}

// ResolveHelmAuth resolves a HelmSource auth block to registry credentials,
// reading referenced Secrets from namespace. Returns (nil, nil) when auth is
// nil (anonymous pull). Errors never contain secret values.
func ResolveHelmAuth(
	ctx context.Context,
	c client.Client,
	namespace string,
	auth *aiplatformv1alpha1.HelmAuth,
	chartURL string,
) (*RegistryCredentials, error) {
	if auth == nil {
		return nil, nil
	}

	switch {
	case auth.Basic != nil:
		user, token, ok, err := ReadPair(ctx, c, namespace, &auth.Basic.UserSecretRef, &auth.Basic.TokenSecretRef)
		if err != nil {
			return nil, fmt.Errorf("reading basic auth secret: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("basic auth secrets missing or have empty keys (user: %q, token: %q)", auth.Basic.UserSecretRef.Name, auth.Basic.TokenSecretRef.Name)
		}
		return &RegistryCredentials{Username: user, Password: token}, nil

	case auth.Token != nil:
		token, ok, err := readKey(ctx, c, namespace, auth.Token.TokenSecretRef)
		if err != nil {
			return nil, fmt.Errorf("reading token auth secret: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("token auth secret %q missing or has empty key %q", auth.Token.TokenSecretRef.Name, auth.Token.TokenSecretRef.Key)
		}
		return &RegistryCredentials{Username: NvidiaDefaultUsername, Password: token}, nil

	case auth.DockerConfig != nil:
		host, err := chartHost(chartURL)
		if err != nil {
			return nil, err
		}
		var sec corev1.Secret
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: auth.DockerConfig.SecretRef.Name}, &sec); err != nil {
			return nil, fmt.Errorf("reading dockerconfig secret %q: %w", auth.DockerConfig.SecretRef.Name, err)
		}
		blob, ok := sec.Data[corev1.DockerConfigJsonKey]
		if !ok || len(blob) == 0 {
			return nil, fmt.Errorf("dockerconfig secret %q missing key %q", auth.DockerConfig.SecretRef.Name, corev1.DockerConfigJsonKey)
		}
		user, pass, err := dockerConfigCredentials(blob, host)
		if err != nil {
			return nil, err
		}
		return &RegistryCredentials{Username: user, Password: pass}, nil

	default:
		return nil, fmt.Errorf("no auth method set")
	}
}

// readKey reads a single key from a Secret in namespace.
func readKey(ctx context.Context, c client.Client, namespace string, ref aiplatformv1alpha1.SecretKeyRef) (string, bool, error) {
	var sec corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, &sec); err != nil {
		if errors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	v, ok := sec.Data[ref.Key]
	if !ok || len(v) == 0 {
		return "", false, nil
	}
	return string(v), true, nil
}

// chartHost extracts the registry host from an oci:// or https:// chart URL.
func chartHost(chartURL string) (string, error) {
	u, err := urlpkg.Parse(chartURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("cannot determine registry host from chart URL %q", chartURL)
	}
	return u.Host, nil
}
