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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

const finalizerName = "ai-factory.suse.com/finalizer"

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
		logger.Error(err, "cleanup failed")
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(ext, finalizerName)
	if err := r.Update(ctx, ext); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("deleted successfully")
	return ctrl.Result{}, nil
}

func (r *InstallAIExtensionReconciler) cleanup(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
) error {
	logger := log.FromContext(ctx)
	namespace := r.ExtensionNamespace

	names := []string{ext.Spec.Extension.Name}
	if ext.Status.ActiveExtensionName != "" && ext.Status.ActiveExtensionName != ext.Spec.Extension.Name {
		names = append(names, ext.Status.ActiveExtensionName)
	}
	errs := make([]error, 0, len(names)+1)

	for _, name := range names {
		if name == "" {
			continue
		}
		errs = append(errs, r.removeExtension(ctx, name, namespace))
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

	return stderrors.Join(errs...)
}

// removeExtension removes everything installed under an extension name, for
// either source kind: its ClusterRepo, the release of its UI-plugin chart, and
// the UIPlugin.
//
// The UIPlugin is deleted directly as well as through its release. An operator
// release from before the extension chart was installed wrote it without a
// Helm owner, and if the CR is deleted before a reconcile adopts it, nothing
// else removes it. Deleting it after the uninstall makes the call a no-op in
// every other case.
//
// The Helm source's server release is not touched here. It is named after the
// chart URL rather than the extension, and its callers handle it.
func (r *InstallAIExtensionReconciler) removeExtension(ctx context.Context, name, namespace string) error {
	var errs []error

	if err := r.rancherMgr.DeleteClusterRepo(ctx, rancher.ClusterRepoName(name)); err != nil {
		errs = append(errs, err)
	}

	log.FromContext(ctx).Info("uninstalling UIPlugin Helm release", "release", name)
	if helm, err := r.helmFor(namespace); err != nil {
		errs = append(errs, err)
	} else if err := helm.DeleteRelease(ctx, name); err != nil {
		errs = append(errs, err)
	}

	if err := r.rancherMgr.DeleteUIPlugin(ctx, name, namespace); err != nil {
		errs = append(errs, err)
	}

	return stderrors.Join(errs...)
}
