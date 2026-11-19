package api

import (
	"encoding/json"
	"os/exec"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"sigs.k8s.io/yaml"
)

func TestHelmUpgradeRetiresTokenWithoutDeletingUserSettings(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is needed to exercise the rendered chart upgrade")
	}
	for _, reuseValues := range []bool{false, true} {
		args := []string{"template", "aif", "../../../charts/aif-operator", "--namespace", "aif-operator", "--show-only", "templates/settings.yaml"}
		if reuseValues {
			args = append(args, "--set", "rancherCatalog.token=old-unused-token")
		}
		rendered, err := exec.Command("helm", args...).Output()
		if err != nil {
			t.Fatal(err)
		}
		desired, err := yaml.YAMLToJSON(rendered)
		if err != nil {
			t.Fatal(err)
		}
		// This is PR 383's manifest when only rancherCatalog was declared in
		// values. User settings were subsequently added through the API.
		previous := []byte(`{"apiVersion":"ai-factory.suse.com/v1alpha1","kind":"Settings","metadata":{"name":"settings","namespace":"aif-operator"},"spec":{"rancherCatalog":{"tokenSecretRef":{"name":"aif-operator-rancher-catalog","key":"token"}}}}`)
		current := []byte(`{"apiVersion":"ai-factory.suse.com/v1alpha1","kind":"Settings","metadata":{"name":"settings","namespace":"aif-operator"},"spec":{"rancherCatalog":{"tokenSecretRef":{"name":"aif-operator-rancher-catalog","key":"token"}},"fleet":{"repoURL":"https://git.internal/gitops.git"},"customRepos":[{"name":"private","type":"git","gitRepo":"https://git.internal/charts.git"}]}}`)
		patch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(previous, desired, current)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := jsonpatch.MergePatch(current, patch)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Spec struct {
				Fleet          map[string]string
				CustomRepos    []map[string]string
				RancherCatalog any
			}
		}
		if err := json.Unmarshal(updated, &got); err != nil {
			t.Fatal(err)
		}
		if got.Spec.Fleet["repoURL"] != "https://git.internal/gitops.git" || len(got.Spec.CustomRepos) != 1 || got.Spec.RancherCatalog != nil {
			t.Fatalf("reuseValues=%v: upgrade damaged user settings or retained chart-owned token configuration: %s", reuseValues, updated)
		}
	}
}
