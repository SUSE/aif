package controller

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"github.com/SUSE/aif/pkg/conditions"
	"github.com/SUSE/aif/pkg/helm"
)

const (
	extensionFinalizerName = "ai.suse.com/cleanup"
	uiPluginNamespace      = "cattle-ui-plugin-system"
	helmInstallTimeout     = 5 * time.Minute
	readinessRequeue       = 10 * time.Second
	healthCheckInterval    = 60 * time.Second
)

// uiPluginMeta holds the metadata annotations extracted from a Helm repo index.yaml.
type uiPluginMeta struct {
	DisplayName       string
	RancherVersion    string
	ExtensionsVersion string
}

// InstallAIExtensionReconciler reconciles an InstallAIExtension object.
type InstallAIExtensionReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	HelmEngine helm.Engine
	Discovery  discovery.DiscoveryInterface
	Recorder   events.EventRecorder
	HTTPClient *http.Client
}

// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai.suse.com,resources=installaiextensions/finalizers,verbs=update
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=uiplugins,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *InstallAIExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ext aifv1.InstallAIExtension
	if err := r.Get(ctx, req.NamespacedName, &ext); err != nil {
		if errors.IsNotFound(err) {
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
	r.cleanupStaleResources(ctx, ext)

	// Step 1: Check Rancher CRDs exist
	if err := r.checkRancherCRDs(ctx); err != nil {
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

	// Step 3: UIPlugin is created inside each source flow above.
	// All sub-resources ready — set aggregate Ready=True
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

// reconcileHelmSource handles the Helm source mode: install chart, check readiness, create ClusterRepo.
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

	releaseName := deriveReleaseName(helmSource.ChartURL)
	ext.Status.HelmReleaseName = releaseName

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

	// Install or upgrade Helm chart — skip if already deployed at current generation
	chartRef := fmt.Sprintf("%s:%s", helmSource.ChartURL, helmSource.Version)

	existing, statusErr := r.HelmEngine.Status(ctx, uiPluginNamespace, releaseName)
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
	} else {
		installReq := helm.InstallRequest{
			Namespace:   uiPluginNamespace,
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
			// Enrich with pod-level diagnostics (e.g. ErrImagePull, CrashLoopBackOff)
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
		ext.Status.HelmReleaseRevision = int32(status.Revision)
		r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonInstalled, conditions.ActionInstalling, "Helm release %s revision %d deployed", status.Name, status.Revision)
	}

	// Check Deployment readiness — runs every reconcile for health propagation
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
	if err := r.ensureClusterRepo(ctx, ext, serviceURL); err != nil {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeClusterRepoReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonClusterRepoFailed,
			Message:            fmt.Sprintf("ClusterRepo failed: %v", err),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, err
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeClusterRepoReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonClusterRepoCreated,
		Message:            "ClusterRepo created",
		ObservedGeneration: ext.Generation,
	})
	r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonClusterRepoCreated, conditions.ActionCreating, "ClusterRepo %s created pointing to %s", clusterRepoName(ext.Spec.Extension.Name), serviceURL)

	// Ensure UIPlugin: construct endpoint from Service URL, fetch metadata from index.yaml
	pluginEndpoint := fmt.Sprintf("%s/plugin/%s-%s", serviceURL, ext.Spec.Extension.Name, ext.Spec.Extension.Version)
	indexURL := serviceURL + "/index.yaml"
	meta, err := r.fetchIndexMetadata(ctx, indexURL, ext.Spec.Extension.Name, ext.Spec.Extension.Version)
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
			ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
			r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonInvalidSpec, conditions.ActionCreating, msg)
			return ctrl.Result{}, nil
		}
		logger.Info("failed to fetch index metadata, creating UIPlugin without metadata", "error", err)
		meta = uiPluginMeta{}
	}

	if err := r.ensureUIPluginHelm(ctx, ext, pluginEndpoint, meta); err != nil {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeUIPluginReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginNotCreated,
			Message:            fmt.Sprintf("UIPlugin creation failed: %v", err),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonUIPluginNotCreated, conditions.ActionCreating, err.Error())
		return ctrl.Result{}, err
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

// reconcileGitSource handles the Git source mode: create ClusterRepo pointing to git repo.
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

	if err := r.ensureClusterRepoGit(ctx, ext, gitSource.Repo, gitSource.Branch); err != nil {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeClusterRepoReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonClusterRepoFailed,
			Message:            fmt.Sprintf("ClusterRepo failed: %v", err),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, err
	}

	conditions.Set(&ext.Status.Conditions, metav1.Condition{
		Type:               conditions.TypeClusterRepoReady,
		Status:             metav1.ConditionTrue,
		Reason:             conditions.ReasonClusterRepoCreated,
		Message:            "ClusterRepo created for git source",
		ObservedGeneration: ext.Generation,
	})
	r.Recorder.Eventf(ext, nil, corev1.EventTypeNormal, conditions.ReasonClusterRepoCreated, conditions.ActionCreating, "ClusterRepo %s created for git source %s", clusterRepoName(ext.Spec.Extension.Name), gitSource.Repo)

	// Ensure UIPlugin via Helm install from git repo URL.
	// This allows Rancher Dashboard to manage extension versions.
	rawURL, err := gitRepoToRawURL(gitSource.Repo, gitSource.Branch)
	if err != nil {
		conditions.Set(&ext.Status.Conditions, metav1.Condition{
			Type:               conditions.TypeUIPluginReady,
			Status:             metav1.ConditionFalse,
			Reason:             conditions.ReasonUIPluginNotCreated,
			Message:            fmt.Sprintf("cannot derive raw URL: %v", err),
			ObservedGeneration: ext.Generation,
		})
		ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	releaseName := ext.Spec.Extension.Name
	existing, statusErr := r.HelmEngine.Status(ctx, uiPluginNamespace, releaseName)
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
			conditions.Set(&ext.Status.Conditions, metav1.Condition{
				Type:               conditions.TypeUIPluginReady,
				Status:             metav1.ConditionFalse,
				Reason:             conditions.ReasonUIPluginNotCreated,
				Message:            fmt.Sprintf("UIPlugin chart install failed: %v", err),
				ObservedGeneration: ext.Generation,
			})
			ext.Status.Phase = aifv1.InstallAIExtensionPhaseFailed
			r.Recorder.Eventf(ext, nil, corev1.EventTypeWarning, conditions.ReasonUIPluginNotCreated, conditions.ActionInstalling, err.Error())
			return ctrl.Result{}, err
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

// cleanupStaleResources detects spec changes (extension name or source kind)
// and removes resources created by the previous spec before the new reconcile
// proceeds. This prevents orphaned ClusterRepos, UIPlugins, and Helm releases.
func (r *InstallAIExtensionReconciler) cleanupStaleResources(ctx context.Context, ext *aifv1.InstallAIExtension) {
	logger := log.FromContext(ctx)

	oldName := ext.Status.ActiveExtensionName
	newName := ext.Spec.Extension.Name
	oldSource := ext.Status.ActiveSourceKind
	newSource := ext.Spec.Source.Kind

	// Name change: clean up resources created with the old name
	if oldName != "" && oldName != newName {
		logger.Info("extension name changed, cleaning up old resources", "old", oldName, "new", newName)

		if err := r.deleteClusterRepo(ctx, clusterRepoName(oldName)); err != nil {
			logger.Error(err, "failed to delete old ClusterRepo", "name", clusterRepoName(oldName))
		}
		if err := r.deleteUIPlugin(ctx, oldName); err != nil {
			logger.Error(err, "failed to delete old UIPlugin", "name", oldName)
		}

		if oldSource == aifv1.ExtensionSourceKindHelm && ext.Status.HelmReleaseName != "" {
			if err := r.HelmEngine.Uninstall(ctx, uiPluginNamespace, ext.Status.HelmReleaseName); err != nil {
				logger.Error(err, "failed to uninstall old Helm release", "release", ext.Status.HelmReleaseName)
			}
			ext.Status.HelmReleaseName = ""
			ext.Status.HelmReleaseRevision = 0
		}
		if oldSource == aifv1.ExtensionSourceKindGit {
			if err := r.HelmEngine.Uninstall(ctx, uiPluginNamespace, oldName); err != nil {
				logger.Error(err, "failed to uninstall old UIPlugin Helm release", "release", oldName)
			}
		}
	}

	// Source switch: clean up resources from the old source type
	if oldSource != "" && oldSource != newSource {
		logger.Info("source kind changed, cleaning up old source resources", "old", oldSource, "new", newSource)

		if oldSource == aifv1.ExtensionSourceKindHelm {
			if ext.Status.HelmReleaseName != "" {
				if err := r.HelmEngine.Uninstall(ctx, uiPluginNamespace, ext.Status.HelmReleaseName); err != nil {
					logger.Error(err, "failed to uninstall Helm release on source switch", "release", ext.Status.HelmReleaseName)
				}
				ext.Status.HelmReleaseName = ""
				ext.Status.HelmReleaseRevision = 0
			}

			// Clear conditions the Git flow never sets
			meta.RemoveStatusCondition(&ext.Status.Conditions, conditions.TypeHelmInstalled)
			meta.RemoveStatusCondition(&ext.Status.Conditions, conditions.TypeDeploymentReady)
			meta.RemoveStatusCondition(&ext.Status.Conditions, conditions.TypeServiceReady)
		}

		if oldSource == aifv1.ExtensionSourceKindGit {
			name := oldName
			if name == "" {
				name = newName
			}
			if err := r.HelmEngine.Uninstall(ctx, uiPluginNamespace, name); err != nil {
				logger.Error(err, "failed to uninstall UIPlugin Helm release on source switch", "release", name)
			}
		}
	}
}

func (r *InstallAIExtensionReconciler) cleanup(ctx context.Context, ext *aifv1.InstallAIExtension) error {
	logger := log.FromContext(ctx)

	// Clean up by both current and previously-active names to avoid orphans
	names := []string{ext.Spec.Extension.Name}
	if ext.Status.ActiveExtensionName != "" && ext.Status.ActiveExtensionName != ext.Spec.Extension.Name {
		names = append(names, ext.Status.ActiveExtensionName)
	}

	for _, name := range names {
		if err := r.deleteClusterRepo(ctx, clusterRepoName(name)); err != nil {
			logger.Error(err, "failed to delete ClusterRepo", "name", clusterRepoName(name))
		}
		if err := r.deleteUIPlugin(ctx, name); err != nil {
			logger.Error(err, "failed to delete UIPlugin", "name", name)
		}
	}

	// Uninstall Helm releases from both source types if they exist
	if ext.Status.HelmReleaseName != "" {
		logger.Info("uninstalling Helm release", "release", ext.Status.HelmReleaseName)
		if err := r.HelmEngine.Uninstall(ctx, uiPluginNamespace, ext.Status.HelmReleaseName); err != nil {
			logger.Error(err, "failed to uninstall Helm release", "release", ext.Status.HelmReleaseName)
		}
	}

	for _, name := range names {
		logger.Info("uninstalling UIPlugin Helm release", "release", name)
		if err := r.HelmEngine.Uninstall(ctx, uiPluginNamespace, name); err != nil {
			logger.Error(err, "failed to uninstall UIPlugin Helm release", "release", name)
		}
	}

	return nil
}

// checkRancherCRDs verifies that the Rancher catalog CRDs (UIPlugin, ClusterRepo) exist.
func (r *InstallAIExtensionReconciler) checkRancherCRDs(ctx context.Context) error {
	_, err := r.Discovery.ServerResourcesForGroupVersion("catalog.cattle.io/v1")
	if err != nil {
		return fmt.Errorf("catalog.cattle.io/v1 CRDs not found: %w", err)
	}
	return nil
}

// deploymentStatus holds the readiness state and a human-readable message
// describing why the deployment is not ready (e.g. pod-level errors).
type deploymentStatus struct {
	Ready   bool
	Message string
}

// checkDeploymentReady checks if a Deployment with the given release name label
// is available. When not ready, it inspects pod conditions and container
// statuses to surface actionable detail (ImagePullBackOff, CrashLoopBackOff,
// pending scheduling, etc.).
func (r *InstallAIExtensionReconciler) checkDeploymentReady(ctx context.Context, releaseName string) (deploymentStatus, error) {
	var deploys appsv1.DeploymentList
	selector := labels.SelectorFromSet(map[string]string{
		"app.kubernetes.io/instance": releaseName,
	})
	if err := r.List(ctx, &deploys, &client.ListOptions{
		Namespace:     uiPluginNamespace,
		LabelSelector: selector,
	}); err != nil {
		return deploymentStatus{}, fmt.Errorf("list deployments: %w", err)
	}

	if len(deploys.Items) == 0 {
		return deploymentStatus{Message: "Deployment not found"}, nil
	}

	deploy := &deploys.Items[0]
	desired := int32(1)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}
	if deploy.Status.ReadyReplicas >= desired {
		return deploymentStatus{Ready: true, Message: "Deployment is available"}, nil
	}

	msg := r.diagnosePodFailures(ctx, selector)
	if msg == "" {
		msg = fmt.Sprintf("Deployment not yet ready (%d/%d replicas)", deploy.Status.ReadyReplicas, desired)
	}
	return deploymentStatus{Message: msg}, nil
}

// diagnosePodFailures inspects pods matching the selector and returns a
// message describing the first container-level or scheduling failure found.
func (r *InstallAIExtensionReconciler) diagnosePodFailures(ctx context.Context, selector labels.Selector) string {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, &client.ListOptions{
		Namespace:     uiPluginNamespace,
		LabelSelector: selector,
	}); err != nil {
		return ""
	}

	for i := range pods.Items {
		pod := &pods.Items[i]

		// Check container statuses for waiting reasons (ImagePullBackOff, CrashLoopBackOff, etc.)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				msg := fmt.Sprintf("Pod %s container %s: %s", pod.Name, cs.Name, cs.State.Waiting.Reason)
				if cs.State.Waiting.Message != "" {
					msg += " — " + cs.State.Waiting.Message
				}
				return msg
			}
			if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				return fmt.Sprintf("Pod %s container %s: terminated with exit code %d (%s)",
					pod.Name, cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
			}
		}

		// Check pod conditions for scheduling failures
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
				return fmt.Sprintf("Pod %s: %s — %s", pod.Name, cond.Reason, cond.Message)
			}
		}
	}

	return ""
}

// discoverServiceURL finds the Service for a Helm release and returns its in-cluster URL.
func (r *InstallAIExtensionReconciler) discoverServiceURL(ctx context.Context, releaseName string) (string, error) {
	var services corev1.ServiceList
	selector := labels.SelectorFromSet(map[string]string{
		"app.kubernetes.io/instance": releaseName,
	})
	if err := r.List(ctx, &services, &client.ListOptions{
		Namespace:     uiPluginNamespace,
		LabelSelector: selector,
	}); err != nil {
		return "", fmt.Errorf("list services: %w", err)
	}

	if len(services.Items) == 0 {
		return "", fmt.Errorf("no service found for release %s", releaseName)
	}

	svc := &services.Items[0]
	port := int32(8080)
	if len(svc.Spec.Ports) > 0 {
		port = svc.Spec.Ports[0].Port
	}

	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svc.Name, svc.Namespace, port), nil
}

// ensureClusterRepo creates or updates a ClusterRepo pointing to a Service URL (Helm source mode).
func (r *InstallAIExtensionReconciler) ensureClusterRepo(ctx context.Context, ext *aifv1.InstallAIExtension, serviceURL string) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK())
	repo.SetName(clusterRepoName(ext.Spec.Extension.Name))

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, repo, func() error {
		repo.SetLabels(map[string]string{
			"catalog.cattle.io/ui-extensions-catalog-image": ext.Spec.Extension.Name,
			"ai.suse.com/installaiextension":                ext.Name,
		})
		if err := unstructured.SetNestedField(repo.Object, serviceURL, "spec", "url"); err != nil {
			return err
		}
		unstructured.RemoveNestedField(repo.Object, "spec", "gitRepo")
		unstructured.RemoveNestedField(repo.Object, "spec", "gitBranch")
		return nil
	})
	return err
}

// ensureClusterRepoGit creates or updates a ClusterRepo pointing to a git repository (Git source mode).
func (r *InstallAIExtensionReconciler) ensureClusterRepoGit(ctx context.Context, ext *aifv1.InstallAIExtension, repoURL, branch string) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK())
	repo.SetName(clusterRepoName(ext.Spec.Extension.Name))

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, repo, func() error {
		repo.SetLabels(map[string]string{
			"catalog.cattle.io/ui-extensions-catalog-image": ext.Spec.Extension.Name,
			"ai.suse.com/installaiextension":                ext.Name,
		})
		if err := unstructured.SetNestedField(repo.Object, repoURL, "spec", "gitRepo"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(repo.Object, branch, "spec", "gitBranch"); err != nil {
			return err
		}
		unstructured.RemoveNestedField(repo.Object, "spec", "url")
		return nil
	})
	return err
}

// deleteClusterRepo deletes a ClusterRepo by name, ignoring NotFound errors.
func (r *InstallAIExtensionReconciler) deleteClusterRepo(ctx context.Context, name string) error {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK())
	repo.SetName(name)
	if err := r.Delete(ctx, repo); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete ClusterRepo %s: %w", name, err)
	}
	return nil
}

// ensureUIPluginHelm creates or updates the UIPlugin CR in the cattle-ui-plugin-system namespace.
func (r *InstallAIExtensionReconciler) ensureUIPluginHelm(ctx context.Context, ext *aifv1.InstallAIExtension, endpoint string, meta uiPluginMeta) error {
	plugin := &unstructured.Unstructured{}
	plugin.SetGroupVersionKind(uiPluginGVK())
	plugin.SetName(ext.Spec.Extension.Name)
	plugin.SetNamespace(uiPluginNamespace)

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, plugin, func() error {
		plugin.SetLabels(map[string]string{
			"ai.suse.com/installaiextension": ext.Name,
		})

		pluginSpec := map[string]interface{}{
			"name":     ext.Spec.Extension.Name,
			"version":  ext.Spec.Extension.Version,
			"endpoint": endpoint,
			"noAuth":   false,
			"noCache":  false,
		}

		metadata := map[string]interface{}{}
		if meta.DisplayName != "" {
			metadata["catalog.cattle.io/display-name"] = meta.DisplayName
		}
		if meta.RancherVersion != "" {
			metadata["catalog.cattle.io/rancher-version"] = meta.RancherVersion
		}
		if meta.ExtensionsVersion != "" {
			metadata["catalog.cattle.io/ui-extensions-version"] = meta.ExtensionsVersion
		}
		if len(metadata) > 0 {
			pluginSpec["metadata"] = metadata
		}

		return unstructured.SetNestedMap(plugin.Object, pluginSpec, "spec", "plugin")
	})
	return err
}

// ensureUIPluginGit installs the UIPlugin chart from a git-based Helm repository
// via the Helm SDK. This allows Rancher Dashboard to manage extension versions
// through the Extensions page.
func (r *InstallAIExtensionReconciler) ensureUIPluginGit(ctx context.Context, ext *aifv1.InstallAIExtension, repoURL string) error {
	_, err := r.HelmEngine.InstallFromRepoURL(ctx, helm.InstallFromRepoURLRequest{
		Namespace:   uiPluginNamespace,
		ReleaseName: ext.Spec.Extension.Name,
		ChartName:   ext.Spec.Extension.Name,
		RepoURL:     repoURL,
		Version:     ext.Spec.Extension.Version,
		Wait:        true,
		Timeout:     helmInstallTimeout,
	})
	return err
}

// deleteUIPlugin deletes a UIPlugin by name from the cattle-ui-plugin-system namespace.
func (r *InstallAIExtensionReconciler) deleteUIPlugin(ctx context.Context, name string) error {
	plugin := &unstructured.Unstructured{}
	plugin.SetGroupVersionKind(uiPluginGVK())
	plugin.SetName(name)
	plugin.SetNamespace(uiPluginNamespace)
	if err := r.Delete(ctx, plugin); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete UIPlugin %s: %w", name, err)
	}
	return nil
}

func (r *InstallAIExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aifv1.InstallAIExtension{}).
		Complete(r)
}

func clusterRepoGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	}
}

func uiPluginGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "UIPlugin",
	}
}

func clusterRepoName(extensionName string) string {
	return extensionName + "-charts"
}

func deriveReleaseName(chartURL string) string {
	return path.Base(chartURL)
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

func (r *InstallAIExtensionReconciler) fetchIndexMetadata(ctx context.Context, indexURL, chartName, chartVersion string) (uiPluginMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return uiPluginMeta{}, fmt.Errorf("build request: %w", err)
	}

	httpClient := r.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return uiPluginMeta{}, fmt.Errorf("fetch index.yaml: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return uiPluginMeta{}, fmt.Errorf("index.yaml returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return uiPluginMeta{}, fmt.Errorf("read index.yaml: %w", err)
	}

	ann, err := helm.FindChartAnnotations(body, chartName, chartVersion)
	if err != nil {
		return uiPluginMeta{}, err
	}

	return uiPluginMeta{
		DisplayName:       ann.DisplayName,
		RancherVersion:    ann.RancherVersion,
		ExtensionsVersion: ann.ExtensionsVersion,
	}, nil
}

func gitRepoToRawURL(repoURL, branch string) (string, error) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("parse git URL: %w", err)
	}

	if parsed.Host != "github.com" {
		return "", fmt.Errorf("unsupported git host %q: only github.com is supported", parsed.Host)
	}

	repoPath := strings.TrimSuffix(parsed.Path, ".git")
	return fmt.Sprintf("https://raw.githubusercontent.com%s/refs/heads/%s", repoPath, branch), nil
}
