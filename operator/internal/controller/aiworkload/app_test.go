package aiworkload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// newAppTestScheme builds a runtime.Scheme that knows about every type
// reconcileAppPullSecrets touches: aiplatform CRs, corev1 (Secret), and
// the unstructured ClusterRepo / Settings GVKs used by the injectors.
func newAppTestScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})
	return scheme
}

// newAppTestClusterRepo returns an unstructured ClusterRepo with a single
// spec.url field — enough for resolveClusterRepo to succeed.
func newAppTestClusterRepo(name, url string) *unstructured.Unstructured {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName(name)
	_ = unstructured.SetNestedField(repo.Object, url, "spec", "url")
	return repo
}

func TestReconcileAppPullSecrets_NvidiaVendorCreatesBothSecrets(t *testing.T) {
	const opNS = "aif-operator"
	const targetNS = "myapp-ns"

	scheme := newAppTestScheme(t)

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: "token"},
			},
		},
	}
	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-user", Namespace: opNS},
		Data:       map[string][]byte{"username": []byte("$oauthtoken")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-token", Namespace: opNS},
		Data:       map[string][]byte{"token": []byte("nvapi-test")},
	}
	repo := newAppTestClusterRepo("nvidia-blueprint-charts", "https://helm.ngc.nvidia.com/nvidia/blueprint")

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(settings, userSecret, tokenSecret).
		WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: targetNS,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App: &aiplatformv1alpha1.AppSource{
					ChartRepo:    "nvidia-blueprint-charts",
					ChartName:    "nvidia-blueprint-rag",
					ChartVersion: "v2.6.0",
					Release:      "rag",
					Vendor:       aiplatformv1alpha1.ComponentVendorNvidia,
				},
			},
		},
	}

	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Fatalf("reconcileAppPullSecrets: %v", err)
	}

	// Status must list both NVIDIA secret names, scoped to targetNS.
	have := map[string]bool{}
	for _, d := range w.Status.PullSecretDeliveries {
		if d.Namespace != targetNS {
			t.Errorf("Status.PullSecretDeliveries unexpected namespace %q", d.Namespace)
			continue
		}
		for _, n := range d.Names {
			have[n] = true
		}
	}
	for _, want := range []string{nvidiaImagePullSecretName, nvidiaAPISecretName} {
		if !have[want] {
			t.Errorf("PullSecretDeliveries missing %q in %s; got %+v", want, targetNS, w.Status.PullSecretDeliveries)
		}
	}

	// Both Secrets must materialize on the local (mgmt) cluster in targetNS.
	pull := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaImagePullSecretName}, pull); err != nil {
		t.Errorf("ngc-secret missing in %s: %v", targetNS, err)
	} else if pull.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("ngc-secret type = %v, want %v", pull.Type, corev1.SecretTypeDockerConfigJson)
	}
	api := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaAPISecretName}, api); err != nil {
		t.Errorf("ngc-api missing in %s: %v", targetNS, err)
	} else if api.Type != corev1.SecretTypeOpaque {
		t.Errorf("ngc-api type = %v, want %v", api.Type, corev1.SecretTypeOpaque)
	}
}

func TestReconcileAppPullSecrets_DefaultVendorRoutesToSuseInjector(t *testing.T) {
	const opNS = "aif-operator"
	const targetNS = "myapp-ns"

	scheme := newAppTestScheme(t)

	// AppCollection creds wired up → suseInjector produces a combined secret
	// keyed off the AppCollection host.
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec: aiplatformv1alpha1.SettingsSpec{
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "token"},
			},
		},
	}
	appcoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: opNS},
		Data: map[string][]byte{
			"username": []byte("user@example.com"),
			"token":    []byte("some-token"),
		},
	}
	repo := newAppTestClusterRepo("application-collection", "oci://dp.apps.rancher.io/charts")

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(settings, appcoSecret).
		WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: targetNS,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App: &aiplatformv1alpha1.AppSource{
					ChartRepo:    "application-collection",
					ChartName:    "milvus",
					ChartVersion: "1.0.0",
					Release:      "milvus",
					// Vendor unset → CRD default "suse" → suseInjector
				},
			},
		},
	}

	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Fatalf("reconcileAppPullSecrets: %v", err)
	}

	// Status must list the combined secret name (suseInjector's product), scoped to targetNS.
	if len(w.Status.PullSecretDeliveries) != 1 ||
		w.Status.PullSecretDeliveries[0].Namespace != targetNS ||
		len(w.Status.PullSecretDeliveries[0].Names) != 1 ||
		w.Status.PullSecretDeliveries[0].Names[0] != combinedPullSecretName {
		t.Errorf("PullSecretDeliveries = %+v, want exactly [{%q, [%q]}]",
			w.Status.PullSecretDeliveries, targetNS, combinedPullSecretName)
	}
	// Suse-injector must NOT have created NVIDIA-named secrets.
	nvSec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaImagePullSecretName}, nvSec); err == nil {
		t.Errorf("default vendor unexpectedly produced %q (NVIDIA path)", nvidiaImagePullSecretName)
	}
}

func TestReconcileAppPullSecrets_NoCredsConfigured_NoOp(t *testing.T) {
	const opNS = "aif-operator"
	scheme := newAppTestScheme(t)
	// Settings has zero credential refs → both injectors return nil names.
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	repo := newAppTestClusterRepo("application-collection", "oci://dp.apps.rancher.io/charts")
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(settings).WithObjects(repo).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "x",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App: &aiplatformv1alpha1.AppSource{
					ChartRepo: "application-collection", ChartName: "x", ChartVersion: "1", Release: "x",
					Vendor: aiplatformv1alpha1.ComponentVendorNvidia,
				},
			},
		},
	}
	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Errorf("expected nil error when no creds are configured, got %v", err)
	}
	if len(w.Status.PullSecretDeliveries) != 0 {
		t.Errorf("expected no deliveries when no creds configured, got %v", w.Status.PullSecretDeliveries)
	}
}

func TestReconcileAppPullSecrets_NoTargetNamespace_NoOp(t *testing.T) {
	scheme := newAppTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			// TargetNamespace deliberately empty
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App: &aiplatformv1alpha1.AppSource{
					ChartRepo: "x", ChartName: "x", ChartVersion: "1", Release: "x",
					Vendor: aiplatformv1alpha1.ComponentVendorNvidia,
				},
			},
		},
	}
	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Errorf("expected nil error with empty TargetNamespace, got %v", err)
	}
}

// TestReconcileAppPullSecrets_MissingClusterRepo_NoOp verifies the
// fail-soft path: when the chart's ClusterRepo doesn't exist (Rancher
// hasn't synced it yet, or the user is installing from a direct OCI URL
// without registering the repo), pull-secret injection is skipped but
// the rest of the App reconcile proceeds — secrets are an enhancement,
// not a precondition.
func TestReconcileAppPullSecrets_MissingClusterRepo_NoOp(t *testing.T) {
	scheme := newAppTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "x",
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App: &aiplatformv1alpha1.AppSource{
					ChartRepo: "does-not-exist", ChartName: "x", ChartVersion: "1", Release: "x",
					Vendor: aiplatformv1alpha1.ComponentVendorNvidia,
				},
			},
		},
	}
	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Fatalf("expected nil (fail-soft) for missing ClusterRepo, got: %v", err)
	}
	if len(w.Status.PullSecretDeliveries) != 0 {
		t.Errorf("expected no deliveries, got %v", w.Status.PullSecretDeliveries)
	}
}
