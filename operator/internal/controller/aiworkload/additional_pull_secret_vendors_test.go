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

package aiworkload

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const extraVendorNS = "runai"

func extraVendorReconciler(t *testing.T, extra ...client.Object) (client.Client, *AIWorkloadReconciler) {
	t.Helper()
	objs := append(newSUSESettingsWithAppCo(), extra...)
	// newSUSESettingsWithAppCo returns the Settings first; add NVIDIA creds to it.
	settings := objs[0].(*aiplatformv1alpha1.Settings)
	settings.Spec.Nvidia = aiplatformv1alpha1.NvidiaSettings{
		UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc", Key: "username"},
		TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc", Key: "token"},
	}
	objs = append(objs, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc", Namespace: customTestOpNS},
		Data:       map[string][]byte{"username": []byte("$oauthtoken"), "token": []byte("nvapi-x")},
	})
	scheme := newAppTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return c, &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}
}

func runaiClusterComponent(extra ...aiplatformv1alpha1.ComponentVendor) aiplatformv1alpha1.BlueprintComponent {
	return aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "nvidia-runai", ChartName: "runai-cluster", ChartVersion: "2.26.50",
		Vendor: aiplatformv1alpha1.ComponentVendorNvidia, AdditionalPullSecretVendors: extra,
	}
}

func deliveredNames(w *aiplatformv1alpha1.AIWorkload, ns string) []string {
	for _, d := range w.Status.PullSecretDeliveries {
		if d.Namespace == ns {
			return d.Names
		}
	}
	return nil
}

func secretExists(t *testing.T, c client.Client, ns, name string) bool {
	t.Helper()
	err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &corev1.Secret{})
	return err == nil
}

func TestInjectComponentPullSecrets_AdditionalVendorDeliversWithoutTouchingValues(t *testing.T) {
	newVals := func() map[string]any {
		return map[string]any{"global": map[string]any{"additionalImagePullSecrets": []any{"ngc-secret"}, "customCA": map[string]any{"enabled": true}}}
	}
	run := func(c aiplatformv1alpha1.BlueprintComponent) (map[string]any, *aiplatformv1alpha1.AIWorkload, client.Client) {
		cl, r := extraVendorReconciler(t)
		w := newCustomTestWorkload(nil)
		vals := newVals()
		if err := r.injectComponentPullSecrets(context.Background(), w, c, clusterRepoInfo{}, extraVendorNS, vals); err != nil {
			t.Fatalf("injectComponentPullSecrets: %v", err)
		}
		return vals, w, cl
	}

	baseVals, baseW, _ := run(runaiClusterComponent())
	if got, want := deliveredNames(baseW, extraVendorNS), []string{nvidiaImagePullSecretName, nvidiaAPISecretName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("without extras: names = %v, want %v", got, want)
	}

	vals, w, c := run(runaiClusterComponent(aiplatformv1alpha1.ComponentVendorSUSE))
	if !reflect.DeepEqual(vals, baseVals) {
		t.Fatalf("an additional vendor changed the values:\n got %#v\nwant %#v", vals, baseVals)
	}
	want := []string{nvidiaImagePullSecretName, nvidiaAPISecretName, combinedPullSecretName}
	if got := deliveredNames(w, extraVendorNS); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for _, n := range want {
		if !secretExists(t, c, extraVendorNS, n) {
			t.Errorf("secret %s not written locally", n)
		}
	}
}

func TestInjectComponentPullSecrets_DedupesRepeatedSecrets(t *testing.T) {
	_, r := extraVendorReconciler(t)
	w := newCustomTestWorkload(nil)
	c := runaiClusterComponent(aiplatformv1alpha1.ComponentVendorNvidia, aiplatformv1alpha1.ComponentVendorSUSE, aiplatformv1alpha1.ComponentVendorSUSE)
	if err := r.injectComponentPullSecrets(context.Background(), w, c, clusterRepoInfo{}, extraVendorNS, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	want := []string{nvidiaImagePullSecretName, nvidiaAPISecretName, combinedPullSecretName}
	if got := deliveredNames(w, extraVendorNS); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestInjectComponentPullSecrets_SeveralComponentsShareOneNamespaceBucket(t *testing.T) {
	_, r := extraVendorReconciler(t)
	w := newCustomTestWorkload(nil)
	plain := aiplatformv1alpha1.BlueprintComponent{ChartRepo: "r", ChartName: "other", ChartVersion: "1"}
	for _, c := range []aiplatformv1alpha1.BlueprintComponent{runaiClusterComponent(aiplatformv1alpha1.ComponentVendorSUSE), plain} {
		if err := r.injectComponentPullSecrets(context.Background(), w, c, clusterRepoInfo{}, extraVendorNS, map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.Status.PullSecretDeliveries) != 1 {
		t.Fatalf("want one bucket, got %+v", w.Status.PullSecretDeliveries)
	}
	want := []string{nvidiaImagePullSecretName, nvidiaAPISecretName, combinedPullSecretName}
	if got := deliveredNames(w, extraVendorNS); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestInjectComponentPullSecrets_DownstreamOnlyWritesNothingLocally(t *testing.T) {
	c, r := extraVendorReconciler(t)
	w := newCustomTestWorkload([]string{"edge-1"})
	if err := r.injectComponentPullSecrets(context.Background(), w, runaiClusterComponent(aiplatformv1alpha1.ComponentVendorSUSE), clusterRepoInfo{}, extraVendorNS, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	want := []string{nvidiaImagePullSecretName, nvidiaAPISecretName, combinedPullSecretName}
	if got := deliveredNames(w, extraVendorNS); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for _, n := range want {
		if secretExists(t, c, extraVendorNS, n) {
			t.Errorf("secret %s written locally for a downstream-only workload", n)
		}
	}
}

// A custom repo's components only ever get the repo's own credentials, whatever
// vendors are requested.
func TestInjectComponentPullSecrets_CustomRepoNeverLeaksVendorCredentials(t *testing.T) {
	c, r := extraVendorReconciler(t,
		newCustomTestRepo("oci://ghcr.io/example/charts/demo"), newCustomTestAuthSecret("repo-user", "repo-token"))
	repoInfo, err := r.resolveClusterRepo(context.Background(), customTestRepo)
	if err != nil || !repoInfo.Custom {
		t.Fatalf("resolveClusterRepo = %+v, %v; want custom repo", repoInfo, err)
	}
	w := newCustomTestWorkload(nil)
	comp := runaiClusterComponent(aiplatformv1alpha1.ComponentVendorSUSE)
	comp.ChartRepo = customTestRepo
	if err := r.injectComponentPullSecrets(context.Background(), w, comp, repoInfo, extraVendorNS, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	want := []string{customRepoPullSecretName(customTestRepo)}
	if got := deliveredNames(w, extraVendorNS); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want only the repo's own %v", got, want)
	}
	for _, n := range []string{combinedPullSecretName, nvidiaImagePullSecretName, nvidiaAPISecretName} {
		if secretExists(t, c, extraVendorNS, n) {
			t.Errorf("%s leaked into a custom-repo component", n)
		}
	}
}

func TestDigest_AdditionalPullSecretVendors(t *testing.T) {
	if got := renderDigestLabelValue(perHelmOpRenderDigest(goldenNoOverlayInputs())); got != goldenNoOverlayDigest {
		t.Fatalf("digest of a component without extra vendors changed: %s", got)
	}
	with := goldenNoOverlayInputs()
	with.ExtraVendors = extraVendorStrings([]aiplatformv1alpha1.ComponentVendor{"suse"})
	if perHelmOpRenderDigest(with) == perHelmOpRenderDigest(goldenNoOverlayInputs()) {
		t.Fatal("additional vendors must change the render digest")
	}
	a := extraVendorStrings([]aiplatformv1alpha1.ComponentVendor{"suse", "nvidia"})
	b := extraVendorStrings([]aiplatformv1alpha1.ComponentVendor{"nvidia", "suse"})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("vendor order must not matter: %v vs %v", a, b)
	}
	if extraVendorStrings(nil) != nil {
		t.Fatal("no vendors must yield nil so the digest field is omitted")
	}
}
