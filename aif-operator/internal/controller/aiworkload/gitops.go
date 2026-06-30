package aiworkload

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	igit "github.com/SUSE/aif-operator/internal/git"
)

const (
	gitSyncAnnotation    = "ai-platform.suse.com/last-git-sync"
	operatorSettingsName = "settings"
)

// getHelmOp returns the HelmOp CR with the given name from either fleet namespace,
// or nil if not found in either.
func (r *AIWorkloadReconciler) getHelmOp(ctx context.Context, name string) (*unstructured.Unstructured, error) {
	for _, ns := range fleetNamespaces {
		ho := &unstructured.Unstructured{}
		ho.SetGroupVersionKind(helmOpGVK)
		err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, ho)
		if err == nil {
			return ho, nil
		}
		if !errors.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, nil
}

// reconcileGitOpsStatus handles the GitOps strategy reconcile loop.
func (r *AIWorkloadReconciler) reconcileGitOpsStatus(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) error {
	if len(w.Spec.FleetBundleNames) == 0 {
		return nil
	}
	bundleName := w.Spec.FleetBundleNames[0]
	ho, err := r.getHelmOp(ctx, bundleName)
	if err != nil {
		return err
	}
	if ho == nil {
		if w.Annotations[gitSyncAnnotation] == "" {
			// HelmOp not yet created — Fleet is still initialising from the git commit. Wait.
			w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhasePending
			return nil
		}
		// HelmOp existed before (we have a prior sync) and is now gone — git file deleted externally.
		return r.deleteAIWorkload(ctx, w)
	}
	if err := r.syncSpecFromHelmOp(ctx, w, ho); err != nil {
		return err
	}
	return r.mirrorFleetStatus(ctx, w)
}

// helmOpNamespace returns the effective namespace stored on a HelmOp's spec.
// Phase 5+ writes spec.defaultNamespace (Fleet DEFAULTER, allows cluster-scoped);
// older operator versions wrote spec.namespace (Fleet FORCER). Prefer the new
// field; fall back to the old one so sync-back works during the transition for
// HelmOps last written by an older operator.
func helmOpNamespace(obj *unstructured.Unstructured) string {
	if v, _, _ := unstructured.NestedString(obj.Object, "spec", "defaultNamespace"); v != "" {
		return v
	}
	v, _, _ := unstructured.NestedString(obj.Object, "spec", "namespace")
	return v
}

// syncSpecFromHelmOp reads chartVersion, namespace, and values from the HelmOp and
// writes them into the AIWorkload spec. Writes the last-git-sync annotation to
// prevent write-back loops in the Epic 3 UI edit path. No-ops if nothing changed.
func (r *AIWorkloadReconciler) syncSpecFromHelmOp(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, ho *unstructured.Unstructured) error {
	version, _, _ := unstructured.NestedString(ho.Object, "spec", "helm", "version")
	namespace := helmOpNamespace(ho)
	values, _, _ := unstructured.NestedMap(ho.Object, "spec", "helm", "values")

	newHash := helmOpHash(version, namespace, values)
	if w.Annotations[gitSyncAnnotation] == newHash {
		return nil
	}

	if w.Spec.Source.App != nil && version != "" {
		w.Spec.Source.App.ChartVersion = version
	}
	if namespace != "" {
		w.Spec.TargetNamespace = namespace
	}

	if len(values) > 0 && w.Spec.Source.App != nil {
		valJSON, err := json.Marshal(values)
		if err != nil {
			return fmt.Errorf("marshal helmop values: %w", err)
		}
		chartName := w.Spec.Source.App.ChartName
		updated := false
		for i, cv := range w.Spec.ComponentValues {
			if cv.ComponentName == chartName {
				w.Spec.ComponentValues[i].Values = &apixv1.JSON{Raw: valJSON}
				updated = true
				break
			}
		}
		if !updated {
			w.Spec.ComponentValues = append(w.Spec.ComponentValues, aiplatformv1alpha1.ComponentValueOverride{
				ComponentName: chartName,
				Values:        &apixv1.JSON{Raw: valJSON},
			})
		}
	}

	metav1.SetMetaDataAnnotation(&w.ObjectMeta, gitSyncAnnotation, newHash)
	return r.Update(ctx, w)
}

// helmOpHash returns a SHA-256 hex digest of the HelmOp fields used for spec sync.
func helmOpHash(version, namespace string, values map[string]any) string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	_ = enc.Encode(version)
	_ = enc.Encode(namespace)
	_ = enc.Encode(values)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// deleteAIWorkload removes the finalizer and deletes the AIWorkload CR.
// Used when the HelmOp disappears for a GitOps workload (git file deleted externally).
func (r *AIWorkloadReconciler) deleteAIWorkload(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) error {
	controllerutil.RemoveFinalizer(w, aiWorkloadFinalizer)
	if err := r.Update(ctx, w); err != nil {
		return err
	}
	return r.Delete(ctx, w)
}

// deleteGitFileByName commits the removal of workloads/<bundleName>.yaml from git.
// Errors are logged by the caller but do not block finalizer removal.
func (r *AIWorkloadReconciler) deleteGitFileByName(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, bundleName string) error {
	var s aiplatformv1alpha1.Settings
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: r.OperatorNamespace,
		Name:      operatorSettingsName,
	}, &s); err != nil {
		return fmt.Errorf("read settings: %w", err)
	}

	gc, err := igit.NewFromSettings(ctx, &s, r.OperatorNamespace, &controllerSecretReader{r.Client})
	if err != nil {
		return fmt.Errorf("init git client: %w", err)
	}

	filePath := "workloads/" + bundleName + ".yaml"
	_, err = gc.DeleteFile(ctx, filePath, "chore: remove workload "+bundleName)
	return err
}

// controllerSecretReader implements git.SecretReader using the controller's client.Client.
type controllerSecretReader struct {
	client client.Client
}

func (r *controllerSecretReader) ReadSecretKey(ctx context.Context, namespace, name, key string) (string, error) {
	var secret corev1.Secret
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return "", fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", key, name)
	}
	return string(val), nil
}
