// Package labcatalog reconciles LabCatalog CRs: it fills status.entries from the
// embedded catalog (builtin) or from an external HTTPS document.
package labcatalog

import (
	"context"
	"fmt"
	"reflect"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/labs"
)

// RefreshInterval is how often external catalogs are re-fetched.
const RefreshInterval = 10 * time.Minute

// LabCatalogReconciler reconciles LabCatalog objects.
type LabCatalogReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Fetch downloads an external catalog; nil means labs.SafeFetch.
	Fetch labs.Fetcher
	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labcatalogs,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labcatalogs/status,verbs=get;update;patch

// EnsureBuiltin creates the builtin LabCatalog if it does not exist.
func EnsureBuiltin(ctx context.Context, c client.Client) error {
	cat := &v1alpha1.LabCatalog{}
	cat.Name = v1alpha1.LabCatalogBuiltin
	cat.Spec.DisplayName = "Built-in"
	if err := c.Create(ctx, cat); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *LabCatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cat v1alpha1.LabCatalog
	if err := r.Get(ctx, req.NamespacedName, &cat); err != nil {
		if apierrors.IsNotFound(err) && req.Name == v1alpha1.LabCatalogBuiltin {
			return ctrl.Result{}, EnsureBuiltin(ctx, r.Client)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !cat.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	fetch := r.Fetch
	if fetch == nil {
		fetch = labs.SafeFetch
	}

	s := *cat.Status.DeepCopy()
	s.ObservedGeneration = cat.Generation
	s.LastFetched = &metav1.Time{Time: now()}
	requeue := ctrl.Result{}

	switch {
	case cat.Spec.URL == "" && cat.Name == v1alpha1.LabCatalogBuiltin:
		s.Phase, s.Message, s.Skipped = v1alpha1.LabCatalogPhaseReady, "", 0
		s.Entries = labs.Catalog()
	case cat.Spec.URL == "":
		s.Phase, s.Message = v1alpha1.LabCatalogPhaseError, "spec.url is required"
	default:
		requeue.RequeueAfter = RefreshInterval
		doc, err := fetch(ctx, cat.Spec.URL)
		var entries []labs.Entry
		var skipped int
		var msg string
		if err == nil {
			entries, skipped, msg, err = labs.ParseExternal(doc)
		}
		if err != nil {
			// keep previous entries
			s.Phase, s.Message = v1alpha1.LabCatalogPhaseError, fmt.Sprintf("fetch failed: %v", err)
			break
		}
		s.Phase, s.Message, s.Skipped, s.Entries = v1alpha1.LabCatalogPhaseReady, msg, skipped, entries
	}

	// Builtin never refetches: skip the write when nothing changed. External
	// catalogs always write so lastFetched stays current (one event per 10m).
	if cat.Spec.URL == "" {
		prev := cat.Status.DeepCopy()
		prev.LastFetched, s.LastFetched = nil, nil
		if reflect.DeepEqual(*prev, s) {
			return requeue, nil
		}
		s.LastFetched = &metav1.Time{Time: now()}
	}
	cat.Status = s
	return requeue, r.Status().Update(ctx, &cat)
}

func (r *LabCatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.Add(ensureBuiltinRunnable{c: mgr.GetClient()}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.LabCatalog{}).
		Named("labcatalog").
		Complete(r)
}

// ensureBuiltinRunnable creates the builtin LabCatalog once the manager (and its
// cache) has started.
type ensureBuiltinRunnable struct{ c client.Client }

func (e ensureBuiltinRunnable) Start(ctx context.Context) error {
	if err := EnsureBuiltin(ctx, e.c); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
