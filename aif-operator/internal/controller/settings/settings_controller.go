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

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// SettingsReconciler reconciles a Settings object.
type SettingsReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=settings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=settings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fleet.cattle.io,resources=gitrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *SettingsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	var s aiplatformv1alpha1.Settings
	if err := r.Get(ctx, req.NamespacedName, &s); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.reconcileFleetGitRepo(ctx, &s); err != nil {
		l.Error(err, "failed to reconcile Fleet GitRepo")
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	s.Status.LastApplied = &now
	s.Status.ObservedGeneration = s.Generation

	if err := r.Status().Update(ctx, &s); err != nil {
		l.Error(err, "failed to update settings status")
		return ctrl.Result{}, err
	}

	l.Info("reconciled settings", "name", s.Name)
	return ctrl.Result{}, nil
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
		})).
		Complete(r)
}

const (
	fleetGitRepoName      = "suse-ai-fleet-repo"
	fleetGitRepoNamespace = "fleet-local"
)

var fleetGitRepoGVK = schema.GroupVersionKind{
	Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepo",
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
