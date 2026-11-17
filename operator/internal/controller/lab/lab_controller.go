// Package lab reconciles Lab CRs into child InstallAIExtension resources.
package lab

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labs,verbs=get;list;watch
// +kubebuilder:rbac:groups=ai-factory.suse.com,resources=labs/status,verbs=get;update;patch
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

	name := ChildName(lab.Name)
	status := v1alpha1.LabStatus{ExtensionRef: name, ObservedGeneration: lab.Generation, Conditions: lab.Status.Conditions}

	// The child name is fixed, so an InstallAIExtension of that name that this Lab
	// does not control is someone else's: never adopt, update or delete it.
	existing := &v1alpha1.InstallAIExtension{}
	if err := r.Get(ctx, client.ObjectKey{Name: name}, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		existing = nil
	}
	if existing != nil && !metav1.IsControlledBy(existing, &lab) {
		status.Phase = v1alpha1.LabPhaseFailed
		status.Message = fmt.Sprintf("InstallAIExtension %q already exists and is not owned by this lab; "+
			"remove it or use another lab name", name)
		return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
	}

	if !lab.Spec.Enabled {
		if existing != nil {
			uid := existing.UID
			err := r.Delete(ctx, existing, client.Preconditions{UID: &uid})
			if err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		status.Phase = v1alpha1.LabPhaseDisabled
		return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
	}

	src, ext, err := r.resolve(ctx, &lab)
	if err != nil {
		status.Phase = v1alpha1.LabPhaseFailed
		status.Message = err.Error()
		if existing != nil {
			status.Message += "; the previously installed version keeps running"
			setResolved(&status, existing.Spec.Source)
		}
		return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
	}

	if msg, err := r.conflict(ctx, &lab, existing, src, ext); err != nil {
		return ctrl.Result{}, err
	} else if msg != "" {
		status.Phase, status.Message = v1alpha1.LabPhaseFailed, msg
		return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
	}

	// Catalog labs: what is installed stays installed until the admin accepts a
	// new version (spec.catalogRef.version). A catalog change alone, including a
	// remote catalog's, never changes what runs.
	if ref := lab.Spec.CatalogRef; ref != nil {
		offered := entryVersion(src, ext)
		switch {
		case existing == nil && ref.Version != "" && ref.Version != offered:
			status.Phase = v1alpha1.LabPhaseFailed
			status.Message = fmt.Sprintf("accepted version %q is not offered by catalog %q (it offers %q); "+
				"set spec.catalogRef.version to %q to install it", ref.Version, ref.Catalog, offered, offered)
			status.UpdateAvailable = offered
			return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
		case existing != nil && !sameSpec(existing, src, ext):
			current := entryVersion(existing.Spec.Source, existing.Spec.Extension)
			accepted := ref.Version != "" && ref.Version == offered && ref.Version != current
			if !accepted {
				mirrorPhase(&status, existing)
				setResolved(&status, existing.Spec.Source)
				if offered != current {
					status.UpdateAvailable = offered
				} else if status.Message == "" {
					status.Message = fmt.Sprintf("catalog %q changed entry %q without a new version; not applied "+
						"(disable and re-enable the lab to accept it)", ref.Catalog, ref.Entry)
				}
				return ctrl.Result{}, r.updateStatus(ctx, &lab, status)
			}
		}
	}

	child := &v1alpha1.InstallAIExtension{}
	child.Name = name
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, child, func() error {
		if child.UID != "" && !metav1.IsControlledBy(child, &lab) {
			return fmt.Errorf("InstallAIExtension %q is not owned by lab %q", name, lab.Name)
		}
		child.Spec.Source = *src.DeepCopy()
		child.Spec.Extension = ext
		return controllerutil.SetControllerReference(&lab, child, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	setResolved(&status, child.Spec.Source)
	mirrorPhase(&status, child)
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

// conflict reports (as a status message) another InstallAIExtension that already
// uses this lab's UIPlugin name or Helm release. Two children sharing either would
// fight over one release, and deleting one would uninstall it for both. The older
// object wins; a lab whose own child is older keeps running.
func (r *LabReconciler) conflict(ctx context.Context, lab *v1alpha1.Lab, own *v1alpha1.InstallAIExtension,
	src v1alpha1.ExtensionSource, ext v1alpha1.ExtensionConfig) (string, error) {
	var list v1alpha1.InstallAIExtensionList
	if err := r.List(ctx, &list); err != nil {
		return "", err
	}
	mine := claims(src, ext)
	for i := range list.Items {
		o := &list.Items[i]
		if o.Name == ChildName(lab.Name) || !o.DeletionTimestamp.IsZero() {
			continue
		}
		if own != nil && olderThan(own, o) {
			continue
		}
		for n := range claims(o.Spec.Source, o.Spec.Extension) {
			if mine[n] {
				return fmt.Sprintf("name %q is already used by InstallAIExtension %q; "+
					"a lab must not share a UIPlugin or Helm release with another extension", n, o.Name), nil
			}
		}
	}
	return "", nil
}

// claims is the set of Helm release and UIPlugin names an extension occupies: the
// server release (from the chart URL) and the extension name (UIPlugin, and the
// extension chart's release).
func claims(src v1alpha1.ExtensionSource, ext v1alpha1.ExtensionConfig) map[string]bool {
	m := map[string]bool{ext.Name: true}
	if rel := labs.ReleaseName(src); rel != "" {
		m[rel] = true
	}
	return m
}

func olderThan(a, b *v1alpha1.InstallAIExtension) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}
	return a.Name < b.Name
}

// entryVersion is the version an admin accepts: the chart version for Helm, the
// extension version for Git.
func entryVersion(src v1alpha1.ExtensionSource, ext v1alpha1.ExtensionConfig) string {
	if src.Helm != nil {
		return src.Helm.Version
	}
	return ext.Version
}

func sameSpec(child *v1alpha1.InstallAIExtension, src v1alpha1.ExtensionSource, ext v1alpha1.ExtensionConfig) bool {
	return reflect.DeepEqual(child.Spec.Source, src) && child.Spec.Extension == ext
}

func setResolved(s *v1alpha1.LabStatus, src v1alpha1.ExtensionSource) {
	if src.Helm != nil {
		s.ResolvedChartURL, s.ResolvedVersion = src.Helm.ChartURL, src.Helm.Version
	}
}

func mirrorPhase(s *v1alpha1.LabStatus, child *v1alpha1.InstallAIExtension) {
	switch child.Status.Phase {
	case v1alpha1.InstallAIExtensionPhaseInstalling:
		s.Phase = v1alpha1.LabPhaseInstalling
	case v1alpha1.InstallAIExtensionPhaseInstalled:
		s.Phase = v1alpha1.LabPhaseReady
	case v1alpha1.InstallAIExtensionPhaseFailed:
		s.Phase = v1alpha1.LabPhaseFailed
		for _, c := range child.Status.Conditions {
			if c.Status == metav1.ConditionFalse && c.Message != "" {
				s.Message = c.Message
				break
			}
		}
	default:
		s.Phase = v1alpha1.LabPhasePending
	}
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

// failedLabs enqueues Failed Labs on any InstallAIExtension change, so a lab blocked
// by a name conflict or a foreign lab-<name> object recovers once that is removed.
func (r *LabReconciler) failedLabs(ctx context.Context, _ client.Object) []reconcile.Request {
	var list v1alpha1.LabList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, l := range list.Items {
		if l.Status.Phase == v1alpha1.LabPhaseFailed {
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
		Watches(&v1alpha1.InstallAIExtension{}, handler.EnqueueRequestsFromMapFunc(r.failedLabs)).
		Watches(&v1alpha1.LabCatalog{}, handler.EnqueueRequestsFromMapFunc(r.labsForCatalog)).
		Named("lab").
		Complete(r)
}
