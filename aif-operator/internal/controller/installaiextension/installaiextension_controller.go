package controller

import (
	"context"
	stderrors "errors"
	"fmt"
	"path"
	"strings"
	"time"

	urlpkg "net/url"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/infra/kubernetes"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
	"github.com/SUSE/aif-operator/internal/installaiextension"
)

const (
	defaultReadinessTimeout = 5 * time.Minute
	readinessRequeue        = 10 * time.Second
	healthCheckInterval     = 60 * time.Second

	conditionTypeReady           = "Ready"
	conditionTypeHelmInstalled   = "HelmInstalled"
	conditionTypeDeploymentReady = "DeploymentReady"
	conditionTypeServiceReady    = "ServiceReady"
	conditionTypeClusterRepo     = "ClusterRepoReady"
	conditionTypeUIPlugin        = "UIPluginReady"
)

type InstallAIExtensionReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	ExtensionNamespace string
	ReadinessTimeout   time.Duration
	rancherMgr         *rancher.Manager
}

// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=installaiextensions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=installaiextensions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai-platform.suse.com,resources=installaiextensions/finalizers,verbs=update
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

func (r *InstallAIExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ext v1alpha1.InstallAIExtension
	if err := r.Get(ctx, req.NamespacedName, &ext); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !ext.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &ext)
	}

	added, err := r.ensureFinalizer(ctx, &ext)
	if err != nil {
		return ctrl.Result{}, err
	}
	if added {
		return ctrl.Result{Requeue: true}, nil
	}

	if ext.Status.Phase == "" || ext.Status.Phase == v1alpha1.InstallAIExtensionPhasePending {
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalling
		if err := r.Status().Update(ctx, &ext); err != nil {
			logger.Error(err, "failed to flush initial status")
			return ctrl.Result{}, err
		}
	}

	result, reconcileErr := r.reconcile(ctx, &ext)

	if reconcileErr == nil && ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseInstalled {
		ext.Status.ObservedGeneration = ext.Generation
	}
	if err := r.Status().Update(ctx, &ext); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return result, reconcileErr
}

func (r *InstallAIExtensionReconciler) reconcile(ctx context.Context, ext *v1alpha1.InstallAIExtension) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	namespace := r.ExtensionNamespace

	ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalling

	if err := r.cleanupStaleResources(ctx, ext, namespace); err != nil {
		logger.Error(err, "stale resource cleanup failed, retrying")
		return ctrl.Result{}, err
	}

	if err := r.rancherMgr.CheckCRDs(ctx, []string{
		"uiplugins.catalog.cattle.io",
		"clusterrepos.catalog.cattle.io",
	}); err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"CRDsMissing", fmt.Sprintf("Rancher CRDs not found: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	switch ext.Spec.Source.Kind {
	case v1alpha1.ExtensionSourceKindHelm:
		if result, err := r.reconcileHelmSource(ctx, ext, namespace); err != nil || !result.IsZero() {
			return result, err
		}
	case v1alpha1.ExtensionSourceKindGit:
		if result, err := r.reconcileGitSource(ctx, ext, namespace); err != nil || !result.IsZero() {
			return result, err
		}
	default:
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"InvalidSpec", fmt.Sprintf("unsupported source kind: %s", ext.Spec.Source.Kind), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	if ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseFailed {
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionTrue,
		"Installed", "Extension installed successfully", ext.Generation)
	ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseInstalled
	ext.Status.ActiveExtensionName = ext.Spec.Extension.Name
	ext.Status.ActiveSourceKind = ext.Spec.Source.Kind

	logger.Info("reconciled successfully")
	return ctrl.Result{RequeueAfter: healthCheckInterval}, nil
}

func (r *InstallAIExtensionReconciler) reconcileHelmSource(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	namespace string,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	helmSource := ext.Spec.Source.Helm
	if helmSource == nil {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"InvalidSpec", "source.kind is Helm but source.helm is not set", ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	releaseName := deriveReleaseName(helmSource.ChartURL)

	if ext.Status.HelmReleaseName != "" && ext.Status.HelmReleaseName != releaseName {
		logger.Info("chart URL changed, uninstalling old release", "old", ext.Status.HelmReleaseName, "new", releaseName)
		helm, err := newHelmClientForNamespace(namespace)
		if err == nil {
			_ = helm.DeleteRelease(ctx, ext.Status.HelmReleaseName)
		}
	}

	values, err := helmClient.ConvertHelmValues(helmSource.Values)
	if err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"InvalidSpec", fmt.Sprintf("invalid helm values: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	u, err := urlpkg.Parse(helmSource.ChartURL)
	if err != nil || (u.Scheme != "oci" && u.Scheme != "https") {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"InvalidSpec", fmt.Sprintf("unsupported chart URL: %s", helmSource.ChartURL), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	helm, err := newHelmClientForNamespace(namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := helm.EnsureRelease(ctx, helmClient.ReleaseSpec{
		Name:      releaseName,
		Namespace: namespace,
		ChartRef:  helmSource.ChartURL,
		Version:   helmSource.Version,
		Values:    values,
	}); err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeHelmInstalled, metav1.ConditionFalse,
			"InstallFailed", fmt.Sprintf("Helm install failed: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeHelmInstalled, metav1.ConditionTrue,
		"Installed", fmt.Sprintf("Helm release %s installed", releaseName), ext.Generation)
	ext.Status.HelmReleaseName = releaseName

	releaseInfo, err := helm.GetRelease(ctx, releaseName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if releaseInfo != nil {
		ext.Status.HelmReleaseRevision = int32(releaseInfo.Revision)
	}

	deployStatus, err := kubernetes.IsDeploymentReady(ctx, r.Client, namespace, releaseName, logger)
	if err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeDeploymentReady, metav1.ConditionFalse,
			"CheckFailed", fmt.Sprintf("Failed to check deployment readiness: %v", err), ext.Generation)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}
	if !deployStatus.Ready {
		waitingSince := r.getWaitingSince(ext)
		if waitingSince.IsZero() {
			r.setWaitingSince(ext)
			if err := r.Update(ctx, ext); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if time.Since(waitingSince) > r.ReadinessTimeout {
			msg := fmt.Sprintf("Deployment not ready after %s: %s", r.ReadinessTimeout, deployStatus.Message)
			setCondition(&ext.Status.Conditions, conditionTypeDeploymentReady, metav1.ConditionFalse,
				"TimedOut", msg, ext.Generation)
			ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
			return ctrl.Result{}, nil
		}
		setCondition(&ext.Status.Conditions, conditionTypeDeploymentReady, metav1.ConditionFalse,
			"NotReady", deployStatus.Message, ext.Generation)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}

	if r.getWaitingSince(ext) != (time.Time{}) {
		r.clearWaitingSince(ext)
		if err := r.Update(ctx, ext); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeDeploymentReady, metav1.ConditionTrue,
		"Available", deployStatus.Message, ext.Generation)

	svc, err := kubernetes.ServiceForHelmRelease(ctx, r.Client, namespace, releaseName)
	if err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeServiceReady, metav1.ConditionFalse,
			"ServiceFailed", fmt.Sprintf("Service not found: %v", err), ext.Generation)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}

	svcName, svcNamespace, svcPort, err := installaiextension.ServiceEndpoint(svc)
	if err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeServiceReady, metav1.ConditionFalse,
			"ServiceFailed", fmt.Sprintf("Service endpoint error: %v", err), ext.Generation)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}

	svcURL := fmt.Sprintf("http://%s.%s:%d", svcName, svcNamespace, svcPort)
	setCondition(&ext.Status.Conditions, conditionTypeServiceReady, metav1.ConditionTrue,
		"Available", fmt.Sprintf("Service URL: %s", svcURL), ext.Generation)

	if err := r.rancherMgr.EnsureClusterRepo(ctx, ext, svcURL); err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeClusterRepo, metav1.ConditionFalse,
			"Failed", fmt.Sprintf("ClusterRepo failed: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeClusterRepo, metav1.ConditionTrue,
		"Created", "ClusterRepo created", ext.Generation)

	if err := r.rancherMgr.EnsureUIPlugin(ctx, ext, svcURL, namespace); err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeUIPlugin, metav1.ConditionFalse,
			"Failed", fmt.Sprintf("UIPlugin failed: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeUIPlugin, metav1.ConditionTrue,
		"Created", "UIPlugin created", ext.Generation)

	return ctrl.Result{}, nil
}

func (r *InstallAIExtensionReconciler) reconcileGitSource(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	namespace string,
) (ctrl.Result, error) {
	gitSource := ext.Spec.Source.Git
	if gitSource == nil {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"InvalidSpec", "source.kind is Git but source.git is not set", ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	rawBaseURL, err := rancher.GitRawBaseURL(gitSource.Repo, gitSource.Branch)
	if err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"InvalidSpec", fmt.Sprintf("invalid git repo URL: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	if err := r.rancherMgr.EnsureClusterRepo(ctx, ext, ""); err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeClusterRepo, metav1.ConditionFalse,
			"Failed", fmt.Sprintf("ClusterRepo failed: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeClusterRepo, metav1.ConditionTrue,
		"Created", "ClusterRepo created for git source", ext.Generation)

	if err := r.ensureUIPluginGit(ctx, ext, rawBaseURL, namespace); err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeUIPlugin, metav1.ConditionFalse,
			"Failed", fmt.Sprintf("UIPlugin install failed: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeUIPlugin, metav1.ConditionTrue,
		"Created", "UIPlugin installed from git source", ext.Generation)

	return ctrl.Result{}, nil
}

func (r *InstallAIExtensionReconciler) ensureUIPluginGit(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	repoURL string,
	namespace string,
) error {
	helm, err := newHelmClientForNamespace(namespace)
	if err != nil {
		return err
	}

	info, err := helm.GetRelease(ctx, ext.Spec.Extension.Name)
	if err != nil {
		return fmt.Errorf("failed to check UIPlugin release %q: %w", ext.Spec.Extension.Name, err)
	}
	if info != nil && info.Version == ext.Spec.Extension.Version {
		return nil
	}

	return helm.EnsureRelease(ctx, helmClient.ReleaseSpec{
		Name:      ext.Spec.Extension.Name,
		Namespace: namespace,
		ChartRef:  ext.Spec.Extension.Name,
		RepoURL:   repoURL,
		Version:   ext.Spec.Extension.Version,
	})
}

func (r *InstallAIExtensionReconciler) cleanupStaleResources(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	namespace string,
) error {
	logger := log.FromContext(ctx)
	var errs []error

	oldName := ext.Status.ActiveExtensionName
	newName := ext.Spec.Extension.Name
	oldSource := ext.Status.ActiveSourceKind
	newSource := ext.Spec.Source.Kind

	if oldName != "" && oldName != newName {
		logger.Info("extension name changed, cleaning up old resources", "old", oldName, "new", newName)

		if err := r.rancherMgr.DeleteClusterRepo(ctx, rancher.ClusterRepoName(oldName)); err != nil {
			errs = append(errs, err)
		}
		if err := r.rancherMgr.DeleteUIPlugin(ctx, oldName, namespace); err != nil {
			errs = append(errs, err)
		}

		if oldSource == v1alpha1.ExtensionSourceKindHelm && ext.Status.HelmReleaseName != "" {
			helm, err := newHelmClientForNamespace(namespace)
			if err == nil {
				if err := helm.DeleteRelease(ctx, ext.Status.HelmReleaseName); err != nil {
					errs = append(errs, err)
				}
			}
			ext.Status.HelmReleaseName = ""
			ext.Status.HelmReleaseRevision = 0
		}
		if oldSource == v1alpha1.ExtensionSourceKindGit {
			helm, err := newHelmClientForNamespace(namespace)
			if err == nil {
				_ = helm.DeleteRelease(ctx, oldName)
			}
		}
	}

	if oldSource != "" && oldSource != newSource {
		logger.Info("source kind changed, cleaning up old source resources", "old", oldSource, "new", newSource)

		name := oldName
		if name == "" {
			name = newName
		}

		if err := r.rancherMgr.DeleteClusterRepo(ctx, rancher.ClusterRepoName(name)); err != nil {
			errs = append(errs, err)
		}
		if err := r.rancherMgr.DeleteUIPlugin(ctx, name, namespace); err != nil {
			errs = append(errs, err)
		}

		if oldSource == v1alpha1.ExtensionSourceKindHelm && ext.Status.HelmReleaseName != "" {
			helm, err := newHelmClientForNamespace(namespace)
			if err == nil {
				if err := helm.DeleteRelease(ctx, ext.Status.HelmReleaseName); err != nil {
					errs = append(errs, err)
				}
			}
			ext.Status.HelmReleaseName = ""
			ext.Status.HelmReleaseRevision = 0

			meta.RemoveStatusCondition(&ext.Status.Conditions, conditionTypeHelmInstalled)
			meta.RemoveStatusCondition(&ext.Status.Conditions, conditionTypeDeploymentReady)
			meta.RemoveStatusCondition(&ext.Status.Conditions, conditionTypeServiceReady)
		}

		if oldSource == v1alpha1.ExtensionSourceKindGit {
			helm, err := newHelmClientForNamespace(namespace)
			if err == nil {
				_ = helm.DeleteRelease(ctx, name)
			}
		}
	}

	return stderrors.Join(errs...)
}

func deriveReleaseName(chartURL string) string {
	u, err := urlpkg.Parse(chartURL)
	if err != nil {
		return strings.TrimSuffix(path.Base(chartURL), "-server") + "-server"
	}
	base := path.Base(u.Path)
	return strings.TrimSuffix(base, "-server") + "-server"
}

func setCondition(conditions *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func newHelmClientForNamespace(namespace string) (helmClient.HelmClient, error) {
	settings := cli.New()
	settings.SetNamespace(namespace)
	return helmClient.New(settings)
}

const annotationWaitingSince = "ai-platform.suse.com/waiting-since"

func (r *InstallAIExtensionReconciler) getWaitingSince(ext *v1alpha1.InstallAIExtension) time.Time {
	if ext.Annotations == nil {
		return time.Time{}
	}
	ts, ok := ext.Annotations[annotationWaitingSince]
	if !ok {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (r *InstallAIExtensionReconciler) setWaitingSince(ext *v1alpha1.InstallAIExtension) {
	if ext.Annotations == nil {
		ext.Annotations = make(map[string]string)
	}
	ext.Annotations[annotationWaitingSince] = time.Now().Format(time.RFC3339)
}

func (r *InstallAIExtensionReconciler) clearWaitingSince(ext *v1alpha1.InstallAIExtension) {
	if ext.Annotations != nil {
		delete(ext.Annotations, annotationWaitingSince)
	}
}

func (r *InstallAIExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.ReadinessTimeout == 0 {
		r.ReadinessTimeout = defaultReadinessTimeout
	}
	r.rancherMgr = rancher.NewManager(r.Client)
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.InstallAIExtension{}).
		Named("InstallAIExtension").
		Complete(r)
}
