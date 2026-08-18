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
	"sync"
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
	// DefaultReadinessTimeout bounds how long the controller waits for an
	// extension's pods after applying a chart. Exported so the flag that
	// overrides it cannot drift from it.
	//
	// Ten minutes, for two independent reasons. It is the tolerance Helm's own
	// Wait used to give the rollout before the controller took the wait over, so
	// anything shorter fails upgrades that used to succeed on a slow image pull.
	// And it is the Deployment's default progressDeadlineSeconds, which the chart
	// does not override: give up sooner and the CR calls a rollout dead while
	// Kubernetes is still working on it, which — a readiness timeout being
	// terminal — leaves the CR Failed through a rollout that then succeeds.
	DefaultReadinessTimeout = 10 * time.Minute
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

	// reasonReleasePending and reasonReleasePendingTimedOut are the two verdicts
	// that mean a pending-release wait is still the CR's state. Reconcile keys the
	// marker's lifetime off them, so they are named rather than inline.
	reasonReleasePending         = "ReleasePending"
	reasonReleasePendingTimedOut = "ReleasePendingTimedOut"

	// reasonReadinessTimedOut is the verdict recorded when a readiness wait
	// exceeds ReadinessTimeout, whichever check was being waited on.
	reasonReadinessTimedOut = "TimedOut"

	conditionTypeReady           = "Ready"
	conditionTypeHelmInstalled   = "HelmInstalled"
	conditionTypeDeploymentReady = "DeploymentReady"
	conditionTypeServiceReady    = "ServiceReady"
	conditionTypeClusterRepo     = "ClusterRepoReady"
	conditionTypeUIPlugin        = "UIPluginReady"
)

type InstallAIExtensionReconciler struct {
	client.Client
	// APIReader reads straight from the API server, bypassing the manager's
	// cache. Only the deployment readiness check uses it, and it has to: that
	// check runs in the same pass that applied the manifest, and an informer that
	// has not yet seen the apply serves the previous revision — a self-consistent
	// picture of a rollout that finished, which readiness cannot tell apart from
	// the new one finishing. See deploymentReader.
	//
	// SetupWithManager fills this in, so nothing outside tests has to.
	APIReader          client.Reader
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
	// helmClients memoizes those clients by namespace. See helmFor.
	helmClients sync.Map
}

// helmFor returns the Helm client for a namespace, building it once and reusing
// it for the life of the process.
//
// The reuse is load-bearing, not an optimization. The client carries the
// convergence latch and the downloaded-chart cache, both of which exist to stop
// the operator re-deriving the same verdict on every pass. Both live on the
// client, so handing each reconcile a freshly built one throws them away before
// the next pass can read them: the latch is written, the client is dropped, and
// the following reconcile pulls the chart again to rediscover what the last one
// already knew. That is the exact loop this work set out to remove, so building
// per call silently undoes it while every unit test — each of which holds one
// client across its passes — still passes.
//
// Keyed by namespace because the client's cli.EnvSettings is namespace-scoped.
//
// The helmClientFor seam is consulted through the same cache rather than ahead
// of it. Short-circuiting on the seam is what let this go unnoticed: it made the
// production path the only uncached one, so no end-to-end controller test could
// reach the behaviour that was broken.
func (r *InstallAIExtensionReconciler) helmFor(namespace string) (helmClient.HelmClient, error) {
	if existing, ok := r.helmClients.Load(namespace); ok {
		return existing.(helmClient.HelmClient), nil
	}

	build := newHelmClientForNamespace
	if r.helmClientFor != nil {
		build = r.helmClientFor
	}
	built, err := build(namespace)
	if err != nil {
		// Deliberately not cached: a client that failed to build must be retried
		// on the next pass, not remembered as a permanent failure.
		return nil, err
	}

	// LoadOrStore, not Store: two reconciles for different extensions in one
	// namespace can race here, and the loser must adopt the winner's client
	// rather than install a second one whose latch starts empty.
	actual, _ := r.helmClients.LoadOrStore(namespace, built)
	return actual.(helmClient.HelmClient), nil
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

	// The marker times a wait on an in-flight Helm operation, so it must not
	// outlive that wait. handlePendingRelease is the only thing that clears it, and
	// every path returning above the Helm call — missing Rancher CRDs, a rejected
	// chart URL, an unresolvable auth Secret, a failed ClusterRepo — never reaches
	// it. Once such a failure lasts longer than pendingReleaseTimeout, the next
	// genuine pending release inherits the stale window and fails terminally on its
	// first observation, reporting an upgrade that just started as stuck for 15
	// minutes.
	//
	// A timed-out wait keeps its marker on purpose: it really did exhaust, and
	// clearing it would restart the clock and flap the CR between Failed and
	// waiting forever. A pass that returned an error is left alone too — it reached
	// no verdict about the release.
	if reconcileErr == nil && !inReleasePendingWait(&ext) {
		if err := r.clearReleasePending(ctx, &ext); err != nil {
			logger.Error(err, "failed to clear a stale release-pending marker")
		}
	}

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

	// A readiness check that errors and one that reports not-ready share a clock:
	// both mean the deployment is not usable yet, and a check flapping between the
	// two must not keep restarting the wait.
	deployStatus, err := kubernetes.IsDeploymentReady(ctx, r.deploymentReader(), namespace, releaseName, logger)
	if err != nil {
		return r.awaitReadiness(ctx, ext, annotationWaitingSince, conditionTypeDeploymentReady,
			"CheckFailed", fmt.Sprintf("Failed to check deployment readiness: %v", err))
	}
	if !deployStatus.Ready {
		return r.awaitReadiness(ctx, ext, annotationWaitingSince, conditionTypeDeploymentReady,
			"NotReady", deployStatus.Message)
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

	// Both Service failures share a clock for the same reason the deployment ones
	// do: either way the Service is not yet usable, and the chart is free to
	// create it a moment after the deployment goes ready.
	svc, err := kubernetes.ServiceForHelmRelease(ctx, r.Client, namespace, releaseName)
	if err != nil {
		return r.awaitReadiness(ctx, ext, annotationServiceWaitingSince, conditionTypeServiceReady,
			"ServiceFailed", fmt.Sprintf("Service not found: %v", err))
	}

	svcName, svcNamespace, svcPort, err := installaiextension.ServiceEndpoint(svc)
	if err != nil {
		return r.awaitReadiness(ctx, ext, annotationServiceWaitingSince, conditionTypeServiceReady,
			"ServiceFailed", fmt.Sprintf("Service endpoint error: %v", err))
	}

	// Resolved: drop the marker so a later Service outage is timed from when it
	// starts rather than inheriting this window and failing on first observation.
	if !r.getWaitingSince(ext, annotationServiceWaitingSince).IsZero() {
		r.clearWaitingSince(ext, annotationServiceWaitingSince)
		// updateAnnotations, not Update: status fields set earlier in this pass
		// must survive until persistStatus writes them.
		if err := r.updateAnnotations(ctx, ext); err != nil {
			return ctrl.Result{}, err
		}
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

	// No skip-if-unchanged check here. EnsureRelease already makes that decision
	// against the deployed revision, and it makes it on more than the version — it
	// also reports an in-flight operation the caller has to wait out. Re-deciding
	// it up front short-circuited past that: a release wedged at the version the
	// spec asks for returned success on this path while the Helm path requeued for
	// the identical cluster state. The duplicate check also cost an extra release
	// lookup on every pass that did go on to upgrade.
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

// deploymentReader returns the reader the deployment readiness check must use:
// the uncached one whenever there is one.
//
// The fallback to Client exists for reconcilers built by hand in tests, which
// never go through SetupWithManager. It is safe there because a fake client is
// the API server — there is no second view to be stale relative to — and it is
// unreachable in a running operator, where SetupWithManager is the only way a
// reconciler is ever started.
func (r *InstallAIExtensionReconciler) deploymentReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// awaitReadiness advances a bounded wait on an install step that has not
// succeeded yet. It stamps the start time on the first observation, requeues
// while the wait is still inside ReadinessTimeout, and records a terminal
// failure once it is past it.
//
// Every not-yet-ready path in the Helm install returns through here so that none
// of them can requeue indefinitely. An unbounded readiness requeue is not merely
// a CR that stays un-Ready until someone notices: readinessRequeue is six times
// faster than healthCheckInterval, and every one of those passes re-enters
// EnsureRelease from the top. A check that never recovers therefore drives
// reconciles — and any chart pull they decide to make — at six times the
// intended rate, for as long as the CR exists, while reporting nothing on pass
// 10,000 that it did not already report on pass 2.
//
// annotation names the clock. Two waits that are cleared at different points
// need different keys, or the one cleared earlier resets the other's start time
// and the timeout never fires.
func (r *InstallAIExtensionReconciler) awaitReadiness(
	ctx context.Context,
	ext *v1alpha1.InstallAIExtension,
	annotation string,
	condType string,
	reason string,
	message string,
) (ctrl.Result, error) {
	waitingSince := r.getWaitingSince(ext, annotation)
	switch {
	case waitingSince.IsZero():
		r.setWaitingSince(ext, annotation)
		if err := r.updateAnnotations(ctx, ext); err != nil {
			return ctrl.Result{}, err
		}
		// Fall through to the condition below rather than returning here, for the
		// reason handlePendingRelease gives: the first observation is the one an
		// automated gate is most likely to read, and leaving it uncondition-ed
		// advertises the previous pass's success for a whole requeue interval.

	case time.Since(waitingSince) > r.ReadinessTimeout:
		setTerminalFailure(ext, condType, reasonReadinessTimedOut,
			fmt.Sprintf("%s (still not resolved after %s)", message, r.ReadinessTimeout))
		return ctrl.Result{}, nil
	}

	setCondition(&ext.Status.Conditions, condType, metav1.ConditionFalse,
		reason, message, ext.Generation)
	// RequeueAfter (not Requeue) so the next reconcile's cached Get does not race
	// the annotation write's propagation into the informer cache.
	return ctrl.Result{RequeueAfter: readinessRequeue}, nil
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
		cerr := r.clearReleasePending(ctx, ext)
		if cerr == nil {
			return ctrl.Result{}, false, nil
		}
		if err == nil {
			return ctrl.Result{}, false, cerr
		}
		// err is a real install failure, and the caller answers a nil error here by
		// recording it on the CR. Returning cerr instead would swap that diagnosis
		// for an unrelated write conflict, so the CR would say nothing about why the
		// install failed and the operator would retry blind. The clear is reattempted
		// every pass, so dropping this one costs only a delayed marker removal.
		log.FromContext(ctx).Error(cerr, "could not clear the release-pending marker",
			"releaseError", err)
		return ctrl.Result{}, false, nil
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
		setTerminalFailure(ext, condType, reasonReleasePendingTimedOut, fmt.Sprintf(
			"Helm release still mid-operation after %s; a pending release cannot be "+
				"upgraded over, so resolve it with `helm rollback` or `helm uninstall`: %v",
			pendingReleaseTimeout, err))
		return ctrl.Result{}, true, nil
	}

	msg := fmt.Sprintf("Waiting for in-flight Helm operation: %v", err)
	setCondition(&ext.Status.Conditions, condType, metav1.ConditionFalse,
		reasonReleasePending, msg, ext.Generation)
	// Ready is mirrored for the same reason setTerminalFailure mirrors it: this is
	// not a terminal failure, so that helper does not apply, but a CR that already
	// reached Installed would otherwise keep advertising Ready=True while its
	// upgrade sits wedged.
	setCondition(&ext.Status.Conditions, conditionTypeReady, metav1.ConditionFalse,
		reasonReleasePending, msg, ext.Generation)
	// RequeueAfter (not Requeue) so the next reconcile's cached Get does not race
	// the annotation write's propagation into the informer cache.
	return ctrl.Result{RequeueAfter: pendingReleaseRequeue}, true, nil
}

// inReleasePendingWait reports whether this pass concluded that a Helm operation
// is still in flight, or has been for too long. It reads the Ready condition
// because the operator recomputes the whole status every pass, so the reason
// there is this pass's verdict rather than a leftover from the last one.
func inReleasePendingWait(ext *v1alpha1.InstallAIExtension) bool {
	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
	if cond == nil {
		return false
	}
	return cond.Reason == reasonReleasePending || cond.Reason == reasonReleasePendingTimedOut
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
	// annotationServiceWaitingSince times the wait on the release's Service
	// becoming resolvable. Distinct from annotationWaitingSince for the same
	// reason annotationReleasePendingSince is: the deployment wait is cleared the
	// moment the deployment reports ready, which is every pass that then goes on
	// to look for the Service. Sharing the key would reset the Service wait's
	// start time on each of those passes, so its timeout would never fire.
	annotationServiceWaitingSince = "ai-factory.suse.com/service-waiting-since"
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
	// Annotations are writable by anyone with edit access on the CR, and both
	// callers measure elapsed time as time.Since(start). A start in the future
	// makes that negative, so the wait never exceeds its bound and the operator
	// requeues on it forever — a timeout switched off by a field edit. Treated the
	// same as an unparseable value: no usable start time, so the caller stamps a
	// fresh one and the wait is bounded again from now.
	if t.After(time.Now()) {
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
		r.ReadinessTimeout = DefaultReadinessTimeout
	}
	if r.APIReader == nil {
		r.APIReader = mgr.GetAPIReader()
	}
	r.rancherMgr = rancher.NewManager(r.Client)
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.InstallAIExtension{}).
		Named("InstallAIExtension").
		Complete(r)
}
