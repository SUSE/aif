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

// aif-operator/internal/api/aiworkload.go
package api

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/autovalues"
	"github.com/SUSE/aif-operator/internal/blueprintdeps"
	"github.com/SUSE/aif-operator/internal/blueprintpolicy"
	"github.com/SUSE/aif-operator/internal/kustomizeoverlay"
	"github.com/SUSE/aif-operator/internal/naming"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const aiWorkloadFieldOwner = "aif-operator-api"

const aiWorkloadKind = "AIWorkload"

// validateAIWorkloadSpec enforces the recovery-feature invariants with friendly errors,
// mirroring the CRD CEL rules for clients that read API responses. existing is nil on create.
func validateAIWorkloadSpec(spec aiplatformv1alpha1.AIWorkloadSpec, existing *aiplatformv1alpha1.AIWorkload) error {
	isBlueprint := spec.Source.SourceType == aiplatformv1alpha1.AIWorkloadSourceBlueprint
	if isBlueprint && len(spec.TargetClusters) == 0 {
		return fmt.Errorf("%w: Blueprint workloads require at least one target cluster", ErrInvalidInput)
	}
	if existing != nil && existing.Spec.DeployStrategy != "" && existing.Spec.DeployStrategy != spec.DeployStrategy {
		return fmt.Errorf("%w: deployStrategy is immutable", ErrInvalidInput)
	}
	return nil
}

// validateBlueprintComponentValues rejects ComponentValues entries whose
// componentName doesn't match any component of the referenced Blueprint, and
// rejects a spec that disables every component (a workload that deploys
// nothing is a user error, not a valid state). Install-time dependsOn and
// kustomize overrides are Blueprint-only; dependsOn must name known sibling
// components and keep the effective deploy order acyclic. A typo'd or stale component
// name would otherwise silently no-op at render time (the operator's
// resolveComponentValues skips unmatched entries defensively, since reconcile
// loops must stay self-healing) — this catches it here instead, at submit
// time, with a clear error. It returns the Blueprint's component names, or nil
// when no Blueprint was loaded.
func (h *AIWorkloadHandler) validateBlueprintComponentValues(ctx context.Context, spec aiplatformv1alpha1.AIWorkloadSpec) (map[string]bool, error) {
	if spec.Source.SourceType != aiplatformv1alpha1.AIWorkloadSourceBlueprint {
		if usesBlueprintOnlyOverrides(spec.ComponentValues) {
			return nil, fmt.Errorf("%w: kustomize and dependsOn are only supported for Blueprint workloads", ErrInvalidInput)
		}
		return nil, nil
	}
	if len(spec.ComponentValues) == 0 || spec.Source.Blueprint == nil {
		return nil, nil
	}
	crName := naming.BlueprintCRName(spec.Source.Blueprint.Name, spec.Source.Blueprint.Version)
	var bp aiplatformv1alpha1.Blueprint
	if err := h.client.Get(ctx, client.ObjectKey{Name: crName}, &bp); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: blueprint %q not found", ErrInvalidInput, crName)
		}
		return nil, err
	}
	known := make(map[string]bool, len(bp.Spec.Components))
	for _, c := range bp.Spec.Components {
		known[c.ChartName] = true
	}
	for _, ov := range spec.ComponentValues {
		if !known[ov.ComponentName] {
			return nil, fmt.Errorf("%w: componentValues references unknown component %q", ErrInvalidInput, ov.ComponentName)
		}
	}
	enabledCount := 0
	for _, c := range bp.Spec.Components {
		if effectiveComponentEnabled(spec.ComponentValues, c.ChartName) {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return nil, fmt.Errorf("%w: at least one component must remain enabled", ErrInvalidInput)
	}
	return known, validateInstallOverrides(bp.Spec.Components, spec.ComponentValues, known)
}

// validateInstallOverrides checks install-time dependsOn overrides against the
// Blueprint's components (known indexes their chartNames), and runs the same
// structural kustomize checks the controller runs before embedding an overlay.
// Whether a gitRef repo exists and holds the overlay is left to the controller:
// that can change after submit.
func validateInstallOverrides(components []aiplatformv1alpha1.BlueprintComponent, overrides []aiplatformv1alpha1.ComponentValueOverride, known map[string]bool) error {
	for _, ov := range overrides {
		if err := kustomizeoverlay.Validate(ov.Kustomize); err != nil {
			return fmt.Errorf("%w: componentValues[%s].kustomize: %v", ErrInvalidInput, ov.ComponentName, err)
		}
		if ov.DependsOn == nil {
			continue
		}
		for _, d := range *ov.DependsOn {
			if d == ov.ComponentName {
				return fmt.Errorf("%w: componentValues[%s].dependsOn: a component cannot depend on itself", ErrInvalidInput, ov.ComponentName)
			}
			if !known[d] {
				return fmt.Errorf("%w: componentValues[%s].dependsOn references unknown component %q", ErrInvalidInput, ov.ComponentName, d)
			}
		}
	}
	order := make([]string, len(components))
	for i, c := range components {
		order[i] = c.ChartName
	}
	if cycle := blueprintdeps.Cycle(order, blueprintdeps.EffectiveGraph(components, overrides)); cycle != nil {
		return fmt.Errorf("%w: deploy order has a dependency cycle: %s", ErrInvalidInput, strings.Join(cycle, " -> "))
	}
	return nil
}

// guardedOverrideFields are the install-time override fields only Blueprint
// workloads support, which an outdated AIWorkload CRD would silently prune.
var guardedOverrideFields = []guardedField[aiplatformv1alpha1.ComponentValueOverride]{
	{"kustomize", func(ov aiplatformv1alpha1.ComponentValueOverride) bool { return ov.Kustomize != nil }},
	{"dependsOn", func(ov aiplatformv1alpha1.ComponentValueOverride) bool { return ov.DependsOn != nil }},
}

func overrideKey(ov aiplatformv1alpha1.ComponentValueOverride) string { return ov.ComponentName }

// usesBlueprintOnlyOverrides reports whether any override sets a guarded field.
func usesBlueprintOnlyOverrides(overrides []aiplatformv1alpha1.ComponentValueOverride) bool {
	return anyGuardedFieldSet(overrides, guardedOverrideFields)
}

// droppedOverrideFields lists the override fields sent set that came back unset.
func droppedOverrideFields(sent, stored []aiplatformv1alpha1.ComponentValueOverride) []string {
	return droppedListFields("componentValues", sent, stored, overrideKey, guardedOverrideFields)
}

// writeWithCRDGuard guards an AIWorkload write against an outdated CRD pruning
// the Blueprint-only override fields (see writeGuardedByCRD).
func writeWithCRDGuard(wl *aiplatformv1alpha1.AIWorkload, write func(obj *aiplatformv1alpha1.AIWorkload, dryRun bool) error) error {
	sent := wl.Spec.ComponentValues
	return writeGuardedByCRD(wl, aiWorkloadKind, "ai-factory.suse.com_aiworkloads.yaml", usesBlueprintOnlyOverrides(sent), func(probe *aiplatformv1alpha1.AIWorkload) []string {
		return droppedOverrideFields(sent, probe.Spec.ComponentValues)
	}, write)
}

// validateUpdatePolicy rejects, up front, a Blueprint update the controller
// would refuse because rollback is disabled.
func (h *AIWorkloadHandler) validateUpdatePolicy(ctx context.Context, existing *aiplatformv1alpha1.AIWorkload, spec aiplatformv1alpha1.AIWorkloadSpec) error {
	src := spec.Source.Blueprint
	if spec.Source.SourceType != aiplatformv1alpha1.AIWorkloadSourceBlueprint || src == nil {
		return nil
	}
	blocked, floor, err := blueprintpolicy.MoveBlocked(ctx, h.client, src.Name, src.Version, existing.Status.MinimumSafeVersion, deployedVersion(existing))
	if err != nil {
		return err
	}
	if blocked {
		return rollbackBlockedError(src.Version, floor)
	}
	return nil
}

// deployedVersion is the workload's certified Blueprint version, "" when none.
func deployedVersion(wl *aiplatformv1alpha1.AIWorkload) string {
	if wl.Status.DeployedSource == nil {
		return ""
	}
	return wl.Status.DeployedSource.Version
}

// autoPinTargetsError rejects retargeting onto several clusters while an auto
// pin of a component the new spec still deploys from bp would be reused there.
func autoPinTargetsError(bp aiplatformv1alpha1.Blueprint, existing *aiplatformv1alpha1.AIWorkload, spec aiplatformv1alpha1.AIWorkloadSpec) error {
	if len(spec.TargetClusters) <= 1 || sameStringSet(spec.TargetClusters, existing.Spec.TargetClusters) {
		return nil
	}
	deployed := map[string]bool{}
	for _, c := range bp.Spec.Components {
		deployed[c.ChartName] = effectiveComponentEnabled(spec.ComponentValues, c.ChartName)
	}
	for _, p := range existing.Status.ResolvedValues {
		if p.Source == autovalues.SourceAuto && deployed[p.ComponentName] {
			return fmt.Errorf("%w: value %s of component %s was resolved automatically for one cluster, so this deployment can't target more than one cluster; install it again with the value set explicitly to deploy to several clusters",
				ErrInvalidInput, p.Path, p.ComponentName)
		}
	}
	return nil
}

func rollbackBlockedError(target, floor string) error {
	return fmt.Errorf("%w: %s is older than version %s, which this deployment has deployed or started deploying, and rollback is disabled for this Blueprint; use %s or newer", ErrInvalidInput, target, floor, floor)
}

func sameStringSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// effectiveComponentEnabled resolves whether a component is enabled the same way the
// controller's isComponentEnabled does: first matching override wins, no match or a nil
// Enabled defaults to enabled. Two or more overrides for the same componentName aren't
// rejected as invalid input elsewhere, so this must agree with the controller's own
// resolution — otherwise the validator could reject (or accept) a spec based on a
// different override than the one the controller actually applies.
func effectiveComponentEnabled(overrides []aiplatformv1alpha1.ComponentValueOverride, chartName string) bool {
	for _, ov := range overrides {
		if ov.ComponentName != chartName {
			continue
		}
		return ov.Enabled == nil || *ov.Enabled
	}
	return true
}

// AIWorkloadHandler serves AIWorkload CRUD endpoints.
type AIWorkloadHandler struct {
	client client.Client
	// authz is set by Register; it checks advanced customization.
	authz *Authorizer
	// apiReader, when set, reads the stored workload an update is checked
	// against uncached, so a stale cache cannot pass the advanced check.
	apiReader client.Reader
}

// NewAIWorkloadHandler constructs an AIWorkloadHandler.
func NewAIWorkloadHandler(c client.Client) *AIWorkloadHandler {
	return &AIWorkloadHandler{client: c}
}

// WithAPIReader sets the uncached reader (mgr.GetAPIReader()) for the stored
// workload an update is checked against. Without one the client is used.
func (h *AIWorkloadHandler) WithAPIReader(r client.Reader) *AIWorkloadHandler {
	h.apiReader = r
	return h
}

func (h *AIWorkloadHandler) storedReader() client.Reader {
	if h.apiReader != nil {
		return h.apiReader
	}
	return h.client
}

// Register wires the handler's routes onto the mux.
func (h *AIWorkloadHandler) Register(a *Authorizer) {
	h.authz = a
	a.HandleFunc("GET /api/v1/aiworkloads", h.listAIWorkloads)
	a.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads", h.createAIWorkload)
	a.HandleFunc("PATCH /api/v1/namespaces/{namespace}/aiworkloads/{name}", h.updateAIWorkload)
	a.HandleFunc("DELETE /api/v1/namespaces/{namespace}/aiworkloads/{name}", h.deleteAIWorkload)
	a.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/upgrade", h.upgradeAIWorkload)
	a.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/rollback", h.rollbackAIWorkload)
	a.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/retry", h.retryAIWorkload)
}

func (h *AIWorkloadHandler) listAIWorkloads(w http.ResponseWriter, r *http.Request) {
	var list aiplatformv1alpha1.AIWorkloadList
	if err := h.client.List(r.Context(), &list); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range list.Items {
		list.Items[i].ManagedFields = nil
		// The client strips TypeMeta from list items; restore it so consumers
		// (the UI's YAML view) get a self-describing object.
		list.Items[i].APIVersion = aiplatformv1alpha1.GroupVersion.String()
		list.Items[i].Kind = aiWorkloadKind
	}
	writeJSON(w, http.StatusOK, &list)
}

type aiWorkloadCreateBody struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec   aiplatformv1alpha1.AIWorkloadSpec    `json:"spec"`
	Status *aiplatformv1alpha1.AIWorkloadStatus `json:"status,omitempty"`
}

func (h *AIWorkloadHandler) createAIWorkload(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	if namespace == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: namespace is required", ErrInvalidInput))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body aiWorkloadCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	if err := validateAIWorkloadSpec(body.Spec, nil); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	known, err := h.validateBlueprintComponentValues(r.Context(), body.Spec)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if !stderrors.Is(err, ErrInvalidInput) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err)
		return
	}
	if !h.requireAdvancedCustomization(w, r, nil, body.Spec, known) {
		return
	}

	// Ensure the target namespace exists before creating the namespaced AIWorkload CR.
	ns := &corev1.Namespace{}
	ns.APIVersion = "v1"
	ns.Kind = "Namespace"
	ns.Name = namespace
	if err := h.client.Patch(
		r.Context(), ns, client.Apply,
		client.ForceOwnership,
		client.FieldOwner(aiWorkloadFieldOwner),
	); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("failed to ensure namespace %q: %w", namespace, err))
		return
	}

	wl := &aiplatformv1alpha1.AIWorkload{}
	wl.APIVersion = "ai-factory.suse.com/v1alpha1"
	wl.Kind = aiWorkloadKind
	wl.Name = body.Metadata.Name
	wl.Namespace = namespace
	wl.Spec = body.Spec

	err = writeWithCRDGuard(wl, func(obj *aiplatformv1alpha1.AIWorkload, dryRun bool) error {
		if dryRun {
			return h.client.Create(r.Context(), obj, client.DryRunAll)
		}
		return h.client.Create(r.Context(), obj)
	})
	if err != nil {
		if stderrors.Is(err, ErrInvalidInput) {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		if errors.IsAlreadyExists(err) {
			writeError(w, http.StatusConflict, fmt.Errorf(
				"deployment %q already exists in namespace %q — choose a different instance name or use Manage to update it",
				wl.Name, namespace,
			))
			return
		}
		if errors.IsInvalid(err) {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("%w: %v", ErrInvalidInput, err))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if body.Status != nil {
		if err := h.patchClientStatus(r.Context(), wl, body.Status); err != nil {
			log.Printf("api: failed to set initial AIWorkload status %s/%s: %v", namespace, body.Metadata.Name, err)
		}
	}

	wl.ManagedFields = nil
	writeJSON(w, http.StatusCreated, wl)
}

func (h *AIWorkloadHandler) updateAIWorkload(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")
	if namespace == "" || name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: namespace and name are required", ErrInvalidInput))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body aiWorkloadCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	existing := &aiplatformv1alpha1.AIWorkload{}
	if err := h.storedReader().Get(r.Context(), types.NamespacedName{Namespace: namespace, Name: name}, existing); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("deployment %q not found in namespace %q", name, namespace))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if err := validateAIWorkloadSpec(body.Spec, existing); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	known, err := h.validateBlueprintComponentValues(r.Context(), body.Spec)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if !stderrors.Is(err, ErrInvalidInput) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err)
		return
	}

	if err := h.validateUpdatePolicy(r.Context(), existing, body.Spec); err != nil {
		status := http.StatusBadRequest
		if !stderrors.Is(err, ErrInvalidInput) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err)
		return
	}

	if len(existing.Status.ResolvedValues) > 0 && body.Spec.Source.SourceType == aiplatformv1alpha1.AIWorkloadSourceBlueprint && body.Spec.Source.Blueprint != nil {
		crName := naming.BlueprintCRName(body.Spec.Source.Blueprint.Name, body.Spec.Source.Blueprint.Version)
		var bp aiplatformv1alpha1.Blueprint
		if err := h.client.Get(r.Context(), client.ObjectKey{Name: crName}, &bp); err != nil {
			if errors.IsNotFound(err) {
				writeError(w, http.StatusBadRequest, fmt.Errorf("%w: blueprint %q not found", ErrInvalidInput, crName))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		violations, err := lockedValueViolations(bp, existing.Status.ResolvedValues, body.Spec)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
			return
		}
		if len(violations) > 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %s", ErrInvalidInput, strings.Join(violations, "; ")))
			return
		}
		if err := autoPinTargetsError(bp, existing, body.Spec); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if !h.requireAdvancedCustomization(w, r, &existing.Spec, body.Spec, known) {
		return
	}

	wl := &aiplatformv1alpha1.AIWorkload{}
	wl.APIVersion = "ai-factory.suse.com/v1alpha1"
	wl.Kind = aiWorkloadKind
	wl.Name = name
	wl.Namespace = namespace
	wl.Spec = body.Spec

	err = writeWithCRDGuard(wl, func(obj *aiplatformv1alpha1.AIWorkload, dryRun bool) error {
		opts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(aiWorkloadFieldOwner)}
		if dryRun {
			opts = append(opts, client.DryRunAll)
		}
		return h.client.Patch(r.Context(), obj, client.Apply, opts...)
	})
	if err != nil {
		if stderrors.Is(err, ErrInvalidInput) {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		if errors.IsInvalid(err) {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("%w: %v", ErrInvalidInput, err))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if body.Status != nil {
		if err := h.patchClientStatus(r.Context(), wl, body.Status); err != nil {
			log.Printf("api: failed to update AIWorkload status %s/%s: %v", namespace, name, err)
		}
	}

	wl.ManagedFields = nil
	writeJSON(w, http.StatusOK, wl)
}

// patchClientStatus writes the status fields a client may set — the phase and
// per-cluster statuses the UI records after an install — onto wl's stored
// status. Everything else (pull-secret deliveries, conditions, operations,
// baselines…) is owned by the operator and is never taken from a request. A
// merge patch carries only these two fields and does not depend on the
// version last read, so a concurrent operator write neither fails it nor is
// overwritten by it.
func (h *AIWorkloadHandler) patchClientStatus(ctx context.Context, wl *aiplatformv1alpha1.AIWorkload, src *aiplatformv1alpha1.AIWorkloadStatus) error {
	// The phase is an enum, so an omitted one is sent as null (clearing it)
	// rather than "", which the API server would reject with the whole patch.
	var phase any
	if src.Phase != "" {
		phase = src.Phase
	}
	data, err := json.Marshal(map[string]any{"status": map[string]any{
		"phase":           phase,
		"clusterStatuses": src.ClusterStatuses,
	}})
	if err != nil {
		return err
	}
	return h.client.Status().Patch(ctx, wl, client.RawPatch(types.MergePatchType, data))
}

func (h *AIWorkloadHandler) deleteAIWorkload(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")
	if namespace == "" || name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: namespace and name are required", ErrInvalidInput))
		return
	}

	wl := &aiplatformv1alpha1.AIWorkload{}
	wl.Name = name
	wl.Namespace = namespace

	// A direct downstream Helm release is removed using the caller's existing
	// Rancher session. Only that caller can confirm external cleanup; the local
	// operator has no downstream kubeconfig and must not pretend it verified it.
	if confirmation := r.Header.Get("X-AIF-Completed-Helm-Uninstall"); confirmation != "" {
		if err := h.client.Get(r.Context(), client.ObjectKeyFromObject(wl), wl); err != nil {
			status := http.StatusInternalServerError
			if errors.IsNotFound(err) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}
		if expected := wl.ExternalHelmCleanupID(); expected == "" || confirmation != expected {
			writeError(w, http.StatusConflict, fmt.Errorf("workload release target changed; refresh and verify cleanup before retrying"))
			return
		}
		base := wl.DeepCopy()
		if wl.Annotations == nil {
			wl.Annotations = map[string]string{}
		}
		wl.Annotations[aiplatformv1alpha1.ExternalHelmCleanupAnnotation] = confirmation
		if err := h.client.Patch(r.Context(), wl, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
	}

	options := &client.DeleteOptions{}
	if wl.UID != "" {
		options.Preconditions = &metav1.Preconditions{UID: &wl.UID}
	}
	if err := h.client.Delete(r.Context(), wl, options); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("deployment %q not found in namespace %q", name, namespace))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Compile-time guard.
var _ Handler = (*AIWorkloadHandler)(nil)
