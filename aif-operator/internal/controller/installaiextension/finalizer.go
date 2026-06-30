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

const finalizerName = "ai-platform.suse.com/finalizer"

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
	var errs []error

	names := []string{ext.Spec.Extension.Name}
	if ext.Status.ActiveExtensionName != "" && ext.Status.ActiveExtensionName != ext.Spec.Extension.Name {
		names = append(names, ext.Status.ActiveExtensionName)
	}

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
		helm, err := newHelmClientForNamespace(namespace)
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
			helm, err := newHelmClientForNamespace(namespace)
			if err == nil {
				if err := helm.DeleteRelease(ctx, name); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	return stderrors.Join(errs...)
}
