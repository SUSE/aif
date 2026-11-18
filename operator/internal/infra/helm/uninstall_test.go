package helm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest"
)

func uninstallFixture() *release.Release {
	return &release.Release{
		Name: "demo", Namespace: "apps", Version: 1,
		Chart:    &chart.Chart{Metadata: &chart.Metadata{Name: "demo-chart", Version: "1.0.0"}},
		Info:     &release.Info{Status: release.StatusDeployed},
		Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: apps\n",
	}
}

func TestValidateUninstallRelease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*release.Release)
	}{
		{"namespace", func(r *release.Release) { r.Namespace = "other" }},
		{"release", func(r *release.Release) { r.Name = "other" }},
		{"chart", func(r *release.Release) { r.Chart.Metadata.Name = "other" }},
		{"missing metadata", func(r *release.Release) { r.Chart.Metadata = nil }},
		{"pending operation", func(r *release.Release) { r.Info.Status = release.StatusPendingUpgrade }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := uninstallFixture()
			tc.mutate(r)
			if err := validateUninstallRelease(r, "apps", "demo", "demo-chart"); err == nil {
				t.Fatal("accepted mismatched or pending release")
			}
		})
	}
	if err := validateUninstallRelease(uninstallFixture(), "apps", "demo", "demo-chart"); err != nil {
		t.Fatal(err)
	}
}

type uninstallKube struct {
	kubefake.FailingKubeClient
	deletes int
}

func (k *uninstallKube) DeleteWithPropagationPolicy(resources kube.ResourceList, propagation metav1.DeletionPropagation) (*kube.Result, []error) {
	k.deletes++
	return k.FailingKubeClient.DeleteWithPropagationPolicy(resources, propagation)
}

func uninstallTestConfig(t *testing.T, allow bool) (*rest.Config, *action.Configuration, *uninstallKube) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" || r.Header.Get("Authorization") != "Bearer operator-service-account" {
			t.Errorf("unexpected permission request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var review authorizationv1.SelfSubjectAccessReview
		if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if review.Spec.ResourceAttributes.Namespace != "apps" {
			t.Error("permission check escaped target namespace")
		}
		review.TypeMeta = metav1.TypeMeta{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"}
		review.Status.Allowed = allow
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&review)
	}))
	t.Cleanup(server.Close)
	k := &uninstallKube{FailingKubeClient: kubefake.FailingKubeClient{
		PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard},
		DummyResources: kube.ResourceList{&resource.Info{
			Namespace: "apps", Name: "demo", Object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "apps"}},
			Mapping: &meta.RESTMapping{Resource: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Scope: meta.RESTScopeNamespace},
		}},
	}}
	cfg := &action.Configuration{Releases: storage.Init(driver.NewMemory()), KubeClient: k, Capabilities: chartutil.DefaultCapabilities, Log: func(string, ...interface{}) {}}
	if err := cfg.Releases.Create(uninstallFixture()); err != nil {
		t.Fatal(err)
	}
	return &rest.Config{Host: server.URL, BearerToken: "operator-service-account", ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}}, cfg, k
}

func TestUninstallPermissionDeniedLeavesReleaseIntact(t *testing.T) {
	config, cfg, k := uninstallTestConfig(t, false)
	r, _ := cfg.Releases.Last("demo")
	err := uninstallRelease(context.Background(), config, cfg, r)
	if err == nil || !strings.Contains(err.Error(), "grant scoped uninstall RBAC") {
		t.Fatalf("error = %v", err)
	}
	if k.deletes != 0 {
		t.Fatal("started deletion before permission checks completed")
	}
	r, err = cfg.Releases.Last("demo")
	if err != nil || r.Info.Status != release.StatusDeployed {
		t.Fatalf("release must remain deployed: %+v, %v", r, err)
	}
}

func TestUninstallFailedWaitSurvivesRestartAndRetries(t *testing.T) {
	config, cfg, k := uninstallTestConfig(t, true)
	k.WaitError = errors.New("resources still terminating")
	r, _ := cfg.Releases.Last("demo")
	if err := uninstallRelease(context.Background(), config, cfg, r); err == nil {
		t.Fatal("expected cleanup wait failure")
	}
	r, err := cfg.Releases.Last("demo")
	if err != nil || r.Labels[uninstallStateLabel] != "pending" {
		t.Fatalf("lost durable cleanup state: %+v, %v", r, err)
	}
	// Helm itself reports uninstalled even on this failure. Our marker must
	// force a retry after loading the persisted record in a fresh executor.
	if r.Info.Status != release.StatusUninstalled {
		t.Fatalf("test must exercise Helm's uninstalled-on-failure behavior, got %s", r.Info.Status)
	}
	k.WaitError = nil
	restarted := &action.Configuration{Releases: cfg.Releases, KubeClient: k, Capabilities: chartutil.DefaultCapabilities, Log: cfg.Log}
	if err := uninstallRelease(context.Background(), config, restarted, r); err != nil {
		t.Fatal(err)
	}
	if k.deletes != 2 {
		t.Fatalf("deletes = %d, want retry", k.deletes)
	}
	if _, err := cfg.Releases.Last("demo"); !errors.Is(err, driver.ErrReleaseNotFound) {
		t.Fatalf("history not purged after success: %v", err)
	}
}

func TestUninstallFailedHookKeepsHistory(t *testing.T) {
	config, cfg, k := uninstallTestConfig(t, true)
	r, _ := cfg.Releases.Last("demo")
	r.Hooks = []*release.Hook{{Name: "cleanup", Manifest: r.Manifest, Events: []release.HookEvent{release.HookPostDelete}}}
	if err := cfg.Releases.Update(r); err != nil {
		t.Fatal(err)
	}
	k.WatchUntilReadyError = errors.New("cleanup hook failed")
	if err := uninstallRelease(context.Background(), config, cfg, r); err == nil {
		t.Fatal("expected hook failure")
	}
	r, err := cfg.Releases.Last("demo")
	if err != nil || r.Labels[uninstallStateLabel] != "pending" {
		t.Fatalf("hook failure lost release history: %v", err)
	}
}

func TestServiceRESTGetterUsesOnlyManagerIdentity(t *testing.T) {
	t.Setenv("KUBECONFIG", "/must-not-read-a-user-kubeconfig")
	g := &serviceRESTGetter{config: &rest.Config{Host: "https://kubernetes.default.svc", BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token"}, namespace: "apps"}
	got, err := g.ToRESTConfig()
	if err != nil || got.Host != g.config.Host || got.BearerTokenFile != g.config.BearerTokenFile {
		t.Fatalf("manager config not preserved: %+v %v", got, err)
	}
	got.Host = "changed"
	if g.config.Host == "changed" {
		t.Fatal("caller mutated the shared manager config")
	}
	namespace, _, err := g.ToRawKubeConfigLoader().Namespace()
	if err != nil || namespace != "apps" {
		t.Fatalf("namespace = %q, %v", namespace, err)
	}
}
