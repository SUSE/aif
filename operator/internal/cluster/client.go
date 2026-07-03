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

// Package cluster abstracts read/write access against a target cluster.
//
// The operator runs on its own ("local") cluster but needs to deliver
// resources to downstream clusters as well. localClient writes via the
// in-cluster controller-runtime client; a future bundleClient (see Task
// 2.5) will wrap writes in Fleet Bundles for cross-cluster delivery.
// reconcilePullSecrets uses this interface uniformly so the call sites
// don't need to know which mechanism is in play.
package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client is the minimal surface reconcilePullSecrets needs to talk to any
// target cluster.
type Client interface {
	// ApplySecret writes the secret using server-side apply with the operator
	// as the field owner. Creates if missing, updates if present, idempotent
	// across repeated calls with the same content.
	ApplySecret(ctx context.Context, secret *corev1.Secret) error
}

// NewLocalClient returns a Client backed by the operator's own in-cluster
// controller-runtime client.
func NewLocalClient(c client.Client, scheme *runtime.Scheme) Client {
	return &localClient{c: c, scheme: scheme}
}

type localClient struct {
	c client.Client
	// scheme is unused by ApplySecret today but kept for symmetry with
	// bundleClient (which needs it to register the Bundle GVK) and to keep
	// NewLocalClient's signature stable when a future generic Apply(obj
	// client.Object) lands.
	scheme *runtime.Scheme
}

func (l *localClient) ApplySecret(ctx context.Context, sec *corev1.Secret) error {
	// Defensive copy so callers can reuse the object across multiple cluster
	// targets without aliasing concerns when the apply mutates metadata.
	out := sec.DeepCopy()
	// Server-side apply requires TypeMeta on the payload; typed clientset
	// objects typically have it cleared after decode.
	out.APIVersion = "v1"
	out.Kind = "Secret"
	if err := l.c.Patch(ctx, out, client.Apply, client.ForceOwnership, client.FieldOwner("suse-ai-operator")); err != nil {
		return fmt.Errorf("apply secret %s/%s: %w", out.Namespace, out.Name, err)
	}
	return nil
}
