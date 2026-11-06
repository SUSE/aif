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
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/blueprintdeps"
	"github.com/SUSE/aif-operator/internal/kustomizeoverlay"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const aiWorkloadFieldOwner = "aif-operator-api"

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
// time, with a clear error.
func (h *AIWorkloadHandler) validateBlueprintComponentValues(ctx context.Context, spec aiplatformv1alpha1.AIWorkloadSpec) error {
	if spec.Source.SourceType != aiplatformv1alpha1.AIWorkloadSourceBlueprint {
		if usesBlueprintOnlyOverrides(spec.ComponentValues) {
			return fmt.Errorf("%w: kustomize and dependsOn are only supported for Blueprint workloads", ErrInvalidInput)
		}
		return nil
	}
	if len(spec.ComponentValues) == 0 || spec.Source.Blueprint == nil {
		return nil
	}
	crName := blueprintCRName(spec.Source.Blueprint.Name, spec.Source.Blueprint.Version)
	var bp aiplatformv1alpha1.Blueprint
	if err := h.client.Get(ctx, client.ObjectKey{Name: crName}, &bp); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("%w: blueprint %q not found", ErrInvalidInput, crName)
		}
		return err
	}
	known := make(map[string]bool, len(bp.Spec.Components))
	for _, c := range bp.Spec.Components {
		known[c.ChartName] = true
	}
	for _, ov := range spec.ComponentValues {
		if !known[ov.ComponentName] {
			return fmt.Errorf("%w: componentValues references unknown component %q", ErrInvalidInput, ov.ComponentName)
		}
	}
	enabledCount := 0
	for _, c := range bp.Spec.Components {
		if effectiveComponentEnabled(spec.ComponentValues, c.ChartName) {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return fmt.Errorf("%w: at least one component must remain enabled", ErrInvalidInput)
	}
	return validateInstallOverrides(bp.Spec.Components, spec.ComponentValues, known)
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

// usesBlueprintOnlyOverrides reports whether any override sets an install-time
// field that only Blueprint workloads support (and that an outdated AIWorkload
// CRD would silently prune).
func usesBlueprintOnlyOverrides(overrides []aiplatformv1alpha1.ComponentValueOverride) bool {
	for _, ov := range overrides {
		if ov.Kustomize != nil || ov.DependsOn != nil {
			return true
		}
	}
	return false
}

// droppedOverrideFields lists the install-time override fields that were sent
// set but came back unset from the API server, which prunes fields its stored
// CRD schema doesn't know.
func droppedOverrideFields(sent, stored []aiplatformv1alpha1.ComponentValueOverride) []string {
	var dropped []string
	for i, ov := range sent {
		var got aiplatformv1alpha1.ComponentValueOverride
		if i < len(stored) {
			got = stored[i]
		}
		if ov.Kustomize != nil && got.Kustomize == nil {
			dropped = append(dropped, fmt.Sprintf("componentValues[%s].kustomize", ov.ComponentName))
		}
		if ov.DependsOn != nil && got.DependsOn == nil {
			dropped = append(dropped, fmt.Sprintf("componentValues[%s].dependsOn", ov.ComponentName))
		}
	}
	return dropped
}

// writeWithCRDGuard runs write, preceded by a server-side dry run of the same
// write when the spec uses fields an outdated AIWorkload CRD would silently
// prune (Helm never upgrades CRDs). If the dry run drops any of them, nothing
// is persisted and an ErrInvalidInput naming the fix is returned.
func writeWithCRDGuard(wl *aiplatformv1alpha1.AIWorkload, write func(obj *aiplatformv1alpha1.AIWorkload, dryRun bool) error) error {
	if usesBlueprintOnlyOverrides(wl.Spec.ComponentValues) {
		probe := wl.DeepCopy()
		if err := write(probe, true); err != nil {
			return err
		}
		if dropped := droppedOverrideFields(wl.Spec.ComponentValues, probe.Spec.ComponentValues); len(dropped) > 0 {
			return fmt.Errorf("%w: AIWorkload CRD is outdated (%s were dropped); apply charts/aif-operator/crds/ai-factory.suse.com_aiworkloads.yaml",
				ErrInvalidInput, strings.Join(dropped, ", "))
		}
	}
	return write(wl, false)
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
}

// NewAIWorkloadHandler constructs an AIWorkloadHandler.
func NewAIWorkloadHandler(c client.Client) *AIWorkloadHandler {
	return &AIWorkloadHandler{client: c}
}

// Register wires the handler's routes onto the mux.
func (h *AIWorkloadHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/aiworkloads", h.listAIWorkloads)
	mux.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads", h.createAIWorkload)
	mux.HandleFunc("PATCH /api/v1/namespaces/{namespace}/aiworkloads/{name}", h.updateAIWorkload)
	mux.HandleFunc("DELETE /api/v1/namespaces/{namespace}/aiworkloads/{name}", h.deleteAIWorkload)
	mux.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/upgrade", h.upgradeAIWorkload)
	mux.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/rollback", h.rollbackAIWorkload)
	mux.HandleFunc("POST /api/v1/namespaces/{namespace}/aiworkloads/{name}/retry", h.retryAIWorkload)
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
		list.Items[i].Kind = "AIWorkload"
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
	if err := h.validateBlueprintComponentValues(r.Context(), body.Spec); err != nil {
		status := http.StatusUnprocessableEntity
		if !stderrors.Is(err, ErrInvalidInput) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err)
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
	wl.Kind = "AIWorkload"
	wl.Name = body.Metadata.Name
	wl.Namespace = namespace
	wl.Spec = body.Spec

	err := writeWithCRDGuard(wl, func(obj *aiplatformv1alpha1.AIWorkload, dryRun bool) error {
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
	if err := h.client.Get(r.Context(), types.NamespacedName{Namespace: namespace, Name: name}, existing); err != nil {
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
	if err := h.validateBlueprintComponentValues(r.Context(), body.Spec); err != nil {
		status := http.StatusUnprocessableEntity
		if !stderrors.Is(err, ErrInvalidInput) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err)
		return
	}

	wl := &aiplatformv1alpha1.AIWorkload{}
	wl.APIVersion = "ai-factory.suse.com/v1alpha1"
	wl.Kind = "AIWorkload"
	wl.Name = name
	wl.Namespace = namespace
	wl.Spec = body.Spec

	err := writeWithCRDGuard(wl, func(obj *aiplatformv1alpha1.AIWorkload, dryRun bool) error {
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

	if err := h.client.Delete(r.Context(), wl); err != nil {
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
