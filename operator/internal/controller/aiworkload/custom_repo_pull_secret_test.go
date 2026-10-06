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
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
)

const (
	customTestOpNS     = "aif-operator"
	customTestTargetNS = "demo-ns"
	customTestRepo     = "private-charts"
	customTestAuth     = "private-charts-custom-repo-auth"
)

// newCustomTestRepo returns the custom-labelled ClusterRepo customTestRepo pointing at url,
// with spec.clientSecret referencing customTestAuth in cattle-system.
func newCustomTestRepo(url string) *unstructured.Unstructured {
	repo := newAppTestClusterRepo(customTestRepo, url)
	repo.SetLabels(map[string]string{credentials.CustomRepoLabel: credentials.LabelValueTrue})
	_ = unstructured.SetNestedField(repo.Object, customTestAuth, "spec", "clientSecret", "name")
	_ = unstructured.SetNestedField(repo.Object, "cattle-system", "spec", "clientSecret", "namespace")
	return repo
}

func newCustomTestAuthSecret(username, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: customTestAuth, Namespace: "cattle-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte(username),
			corev1.BasicAuthPasswordKey: []byte(password),
		},
	}
}

// newSUSESettingsWithAppCo configures AppCollection credentials so tests prove
// SUSE credentials never leak into a custom repo's pull secret.
func newSUSESettingsWithAppCo() []client.Object {
	return []client.Object{
		&aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: customTestOpNS},
			Spec: aiplatformv1alpha1.SettingsSpec{
				ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "username"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco", Key: "token"},
				},
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: customTestOpNS},
			Data:       map[string][]byte{"username": []byte("suse-user"), "token": []byte("suse-token")},
		},
	}
}

func newCustomTestWorkload(targetClusters []string) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: customTestTargetNS,
			TargetClusters:  targetClusters,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App: &aiplatformv1alpha1.AppSource{
					ChartRepo:    customTestRepo,
					ChartName:    "demo",
					ChartVersion: "0.1.0",
					Release:      "demo",
				},
			},
		},
	}
}

// dockerAuths decodes a dockerconfigjson secret's auths map.
func dockerAuths(t *testing.T, sec *corev1.Secret) map[string]map[string]any {
	t.Helper()
	var cfg struct {
		Auths map[string]map[string]any `json:"auths"`
	}
	if err := json.Unmarshal(sec.Data[corev1.DockerConfigJsonKey], &cfg); err != nil {
		t.Fatalf("decode dockerconfigjson: %v", err)
	}
	return cfg.Auths
}

func TestCustomRepoPullSecretName_RoundTrip(t *testing.T) {
	name := customRepoPullSecretName("private-charts")
	if name != "aif-custom-pull-private-charts" {
		t.Fatalf("customRepoPullSecretName = %q", name)
	}
	repo, ok := customRepoNameFromPullSecret(name)
	if !ok || repo != "private-charts" {
		t.Fatalf("customRepoNameFromPullSecret(%q) = %q, %v", name, repo, ok)
	}
	for _, other := range []string{combinedPullSecretName, nvidiaImagePullSecretName, "aif-custom-pull-"} {
		if _, ok := customRepoNameFromPullSecret(other); ok {
			t.Errorf("customRepoNameFromPullSecret(%q) unexpectedly matched", other)
		}
	}
}

func TestReconcileAppPullSecrets_CustomOCIRepoDeliversRepoCredentials(t *testing.T) {
	scheme := newAppTestScheme(t)
	objs := append(newSUSESettingsWithAppCo(),
		newCustomTestRepo("oci://ghcr.io/example/charts/demo"),
		newCustomTestAuthSecret("repo-user", "repo-token"))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}

	w := newCustomTestWorkload(nil) // local-default install
	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Fatalf("reconcileAppPullSecrets: %v", err)
	}

	want := customRepoPullSecretName(customTestRepo)
	if len(w.Status.PullSecretDeliveries) != 1 ||
		w.Status.PullSecretDeliveries[0].Namespace != customTestTargetNS ||
		len(w.Status.PullSecretDeliveries[0].Names) != 1 ||
		w.Status.PullSecretDeliveries[0].Names[0] != want {
		t.Fatalf("PullSecretDeliveries = %+v, want [{%q, [%q]}]", w.Status.PullSecretDeliveries, customTestTargetNS, want)
	}

	sec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: customTestTargetNS, Name: want}, sec); err != nil {
		t.Fatalf("custom pull secret not written locally: %v", err)
	}
	if sec.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("type = %v, want %v", sec.Type, corev1.SecretTypeDockerConfigJson)
	}
	auths := dockerAuths(t, sec)
	if len(auths) != 1 {
		t.Fatalf("auths = %v, want exactly the repo host", auths)
	}
	entry, ok := auths["ghcr.io"]
	if !ok || entry["username"] != "repo-user" || entry["password"] != "repo-token" {
		t.Errorf("auths[ghcr.io] = %v, want repo credentials", entry)
	}

	// The PR #244 gate still holds: no SUSE combined secret for a custom repo.
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: customTestTargetNS, Name: combinedPullSecretName}, &corev1.Secret{}); err == nil {
		t.Errorf("custom repo unexpectedly produced %q", combinedPullSecretName)
	}
}

func TestReconcileAppPullSecrets_CustomRepoHostKeepsPort(t *testing.T) {
	scheme := newAppTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newCustomTestRepo("oci://harbor.example.com:5443/charts"),
		newCustomTestAuthSecret("u", "p")).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}

	w := newCustomTestWorkload(nil)
	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Fatalf("reconcileAppPullSecrets: %v", err)
	}
	sec := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: customTestTargetNS, Name: customRepoPullSecretName(customTestRepo)}, sec); err != nil {
		t.Fatalf("custom pull secret missing: %v", err)
	}
	if _, ok := dockerAuths(t, sec)["harbor.example.com:5443"]; !ok {
		t.Errorf("auths = %v, want key harbor.example.com:5443", dockerAuths(t, sec))
	}
}

func TestReconcileAppPullSecrets_CustomRepoDownstreamOnlyWritesNothingLocally(t *testing.T) {
	scheme := newAppTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newCustomTestRepo("oci://ghcr.io/example/charts"),
		newCustomTestAuthSecret("u", "p")).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}

	w := newCustomTestWorkload([]string{"c-downstream"})
	if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
		t.Fatalf("reconcileAppPullSecrets: %v", err)
	}
	want := customRepoPullSecretName(customTestRepo)
	if len(w.Status.PullSecretDeliveries) != 1 || w.Status.PullSecretDeliveries[0].Names[0] != want {
		t.Fatalf("PullSecretDeliveries = %+v, want name %q recorded for downstream delivery", w.Status.PullSecretDeliveries, want)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: customTestTargetNS, Name: want}, &corev1.Secret{}); err == nil {
		t.Errorf("downstream-only workload wrote %q to the local cluster", want)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: customTestTargetNS}, &corev1.Namespace{}); err == nil {
		t.Errorf("downstream-only workload created local namespace %q", customTestTargetNS)
	}
}

func TestReconcileAppPullSecrets_CustomRepoWithoutUsableCredentials_NoDelivery(t *testing.T) {
	cases := []struct {
		name string
		url  string
		objs func() []client.Object
	}{
		{
			name: "credential secret missing",
			url:  "oci://ghcr.io/example/charts",
			objs: func() []client.Object { return nil },
		},
		{
			name: "empty password",
			url:  "oci://ghcr.io/example/charts",
			objs: func() []client.Object { return []client.Object{newCustomTestAuthSecret("u", "")} },
		},
		{
			name: "https repo with credentials",
			url:  "https://charts.example.com",
			objs: func() []client.Object { return []client.Object{newCustomTestAuthSecret("u", "p")} },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := newAppTestScheme(t)
			objs := append(tc.objs(), newCustomTestRepo(tc.url))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}

			w := newCustomTestWorkload(nil)
			if err := r.reconcileAppPullSecrets(context.Background(), w); err != nil {
				t.Fatalf("reconcileAppPullSecrets: %v", err)
			}
			if len(w.Status.PullSecretDeliveries) != 0 {
				t.Errorf("PullSecretDeliveries = %+v, want none", w.Status.PullSecretDeliveries)
			}
		})
	}
}

func TestPullSecretFactory_RebuildsCustomRepoSecret(t *testing.T) {
	scheme := newAppTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newCustomTestRepo("oci://ghcr.io/example/charts"),
		newCustomTestAuthSecret("repo-user", "rotated-token")).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}

	sec, err := r.pullSecretFactory(context.Background())("other-ns", customRepoPullSecretName(customTestRepo))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if sec == nil {
		t.Fatal("factory returned nil for a configured custom repo")
		return
	}
	if sec.Namespace != "other-ns" || sec.Name != customRepoPullSecretName(customTestRepo) {
		t.Errorf("secret = %s/%s", sec.Namespace, sec.Name)
	}
	if entry := dockerAuths(t, sec)["ghcr.io"]; entry["password"] != "rotated-token" {
		t.Errorf("auths[ghcr.io] = %v, want current repo credentials", entry)
	}
}

func TestPullSecretFactory_CustomRepoGoneOrNotCustom_Skips(t *testing.T) {
	notCustom := newAppTestClusterRepo(customTestRepo, "oci://ghcr.io/example/charts")
	_ = unstructured.SetNestedField(notCustom.Object, customTestAuth, "spec", "clientSecret", "name")
	cases := []struct {
		name string
		objs []client.Object
	}{
		{name: "repo deleted", objs: []client.Object{newCustomTestAuthSecret("u", "p")}},
		{name: "repo no longer custom", objs: []client.Object{notCustom, newCustomTestAuthSecret("u", "p")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := newAppTestScheme(t)
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objs...).Build()
			r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: customTestOpNS}

			sec, err := r.pullSecretFactory(context.Background())(customTestTargetNS, customRepoPullSecretName(customTestRepo))
			if err != nil {
				t.Fatalf("factory: %v", err)
			}
			if sec != nil {
				t.Errorf("factory returned %s, want nil", sec.Name)
			}
		})
	}
}
