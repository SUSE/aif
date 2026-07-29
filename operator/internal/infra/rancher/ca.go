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

package rancher

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Location of the CA that signs Rancher's in-cluster serving certificate.
//
// This is NOT the `cacerts` Setting. That holds the ingress/public CA and is a
// different certificate — trusting it against the in-cluster endpoint produces
// an x509 failure. Getting this wrong is the reason discovery exists.
const (
	InternalCANamespace = "cattle-system"
	InternalCAName      = "tls-rancher-internal-ca"
	InternalCAKey       = "tls.crt"
)

// ErrCANotFound indicates Rancher's internal CA Secret (or its tls.crt key) is
// absent — for example on a cluster where Rancher is not installed, or where the
// Secret has been renamed. Callers fall back to the system roots.
var ErrCANotFound = errors.New("rancher internal CA secret not found")

// DiscoverInternalCA returns the PEM that signed Rancher's in-cluster serving
// certificate, read from cattle-system/tls-rancher-internal-ca.
//
// Only the tls.crt key is read. The same Secret also holds tls.key — the CA
// private key, sufficient to mint certificates the cluster agents trust — and
// there is no reason for it to enter this process's memory.
//
// It takes a client.Reader rather than a full client so it can be tested against
// a fake client with no Rancher present.
func DiscoverInternalCA(ctx context.Context, r client.Reader) ([]byte, error) {
	var sec corev1.Secret
	key := types.NamespacedName{Namespace: InternalCANamespace, Name: InternalCAName}
	if err := r.Get(ctx, key, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", ErrCANotFound, InternalCANamespace, InternalCAName)
		}
		return nil, fmt.Errorf("read %s/%s: %w", InternalCANamespace, InternalCAName, err)
	}
	pem := sec.Data[InternalCAKey]
	if len(pem) == 0 {
		return nil, fmt.Errorf("%w: %s/%s has no %s", ErrCANotFound, InternalCANamespace, InternalCAName, InternalCAKey)
	}
	return pem, nil
}
