package controller

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/internal/infra/rancher"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/helm"
)

const (
	extensionFinalizerName = "ai.suse.com/cleanup"
	helmInstallTimeout     = 5 * time.Minute
	readinessRequeue       = 10 * time.Second
	healthCheckInterval    = 60 * time.Second
)

// InstallAIExtensionReconciler reconciles an InstallAIExtension object.
type InstallAIExtensionReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	HelmEngine helm.Engine
	Catalog    rancher.CatalogManager
	Recorder   events.EventRecorder
}

// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions/finalizers,verbs=update
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=uiplugins,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *InstallAIExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ext aifv1.InstallAIExtension
	if err := r.Get(ctx, req.NamespacedName, &ext); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !ext.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &ext)
	}

	if !controllerutil.ContainsFinalizer(&ext, extensionFinalizerName) {
		controllerutil.AddFinalizer(&ext, extensionFinalizerName)
		if err := r.Update(ctx, &ext); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Flush Phase=Installing immediately so the user sees progress before any blocking operations
	if ext.Status.Phase == "" || ext.Status.Phase == aifv1.InstallAIExtensionPhasePending {
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseInstalling
		if err := r.Status().Update(ctx, &ext); err != nil {
			logger.Error(err, "failed to flush initial status")
			return ctrl.Result{}, err
		}
	}

	result, reconcileErr := r.reconcile(ctx, &ext)

	ext.Status.ObservedGeneration = ext.Generation
	if err := r.Status().Update(ctx, &ext); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return result, reconcileErr
}

func (r *InstallAIExtensionReconciler) reconcile(ctx context.Context, ext *aifv1.InstallAIExtension) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	ext.Status.Phase = aifv1.InstallAIExtensionPhaseInstalling

	// Step 0: Clean up resources from previous spec if name or source changed
	if err := r.cleanupStaleResources(ctx, ext); err != nil {
		r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonReconcileFailed, conditions.ActionReconciling, "stale resource cleanup incomplete: %v", err)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}

	// Step 1: Check Rancher CRDs exist
	if err := r.Catalog.CheckCRDs(ctx); err != nil {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginCRDMissing,
			Message:            fmt.Sprintf("Rancher CRDs not found: %v", err),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonUIPluginCRDMissing, conditions.ActionChecking, err.Error())
		return ctrl.Result{}, nil
	}

	// Step 2: Source-specific reconciliation
	switch ext.Spec.Source.Kind {
	case aifv1.ExtensionSourceKindHelm:
		if result, err := r.reconcileHelmSource(ctx, ext); err != nil || !result.IsZero() {
			return result, err
		}
	case aifv1.ExtensionSourceKindGit:
		if result, err := r.reconcileGitSource(ctx, ext); err != nil || !result.IsZero() {
			return result, err
		}
	default:
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonInvalidSpec,
			Message:            fmt.Sprintf("unsupported source kind: %s", ext.Spec.Source.Kind),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	// Guard: source reconcilers set Phase=Failed and return zero Result
	// to avoid blocking retry loops. Don't overwrite with success.
	if ext.Status.Phase == aifv1.InstallAIExtensionPhaseFailed {
		return ctrl.Result{}, nil
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonInstalled,
		Message:            "Extension installed successfully",
		ObservedGeneration: ext.Generation,
	})
	ext.Status.Phase = aifv1.InstallAIExtensionPhaseInstalled
	ext.Status.ActiveExtensionName = ext.Spec.Extension.Name
	ext.Status.ActiveSourceKind = ext.Spec.Source.Kind
	r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonInstalled, conditions.ActionInstalling, "Extension installed successfully")

	logger.Info("reconciled successfully")
	return ctrl.Result{RequeueAfter: healthCheckInterval}, nil
}

func (r *InstallAIExtensionReconciler) reconcileHelmSource(ctx context.Context, ext *aifv1.InstallAIExtension) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	helmSource := ext.Spec.Source.Helm
	if helmSource == nil {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonInvalidSpec,
			Message:            "source.kind is Helm but source.helm is not set",
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	releaseName := rancher.DeriveReleaseName(helmSource.ChartURL)

	// If the chart URL changed, the release name changes too. Uninstall the
	// old release before installing the new one so it doesn't get orphaned.
	if ext.Status.HelmReleaseName != "" && ext.Status.HelmReleaseName != releaseName {
		logger.Info("chart URL changed, uninstalling old release", "old", ext.Status.HelmReleaseName, "new", releaseName)
		if err := r.HelmEngine.Uninstall(ctx, rancher.UIPluginNamespace, ext.Status.HelmReleaseName); err != nil {
			logger.Error(err, "failed to uninstall old Helm release", "release", ext.Status.HelmReleaseName)
		}
	}

	overrides := helm.Overrides{}
	if len(helmSource.Values) > 0 {
		vals, err := convertValues(helmSource.Values)
		if err != nil {
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonInvalidSpec,
				Message:            fmt.Sprintf("invalid helm values: %v", err),
				ObservedGeneration: ext.Generation,
			})
			ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
			return ctrl.Result{}, nil
		}
		overrides.Workload = vals
	}

	chartRef := fmt.Sprintf("%s:%s", helmSource.ChartURL, helmSource.Version)

	existing, statusErr := r.HelmEngine.Status(ctx, rancher.UIPluginNamespace, releaseName)
	alreadyDeployed := statusErr == nil && existing.Status == "deployed" &&
		ext.Status.ObservedGeneration == ext.Generation

	if alreadyDeployed {
		logger.Info("Helm release already deployed, skipping install", "release", releaseName, "revision", existing.Revision)
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeHelmInstalled,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.ReasonInstalled,
			Message:            fmt.Sprintf("Helm release %s revision %d", existing.Name, existing.Revision),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.HelmReleaseName = releaseName
	} else {
		installReq := helm.InstallRequest{
			Namespace:   rancher.UIPluginNamespace,
			ReleaseName: releaseName,
			ChartRef:    chartRef,
			Overrides:   overrides,
			Wait:        true,
			Timeout:     helmInstallTimeout,
		}

		action := "Installing"
		if existing.Revision > 0 {
			action = "Upgrading"
		}
		logger.Info(action+" Helm chart", "chartRef", chartRef, "release", releaseName)
		r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonHelmInstallStarted, conditions.ActionInstalling, "%s Helm chart %s", action, chartRef)

		status, err := r.HelmEngine.InstallChartFromRepo(ctx, installReq)
		if err != nil {
			msg := fmt.Sprintf("Helm install failed: %v", err)
			if ds, diagErr := r.checkDeploymentReady(ctx, releaseName); diagErr == nil && !ds.Ready && ds.Message != "" {
				msg = fmt.Sprintf("Helm install failed: %s", ds.Message)
			}
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeHelmInstalled,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonInstallFailed,
				Message:            msg,
				ObservedGeneration: ext.Generation,
			})
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonInstallFailed,
				Message:            msg,
				ObservedGeneration: ext.Generation,
			})
			ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
			r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonInstallFailed, conditions.ActionInstalling, msg)
			return ctrl.Result{}, nil
		}

		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeHelmInstalled,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.ReasonInstalled,
			Message:            fmt.Sprintf("Helm release %s revision %d", status.Name, status.Revision),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.HelmReleaseName = releaseName
		ext.Status.HelmReleaseRevision = int32(status.Revision)
		r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonInstalled, conditions.ActionInstalling, "Helm release %s revision %d deployed", status.Name, status.Revision)
	}

	// Check Deployment readiness
	deployStatus, err := r.checkDeploymentReady(ctx, releaseName)
	if err != nil {
		logger.Error(err, "failed to check deployment readiness")
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}
	if !deployStatus.Ready {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeDeploymentReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonDeploymentCreated,
			Message:            deployStatus.Message,
			ObservedGeneration: ext.Generation,
		})
		r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonDeploymentFailed, conditions.ActionInstalling, deployStatus.Message)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeDeploymentReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonDeploymentAvailable,
		Message:            deployStatus.Message,
		ObservedGeneration: ext.Generation,
	})

	// Discover Service URL
	serviceURL, err := r.discoverServiceURL(ctx, releaseName)
	if err != nil {
		msg := fmt.Sprintf("Service not found: %v", err)
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeServiceReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonServiceFailed,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonServiceFailed, conditions.ActionInstalling, msg)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeServiceReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonServiceCreated,
		Message:            fmt.Sprintf("Service URL: %s", serviceURL),
		ObservedGeneration: ext.Generation,
	})

	// Ensure ClusterRepo pointing to Service URL
	if err := r.Catalog.EnsureClusterRepo(ctx, rancher.ClusterRepoOpts{
		ExtensionName: ext.Spec.Extension.Name,
		CRName:        ext.Name,
		ServiceURL:    serviceURL,
	}); err != nil {
		msg := fmt.Sprintf("ClusterRepo failed: %v", err)
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeClusterRepoReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonClusterRepoFailed,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonClusterRepoFailed,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeClusterRepoReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonClusterRepoCreated,
		Message:            "ClusterRepo created",
		ObservedGeneration: ext.Generation,
	})
	r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonClusterRepoCreated, conditions.ActionCreating, "ClusterRepo %s created pointing to %s", rancher.ClusterRepoName(ext.Spec.Extension.Name), serviceURL)

	// Ensure UIPlugin: fetch metadata from index.yaml, then create
	pluginEndpoint := fmt.Sprintf("%s/plugin/%s-%s", serviceURL, ext.Spec.Extension.Name, ext.Spec.Extension.Version)
	indexURL := serviceURL + "/index.yaml"
	pluginMeta, err := r.Catalog.FetchIndexMetadata(ctx, indexURL, ext.Spec.Extension.Name, ext.Spec.Extension.Version)
	if err != nil {
		if stderrors.Is(err, helm.ErrChartNotFound) || stderrors.Is(err, helm.ErrVersionNotFound) {
			msg := fmt.Sprintf("extension name/version mismatch: %v", err)
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeUIPluginReady,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonInvalidSpec,
				Message:            msg,
				ObservedGeneration: ext.Generation,
			})
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonInvalidSpec,
				Message:            msg,
				ObservedGeneration: ext.Generation,
			})
			ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
			r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonInvalidSpec, conditions.ActionCreating, msg)
			return ctrl.Result{}, nil
		}
		logger.Info("failed to fetch index metadata, creating UIPlugin without metadata", "error", err)
		pluginMeta = rancher.PluginMetadata{}
	}

	if err := r.Catalog.EnsureUIPlugin(ctx, rancher.UIPluginOpts{
		ExtensionName:    ext.Spec.Extension.Name,
		ExtensionVersion: ext.Spec.Extension.Version,
		CRName:           ext.Name,
		Endpoint:         pluginEndpoint,
		Metadata:         pluginMeta,
	}); err != nil {
		msg := fmt.Sprintf("UIPlugin creation failed: %v", err)
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeUIPluginReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginNotCreated,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginNotCreated,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonUIPluginNotCreated, conditions.ActionCreating, err.Error())
		return ctrl.Result{}, nil
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeUIPluginReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonUIPluginVerified,
		Message:            "UIPlugin created",
		ObservedGeneration: ext.Generation,
	})
	r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonUIPluginVerified, conditions.ActionCreating, "UIPlugin %s created", ext.Spec.Extension.Name)

	return ctrl.Result{}, nil
}

func (r *InstallAIExtensionReconciler) reconcileGitSource(ctx context.Context, ext *aifv1.InstallAIExtension) (ctrl.Result, error) {
	gitSource := ext.Spec.Source.Git
	if gitSource == nil {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonInvalidSpec,
			Message:            "source.kind is Git but source.git is not set",
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	if err := r.Catalog.EnsureClusterRepoGit(ctx, rancher.ClusterRepoGitOpts{
		ExtensionName: ext.Spec.Extension.Name,
		CRName:        ext.Name,
		RepoURL:       gitSource.Repo,
		Branch:        gitSource.Branch,
	}); err != nil {
		msg := fmt.Sprintf("ClusterRepo failed: %v", err)
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeClusterRepoReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonClusterRepoFailed,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonClusterRepoFailed,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeClusterRepoReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonClusterRepoCreated,
		Message:            "ClusterRepo created for git source",
		ObservedGeneration: ext.Generation,
	})
	r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonClusterRepoCreated, conditions.ActionCreating, "ClusterRepo %s created for git source %s", rancher.ClusterRepoName(ext.Spec.Extension.Name), gitSource.Repo)

	rawURL, err := rancher.GitRepoToRawURL(gitSource.Repo, gitSource.Branch)
	if err != nil {
		msg := fmt.Sprintf("cannot derive raw URL: %v", err)
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeUIPluginReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginNotCreated,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginNotCreated,
			Message:            msg,
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	releaseName := ext.Spec.Extension.Name
	existing, statusErr := r.HelmEngine.Status(ctx, rancher.UIPluginNamespace, releaseName)
	alreadyDeployed := statusErr == nil && existing.Status == "deployed" &&
		ext.Status.ObservedGeneration == ext.Generation

	if alreadyDeployed {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeUIPluginReady,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.ReasonUIPluginVerified,
			Message:            fmt.Sprintf("UIPlugin release %s revision %d", existing.Name, existing.Revision),
			ObservedGeneration: ext.Generation,
		})
	} else {
		if err := r.ensureUIPluginGit(ctx, ext, rawURL); err != nil {
			msg := fmt.Sprintf("UIPlugin chart install failed: %v", err)
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeUIPluginReady,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonUIPluginNotCreated,
				Message:            msg,
				ObservedGeneration: ext.Generation,
			})
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonUIPluginNotCreated,
				Message:            msg,
				ObservedGeneration: ext.Generation,
			})
			ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
			r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonUIPluginNotCreated, conditions.ActionInstalling, err.Error())
			return ctrl.Result{}, nil
		}

		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeUIPluginReady,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.ReasonUIPluginVerified,
			Message:            "UIPlugin installed via Helm from git source",
			ObservedGeneration: ext.Generation,
		})
		r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonUIPluginVerified, conditions.ActionInstalling, "UIPlugin %s installed from git source", ext.Spec.Extension.Name)
	}

	return ctrl.Result{}, nil
}

func (r *InstallAIExtensionReconciler) handleDeletion(ctx context.Context, ext *aifv1.InstallAIExtension) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(ext, extensionFinalizerName) {
		return ctrl.Result{}, nil
	}

	if err := r.cleanup(ctx, ext); err != nil {
		logger.Error(err, "cleanup failed")
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(ext, extensionFinalizerName)
	if err := r.Update(ctx, ext); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("deleted successfully")
	return ctrl.Result{}, nil
}

func (r *InstallAIExtensionReconciler) cleanupStaleResources(ctx context.Context, ext *aifv1.InstallAIExtension) error {
	logger := log.FromContext(ctx)
	var errs []error

	oldName := ext.Status.ActiveExtensionName
	newName := ext.Spec.Extension.Name
	oldSource := ext.Status.ActiveSourceKind
	newSource := ext.Spec.Source.Kind

	if oldName != "" && oldName != newName {
		logger.Info("extension name changed, cleaning up old resources", "old", oldName, "new", newName)

		if err := r.Catalog.DeleteClusterRepo(ctx, oldName); err != nil {
			logger.Error(err, "failed to delete old ClusterRepo", "name", oldName)
			errs = append(errs, err)
		}
		if err := r.Catalog.DeleteUIPlugin(ctx, oldName); err != nil {
			logger.Error(err, "failed to delete old UIPlugin", "name", oldName)
			errs = append(errs, err)
		}

		if oldSource == aifv1.ExtensionSourceKindHelm && ext.Status.HelmReleaseName != "" {
			if err := r.HelmEngine.Uninstall(ctx, rancher.UIPluginNamespace, ext.Status.HelmReleaseName); err != nil {
				logger.Error(err, "failed to uninstall old Helm release", "release", ext.Status.HelmReleaseName)
				errs = append(errs, err)
			}
			ext.Status.HelmReleaseName = ""
			ext.Status.HelmReleaseRevision = 0
		}
		if oldSource == aifv1.ExtensionSourceKindGit {
			if err := r.HelmEngine.Uninstall(ctx, rancher.UIPluginNamespace, oldName); err != nil {
				logger.Error(err, "failed to uninstall old UIPlugin Helm release", "release", oldName)
				errs = append(errs, err)
			}
		}
	}

	if oldSource != "" && oldSource != newSource {
		logger.Info("source kind changed, cleaning up old source resources", "old", oldSource, "new", newSource)

		// Delete ClusterRepo and UIPlugin from the old source so the new
		// source can recreate them cleanly (different backing: URL-based
		// vs git-based ClusterRepo, Helm-managed vs unstructured UIPlugin).
		name := oldName
		if name == "" {
			name = newName
		}
		if name != "" {
			if err := r.Catalog.DeleteClusterRepo(ctx, name); err != nil {
				logger.Error(err, "failed to delete ClusterRepo on source switch", "name", name)
				errs = append(errs, err)
			}
			if err := r.Catalog.DeleteUIPlugin(ctx, name); err != nil {
				logger.Error(err, "failed to delete UIPlugin on source switch", "name", name)
				errs = append(errs, err)
			}
		}

		if oldSource == aifv1.ExtensionSourceKindHelm {
			if ext.Status.HelmReleaseName != "" {
				if err := r.HelmEngine.Uninstall(ctx, rancher.UIPluginNamespace, ext.Status.HelmReleaseName); err != nil {
					logger.Error(err, "failed to uninstall Helm release on source switch", "release", ext.Status.HelmReleaseName)
					errs = append(errs, err)
				}
				ext.Status.HelmReleaseName = ""
				ext.Status.HelmReleaseRevision = 0
			}

			meta.RemoveStatusCondition(&ext.Status.Conditions, conditions.TypeHelmInstalled)
			meta.RemoveStatusCondition(&ext.Status.Conditions, conditions.TypeDeploymentReady)
			meta.RemoveStatusCondition(&ext.Status.Conditions, conditions.TypeServiceReady)
		}

		if oldSource == aifv1.ExtensionSourceKindGit {
			if err := r.HelmEngine.Uninstall(ctx, rancher.UIPluginNamespace, name); err != nil {
				logger.Error(err, "failed to uninstall UIPlugin Helm release on source switch", "release", name)
				errs = append(errs, err)
			}
		}
	}

	return stderrors.Join(errs...)
}

func (r *InstallAIExtensionReconciler) cleanup(ctx context.Context, ext *aifv1.InstallAIExtension) error {
	logger := log.FromContext(ctx)
	var errs []error

	names := []string{ext.Spec.Extension.Name}
	if ext.Status.ActiveExtensionName != "" && ext.Status.ActiveExtensionName != ext.Spec.Extension.Name {
		names = append(names, ext.Status.ActiveExtensionName)
	}

	for _, name := range names {
		if name == "" {
			logger.Info("skipping cleanup for empty extension name")
			continue
		}
		if err := r.Catalog.DeleteClusterRepo(ctx, name); err != nil {
			logger.Error(err, "failed to delete ClusterRepo", "name", name)
			errs = append(errs, err)
		}
		if err := r.Catalog.DeleteUIPlugin(ctx, name); err != nil {
			logger.Error(err, "failed to delete UIPlugin", "name", name)
			errs = append(errs, err)
		}
	}

	if ext.Status.HelmReleaseName != "" {
		logger.Info("uninstalling Helm release", "release", ext.Status.HelmReleaseName)
		if err := r.HelmEngine.Uninstall(ctx, rancher.UIPluginNamespace, ext.Status.HelmReleaseName); err != nil {
			logger.Error(err, "failed to uninstall Helm release", "release", ext.Status.HelmReleaseName)
			errs = append(errs, err)
		}
	}

	// Git mode installs the UIPlugin chart as a Helm release named after the
	// extension. Helm mode uses Catalog.EnsureUIPlugin instead (no per-name release).
	if ext.Status.ActiveSourceKind == aifv1.ExtensionSourceKindGit || ext.Spec.Source.Kind == aifv1.ExtensionSourceKindGit {
		for _, name := range names {
			if name == ext.Status.HelmReleaseName {
				continue
			}
			logger.Info("uninstalling UIPlugin Helm release", "release", name)
			if err := r.HelmEngine.Uninstall(ctx, rancher.UIPluginNamespace, name); err != nil {
				logger.Error(err, "failed to uninstall UIPlugin Helm release", "release", name)
				errs = append(errs, err)
			}
		}
	}

	return stderrors.Join(errs...)
}

// ensureUIPluginGit installs the UIPlugin chart from a git-based Helm repository.
func (r *InstallAIExtensionReconciler) ensureUIPluginGit(ctx context.Context, ext *aifv1.InstallAIExtension, repoURL string) error {
	_, err := r.HelmEngine.InstallFromRepoURL(ctx, helm.InstallFromRepoURLRequest{
		Namespace:   rancher.UIPluginNamespace,
		ReleaseName: ext.Spec.Extension.Name,
		ChartName:   ext.Spec.Extension.Name,
		RepoURL:     repoURL,
		Version:     ext.Spec.Extension.Version,
		Wait:        true,
		Timeout:     helmInstallTimeout,
	})
	return err
}

func (r *InstallAIExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.InstallAIExtension{}).
		Complete(r)
}

func convertValues(values map[string]apiextensionsv1.JSON) (map[string]any, error) {
	result := make(map[string]any, len(values))
	for k, v := range values {
		var parsed any
		if err := json.Unmarshal(v.Raw, &parsed); err != nil {
			return nil, fmt.Errorf("invalid value for key %q: %w", k, err)
		}
		result[k] = parsed
	}
	return result, nil
}
