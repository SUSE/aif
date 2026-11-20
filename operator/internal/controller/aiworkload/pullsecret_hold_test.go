package aiworkload

import (
	"context"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

// newHeldDeliveryReconciler builds a two-component Blueprint workload whose
// "cluster" component stays held behind "control-plane" (no Bundle yet). With
// withCreds, Settings carry AppCollection credentials so rendering
// control-plane records a pull-secret delivery.
func newHeldDeliveryReconciler(t *testing.T, targetCluster string, withCreds bool, extra ...client.Object) (*AIWorkloadReconciler, client.Client) {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	addBundleScheme(scheme)

	w := newBlueprintWorkload(time.Now())
	w.Finalizers = []string{aiWorkloadFinalizer}
	w.Spec.TargetClusters = []string{targetCluster}
	w.Spec.Source.Blueprint = &aiplatformv1alpha1.BlueprintSource{Name: "hold", Version: "1.0.0"}

	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("hold", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: "application-collection", ChartName: "cluster", ChartVersion: "1.0.0", Vendor: "suse", DependsOn: []string{"control-plane"}},
		{ChartRepo: "application-collection", ChartName: "control-plane", ChartVersion: "1.0.0", Vendor: "suse"},
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName("application-collection")
	_ = unstructured.SetNestedField(repo.Object, "https://charts.example.com", "spec", "url")

	objs := append([]client.Object{w, bp, repo}, extra...)
	if withCreds {
		objs = append(objs, appCollectionCredObjects()...)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		Build()
	return &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}, c
}

// appCollectionCredObjects configures AppCollection credentials in Settings so
// the combined pull secret can be built.
func appCollectionCredObjects() []client.Object {
	return []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: "aif-operator"},
			Data:       map[string][]byte{"username": []byte("user@example.com"), "token": []byte("appco-token")},
		},
		&aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
			Spec: aiplatformv1alpha1.SettingsSpec{
				ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "username"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "token"},
				},
			},
		},
	}
}

func reconcileHeldWorkload(t *testing.T, r *AIWorkloadReconciler) (ctrl.Result, *aiplatformv1alpha1.AIWorkload) {
	t.Helper()
	key := types.NamespacedName{Name: "wl", Namespace: "aif-operator"}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var got aiplatformv1alpha1.AIWorkload
	if err := r.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get workload: %v", err)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, conditionTypeReady); cond == nil || cond.Reason != "WaitingForDependencies" {
		t.Fatalf("expected Ready reason WaitingForDependencies, got %+v", cond)
	}
	return res, &got
}

func pullSecretBundleNames(t *testing.T, c client.Client) []string {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(bundleGVK.GroupVersion().WithKind("BundleList"))
	if err := c.List(context.Background(), list, client.InNamespace("fleet-default")); err != nil {
		t.Fatalf("list bundles: %v", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, b := range list.Items {
		names = append(names, b.GetName())
	}
	return names
}

func TestReconcile_HeldComponentsStillDeliverDownstreamPullSecrets(t *testing.T) {
	r, c := newHeldDeliveryReconciler(t, "c-down", true)

	res, got := reconcileHeldWorkload(t, r)

	if len(got.Status.PullSecretDeliveries) == 0 {
		t.Fatal("rendering control-plane should record a pull-secret delivery")
	}
	want := "ai-pullsecrets-wl-c-down-install-ns"
	if names := pullSecretBundleNames(t, c); !slices.Contains(names, want) {
		t.Fatalf("pull-secret Bundle %q must be delivered while components are held; fleet-default has %v", want, names)
	}
	if res.RequeueAfter != dependencyHoldRequeue {
		t.Fatalf("expected the hold requeue %v, got %+v", dependencyHoldRequeue, res)
	}
}

func TestReconcile_HeldComponentsLocalDeliveryRunsDuringHold(t *testing.T) {
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "install-ns"}}
	r, c := newHeldDeliveryReconciler(t, "local", true, sa)

	res, _ := reconcileHeldWorkload(t, r)

	var patched corev1.ServiceAccount
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "install-ns", Name: "default"}, &patched); err != nil {
		t.Fatalf("get SA: %v", err)
	}
	if !slices.ContainsFunc(patched.ImagePullSecrets, func(ref corev1.LocalObjectReference) bool { return ref.Name == combinedPullSecretName }) {
		t.Fatalf("delivery must merge %s into the default SA while components are held, got %+v", combinedPullSecretName, patched.ImagePullSecrets)
	}
	if res.RequeueAfter != dependencyHoldRequeue {
		t.Fatalf("expected the hold requeue %v, got %+v", dependencyHoldRequeue, res)
	}
}

func TestReconcile_HeldComponentsWithoutDeliveriesUnchanged(t *testing.T) {
	r, c := newHeldDeliveryReconciler(t, "c-down", false)

	res, got := reconcileHeldWorkload(t, r)

	if len(got.Status.PullSecretDeliveries) != 0 {
		t.Fatalf("expected no deliveries without credentials, got %+v", got.Status.PullSecretDeliveries)
	}
	if names := pullSecretBundleNames(t, c); len(names) != 0 {
		t.Fatalf("expected no Bundles in fleet-default, got %v", names)
	}
	if res != (ctrl.Result{RequeueAfter: dependencyHoldRequeue}) {
		t.Fatalf("expected the hold requeue %v, got %+v", dependencyHoldRequeue, res)
	}
}

// A status requeue longer than the unsettled-delivery one (no catalog client
// for a git-backed component: 1 minute) yields to the delivery's 30 s.
func TestReconcile_UnsettledDeliveryBeatsLongerStatusRequeue(t *testing.T) {
	scheme := sentinelResultScheme(t)
	w := newBlueprintWorkload(time.Now())
	w.Finalizers = []string{aiWorkloadFinalizer}
	w.Status.PullSecretDeliveries = []aiplatformv1alpha1.PullSecretDelivery{
		{Namespace: "install-ns", Names: []string{combinedPullSecretName}},
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "install-ns"}}
	objs := append([]client.Object{w, gitBlueprintCR("rancher-charts"), gitClusterRepo("rancher-charts"), sa}, appCollectionCredObjects()...)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&aiplatformv1alpha1.AIWorkload{}).
		Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	key := types.NamespacedName{Name: "wl", Namespace: "aif-operator"}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got aiplatformv1alpha1.AIWorkload
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get workload: %v", err)
	}
	if cond := meta.FindStatusCondition(got.Status.Conditions, conditionTypeReady); cond == nil || cond.Reason != "CatalogClientNotConfigured" {
		t.Fatalf("expected Ready reason CatalogClientNotConfigured (1 minute requeue), got %+v", cond)
	}
	if res != (ctrl.Result{RequeueAfter: 30 * time.Second}) {
		t.Fatalf("expected the unsettled-delivery requeue 30s over the 1 minute status one, got %+v", res)
	}
}

func TestSoonerRequeue(t *testing.T) {
	cases := []struct {
		name string
		a, b ctrl.Result
		want ctrl.Result
	}{
		{"neither", ctrl.Result{}, ctrl.Result{}, ctrl.Result{}},
		{"only first", ctrl.Result{RequeueAfter: 15 * time.Second}, ctrl.Result{}, ctrl.Result{RequeueAfter: 15 * time.Second}},
		{"only second", ctrl.Result{}, ctrl.Result{RequeueAfter: 30 * time.Second}, ctrl.Result{RequeueAfter: 30 * time.Second}},
		{"shorter first", ctrl.Result{RequeueAfter: 15 * time.Second}, ctrl.Result{RequeueAfter: 30 * time.Second}, ctrl.Result{RequeueAfter: 15 * time.Second}},
		{"shorter second", ctrl.Result{RequeueAfter: time.Minute}, ctrl.Result{RequeueAfter: 30 * time.Second}, ctrl.Result{RequeueAfter: 30 * time.Second}},
		{"immediate wins", ctrl.Result{Requeue: true}, ctrl.Result{RequeueAfter: 30 * time.Second}, ctrl.Result{Requeue: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := soonerRequeue(tc.a, tc.b); got != tc.want {
				t.Fatalf("soonerRequeue(%+v, %+v) = %+v, want %+v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
