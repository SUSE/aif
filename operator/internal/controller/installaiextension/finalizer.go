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

package controller

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

const finalizerName = "ai-factory.suse.com/finalizer"

const (
	// cleanupTimeout bounds the whole cleanup call, not any error within it
	// individually: past this point every kind of failure gets the same verdict,
	// because whatever the specific cause, it needs an admin's attention and
	// "minimal user interaction" means the CR must not stay stuck on its own
	// finalizer forever waiting for one. ignoreGone already closes the single
	// most common cause (Rancher's CRDs are gone), so what reaches this bound is
	// whatever that does not cover — this is the backstop, not the fix.
	cleanupTimeout = 120 * time.Second
	// cleanupRetryInterval is deliberately fixed rather than widening like
	// failureRetryInterval: the window is short (2 minutes) and deletion is not
	// a steady-state loop an admin is watching metrics on, so there is no
	// hot-loop cost worth trading against a simpler timeline.
	cleanupRetryInterval = 15 * time.Second
	// annotationCleanupWaitingSince times the bound above, the same pattern
	// awaitReadiness uses for its own waits. A separate key: this clock starts
	// at deletion, which is unrelated to whatever readiness or release wait was
	// running (and already cleared) before deletion began.
	annotationCleanupWaitingSince = "ai-factory.suse.com/cleanup-waiting-since"
)

func (r *InstallAIExtensionReconciler) ensureFinalizer(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
) (bool, error) {
	if controllerutil.ContainsFinalizer(ext, finalizerName) {
		return false, nil
	}

	controllerutil.AddFinalizer(ext, finalizerName)
	if err := r.Update(ctx, ext); err != nil {
		return false, err
	}
	return true, nil
}

func (r *InstallAIExtensionReconciler) handleDeletion(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(ext, finalizerName) {
		return ctrl.Result{}, nil
	}

	if err := r.cleanup(ctx, ext); err != nil {
		return r.handleCleanupFailure(ctx, ext, err)
	}

	if !r.getWaitingSince(ext, annotationCleanupWaitingSince).IsZero() {
		r.clearWaitingSince(ext, annotationCleanupWaitingSince)
	}

	controllerutil.RemoveFinalizer(ext, finalizerName)
	if err := r.Update(ctx, ext); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("deleted successfully")
	return ctrl.Result{}, nil
}

// handleCleanupFailure turns a failing cleanup into a bounded wait rather than
// the unconditional retry-forever this replaced. Before ignoreGone, a Rancher
// CRD deleted along with its namespace meant DeleteClusterRepo/DeleteUIPlugin
// failed on every pass with no way to ever succeed — the CR stuck in
// Terminating until someone stripped the finalizer by hand, the opposite of
// "minimal user interaction". ignoreGone closes that specific cause; this is
// what still applies once it does not: whatever kind of failure reaches here,
// past cleanupTimeout the finalizer is removed anyway so the CR's own deletion
// is never held hostage by one.
//
// Giving up is not silent: the event and the log line are what an admin has
// to go on afterwards, since removing the finalizer here means nothing else
// will report this CR's cleanup again.
func (r *InstallAIExtensionReconciler) handleCleanupFailure(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	cleanupErr error,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	waitingSince := r.getWaitingSince(ext, annotationCleanupWaitingSince)
	if waitingSince.IsZero() {
		r.setWaitingSince(ext, annotationCleanupWaitingSince)
		if err := r.updateAnnotations(ctx, ext); err != nil {
			return ctrl.Result{}, err
		}
		logger.Error(cleanupErr, "cleanup failed, retrying")
		return ctrl.Result{RequeueAfter: cleanupRetryInterval}, nil
	}

	if time.Since(waitingSince) < cleanupTimeout {
		logger.Error(cleanupErr, "cleanup failed, retrying")
		return ctrl.Result{RequeueAfter: cleanupRetryInterval}, nil
	}

	logger.Error(cleanupErr, "cleanup did not complete within the bound; "+
		"removing the finalizer anyway so deletion is not stuck on it",
		"bound", cleanupTimeout)
	r.event(ext, corev1.EventTypeWarning, "CleanupIncomplete", "%s", fmt.Sprintf(
		"Cleanup of Rancher/Helm resources did not finish within %s: %v. The "+
			"finalizer was removed so this InstallAIExtension can finish deleting; "+
			"check for leftover ClusterRepo, UIPlugin, or Helm release objects "+
			"named %q.", cleanupTimeout, cleanupErr, ext.Spec.Extension.Name))

	r.clearWaitingSince(ext, annotationCleanupWaitingSince)
	controllerutil.RemoveFinalizer(ext, finalizerName)
	if err := r.Update(ctx, ext); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

// extensionNames returns the extension name(s) this CR currently has state
// for: the spec's current name, plus the previously-active one if a rename
// hasn't finished cleaning up yet. Shared by cleanup (deleting everything on
// CR deletion) and reapOrphanedClusterRepo (deleting just the ClusterRepo
// when the extension namespace disappears) — both need the same pair, since a
// rename in flight leaves state under both names until the old one is swept.
func extensionNames(ext *v1alpha1.InstallAIExtension) []string {
	names := []string{ext.Spec.Extension.Name}
	if ext.Status.ActiveExtensionName != "" && ext.Status.ActiveExtensionName != ext.Spec.Extension.Name {
		names = append(names, ext.Status.ActiveExtensionName)
	}
	return names
}

func (r *InstallAIExtensionReconciler) cleanup(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
) error {
	logger := log.FromContext(ctx)
	namespace := r.ExtensionNamespace
	var errs []error

	names := extensionNames(ext)
	for _, name := range names {
		if name == "" {
			continue
		}
		if err := r.rancherMgr.DeleteClusterRepo(ctx, rancher.ClusterRepoName(name)); err != nil {
			errs = append(errs, err)
		}
		if err := r.rancherMgr.DeleteUIPlugin(ctx, name, namespace); err != nil {
			errs = append(errs, err)
		}
	}

	if ext.Status.HelmReleaseName != "" {
		logger.Info("uninstalling Helm release", "release", ext.Status.HelmReleaseName)
		// helmFor, not a fresh client: DeleteRelease drops the release's
		// convergence verdict, and it has to drop it on the client that holds it.
		// A throwaway would clear its own empty map and leave the real one behind.
		helm, err := r.helmFor(namespace)
		if err == nil {
			if err := helm.DeleteRelease(ctx, ext.Status.HelmReleaseName); err != nil {
				errs = append(errs, err)
			}
		} else {
			errs = append(errs, err)
		}
	}

	if ext.Status.ActiveSourceKind == v1alpha1.ExtensionSourceKindGit ||
		ext.Spec.Source.Kind == v1alpha1.ExtensionSourceKindGit {
		for _, name := range names {
			if name == ext.Status.HelmReleaseName {
				continue
			}
			logger.Info("uninstalling UIPlugin Helm release", "release", name)
			helm, err := r.helmFor(namespace)
			if err == nil {
				if err := helm.DeleteRelease(ctx, name); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	return stderrors.Join(errs...)
}
