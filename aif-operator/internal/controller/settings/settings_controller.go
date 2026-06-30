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

package settings

import (
	"context"
	"fmt"
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// SettingsReconciler reconciles a Settings object.
type SettingsReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=settings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=settings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=gitrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *SettingsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	var s aiplatformv1alpha1.Settings
	if err := r.Get(ctx, req.NamespacedName, &s); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.ensureWellKnownSecretRefs(ctx, &s); err != nil {
		l.Error(err, "failed to wire well-known registry secret refs")
		return ctrl.Result{}, err
	}

	if err := r.reconcileFleetGitRepo(ctx, &s); err != nil {
		l.Error(err, "failed to reconcile Fleet GitRepo")
		return ctrl.Result{}, err
	}

	if err := r.reconcileClusterRepos(ctx, &s); err != nil {
		l.Error(err, "failed to reconcile ClusterRepos")
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, req.NamespacedName); err != nil {
		l.Error(err, "failed to update settings status")
		return ctrl.Result{}, err
	}

	l.Info("reconciled settings", "name", s.Name)
	return ctrl.Result{}, nil
}

// updateStatus stamps LastApplied/ObservedGeneration, re-fetching the latest
// object on each attempt and retrying on conflict. Earlier reconcile steps
// patch the Settings spec and write registry secrets (which re-enqueue this
// controller via the secret watch), so the in-memory object can be stale by
// the time we write status — a plain Status().Update would intermittently
// conflict.
func (r *SettingsReconciler) updateStatus(ctx context.Context, key types.NamespacedName) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var s aiplatformv1alpha1.Settings
		if err := r.Get(ctx, key, &s); err != nil {
			return err
		}
		now := metav1.Now()
		s.Status.LastApplied = &now
		s.Status.ObservedGeneration = s.Generation
		return r.Status().Update(ctx, &s)
	})
}

// SetupWithManager registers the controller with the Manager.
func (r *SettingsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gitRepo := &unstructured.Unstructured{}
	gitRepo.SetGroupVersionKind(fleetGitRepoGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(&aiplatformv1alpha1.Settings{}).
		Watches(gitRepo, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			// Only react to the GitRepo we manage.
			if obj.GetName() != fleetGitRepoName || obj.GetNamespace() != fleetGitRepoNamespace {
				return nil
			}
			return r.allSettingsRequests(ctx)
		})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.enqueueSettingsForRegistrySecret)).
		Complete(r)
}

func (r *SettingsReconciler) allSettingsRequests(ctx context.Context) []reconcile.Request {
	var list aiplatformv1alpha1.SettingsList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for _, s := range list.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: s.Name, Namespace: s.Namespace},
		})
	}
	return reqs
}

func (r *SettingsReconciler) enqueueSettingsForRegistrySecret(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() != r.OperatorNamespace {
		return nil
	}
	if !credentials.IsWellKnownSecret(obj.GetName()) {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      credentials.SettingsName,
			Namespace: r.OperatorNamespace,
		},
	}}
}

const (
	fleetGitRepoName      = "suse-ai-fleet-repo"
	fleetGitRepoNamespace = "fleet-local"
)

var fleetGitRepoGVK = schema.GroupVersionKind{
	Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepo",
}

// ensureWellKnownSecretRefs discovers operator-namespace registry secrets and
// writes their SecretKeyRefs into Settings when missing.
func (r *SettingsReconciler) ensureWellKnownSecretRefs(ctx context.Context, s *aiplatformv1alpha1.Settings) error {
	orig := s.DeepCopy()
	changed, err := credentials.WireSpec(ctx, r.Client, &s.Spec, s.Namespace)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	if err := r.Patch(ctx, s, client.MergeFrom(orig)); err != nil {
		return fmt.Errorf("patch settings secret refs: %w", err)
	}
	return nil
}

func (r *SettingsReconciler) reconcileFleetGitRepo(ctx context.Context, s *aiplatformv1alpha1.Settings) error {
	desired := s.Spec.Fleet.RepoURL != ""

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(fleetGitRepoGVK)
	err := r.Get(ctx, types.NamespacedName{
		Name:      fleetGitRepoName,
		Namespace: fleetGitRepoNamespace,
	}, existing)

	switch {
	case err != nil && !errors.IsNotFound(err):
		return fmt.Errorf("get GitRepo: %w", err)
	case !desired && err == nil:
		return r.Delete(ctx, existing)
	case !desired:
		return nil
	default:
		return r.applyFleetGitRepo(ctx, s)
	}
}

func (r *SettingsReconciler) applyFleetGitRepo(ctx context.Context, s *aiplatformv1alpha1.Settings) error {
	branch := s.Spec.Fleet.Branch
	if branch == "" {
		branch = "main"
	}

	spec := map[string]any{
		"repo":   s.Spec.Fleet.RepoURL,
		"branch": branch,
		"paths":  []any{"workloads"},
	}
	if s.Spec.Fleet.CredSecretRef != nil {
		if err := r.mirrorGitCredSecret(ctx, s); err != nil {
			return fmt.Errorf("mirror git credential secret: %w", err)
		}
		spec["clientSecretName"] = s.Spec.Fleet.CredSecretRef.Name
	}

	gitRepo := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "fleet.cattle.io/v1alpha1",
			"kind":       "GitRepo",
			"metadata": map[string]any{
				"name":      fleetGitRepoName,
				"namespace": fleetGitRepoNamespace,
			},
			"spec": spec,
		},
	}

	return r.Patch(ctx, gitRepo,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("aif-operator-settings"),
	)
}

// mirrorGitCredSecret copies the git credential secret from the Settings namespace
// into fleet-local in the format Fleet expects for HTTPS auth.
// Fleet detects auth type from secret keys: it requires username+password
// (kubernetes.io/basic-auth) for token/basic auth. A raw Opaque secret with a
// single "token" key is misidentified as GitHub App auth.
func (r *SettingsReconciler) mirrorGitCredSecret(ctx context.Context, s *aiplatformv1alpha1.Settings) error {
	ref := s.Spec.Fleet.CredSecretRef

	var src corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: s.Namespace, Name: ref.Name}, &src); err != nil {
		return fmt.Errorf("read source secret %s/%s: %w", s.Namespace, ref.Name, err)
	}

	mirrorData := src.Data
	mirrorType := src.Type

	// For token and basic auth, Fleet requires kubernetes.io/basic-auth with
	// username and password keys. Transform if the source is a raw token secret.
	if s.Spec.Fleet.AuthType == "token" || s.Spec.Fleet.AuthType == "basic" {
		token := src.Data[ref.Key]
		username := src.Data["username"]
		if len(username) == 0 {
			username = []byte("token")
		}
		mirrorData = map[string][]byte{
			"username": username,
			"password": token,
		}
		mirrorType = corev1.SecretTypeBasicAuth
	}

	// Check if the existing mirror has the wrong type — secret type is immutable,
	// so we must delete and recreate rather than patch.
	var existing corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Namespace: fleetGitRepoNamespace, Name: ref.Name}, &existing)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("get mirror secret: %w", err)
	}
	if err == nil && existing.Type != mirrorType {
		if delErr := r.Delete(ctx, &existing); delErr != nil && !errors.IsNotFound(delErr) {
			return fmt.Errorf("delete stale mirror secret: %w", delErr)
		}
	}

	mirror := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: fleetGitRepoNamespace,
		},
		Type: mirrorType,
		Data: mirrorData,
	}

	return r.Patch(ctx, mirror,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner("aif-operator-settings"),
	)
}

// authSecretNamespaces lists every namespace the operator-managed registry
// basic-auth secret must exist in: cattle-system for ClusterRepo catalog pulls,
// and the Fleet workspaces for HelmOp `helmSecretName` chart pulls. Writing all
// of them here keeps a rotated key in lockstep across copies — the per-workload
// ensureFleetAuthSecret only refreshes the Fleet mirrors on an AIWorkload
// reconcile, which a key rotation does not trigger, so they would otherwise go
// stale and gated HelmOp installs would fail with a 403 reading the index.
var authSecretNamespaces = []string{"cattle-system", "fleet-local", "fleet-default"}

func (r *SettingsReconciler) applyRegistryAuthSecret(
	ctx context.Context,
	ns string,
	secretName string,
	userRef, tokenRef *aiplatformv1alpha1.SecretKeyRef,
) (name string, changed bool, err error) {
	user, token, ok, err := credentials.ReadPair(ctx, r.Client, ns, userRef, tokenRef)
	if err != nil {
		return "", false, fmt.Errorf("read registry credentials: %w", err)
	}
	if !ok {
		return "", false, nil
	}

	// Capture whether the credentials rotated BEFORE overwriting the mirror.
	// Rancher's ClusterRepo controller does not watch the clientSecret's
	// content, so a rotated key only takes effect on its ~1h periodic retry
	// (and a cached auth failure can linger). The caller bumps spec.forceUpdate
	// when this reports a change so Rancher re-reads the secret immediately.
	changed = r.registryAuthChanged(ctx, secretName, user, token)

	for _, targetNS := range authSecretNamespaces {
		mirror := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: targetNS,
			},
			Type: corev1.SecretTypeBasicAuth,
			Data: map[string][]byte{
				"username": []byte(user),
				"password": []byte(token),
			},
		}
		if err := r.Patch(ctx, mirror, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator-settings")); err != nil {
			// The Fleet workspaces are absent on clusters without Fleet; only
			// cattle-system is mandatory (the ClusterRepo's clientSecret lives there).
			if targetNS != "cattle-system" && errors.IsNotFound(err) {
				continue
			}
			return "", false, fmt.Errorf("apply auth secret %s/%s: %w", targetNS, secretName, err)
		}
	}

	return secretName, changed, nil
}

// registryAuthChanged reports whether the cattle-system basic-auth mirror named
// secretName differs from the freshly-resolved (user, token) — i.e. the
// credentials rotated. cattle-system is the copy the ClusterRepo authenticates
// with. A missing mirror counts as changed (first write); an unreadable mirror
// counts as unchanged to avoid spurious force-updates that would churn the
// ClusterRepo into a re-download every reconcile.
func (r *SettingsReconciler) registryAuthChanged(ctx context.Context, secretName, user, token string) bool {
	var existing corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Namespace: "cattle-system", Name: secretName}, &existing)
	if errors.IsNotFound(err) {
		return true
	}
	if err != nil {
		return false
	}
	return string(existing.Data["username"]) != user || string(existing.Data["password"]) != token
}

// forceUpdateClusterRepo bumps spec.forceUpdate to now (RFC3339) so Rancher
// re-reads the clientSecret and re-downloads the index. A plain merge patch
// keeps forceUpdate out of the SSA-managed field set (applyClusterRepo owns
// url + clientSecret), so the two never fight over ownership.
func (r *SettingsReconciler) forceUpdateClusterRepo(ctx context.Context, name string) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	repo.SetName(name)
	patch := []byte(fmt.Sprintf(`{"spec":{"forceUpdate":%q}}`, time.Now().UTC().Format(time.RFC3339)))
	if err := r.Patch(ctx, repo, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return fmt.Errorf("force-update ClusterRepo %s: %w", name, err)
	}
	return nil
}

func (r *SettingsReconciler) applyClusterRepo(ctx context.Context, name, url, clientSecretName string) error {
	repo := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "catalog.cattle.io/v1",
			"kind":       "ClusterRepo",
			"metadata": map[string]any{
				"name": name,
			},
			"spec": map[string]any{
				"url": url,
			},
		},
	}

	if clientSecretName != "" {
		_ = unstructured.SetNestedField(repo.Object, clientSecretName, "spec", "clientSecret", "name")
		_ = unstructured.SetNestedField(repo.Object, "cattle-system", "spec", "clientSecret", "namespace")
	}

	return r.Patch(ctx, repo, client.Apply, client.ForceOwnership, client.FieldOwner("aif-operator-settings"))
}

// deleteClusterRepo removes a ClusterRepo by name, ignoring NotFound. Used to
// prune repos the operator created once a registry's credentials are gone.
func (r *SettingsReconciler) deleteClusterRepo(ctx context.Context, name string) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	repo.SetName(name)
	return client.IgnoreNotFound(r.Delete(ctx, repo))
}

// deleteAuthSecret removes a cattle-system basic-auth mirror by name, ignoring
// NotFound. Pairs with deleteClusterRepo when pruning a registry.
func (r *SettingsReconciler) deleteAuthSecret(ctx context.Context, name string) error {
	var firstErr error
	for _, ns := range authSecretNamespaces {
		sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
		if err := client.IgnoreNotFound(r.Delete(ctx, sec)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *SettingsReconciler) reconcileClusterRepos(ctx context.Context, s *aiplatformv1alpha1.Settings) error {
	acURL := credentials.DefaultApplicationCollectionURL
	if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.ApplicationCollection != "" {
		acURL = s.Spec.RegistryEndpoints.ApplicationCollection
	}
	acUser, acToken := credentials.EffectiveRefs(ctx, r.Client, s.Namespace,
		s.Spec.ApplicationCollection.UserSecretRef,
		s.Spec.ApplicationCollection.TokenSecretRef,
		credentials.RegistryApplicationCollection,
	)
	if err := r.reconcileRegistryRepo(ctx, s.Namespace,
		acUser, acToken,
		credentials.AuthSecretApplicationCollection,
		acURL,
		[]string{credentials.ClusterRepoApplicationCollection},
	); err != nil {
		return err
	}

	srURL := credentials.DefaultSUSERegistryURL
	if s.Spec.RegistryEndpoints != nil && s.Spec.RegistryEndpoints.SUSERegistry != "" {
		srURL = s.Spec.RegistryEndpoints.SUSERegistry
	}
	srUser, srToken := credentials.EffectiveRefs(ctx, r.Client, s.Namespace,
		s.Spec.SUSERegistry.UserSecretRef,
		s.Spec.SUSERegistry.TokenSecretRef,
		credentials.RegistrySUSERegistry,
	)
	if err := r.reconcileRegistryRepo(ctx, s.Namespace,
		srUser, srToken,
		credentials.AuthSecretSUSERegistry,
		srURL,
		[]string{credentials.ClusterRepoSUSERegistry},
	); err != nil {
		return err
	}

	return r.reconcileNvidiaRepos(ctx, s)
}

// reconcileRegistryRepo applies (or prunes) a single-repo registry. When the
// credentials resolve, it writes the cattle-system basic-auth mirror and the
// ClusterRepo(s); otherwise it prunes both so removing credentials tears the
// generated objects back down. repoNames may list more than one repo sharing
// the same URL+mirror (none do today, but nvidia uses the sibling helper).
func (r *SettingsReconciler) reconcileRegistryRepo(
	ctx context.Context,
	namespace string,
	userRef, tokenRef *aiplatformv1alpha1.SecretKeyRef,
	authSecretName, url string,
	repoNames []string,
) error {
	secretName := ""
	changed := false
	if userRef != nil && tokenRef != nil {
		var err error
		secretName, changed, err = r.applyRegistryAuthSecret(ctx, namespace, authSecretName, userRef, tokenRef)
		if err != nil {
			return err
		}
	}

	if secretName == "" {
		return r.pruneRegistryRepos(ctx, authSecretName, repoNames)
	}

	for _, name := range repoNames {
		if err := r.applyClusterRepo(ctx, name, url, secretName); err != nil {
			return err
		}
		if changed {
			if err := r.forceUpdateClusterRepo(ctx, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconcileNvidiaRepos handles NVIDIA's two-mode topology: a single gated OCI
// repo when registryEndpoints.nvidia is set (air-gap), or the public NGC charts
// + blueprint pair otherwise. Either way it prunes every NVIDIA repo + mirror
// when credentials are gone.
func (r *SettingsReconciler) reconcileNvidiaRepos(ctx context.Context, s *aiplatformv1alpha1.Settings) error {
	nvUser, nvToken := credentials.EffectiveRefs(ctx, r.Client, s.Namespace,
		s.Spec.Nvidia.UserSecretRef,
		s.Spec.Nvidia.TokenSecretRef,
		credentials.RegistryNvidia,
	)
	nvURL := ""
	if s.Spec.RegistryEndpoints != nil {
		nvURL = s.Spec.RegistryEndpoints.Nvidia
	}

	allNvidiaRepos := []string{credentials.ClusterRepoNvidia, credentials.ClusterRepoNvidiaBlueprint}

	secretName := ""
	changed := false
	if nvUser != nil && nvToken != nil {
		var err error
		secretName, changed, err = r.applyRegistryAuthSecret(ctx, s.Namespace, credentials.AuthSecretNvidia, nvUser, nvToken)
		if err != nil {
			return err
		}
	}

	if secretName == "" {
		return r.pruneRegistryRepos(ctx, credentials.AuthSecretNvidia, allNvidiaRepos)
	}

	if nvURL != "" {
		// Air-gap: a single gated OCI repo. Prune the public blueprint repo
		// in case we are switching modes.
		if err := r.deleteClusterRepo(ctx, credentials.ClusterRepoNvidiaBlueprint); err != nil {
			return err
		}
		if err := r.applyClusterRepo(ctx, credentials.ClusterRepoNvidia, nvURL, secretName); err != nil {
			return err
		}
		if changed {
			return r.forceUpdateClusterRepo(ctx, credentials.ClusterRepoNvidia)
		}
		return nil
	}

	// Public NGC charts catalog: created WITHOUT a clientSecret (anonymous).
	// NGC serves https://helm.ngc.nvidia.com/nvidia/index.yaml anonymously
	// (HTTP 302 -> valid index), but returns 403 when presented a valid NGC key
	// that is NOT entitled to the full /nvidia catalog (e.g. a key scoped to
	// /nvidia/blueprint). Rancher then surfaces that 403 body as the misleading
	// "no API version specified". Sending no credential restores public access.
	// (This matches the pre-SettingsReconciler Helm chart, which left this repo
	// anonymous.) The nvidia presence of credentials still gates creation above;
	// blueprint and the air-gap OCI repo keep their auth because those paths are
	// what NGC keys are typically entitled to / require.
	if err := r.applyClusterRepo(ctx, credentials.ClusterRepoNvidia, credentials.DefaultNvidiaChartsURL, ""); err != nil {
		return err
	}
	if err := r.applyClusterRepo(ctx, credentials.ClusterRepoNvidiaBlueprint, credentials.DefaultNvidiaBlueprintURL, secretName); err != nil {
		return err
	}
	if changed {
		// Only the blueprint repo authenticates (the public charts repo is
		// anonymous), so it's the one whose cached auth must be refreshed.
		return r.forceUpdateClusterRepo(ctx, credentials.ClusterRepoNvidiaBlueprint)
	}
	return nil
}

// pruneRegistryRepos deletes the given ClusterRepos and the registry's
// cattle-system basic-auth mirror, all NotFound-tolerant.
func (r *SettingsReconciler) pruneRegistryRepos(ctx context.Context, authSecretName string, repoNames []string) error {
	for _, name := range repoNames {
		if err := r.deleteClusterRepo(ctx, name); err != nil {
			return err
		}
	}
	return r.deleteAuthSecret(ctx, authSecretName)
}
