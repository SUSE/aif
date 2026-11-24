package helm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestServiceAccountUninstallAgainstKubernetes(t *testing.T) {
	env := &envtest.Environment{}
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		entries, _ := os.ReadDir("../../../bin/k8s")
		for _, entry := range entries {
			if entry.IsDir() {
				env.BinaryAssetsDirectory = filepath.Join("../../../bin/k8s", entry.Name())
				break
			}
		}
		if env.BinaryAssetsDirectory == "" {
			t.Skip("envtest binaries not found; run make setup-envtest")
		}
	}
	config, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "apps"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo", "kept"} {
		if _, err := admin.CoreV1().ConfigMaps("apps").Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	role, err := admin.RbacV1().Roles("apps").Create(ctx, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "uninstaller"}, Rules: []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "update", "delete"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get"}},
	}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.RbacV1().RoleBindings("apps").Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "uninstaller"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "operator", Namespace: "apps"}},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: role.Name},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	// envtest issues an x509 identity with the ServiceAccount username. The
	// Kubernetes RBAC authorizer evaluates the actual SA RoleBinding above.
	identity, err := env.AddUser(envtest.User{Name: "system:serviceaccount:apps:operator", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:apps"}}, config)
	if err != nil {
		t.Fatal(err)
	}
	service, err := kubernetes.NewForConfig(identity.Config())
	if err != nil {
		t.Fatal(err)
	}
	permission := func(verb, resource string) bool {
		review, err := service.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Namespace: "apps", Verb: verb, Resource: resource}},
		}, metav1.CreateOptions{})
		return err == nil && review.Status.Allowed
	}
	waitForPermission := func(verb, resource string) {
		t.Helper()
		for !permission(verb, resource) {
			select {
			case <-ctx.Done():
				t.Fatal("RBAC grant did not become effective")
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	waitForPermission("list", "secrets")
	store := storage.Init(driver.NewSecrets(admin.CoreV1().Secrets("apps")))
	r := uninstallFixture()
	r.Manifest += "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: kept\n  namespace: apps\n  annotations:\n    helm.sh/resource-policy: keep\n"
	if err := store.Create(r); err != nil {
		t.Fatal(err)
	}
	u := NewServiceAccountUninstaller(identity.Config())
	if err := u.Uninstall(ctx, "apps", "demo", "demo-chart"); err == nil || !strings.Contains(err.Error(), "grant scoped uninstall RBAC") {
		t.Fatalf("expected scoped-permission failure: %v", err)
	}
	stored, err := store.Last("demo")
	if err != nil || stored.Info.Status != release.StatusDeployed {
		t.Fatalf("permission failure changed release state: %v", err)
	}
	if _, err := admin.CoreV1().ConfigMaps("apps").Get(ctx, "demo", metav1.GetOptions{}); err != nil {
		t.Fatal("permission failure removed release resources")
	}
	role.Rules[1].Verbs = append(role.Rules[1].Verbs, "delete")
	if _, err := admin.RbacV1().Roles("apps").Update(ctx, role, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitForPermission("delete", "configmaps")
	// envtest has no garbage-collector controller. Emulate its removal of the
	// foreground-deletion finalizer for this leaf fixture so Helm's real wait
	// observes the same disappearance it would observe on a running cluster.
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		for {
			cm, err := admin.CoreV1().ConfigMaps("apps").Get(ctx, "demo", metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return
			}
			if err == nil && cm.DeletionTimestamp != nil {
				cm.Finalizers = nil
				_, _ = admin.CoreV1().ConfigMaps("apps").Update(ctx, cm, metav1.UpdateOptions{})
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	defer func() { cancel(); <-gcDone }()
	if err := u.Uninstall(ctx, "apps", "demo", "demo-chart"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.CoreV1().ConfigMaps("apps").Get(ctx, "demo", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("release resource survived: %v", err)
	}
	if _, err := admin.CoreV1().ConfigMaps("apps").Get(ctx, "kept", metav1.GetOptions{}); err != nil {
		t.Fatalf("Helm keep policy was not respected: %v", err)
	}
	if _, err := store.Last("demo"); !errors.Is(err, driver.ErrReleaseNotFound) {
		t.Fatalf("release history survived successful cleanup: %v", err)
	}
	if err := u.Uninstall(ctx, "apps", "demo", "demo-chart"); err != nil {
		t.Fatalf("retry after cleanup is not idempotent: %v", err)
	}
}
