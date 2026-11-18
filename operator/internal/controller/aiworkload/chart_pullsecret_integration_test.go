package aiworkload

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

// Exercise the actual Blueprint output paths with override-only values. Helm
// merges the chart defaults later, so the injector must not guess their types.
func TestBlueprintNvidiaChartValues(t *testing.T) {
	for _, chartName := range []string{"network-operator", "gpu-operator", nimOperatorChartName} {
		for _, delivery := range []string{"helmop", "gitops", "git-chart"} {
			t.Run(chartName+"/"+delivery, func(t *testing.T) {
				ctx := context.Background()
				cl, r := buildNvidiaInjectorFixture(t)
				r.Scheme.AddKnownTypeWithName(clusterRepoGVK, &unstructured.Unstructured{})
				r.Scheme.AddKnownTypeWithName(helmOpGVK, &unstructured.Unstructured{})
				r.Scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
				repoSpec := map[string]any{"url": "https://charts.example.test"}
				if delivery == "git-chart" {
					repoSpec = map[string]any{"gitRepo": "https://git.example.test/charts", "gitBranch": "main"}
					r.CatalogClient = rancher.NewHolder()
					r.CatalogClient.Set(fakeCatalog{tgz: makeChartTgz(t, map[string]string{
						chartName + "/Chart.yaml": "apiVersion: v2\nname: " + chartName + "\nversion: 1.0.0\n",
					})})
				}
				if err := cl.Create(ctx, repoObj("nvidia", repoSpec)); err != nil {
					t.Fatal(err)
				}
				w := newGitOpsTestWorkload()
				w.Spec.TargetClusters = []string{"c-downstream"}
				if err := cl.Create(ctx, w); err != nil {
					t.Fatal(err)
				}
				c := aiplatformv1alpha1.BlueprintComponent{
					ChartRepo: "nvidia", ChartName: chartName, ChartVersion: "1.0.0",
					Vendor: aiplatformv1alpha1.ComponentVendorNvidia,
				}
				var output map[string]any
				if delivery == "gitops" {
					remote := newBlueprintGitOpsRemote(t)
					var settings aiplatformv1alpha1.Settings
					if err := cl.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: operatorSettingsName}, &settings); err != nil {
						t.Fatal(err)
					}
					settings.Spec.Fleet.GitRepoSource = aiplatformv1alpha1.GitRepoSource{RepoURL: remote, Branch: "main"}
					if err := cl.Update(ctx, &settings); err != nil {
						t.Fatal(err)
					}
					if _, err := r.ensureBlueprintGitFile(ctx, w, c, "wl-operator"); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(readBlueprintGitOpsFile(t, remote, "workloads/wl-operator.yaml")), &output); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := r.ensureBlueprintHelmOp(ctx, w, c, "wl-operator"); err != nil {
						t.Fatal(err)
					}
					obj := &unstructured.Unstructured{}
					obj.SetGroupVersionKind(helmOpGVK)
					if delivery == "git-chart" {
						obj.SetGroupVersionKind(bundleGVK)
					}
					if err := cl.Get(ctx, types.NamespacedName{Namespace: "fleet-default", Name: "wl-operator"}, obj); err != nil {
						t.Fatal(err)
					}
					output = obj.Object
				}
				values, found, err := unstructured.NestedMap(output, "spec", "helm", "values")
				if err != nil || !found {
					t.Fatalf("chart values: found=%v err=%v", found, err)
				}
				op, _ := values["operator"].(map[string]any)
				if chartName == nimOperatorChartName {
					image, _ := op["image"].(map[string]any)
					if !reflect.DeepEqual(image["pullSecrets"], []any{nvidiaImagePullSecretName}) {
						t.Fatalf("missing NIM pull secret: %#v", op)
					}
				} else if _, exists := op["image"]; exists {
					t.Fatalf("chart image default overridden: %#v", op)
				}
				if chartName == "network-operator" && !reflect.DeepEqual(op["imagePullSecrets"], []any{nvidiaImagePullSecretName}) {
					t.Fatalf("network operator pull secrets must be strings: %#v", op)
				}
			})
		}
	}
}

// These expressions reproduce network-operator 26.7.1's operator image and
// pull-secret contracts from templates/operator.yaml and templates/_helpers.tpl:
// Chart source: https://helm.ngc.nvidia.com/nvidia (version 26.7.1).
// The Deployment and keep-ncp/upgrade-crd hooks all use these same expressions.
// Keep this fixture offline; it exercises Helm's default-value coalescing as
// well as Kubernetes decoding of the resulting LocalObjectReference list.
func TestNetworkOperatorHelmValuesContract(t *testing.T) {
	const podSpec = `containers:
- name: operator
  image: "{{ .Values.operator.repository }}/{{ .Values.operator.image }}:{{ .Values.operator.tag | default .Chart.AppVersion }}"
imagePullSecrets:
{{- range (.Values.operator.imagePullSecrets | default .Values.imagePullSecrets) }}
- {{ dict "name" . | toJson }}
{{- end }}
`
	for _, scenario := range []string{"defaults", "private-mirror", "custom-image"} {
		t.Run(scenario, func(t *testing.T) {
			values := map[string]any{}
			wantImage := "nvcr.io/nvidia/cloud-native/network-operator:v26.7.1"
			wantSecrets := []corev1.LocalObjectReference{{Name: nvidiaImagePullSecretName}}
			if scenario != "defaults" {
				values["operator"] = map[string]any{
					"repository":       "registry.internal.example/nvidia",
					"imagePullSecrets": []any{"mirror-auth"},
				}
				wantImage = "registry.internal.example/nvidia/network-operator:v26.7.1"
				wantSecrets = append(wantSecrets, corev1.LocalObjectReference{Name: "mirror-auth"})
			}
			if scenario == "custom-image" {
				op := values["operator"].(map[string]any)
				op["image"], op["tag"] = "mirrored-network-operator", "26.7.1-internal"
				wantImage = "registry.internal.example/nvidia/mirrored-network-operator:26.7.1-internal"
			}
			injectNvidiaPullSecretRefs(values, "network-operator")
			adaptPullSecretShapeForChart("network-operator", values)
			// Reconciliation must remain idempotent.
			injectNvidiaPullSecretRefs(values, "network-operator")
			adaptPullSecretShapeForChart("network-operator", values)
			fixture := &chart.Chart{
				Metadata: &chart.Metadata{APIVersion: "v2", Name: "network-operator", Version: "26.7.1", AppVersion: "v26.7.1"},
				Values: map[string]any{"operator": map[string]any{
					"repository": "nvcr.io/nvidia/cloud-native", "image": "network-operator", "imagePullSecrets": []any{},
				}},
				Templates: []*chart.File{{Name: "templates/podspec.yaml", Data: []byte(podSpec)}},
			}
			renderValues, err := chartutil.ToRenderValues(fixture, values, chartutil.ReleaseOptions{Name: "test", Namespace: "test", IsInstall: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := engine.Render(fixture, renderValues)
			if err != nil {
				t.Fatal(err)
			}
			var spec corev1.PodSpec
			if err := yaml.UnmarshalStrict([]byte(rendered["network-operator/templates/podspec.yaml"]), &spec); err != nil {
				t.Fatalf("chart rendered invalid PodSpec: %v", err)
			}
			if len(spec.Containers) != 1 || spec.Containers[0].Image != wantImage {
				t.Fatalf("rendered containers: %#v, want image %q", spec.Containers, wantImage)
			}
			if !reflect.DeepEqual(spec.ImagePullSecrets, wantSecrets) {
				t.Fatalf("imagePullSecrets = %#v, want %#v", spec.ImagePullSecrets, wantSecrets)
			}
		})
	}
}
