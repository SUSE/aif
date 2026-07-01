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

// Package credentials resolves registry credentials from the Settings CR and
// from well-known operator-namespace secrets (the kubectl "official" setup).
package credentials

import (
	"context"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const SettingsName = "settings"

// NvidiaDefaultUsername is the conventional NGC username. The NVIDIA "official"
// kubectl setup uses the literal string "$oauthtoken" as the username paired
// with an nvapi- API key as the token. When a discovered nvidia secret carries
// only a token, the operator normalizes it by writing this username.
const NvidiaDefaultUsername = "$oauthtoken"

const (
	DefaultApplicationCollectionURL = "oci://dp.apps.rancher.io/charts"
	DefaultSUSERegistryURL          = "oci://registry.suse.com/ai/charts"
	DefaultNvidiaChartsURL          = "https://helm.ngc.nvidia.com/nvidia"
	DefaultNvidiaBlueprintURL       = "https://helm.ngc.nvidia.com/nvidia/blueprint"
)

// ClusterRepo names align with pkg/aif-ui/services/rancher-apps.ts.
const (
	ClusterRepoApplicationCollection = "application-collection"
	ClusterRepoSUSERegistry          = "suse-ai-registry"
	ClusterRepoNvidia                = "nvidia"
	ClusterRepoNvidiaBlueprint       = "nvidia-blueprints"
)

// Basic-auth secrets written to cattle-system for Rancher catalog / Fleet chart pulls.
const (
	AuthSecretApplicationCollection = "application-collection-auth"
	AuthSecretSUSERegistry          = "suse-ai-registry-auth"
	AuthSecretNvidia                = "ngc-helm-auth"
)

// Registry identifies one of the three credential-bearing artifact sources.
type Registry int

const (
	RegistryApplicationCollection Registry = iota
	RegistrySUSERegistry
	RegistryNvidia
)

// WellKnownSecretNames returns candidate secret names in priority order.
func WellKnownSecretNames(r Registry) []string {
	switch r {
	case RegistryApplicationCollection:
		return []string{"appco", "application-collection"}
	case RegistrySUSERegistry:
		return []string{"suse-registry"}
	case RegistryNvidia:
		return []string{"nvidia", "nvidia-registry"}
	default:
		return nil
	}
}

// IsWellKnownSecret reports whether name is one of the operator-managed registry secrets.
func IsWellKnownSecret(name string) bool {
	for _, r := range []Registry{
		RegistryApplicationCollection,
		RegistrySUSERegistry,
		RegistryNvidia,
	} {
		for _, candidate := range WellKnownSecretNames(r) {
			if candidate == name {
				return true
			}
		}
	}
	return false
}

// EffectiveRefs returns secret refs for a registry. Explicit Settings refs win
// when both are set; otherwise well-known secrets in namespace are discovered.
func EffectiveRefs(
	ctx context.Context,
	c client.Client,
	namespace string,
	explicitUser, explicitToken *aiplatformv1alpha1.SecretKeyRef,
	registry Registry,
) (*aiplatformv1alpha1.SecretKeyRef, *aiplatformv1alpha1.SecretKeyRef) {
	if explicitUser != nil && explicitToken != nil {
		// Explicit refs win only when they actually resolve. Corrupt refs
		// (e.g. an empty or wrong key) fall through to discovery so they
		// self-heal instead of silently shadowing a valid well-known secret.
		if _, _, ok, _ := ReadPair(ctx, c, namespace, explicitUser, explicitToken); ok {
			return explicitUser, explicitToken
		}
	}
	if u, t, ok := discoverRefs(ctx, c, namespace, registry); ok {
		return u, t
	}
	return explicitUser, explicitToken
}

func discoverRefs(
	ctx context.Context,
	c client.Client,
	namespace string,
	registry Registry,
) (*aiplatformv1alpha1.SecretKeyRef, *aiplatformv1alpha1.SecretKeyRef, bool) {
	for _, name := range WellKnownSecretNames(registry) {
		var sec corev1.Secret
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &sec); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, nil, false
		}
		if registry == RegistryNvidia {
			if err := normalizeNvidiaSecret(ctx, c, &sec); err != nil {
				return nil, nil, false
			}
		}
		userKey, tokenKey, ok := keysFromSecret(&sec)
		if !ok {
			continue
		}
		return &aiplatformv1alpha1.SecretKeyRef{Name: name, Key: userKey},
			&aiplatformv1alpha1.SecretKeyRef{Name: name, Key: tokenKey},
			true
	}
	return nil, nil, false
}

// normalizeNvidiaSecret ensures an nvidia well-known secret that carries a
// token but no username gets a "user" key set to NvidiaDefaultUsername, both
// in-memory (so keysFromSecret sees it this pass) and persisted (so every
// later ref-based read resolves). Idempotent: once the key exists it does
// nothing. Mutating the discovered secret keeps the rest of the pipeline
// purely ref-based instead of special-casing a constant username everywhere.
func normalizeNvidiaSecret(ctx context.Context, c client.Client, sec *corev1.Secret) error {
	if nonEmpty(sec.Data, "user") || nonEmpty(sec.Data, "username") {
		return nil
	}
	if !nonEmpty(sec.Data, "token") {
		return nil
	}
	if sec.Data == nil {
		sec.Data = map[string][]byte{}
	}
	sec.Data["user"] = []byte(NvidiaDefaultUsername)
	return c.Update(ctx, sec)
}

func keysFromSecret(sec *corev1.Secret) (userKey, tokenKey string, ok bool) {
	if nonEmpty(sec.Data, "user") && nonEmpty(sec.Data, "token") {
		return "user", "token", true
	}
	if nonEmpty(sec.Data, "username") && nonEmpty(sec.Data, "token") {
		return "username", "token", true
	}
	if nonEmpty(sec.Data, "username") && nonEmpty(sec.Data, "password") {
		return "username", "password", true
	}
	return "", "", false
}

func nonEmpty(data map[string][]byte, key string) bool {
	v, ok := data[key]
	return ok && len(v) > 0
}

// ReadPair loads username and token/password from the given refs in namespace.
// Returns ok=false when refs are nil, keys missing, or values empty. NotFound
// on the secret is treated as ok=false (lenient skip).
func ReadPair(
	ctx context.Context,
	c client.Client,
	namespace string,
	userRef, tokenRef *aiplatformv1alpha1.SecretKeyRef,
) (user, token string, ok bool, err error) {
	if userRef == nil || tokenRef == nil {
		return "", "", false, nil
	}
	var sec corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: userRef.Name}, &sec); err != nil {
		if errors.IsNotFound(err) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	if userRef.Name != tokenRef.Name {
		var tokenSec corev1.Secret
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: tokenRef.Name}, &tokenSec); err != nil {
			if errors.IsNotFound(err) {
				return "", "", false, nil
			}
			return "", "", false, err
		}
		user, ok1 := stringVal(sec.Data, userRef.Key)
		token, ok2 := stringVal(tokenSec.Data, tokenRef.Key)
		if !ok1 || !ok2 || user == "" || token == "" {
			return "", "", false, nil
		}
		return user, token, true, nil
	}
	user, ok1 := stringVal(sec.Data, userRef.Key)
	token, ok2 := stringVal(sec.Data, tokenRef.Key)
	if !ok1 || !ok2 || user == "" || token == "" {
		return "", "", false, nil
	}
	return user, token, true, nil
}

func stringVal(data map[string][]byte, key string) (string, bool) {
	v, ok := data[key]
	if !ok {
		return "", false
	}
	return string(v), true
}

// WireSpec patches missing Settings.spec secret refs from well-known secrets.
// Returns true when spec was modified.
func WireSpec(ctx context.Context, c client.Client, spec *aiplatformv1alpha1.SettingsSpec, namespace string) (bool, error) {
	if spec == nil {
		return false, nil
	}
	changed := false

	if u, t := EffectiveRefs(ctx, c, namespace, spec.ApplicationCollection.UserSecretRef, spec.ApplicationCollection.TokenSecretRef, RegistryApplicationCollection); u != nil && t != nil {
		if !refsEqual(spec.ApplicationCollection.UserSecretRef, u) || !refsEqual(spec.ApplicationCollection.TokenSecretRef, t) {
			spec.ApplicationCollection.UserSecretRef = u
			spec.ApplicationCollection.TokenSecretRef = t
			changed = true
		}
	}

	if u, t := EffectiveRefs(ctx, c, namespace, spec.SUSERegistry.UserSecretRef, spec.SUSERegistry.TokenSecretRef, RegistrySUSERegistry); u != nil && t != nil {
		if !refsEqual(spec.SUSERegistry.UserSecretRef, u) || !refsEqual(spec.SUSERegistry.TokenSecretRef, t) {
			spec.SUSERegistry.UserSecretRef = u
			spec.SUSERegistry.TokenSecretRef = t
			changed = true
		}
	}

	if u, t := EffectiveRefs(ctx, c, namespace, spec.Nvidia.UserSecretRef, spec.Nvidia.TokenSecretRef, RegistryNvidia); u != nil && t != nil {
		if !refsEqual(spec.Nvidia.UserSecretRef, u) || !refsEqual(spec.Nvidia.TokenSecretRef, t) {
			spec.Nvidia.UserSecretRef = u
			spec.Nvidia.TokenSecretRef = t
			changed = true
		}
	}

	return changed, nil
}

func refsEqual(a, b *aiplatformv1alpha1.SecretKeyRef) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Name == b.Name && a.Key == b.Key
}
