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

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	ownershipNS = "aif-operator"
	// helmManager is the field manager Helm uses for server-side apply and Update.
	helmManager = "helm"
)

var partnerCatalogValues = map[string]any{
	"name": "partner-blueprints", "repoURL": "https://github.com/suse/partner-blueprints.git",
	"branch": "main", "paths": []any{"partners"},
}

const partnerCatalogJSON = `{"name":"partner-blueprints","repoURL":"https://github.com/suse/partner-blueprints.git","branch":"main","paths":["partners"]}`

func startOwnershipEnv(t *testing.T) client.Client {
	t.Helper()
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if entries, err := os.ReadDir(filepath.Join("..", "..", "bin", "k8s")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				env.BinaryAssetsDirectory = filepath.Join("..", "..", "bin", "k8s", e.Name())
				break
			}
		}
	}
	if env.BinaryAssetsDirectory == "" && os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("envtest binaries not found; run `make setup-envtest` (make test sets KUBEBUILDER_ASSETS)")
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	c, err := client.New(cfg, client.Options{Scheme: newSettingsScheme(t)})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := c.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ownershipNS}}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	return c
}

// chartSettings builds the Settings object a chart renders. withAnnotation=false
// renders it the way the 2.3.0 chart does (no helm-managed annotation).
func chartSettings(withAnnotation bool, catalogs ...map[string]any) *unstructured.Unstructured {
	meta := map[string]any{"name": "settings", "namespace": ownershipNS}
	obj := map[string]any{"apiVersion": "ai-factory.suse.com/v1alpha1", "kind": "Settings", "metadata": meta}
	if len(catalogs) > 0 {
		items := make([]any, 0, len(catalogs))
		names := make([]string, 0, len(catalogs))
		for _, c := range catalogs {
			items = append(items, c)
			names = append(names, c["name"].(string))
		}
		obj["spec"] = map[string]any{"blueprintCatalogs": items}
		if withAnnotation {
			b, _ := json.Marshal(map[string]any{"blueprintCatalogs": names})
			meta["annotations"] = map[string]any{aiplatformv1alpha1.SettingsHelmManagedAnnotation: string(b)}
		}
	}
	return &unstructured.Unstructured{Object: obj}
}

// applyAsChart server-side applies the chart's Settings the way Helm 4 does;
// manager lets a test stand in for a GitOps tool with a different manager name.
func applyAsChart(t *testing.T, c client.Client, manager string, u *unstructured.Unstructured) {
	t.Helper()
	if err := c.Patch(context.Background(), u, client.Apply, client.FieldOwner(manager), client.ForceOwnership); err != nil {
		t.Fatalf("apply as %s: %v", manager, err)
	}
}

func resetSettings(t *testing.T, c client.Client) {
	t.Helper()
	err := c.Delete(context.Background(), &aiplatformv1alpha1.Settings{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ownershipNS}})
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("delete settings: %v", err)
	}
}

func getSettingsCR(t *testing.T, c client.Client) aiplatformv1alpha1.Settings {
	t.Helper()
	var s aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ownershipNS, Name: "settings"}, &s); err != nil {
		t.Fatalf("get settings: %v", err)
	}
	return s
}

func catalogNames(s aiplatformv1alpha1.Settings) string {
	names := make([]string, 0, len(s.Spec.BlueprintCatalogs))
	for _, c := range s.Spec.BlueprintCatalogs {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// catalogOwners returns the field managers that own the catalog entry, sorted.
func catalogOwners(t *testing.T, c client.Client, name string) string {
	t.Helper()
	key := fmt.Sprintf(`k:{"name":%q}`, name)
	var owners []string
	for _, mf := range getSettingsCR(t, c).ManagedFields {
		if mf.FieldsV1 == nil {
			continue
		}
		var f map[string]any
		if err := json.Unmarshal(mf.FieldsV1.Raw, &f); err != nil {
			t.Fatalf("decode managedFields: %v", err)
		}
		spec, _ := f["f:spec"].(map[string]any)
		cats, _ := spec["f:blueprintCatalogs"].(map[string]any)
		if _, ok := cats[key]; ok {
			owners = append(owners, mf.Manager)
		}
	}
	sort.Strings(owners)
	return strings.Join(owners, ",")
}

func TestSettingsHelmOwnership_Envtest(t *testing.T) {
	c := startOwnershipEnv(t)
	h := newSettingsHandler(c, ownershipNS)
	teamA := `{"name":"team-a","repoURL":"https://git.example.com/a.git","branch":"main"}`

	t.Run("Settings save never co-owns a Helm entry, and Helm removal works afterwards", func(t *testing.T) {
		resetSettings(t, c)
		applyAsChart(t, c, helmManager, chartSettings(true, partnerCatalogValues))

		rec := putJSON(h, `{"spec":{"blueprintCatalogs":[`+partnerCatalogJSON+`,`+teamA+`]}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body)
		}
		var resp aiplatformv1alpha1.Settings
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if catalogNames(resp) != "partner-blueprints,team-a" || resp.Annotations[aiplatformv1alpha1.SettingsHelmManagedAnnotation] == "" {
			t.Fatalf("response must carry the Helm entry and annotation so the UI can render it read-only; got %s %v", catalogNames(resp), resp.Annotations)
		}
		if got := catalogOwners(t, c, "partner-blueprints"); got != helmManager {
			t.Fatalf("partner owners=%q want helm", got)
		}
		if got := catalogOwners(t, c, "team-a"); got != "aif-operator-api" {
			t.Fatalf("team-a owners=%q want aif-operator-api", got)
		}

		// helm upgrade --set-json 'blueprintCatalogs=[]'
		applyAsChart(t, c, helmManager, chartSettings(true))
		if got := catalogNames(getSettingsCR(t, c)); got != "team-a" {
			t.Fatalf("catalogs=%q want team-a (Helm entry removed, UI entry kept)", got)
		}
	})

	t.Run("Settings-page delete cannot remove a Helm entry", func(t *testing.T) {
		resetSettings(t, c)
		applyAsChart(t, c, helmManager, chartSettings(true, partnerCatalogValues))

		rec := putJSON(h, `{"spec":{}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body)
		}
		if got := catalogNames(getSettingsCR(t, c)); got != "partner-blueprints" {
			t.Fatalf("catalogs=%q want partner-blueprints", got)
		}
		if got := catalogOwners(t, c, "partner-blueprints"); got != helmManager {
			t.Fatalf("owners=%q want helm", got)
		}
	})

	t.Run("2.3.0 co-ownership self-heals on the first save after upgrading", func(t *testing.T) {
		resetSettings(t, c)
		// 2.3.0: the chart renders no annotation, and a Settings save co-owns the entry.
		applyAsChart(t, c, helmManager, chartSettings(false, partnerCatalogValues))
		if rec := putJSON(h, `{"spec":{"blueprintCatalogs":[`+partnerCatalogJSON+`]}}`); rec.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body)
		}
		if got := catalogOwners(t, c, "partner-blueprints"); got != "aif-operator-api,helm" {
			t.Fatalf("2.3.0 owners=%q want aif-operator-api,helm", got)
		}

		// helm upgrade to 2.3.1 adds the annotation; an old UI still sends the entry back.
		applyAsChart(t, c, helmManager, chartSettings(true, partnerCatalogValues))
		if rec := putJSON(h, `{"spec":{"blueprintCatalogs":[`+partnerCatalogJSON+`]}}`); rec.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body)
		}
		if got := catalogOwners(t, c, "partner-blueprints"); got != helmManager {
			t.Fatalf("owners after first 2.3.1 save=%q want helm", got)
		}

		applyAsChart(t, c, helmManager, chartSettings(true))
		if got := catalogNames(getSettingsCR(t, c)); got != "" {
			t.Fatalf("catalogs=%q want none", got)
		}
	})

	// Provenance comes from the annotation, not the manager name: Fleet and Argo
	// CD apply charts under their own field managers.
	t.Run("works when a GitOps tool applies the chart", func(t *testing.T) {
		resetSettings(t, c)
		applyAsChart(t, c, "argocd-controller", chartSettings(true, partnerCatalogValues))

		if rec := putJSON(h, `{"spec":{"blueprintCatalogs":[`+partnerCatalogJSON+`]}}`); rec.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body)
		}
		if got := catalogOwners(t, c, "partner-blueprints"); got != "argocd-controller" {
			t.Fatalf("owners=%q want argocd-controller", got)
		}
		applyAsChart(t, c, "argocd-controller", chartSettings(true))
		if got := catalogNames(getSettingsCR(t, c)); got != "" {
			t.Fatalf("catalogs=%q want none", got)
		}
	})

	// Helm 3 writes with client-side Update operations rather than SSA.
	t.Run("works when Helm 3 created the object with an Update", func(t *testing.T) {
		resetSettings(t, c)
		u := chartSettings(true, partnerCatalogValues)
		if err := c.Create(context.Background(), u, client.FieldOwner(helmManager)); err != nil {
			t.Fatalf("create as helm: %v", err)
		}
		if rec := putJSON(h, `{"spec":{"blueprintCatalogs":[`+partnerCatalogJSON+`]}}`); rec.Code != http.StatusOK {
			t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body)
		}
		if got := catalogOwners(t, c, "partner-blueprints"); got != helmManager {
			t.Fatalf("owners=%q want helm", got)
		}
	})
}
