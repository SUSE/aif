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
	"crypto/tls"
	"crypto/x509"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// ResolveHelmTLS builds an in-memory *tls.Config from a HelmSource tls block,
// reading secrets from namespace. Returns (nil, nil) when cfg is nil. Errors
// never contain key material.
func ResolveHelmTLS(
	ctx context.Context,
	c client.Client,
	namespace string,
	cfg *aiplatformv1alpha1.HelmTLS,
) (*tls.Config, error) {
	if cfg == nil {
		return nil, nil
	}
	out := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.InsecureSkipVerify {
		out.InsecureSkipVerify = true
	}

	if cfg.CASecretRef != nil {
		pemBytes, ok, err := readKey(ctx, c, namespace, *cfg.CASecretRef)
		if err != nil {
			return nil, fmt.Errorf("reading CA secret: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("CA secret %q missing or has empty key %q", cfg.CASecretRef.Name, cfg.CASecretRef.Key)
		}
		// Start from the system trust store and add the custom CA, so registries
		// using a public CA still verify while the private CA is also trusted.
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(pemBytes)) {
			return nil, fmt.Errorf("CA secret %q key %q does not contain a valid PEM certificate", cfg.CASecretRef.Name, cfg.CASecretRef.Key)
		}
		out.RootCAs = pool
	}

	if cfg.ClientTLSSecretRef != nil {
		var sec corev1.Secret
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: cfg.ClientTLSSecretRef.Name}, &sec); err != nil {
			return nil, fmt.Errorf("reading client TLS secret %q: %w", cfg.ClientTLSSecretRef.Name, err)
		}
		crt := sec.Data[corev1.TLSCertKey]
		key := sec.Data[corev1.TLSPrivateKeyKey]
		if len(crt) == 0 || len(key) == 0 {
			return nil, fmt.Errorf("client TLS secret %q missing %q or %q", cfg.ClientTLSSecretRef.Name, corev1.TLSCertKey, corev1.TLSPrivateKeyKey)
		}
		pair, err := tls.X509KeyPair(crt, key)
		if err != nil {
			return nil, fmt.Errorf("client TLS secret %q: invalid certificate/key pair", cfg.ClientTLSSecretRef.Name)
		}
		out.Certificates = []tls.Certificate{pair}
	}

	return out, nil
}
