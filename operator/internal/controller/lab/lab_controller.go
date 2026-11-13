// Package lab reconciles Lab CRs into child InstallAIExtension resources.
package lab

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/labs"
)

// LabReconciler reconciles Lab objects.
type LabReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labs/finalizers,verbs=update
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labcatalogs,verbs=get;list;watch
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=installaiextensions,verbs=get;list;watch;create;update;patch;delete

func ChildName(labName string) string { return "lab-" + labName }

func (r *LabReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var lab v1alpha1.Lab
	if err := r.Get(ctx, req.NamespacedName, &lab); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !lab.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil // child garbage-collected via ownerRef
	}

	child := &v1alpha1.InstallAIExtension{}
	child.Name = ChildName(lab.Name)
	status := v1alpha1.LabStatus{ExtensionRef: child.Name, ObservedGeneration: lab.Generation, Conditions: lab.Status.Conditions}

	if !lab.Spec.Enabled {
		if err := r.Delete(ctx, child); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		status.Phase = v1alpha1.LabPhaseDisabled
		return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
	}

	src, ext, err := r.resolve(ctx, &lab)
	if err != nil {
		status.Phase = v1alpha1.LabPhaseFailed
		status.Message = err.Error()
		return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
	}
	if src.Helm != nil {
		status.ResolvedChartURL, status.ResolvedVersion = src.Helm.ChartURL, src.Helm.Version
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, child, func() error {
		child.Spec.Source = *src.DeepCopy()
		child.Spec.Extension = ext
		return controllerutil.SetControllerReference(&lab, child, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	switch child.Status.Phase {
	case v1alpha1.InstallAIExtensionPhaseInstalling:
		status.Phase = v1alpha1.LabPhaseInstalling
	case v1alpha1.InstallAIExtensionPhaseInstalled:
		status.Phase = v1alpha1.LabPhaseReady
	case v1alpha1.InstallAIExtensionPhaseFailed:
		status.Phase = v1alpha1.LabPhaseFailed
		for _, c := range child.Status.Conditions {
			if c.Status == "False" && c.Message != "" {
				status.Message = c.Message
				break
			}
		}
	default:
		status.Phase = v1alpha1.LabPhasePending
	}
	return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
}

// resolve returns the effective source and extension (from the catalog entry or the
// inline spec) after the name checks. ValidateNames applies to every Lab: a lab must
// not claim the Helm release or UIPlugin of the core AIF UI or of any other extension.
func (r *LabReconciler) resolve(ctx context.Context, lab *v1alpha1.Lab) (v1alpha1.ExtensionSource, v1alpha1.ExtensionConfig, error) {
	var src v1alpha1.ExtensionSource
	var ext v1alpha1.ExtensionConfig
	switch ref := lab.Spec.CatalogRef; {
	case ref != nil:
		var cat v1alpha1.LabCatalog
		if err := r.Get(ctx, client.ObjectKey{Name: ref.Catalog}, &cat); err != nil {
			if apierrors.IsNotFound(err) {
				return src, ext, fmt.Errorf("lab catalog %q not found", ref.Catalog)
			}
			return src, ext, err
		}
		found := false
		for _, e := range cat.Status.Entries {
			if e.Name == ref.Entry {
				src, ext, found = *e.Source.DeepCopy(), e.Extension, true
				break
			}
		}
		if !found {
			return src, ext, fmt.Errorf("entry %q not found in lab catalog %q", ref.Entry, ref.Catalog)
		}
	case lab.Spec.Source != nil && lab.Spec.Extension != nil:
		src, ext = *lab.Spec.Source.DeepCopy(), *lab.Spec.Extension
	default:
		return src, ext, fmt.Errorf("set catalogRef, or both source and extension")
	}
	if err := labs.ValidateNames(src, ext); err != nil {
		return src, ext, err
	}
	return src, ext, nil
}

// labsForCatalog enqueues the Labs that reference the changed LabCatalog.
func (r *LabReconciler) labsForCatalog(ctx context.Context, obj client.Object) []reconcile.Request {
	var list v1alpha1.LabList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, l := range list.Items {
		if l.Spec.CatalogRef != nil && l.Spec.CatalogRef.Catalog == obj.GetName() {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&l)})
		}
	}
	return reqs
}

func (r *LabReconciler) updateStatus(ctx context.Context, lab *v1alpha1.Lab, s v1alpha1.LabStatus) error {
	if reflect.DeepEqual(lab.Status, s) {
		return nil
	}
	lab.Status = s
	return r.Status().Update(ctx, lab)
}

func (r *LabReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Lab{}).
		Owns(&v1alpha1.InstallAIExtension{}).
		Watches(&v1alpha1.LabCatalog{}, handler.EnqueueRequestsFromMapFunc(r.labsForCatalog)).
		Named("lab").
		Complete(r)
}
