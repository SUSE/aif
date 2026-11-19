package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// MaxConcurrentUninstalls bounds foreground Helm waits across releases.
const MaxConcurrentUninstalls = 2

// ErrUninstallBusy tells the reconciler to requeue without occupying a worker.
var ErrUninstallBusy = errors.New("local Helm cleanup is busy")

// ServiceAccountUninstaller operates against the manager's Kubernetes config.
// It never obtains credentials from a user, Settings, or Rancher's Steve API.
type ServiceAccountUninstaller struct {
	config *rest.Config
	mu     sync.Mutex
	active map[string]bool
}

func NewServiceAccountUninstaller(config *rest.Config) *ServiceAccountUninstaller {
	return &ServiceAccountUninstaller{config: rest.CopyConfig(config), active: make(map[string]bool)}
}

func (u *ServiceAccountUninstaller) Uninstall(ctx context.Context, namespace, name, chartName string) error {
	if namespace == "" || name == "" || chartName == "" {
		return fmt.Errorf("uninstall requires namespace, release and chart name")
	}
	key := namespace + "/" + name
	u.mu.Lock()
	if u.active[key] || len(u.active) >= MaxConcurrentUninstalls {
		u.mu.Unlock()
		return ErrUninstallBusy
	}
	u.active[key] = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.active, key)
		u.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	config := rest.CopyConfig(u.config)
	// Helm's uninstall SDK has no RunWithContext. Bind its Kubernetes requests
	// to this reconcile's deadline and shutdown context instead.
	config.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) { return next.RoundTrip(r.Clone(ctx)) })
	})
	getter := &serviceRESTGetter{config: config, namespace: namespace}
	cfg := new(action.Configuration)
	if err := cfg.Init(getter, namespace, "secret", func(string, ...interface{}) {}); err != nil {
		return err
	}
	rel, err := cfg.Releases.Last(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Helm release: %w", err)
	}
	if err := validateUninstallRelease(rel, namespace, name, chartName); err != nil {
		return err
	}
	return uninstallRelease(ctx, config, cfg, rel)
}

const uninstallStateLabel = "ai-factory.suse.com/uninstall-state"

func uninstallRelease(ctx context.Context, config *rest.Config, cfg *action.Configuration, rel *release.Release) error {
	if rel.Labels[uninstallStateLabel] == "complete" ||
		(rel.Info.Status == release.StatusUninstalled && rel.Labels[uninstallStateLabel] == "") {
		return purgeReleaseHistory(cfg, rel.Name)
	}
	if err := preflightUninstall(ctx, config, cfg, rel); err != nil {
		return err
	}
	if rel.Labels == nil {
		rel.Labels = map[string]string{}
	}
	rel.Labels[uninstallStateLabel] = "pending"
	// Helm records "uninstalled" even when a post-delete hook or wait failed.
	// Keep a separate durable marker, and keep history, until the whole action
	// succeeds. A restart must retain enough information to retry cleanup.
	rel.Info.Status = release.StatusUninstalling
	if err := cfg.Releases.Update(rel); err != nil {
		return fmt.Errorf("record Helm cleanup intent: %w", err)
	}
	uninstall := action.NewUninstall(cfg)
	uninstall.DeletionPropagation = "foreground"
	uninstall.Timeout = 2 * time.Minute
	uninstall.Wait = true
	uninstall.KeepHistory = true
	result, err := uninstall.Run(rel.Name)
	if err != nil {
		return fmt.Errorf("Helm uninstall %s/%s: %w", rel.Namespace, rel.Name, err)
	}
	result.Release.Labels[uninstallStateLabel] = "complete"
	if err := cfg.Releases.Update(result.Release); err != nil {
		return fmt.Errorf("record completed Helm cleanup: %w", err)
	}
	return purgeReleaseHistory(cfg, rel.Name)
}

func purgeReleaseHistory(cfg *action.Configuration, name string) error {
	history, err := cfg.Releases.History(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// Keep the newest revision (and its completion marker) until last so a
	// partial purge cannot make a stale revision look like an active release.
	sort.Slice(history, func(i, j int) bool { return history[i].Version < history[j].Version })
	for _, revision := range history {
		if _, err := cfg.Releases.Delete(name, revision.Version); err != nil {
			return fmt.Errorf("purge Helm release history: %w", err)
		}
	}
	return nil
}

func validateUninstallRelease(rel *release.Release, namespace, name, chartName string) error {
	if rel == nil || rel.Name != name || rel.Namespace != namespace || rel.Chart == nil || rel.Chart.Metadata == nil || rel.Chart.Metadata.Name != chartName {
		return fmt.Errorf("refusing to uninstall a Helm release that does not match the AIWorkload namespace, release and chart")
	}
	if rel.Info == nil {
		return fmt.Errorf("Helm release has no status")
	}
	if rel.Info.Status.IsPending() {
		return fmt.Errorf("Helm release has an operation in progress; retry after it completes")
	}
	return nil
}

// Check all required permissions before Helm starts deleting resources. A
// chart with cluster-scoped resources or hooks can require explicit additional
// RBAC; never silently fall back to a personal token or grant cluster-admin.
func preflightUninstall(ctx context.Context, config *rest.Config, cfg *action.Configuration, rel *release.Release) error {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	seen := map[authorizationv1.ResourceAttributes]bool{}
	check := func(manifest string, honorKeep bool, verbs ...string) error {
		objects, err := cfg.KubeClient.Build(bytes.NewBufferString(manifest), false)
		if err != nil {
			return fmt.Errorf("inspect Helm uninstall resources: %w", err)
		}
		return objects.Visit(func(info *resource.Info, err error) error {
			if err != nil {
				return err
			}
			metadata, err := meta.Accessor(info.Object)
			if err != nil {
				return err
			}
			if honorKeep && strings.EqualFold(strings.TrimSpace(metadata.GetAnnotations()[kube.ResourcePolicyAnno]), kube.KeepPolicy) {
				return nil
			}
			for _, verb := range verbs {
				attrs := authorizationv1.ResourceAttributes{Namespace: info.Namespace, Verb: verb, Group: info.Mapping.Resource.Group, Resource: info.Mapping.Resource.Resource}
				if verb == "get" || verb == "delete" {
					attrs.Name = info.Name
				}
				if seen[attrs] {
					continue
				}
				seen[attrs] = true
				review, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &attrs}}, metav1.CreateOptions{})
				if err != nil {
					return fmt.Errorf("check uninstall permission: %w", err)
				}
				if !review.Status.Allowed {
					return fmt.Errorf("operator ServiceAccount needs %s on %s in namespace %q for this chart; grant scoped uninstall RBAC or uninstall the release from local Rancher Apps", verb, info.Mapping.Resource.GroupResource(), attrs.Namespace)
				}
			}
			return nil
		})
	}
	if err := check(rel.Manifest, true, "get", "delete"); err != nil {
		return err
	}
	for _, hook := range rel.Hooks {
		for _, event := range hook.Events {
			if event == release.HookPreDelete || event == release.HookPostDelete {
				if err := check(hook.Manifest, false, "get", "list", "watch", "create", "delete"); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type serviceRESTGetter struct {
	config    *rest.Config
	namespace string
}

func (g *serviceRESTGetter) ToRESTConfig() (*rest.Config, error) {
	return rest.CopyConfig(g.config), nil
}
func (g *serviceRESTGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	c, err := discovery.NewDiscoveryClientForConfig(g.config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(c), nil
}
func (g *serviceRESTGetter) ToRESTMapper() (meta.RESTMapper, error) {
	d, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(d), nil
}
func (g *serviceRESTGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveClientConfig(clientcmdapi.Config{
		CurrentContext: "operator", Contexts: map[string]*clientcmdapi.Context{"operator": {Namespace: g.namespace}},
	}, "operator", &clientcmd.ConfigOverrides{Context: clientcmdapi.Context{Namespace: g.namespace}}, nil)
}
