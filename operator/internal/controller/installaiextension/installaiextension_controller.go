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
	"path"
	"strings"
	"time"

	urlpkg "net/url"

	"helm.sh/helm/v3/pkg/cli"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/config"
	"github.com/SUSE/aif-operator/internal/credentials"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/infra/kubernetes"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
	"github.com/SUSE/aif-operator/internal/installaiextension"
)

const (
	defaultReadinessTimeout = 5 * time.Minute
	readinessRequeue        = 10 * time.Second
	uiConfigMapName         = "aif-ui-config"
	healthCheckInterval     = 60 * time.Second
	// resolutionRetryInterval requeues the CR after a registry auth/TLS
	// resolution failure so it self-heals when a referenced Secret is created
	// or corrected (the controller has no Secret watch).
	resolutionRetryInterval = 30 * time.Second
	// pendingReleaseRequeue requeues the CR while a Helm operation is still in
	// flight. Deliberately slower than readinessRequeue: an upgrade waits on pod
	// readiness for up to 10 minutes (helm upgrade Timeout), so polling every few
	// seconds only adds API reads without converging any sooner.
	pendingReleaseRequeue = 30 * time.Second
	// pendingReleaseTimeout bounds that requeue. Helm marks a release pending for
	// the duration of an operation, but a process killed mid-upgrade leaves the
	// marker behind with nothing to clear it, and no amount of requeuing will
	// resolve that — only `helm rollback` or `helm uninstall` will. Longer than
	// helm's own 10-minute upgrade Timeout so a legitimately slow upgrade is never
	// the thing that trips it.
	pendingReleaseTimeout = 15 * time.Minute

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
	// AllowInsecureRegistryTLS gates spec.source.helm.tls.insecureSkipVerify. When
	// false (the default), a CR requesting insecureSkipVerify is failed at reconcile
	// instead of pulling with TLS verification disabled. Set by the platform admin
	// at deploy time (manager.allowInsecureRegistryTLS / --allow-insecure-registry-tls).
	AllowInsecureRegistryTLS bool
	// AllowedRegistryHosts optionally restricts which registry hosts the operator
	// will contact (and send resolved credentials to) for a chart pull. Empty means
	// all hosts are allowed. Set by the platform admin (manager.allowedRegistryHosts /
	// --allowed-registry-hosts) to bound the CR-supplied chartURL and prevent
	// credential exfiltration to an attacker-chosen registry (confused-deputy).
	AllowedRegistryHosts []string
	rancherMgr           *rancher.Manager
	// helmClientFor builds the Helm client for a namespace. A field rather than a
	// direct call so tests can drive the reconcile paths end to end against a stub
	// release backend; nil means newHelmClientForNamespace.
	helmClientFor func(namespace string) (helmClient.HelmClient, error)
}

func (r *InstallAIExtensionReconciler) helmFor(namespace string) (helmClient.HelmClient, error) {
	if r.helmClientFor != nil {
		return r.helmClientFor(namespace)
	}
	return newHelmClientForNamespace(namespace)
}

// registryHostAllowed reports whether the chart's registry host may be contacted.
// An empty allowlist permits all hosts (opt-in hardening); when non-empty, host
// must match an entry case-insensitively, compared against both the "host:port"
// authority and the bare hostname so admins can list either form.
func (r *InstallAIExtensionReconciler) registryHostAllowed(host, hostname string) bool {
	if len(r.AllowedRegistryHosts) == 0 {
		return true
	}
	for _, h := range r.AllowedRegistryHosts {
		if strings.EqualFold(h, host) || strings.EqualFold(h, hostname) {
			return true
		}
	}
	return false
}

// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=installaiextensions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=installaiextensions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=installaiextensions/finalizers,verbs=update
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=catalog.cattle.io,resources=clusterrepos/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

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

	// Snapshot the object before mutating status so we can persist status with a
	// resourceVersion-free merge patch. reconcile() sets Phase=Installing itself,
	// so the single terminal write below is enough — no early status flush needed.
	original := ext.DeepCopy()

	result, reconcileErr := r.reconcile(ctx, &ext)

	if reconcileErr == nil && ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseInstalled {
		ext.Status.ObservedGeneration = ext.Generation
	}
	if err := r.persistStatus(ctx, &ext, original); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return result, reconcileErr
}

// persistStatus writes the object's status via a merge patch. MergeFrom (as
// opposed to MergeFromWithOptimisticLock) omits the resourceVersion precondition,
// so a status write cannot fail with an "object has been modified" (409) conflict
// when the informer cache lagged the server between our Get and this write. This
// is safe because the operator is the sole writer of InstallAIExtension status;
// the /status subresource endpoint also ignores any non-status fields that appear
// in the patch body.
//
// Design note — why this differs from SettingsReconciler.updateStatus, which
// uses retry.RetryOnConflict: that pattern fits a surgical few-field status
// update (it re-reads and re-applies only LastApplied/ObservedGeneration, so
// concurrent changes to other fields survive). This controller instead computes
// and owns the *entire* status each reconcile, so a resourceVersion-free merge
// patch is the better fit here: it cannot 409, needs no extra read or retry
// loop, avoids per-reconcile resourceVersion churn (an unchanged status yields a
// no-op patch), and — because a merge patch only sends the fields that changed —
// still preserves any concurrent writer's changes to fields it did not touch.
// Revisit (e.g. switch to RetryOnConflict) if a second writer of this status is
// ever introduced.
func (r *InstallAIExtensionReconciler) persistStatus(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	base *v1alpha1.InstallAIExtension,
) error {
	return r.Status().Patch(ctx, ext, client.MergeFrom(base))
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

	if err := r.syncUIConfigMap(ctx); err != nil {
		logger.Error(err, "failed to sync operator coordinates to UI ConfigMap")
		return ctrl.Result{Requeue: true}, nil
	}

	logger.Info("reconciled successfully")
	return ctrl.Result{RequeueAfter: healthCheckInterval}, nil
}

// syncUIConfigMap writes the operator namespace and service name into the
// aif-ui-config ConfigMap so the UI extension can reach the operator without
// manual configuration. It runs on every successful reconcile loop, giving
// self-healing behaviour if the ConfigMap is deleted or corrupted.
// The ConfigMap is intentionally not deleted when the CR is removed — the UI
// retains the last-known operator coordinates so it remains functional.
func (r *InstallAIExtensionReconciler) syncUIConfigMap(ctx context.Context) error {
	logger := log.FromContext(ctx)
	ns, svc := config.GetOperatorNamespace(), config.GetOperatorService()
	logger.V(1).Info("syncing UI ConfigMap", "operatorNamespace", ns, "operatorService", svc)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uiConfigMapName,
			Namespace: r.ExtensionNamespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = make(map[string]string)
		}
		cm.Data["operatorNamespace"] = ns
		cm.Data["operatorService"] = svc
		return nil
	})
	return err
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

	// Operator-level gate: refuse to pull with TLS verification disabled unless the
	// platform admin explicitly enabled it at deploy time. Checked before any chart
	// work so we fail fast and never build an insecure client. The CR's
	// acknowledgeInsecure (CEL-enforced) only proves author intent; this flag is the
	// authority check.
	if helmSource.TLS != nil && helmSource.TLS.InsecureSkipVerify && !r.AllowInsecureRegistryTLS {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"InsecureTLSNotAllowed",
			"spec.source.helm.tls.insecureSkipVerify is set but the operator was not deployed with insecure "+
				"registry TLS enabled (manager.allowInsecureRegistryTLS / --allow-insecure-registry-tls)",
			ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	releaseName := deriveReleaseName(helmSource.ChartURL)

	if ext.Status.HelmReleaseName != "" && ext.Status.HelmReleaseName != releaseName {
		logger.Info("chart URL changed, uninstalling old release", "old", ext.Status.HelmReleaseName, "new", releaseName)
		helm, err := r.helmFor(namespace)
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

	// Registry-host allowlist gate: bound the CR-supplied chartURL to admin-approved
	// hosts. Checked before the Helm client is built or any auth Secret is read, so a
	// disallowed host can never cause the operator to resolve and transmit credentials
	// to an attacker-chosen registry (confused-deputy). Empty allowlist permits all.
	if !r.registryHostAllowed(u.Host, u.Hostname()) {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"RegistryHostNotAllowed",
			fmt.Sprintf("registry host %q is not permitted by the operator's registry host allowlist "+
				"(manager.allowedRegistryHosts / --allowed-registry-hosts)", u.Host),
			ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{}, nil
	}

	helm, err := r.helmFor(namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	regAuth, err := credentials.ResolveHelmAuth(ctx, r.Client, config.GetOperatorNamespace(), helmSource.Auth, helmSource.ChartURL)
	if err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"AuthResolutionFailed", fmt.Sprintf("registry auth resolution failed: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{RequeueAfter: resolutionRetryInterval}, nil
	}

	tlsCfg, err := credentials.ResolveHelmTLS(ctx, r.Client, config.GetOperatorNamespace(), helmSource.TLS)
	if err != nil {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
			"TLSResolutionFailed", fmt.Sprintf("registry TLS resolution failed: %v", err), ext.Generation)
		ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
		return ctrl.Result{RequeueAfter: resolutionRetryInterval}, nil
	}
	if helmSource.TLS != nil && helmSource.TLS.InsecureSkipVerify {
		logger.Info("WARNING: insecureSkipVerify is enabled for the extension chart registry; TLS certificate verification is disabled")
	}
	releaseSpec := helmClient.ReleaseSpec{
		Name:      releaseName,
		Namespace: namespace,
		ChartRef:  helmSource.ChartURL,
		Version:   helmSource.Version,
		Values:    values,
	}
	if regAuth != nil {
		releaseSpec.RegistryAuth = &helmClient.RegistryAuth{
			Username: regAuth.Username,
			Password: regAuth.Password,
		}
	}
	if tlsCfg != nil {
		releaseSpec.TLSConfig = tlsCfg
	}

	ensureErr := helm.EnsureRelease(ctx, releaseSpec)
	result, handled, err := r.handlePendingRelease(ctx, ext, conditionTypeHelmInstalled, ensureErr)
	if err != nil {
		return ctrl.Result{}, err
	}
	if handled {
		return result, nil
	}
	if ensureErr != nil {
		setTerminalFailure(ext, conditionTypeHelmInstalled,
			"InstallFailed", fmt.Sprintf("Helm install failed: %v", ensureErr))
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeHelmInstalled, metav1.ConditionTrue,
		"Installed", fmt.Sprintf("Helm release %s installed", releaseName), ext.Generation)
	ext.Status.HelmReleaseName = releaseName

	// LastRelease, not DeployedRelease: the status field mirrors what Helm last
	// recorded, which is the highest revision number rather than the running one.
	releaseInfo, err := helm.LastRelease(ctx, releaseName)
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
		waitingSince := r.getWaitingSince(ext, annotationWaitingSince)
		if waitingSince.IsZero() {
			r.setWaitingSince(ext, annotationWaitingSince)
			if err := r.updateAnnotations(ctx, ext); err != nil {
				return ctrl.Result{}, err
			}
			// RequeueAfter (not Requeue) so the next reconcile's cached Get does
			// not race this write's propagation into the informer cache.
			return ctrl.Result{RequeueAfter: readinessRequeue}, nil
		} else if time.Since(waitingSince) > r.ReadinessTimeout {
			msg := fmt.Sprintf("Deployment not ready after %s: %s", r.ReadinessTimeout, deployStatus.Message)
			setTerminalFailure(ext, conditionTypeDeploymentReady, "TimedOut", msg)
			return ctrl.Result{}, nil
		}
		setCondition(&ext.Status.Conditions, conditionTypeDeploymentReady, metav1.ConditionFalse,
			"NotReady", deployStatus.Message, ext.Generation)
		return ctrl.Result{RequeueAfter: readinessRequeue}, nil
	}

	// Deployment is ready: clear the waiting marker and continue in the same pass
	// rather than requeuing, so install completes immediately once readiness is
	// reached. Continuing inline also avoids the cache-propagation race — there is
	// no follow-up reconcile whose cached Get could still observe the stale marker,
	// and no further main-resource write happens this pass (only the status patch).
	if r.getWaitingSince(ext, annotationWaitingSince) != (time.Time{}) {
		r.clearWaitingSince(ext, annotationWaitingSince)
		// updateAnnotations, not Update: HelmReleaseName and HelmReleaseRevision were
		// set earlier in this pass and a bare Update would drop both before
		// persistStatus ever sees them.
		if err := r.updateAnnotations(ctx, ext); err != nil {
			return ctrl.Result{}, err
		}
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
		setTerminalFailure(ext, conditionTypeClusterRepo,
			"Failed", fmt.Sprintf("ClusterRepo failed: %v", err))
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeClusterRepo, metav1.ConditionTrue,
		"Created", "ClusterRepo created", ext.Generation)

	if err := r.rancherMgr.EnsureUIPlugin(ctx, ext, svcURL, namespace); err != nil {
		setTerminalFailure(ext, conditionTypeUIPlugin,
			"Failed", fmt.Sprintf("UIPlugin failed: %v", err))
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
		setTerminalFailure(ext, conditionTypeClusterRepo,
			"Failed", fmt.Sprintf("ClusterRepo failed: %v", err))
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, conditionTypeClusterRepo, metav1.ConditionTrue,
		"Created", "ClusterRepo created for git source", ext.Generation)

	pluginErr := r.ensureUIPluginGit(ctx, ext, rawBaseURL, namespace)
	result, handled, err := r.handlePendingRelease(ctx, ext, conditionTypeUIPlugin, pluginErr)
	if err != nil {
		return ctrl.Result{}, err
	}
	if handled {
		return result, nil
	}
	if pluginErr != nil {
		setTerminalFailure(ext, conditionTypeUIPlugin,
			"Failed", fmt.Sprintf("UIPlugin install failed: %v", pluginErr))
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
	helm, err := r.helmFor(namespace)
	if err != nil {
		return err
	}

	// DeployedRelease, not LastRelease: this is a skip-if-unchanged check, so it
	// must compare against the revision the cluster is actually running. A failed
	// revision carrying the requested version would otherwise suppress the retry.
	info, err := helm.DeployedRelease(ctx, ext.Spec.Extension.Name)
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
			helm, err := r.helmFor(namespace)
			if err == nil {
				if err := helm.DeleteRelease(ctx, ext.Status.HelmReleaseName); err != nil {
					errs = append(errs, err)
				}
			}
			ext.Status.HelmReleaseName = ""
			ext.Status.HelmReleaseRevision = 0
		}
		if oldSource == v1alpha1.ExtensionSourceKindGit {
			helm, err := r.helmFor(namespace)
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
			helm, err := r.helmFor(namespace)
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
			helm, err := r.helmFor(namespace)
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

// setTerminalFailure records a terminal reconcile failure: it sets the specific
// sub-condition to False and mirrors the same reason/message onto the top-level Ready
// condition, then marks the phase Failed. Mirroring keeps Ready from showing a stale
// success while phase is Failed (a pull/deployment/Rancher failure otherwise updated only
// its own sub-condition). Sites that already set Ready directly do not need this.
func setTerminalFailure(ext *v1alpha1.InstallAIExtension, condType, reason, message string) {
	setCondition(&ext.Status.Conditions, condType, metav1.ConditionFalse, reason, message, ext.Generation)
	if condType != conditionTypeReady {
		setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reason, message, ext.Generation)
	}
	ext.Status.Phase = v1alpha1.InstallAIExtensionPhaseFailed
}

// handlePendingRelease turns an in-flight Helm operation into a bounded requeue,
// and reports whether it took ownership of the outcome. Callers pass the
// EnsureRelease error verbatim — including nil — and fall through to their own
// handling when it returns false.
//
// A pending release is a timing state, not a verdict: Helm marks a release
// pending for the whole duration of an install or upgrade. Failing terminally on
// it would give up on an operation that is still running. But the marker also
// survives a process killed mid-upgrade, and nothing in the reconcile loop can
// clear that — so the wait is timed, and past pendingReleaseTimeout the CR fails
// terminally with the manual step named rather than requeuing forever.
//
// Shared by both source kinds deliberately. Every path that calls EnsureRelease
// can see this error, and handling it in only one of them means the same cluster
// state produces a requeue or a terminal failure depending on how the extension
// happens to be sourced.
func (r *InstallAIExtensionReconciler) handlePendingRelease(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	condType string,
	err error,
) (ctrl.Result, bool, error) {
	if !stderrors.Is(err, helmClient.ErrReleasePending) {
		// The release either settled or failed for some other reason. Either way the
		// wait is over, so drop the marker — left behind, it would make the next
		// pending release inherit this window and time out on its first observation.
		return ctrl.Result{}, false, r.clearReleasePending(ctx, ext)
	}

	pendingSince := r.getWaitingSince(ext, annotationReleasePendingSince)
	switch {
	case pendingSince.IsZero():
		r.setWaitingSince(ext, annotationReleasePendingSince)
		if uerr := r.updateAnnotations(ctx, ext); uerr != nil {
			return ctrl.Result{}, true, uerr
		}
		// Fall through to the conditions below rather than returning here: the
		// first observation is the one an automated gate is most likely to read,
		// right after a spec change, and leaving it uncondition-ed advertises the
		// previous pass's success for a whole requeue interval.

	case time.Since(pendingSince) > pendingReleaseTimeout:
		setTerminalFailure(ext, condType, "ReleasePendingTimedOut", fmt.Sprintf(
			"Helm release still mid-operation after %s; a pending release cannot be "+
				"upgraded over, so resolve it with `helm rollback` or `helm uninstall`: %v",
			pendingReleaseTimeout, err))
		return ctrl.Result{}, true, nil
	}

	msg := fmt.Sprintf("Waiting for in-flight Helm operation: %v", err)
	setCondition(&ext.Status.Conditions, condType, metav1.ConditionFalse,
		"ReleasePending", msg, ext.Generation)
	// Ready is mirrored for the same reason setTerminalFailure mirrors it: this is
	// not a terminal failure, so that helper does not apply, but a CR that already
	// reached Installed would otherwise keep advertising Ready=True while its
	// upgrade sits wedged.
	setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
		"ReleasePending", msg, ext.Generation)
	// RequeueAfter (not Requeue) so the next reconcile's cached Get does not race
	// the annotation write's propagation into the informer cache.
	return ctrl.Result{RequeueAfter: pendingReleaseRequeue}, true, nil
}

// clearReleasePending drops the pending-wait marker, writing only when one is
// actually set so the common path costs no API call.
func (r *InstallAIExtensionReconciler) clearReleasePending(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
) error {
	if r.getWaitingSince(ext, annotationReleasePendingSince).IsZero() {
		return nil
	}
	r.clearWaitingSince(ext, annotationReleasePendingSince)
	return r.updateAnnotations(ctx, ext)
}

// updateAnnotations persists ext's metadata without rolling back the status this
// reconcile pass has already accumulated.
//
// The CRD has a status subresource, so the API server strips status from an
// Update of the main resource and echoes the *stored* copy back in the response
// body — which controller-runtime's typed client decodes straight into ext
// (typed_client.go: Body(obj)...Do(ctx).Into(obj)). Every condition, phase and
// status field set before the write is silently reverted, and persistStatus then
// computes its patch from the reverted values. Worse, a pass that had already set
// Phase=Installing gets Installed back, which is the gate Reconcile uses to stamp
// ObservedGeneration — so the CR reports a generation as applied when it was not.
//
// Snapshotting keeps an annotation write what it reads as: metadata only. The
// copy is deep because the response is decoded into ext's existing Conditions
// backing array, so a shallow save would hand back overwritten elements.
func (r *InstallAIExtensionReconciler) updateAnnotations(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
) error {
	status := ext.Status.DeepCopy()
	err := r.Update(ctx, ext)
	ext.Status = *status
	return err
}

func newHelmClientForNamespace(namespace string) (helmClient.HelmClient, error) {
	settings := cli.New()
	settings.SetNamespace(namespace)
	return helmClient.New(settings)
}

const (
	annotationWaitingSince = "ai-factory.suse.com/waiting-since"
	// annotationReleasePendingSince times the wait on an in-flight Helm operation.
	// A separate key from annotationWaitingSince on purpose: that one belongs to
	// the deployment readiness wait, and the two can be live in the same reconcile
	// pass, so sharing a key would let either clear or inherit the other's start
	// time and time out against the wrong clock.
	annotationReleasePendingSince = "ai-factory.suse.com/release-pending-since"
)

func (r *InstallAIExtensionReconciler) getWaitingSince(ext *v1alpha1.InstallAIExtension, key string) time.Time {
	if ext.Annotations == nil {
		return time.Time{}
	}
	ts, ok := ext.Annotations[key]
	if !ok {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (r *InstallAIExtensionReconciler) setWaitingSince(ext *v1alpha1.InstallAIExtension, key string) {
	if ext.Annotations == nil {
		ext.Annotations = make(map[string]string)
	}
	ext.Annotations[key] = time.Now().Format(time.RFC3339)
}

func (r *InstallAIExtensionReconciler) clearWaitingSince(ext *v1alpha1.InstallAIExtension, key string) {
	if ext.Annotations != nil {
		delete(ext.Annotations, key)
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
