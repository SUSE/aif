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
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/blueprintpolicy"
	"github.com/SUSE/aif-operator/internal/kustomizeoverlay"
	"github.com/SUSE/aif-operator/internal/naming"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var nonAlphanumRE = regexp.MustCompile(`[^a-z0-9]+`)

// validateComponentOverlays runs the controller's structural kustomize checks
// on every component overlay, so a bad one is rejected on save rather than
// surfacing later as a failed install.
func validateComponentOverlays(components []aiplatformv1alpha1.BlueprintComponent) error {
	for _, c := range components {
		if err := kustomizeoverlay.Validate(c.Kustomize); err != nil {
			return fmt.Errorf("%w: components[%s].kustomize: %v", ErrInvalidInput, c.ChartName, err)
		}
	}
	return nil
}

// validateComponentValuesFrom checks every valuesFrom reference with the API
// server's own name rules, so a typo is rejected on save rather than surfacing
// as a failed Fleet deploy.
func validateComponentValuesFrom(components []aiplatformv1alpha1.BlueprintComponent) error {
	for _, c := range components {
		for i, vf := range c.ValuesFrom {
			if err := validateValuesFromRef(vf); err != nil {
				return fmt.Errorf("%w: components[%s].valuesFrom[%d]: %s", ErrInvalidInput, c.ChartName, i, err)
			}
		}
	}
	return nil
}

func validateValuesFromRef(vf aiplatformv1alpha1.ComponentValuesFrom) error {
	if (vf.ConfigMapKeyRef == nil) == (vf.SecretKeyRef == nil) {
		return fmt.Errorf("set exactly one of configMapKeyRef or secretKeyRef")
	}
	ref := vf.SecretKeyRef
	if ref == nil {
		ref = vf.ConfigMapKeyRef
	}
	if errs := validation.IsDNS1123Subdomain(ref.Name); len(errs) > 0 {
		return fmt.Errorf("name %q: %s", ref.Name, strings.Join(errs, "; "))
	}
	if ref.Namespace != "" {
		if errs := validation.IsDNS1123Label(ref.Namespace); len(errs) > 0 {
			return fmt.Errorf("namespace %q: %s", ref.Namespace, strings.Join(errs, "; "))
		}
	}
	if ref.Key != "" {
		if errs := validation.IsConfigMapKey(ref.Key); len(errs) > 0 {
			return fmt.Errorf("key %q: %s", ref.Key, strings.Join(errs, "; "))
		}
	}
	return nil
}

func validateBlueprintComponents(components []aiplatformv1alpha1.BlueprintComponent) error {
	if err := validateComponentOverlays(components); err != nil {
		return err
	}
	if err := validateComponentValuesFrom(components); err != nil {
		return err
	}
	if err := blueprintpolicy.CheckValuesFromNamespaces(components, ""); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return nil
}

// guardedComponentFields are the Blueprint component fields an outdated CRD
// would silently prune.
var guardedComponentFields = []guardedField[aiplatformv1alpha1.BlueprintComponent]{
	{"valuesFrom", func(c aiplatformv1alpha1.BlueprintComponent) bool { return len(c.ValuesFrom) > 0 }},
	{"dependsOn", func(c aiplatformv1alpha1.BlueprintComponent) bool { return len(c.DependsOn) > 0 }},
	{"kustomize", func(c aiplatformv1alpha1.BlueprintComponent) bool { return c.Kustomize != nil }},
}

func componentKey(c aiplatformv1alpha1.BlueprintComponent) string { return c.ChartName }

// usesNewBlueprintFields reports whether spec sets a field an outdated
// Blueprint CRD would silently prune.
func usesNewBlueprintFields(spec aiplatformv1alpha1.BlueprintSpec) bool {
	return spec.Rollback != "" || anyGuardedFieldSet(spec.Components, guardedComponentFields)
}

// droppedBlueprintFields lists the fields sent set that came back unset.
func droppedBlueprintFields(sent, stored aiplatformv1alpha1.BlueprintSpec) []string {
	var dropped []string
	if sent.Rollback != "" && stored.Rollback == "" {
		dropped = append(dropped, "rollback")
	}
	return append(dropped, droppedListFields("components", sent.Components, stored.Components, componentKey, guardedComponentFields)...)
}

// writeBlueprintWithCRDGuard guards a Blueprint write against an outdated CRD
// pruning the fields above (see writeGuardedByCRD).
func writeBlueprintWithCRDGuard(bp *aiplatformv1alpha1.Blueprint, write func(obj *aiplatformv1alpha1.Blueprint, dryRun bool) error) error {
	sent := bp.Spec
	return writeGuardedByCRD(bp, "Blueprint", "ai-factory.suse.com_blueprints.yaml", usesNewBlueprintFields(sent), func(probe *aiplatformv1alpha1.Blueprint) []string {
		return droppedBlueprintFields(sent, probe.Spec)
	}, write)
}

// BlueprintHandler serves Blueprint CRUD endpoints.
type BlueprintHandler struct {
	client client.Client
}

// NewBlueprintHandler constructs a BlueprintHandler.
func NewBlueprintHandler(c client.Client) *BlueprintHandler {
	return &BlueprintHandler{client: c}
}

// Register wires the handler's routes onto the mux.
func (h *BlueprintHandler) Register(a *Authorizer) {
	a.HandleFunc("GET /api/v1/blueprints", h.listBlueprints)
	a.HandleFunc("POST /api/v1/blueprints", h.createBlueprint)
	a.HandleFunc("GET /api/v1/blueprints/{name}", h.getBlueprint)
	a.HandleFunc("PUT /api/v1/blueprints/{name}", h.updateBlueprint)
	a.HandleFunc("DELETE /api/v1/blueprints/{name}", h.deleteBlueprint)
}

func (h *BlueprintHandler) listBlueprints(w http.ResponseWriter, r *http.Request) {
	var list aiplatformv1alpha1.BlueprintList
	if err := h.client.List(r.Context(), &list); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range list.Items {
		list.Items[i].ManagedFields = nil
	}
	writeJSON(w, http.StatusOK, &list)
}

type blueprintCreateBody struct {
	// BlueprintName is the optional grouping identity (the blueprint-name label)
	// of an existing family. The UI sends it on "save as new version" so the new
	// version stays grouped under the original tile even when that label differs
	// from slugify(displayName) — as it does for several bundled blueprints. When
	// empty (a brand-new blueprint) the family is derived from the display name.
	BlueprintName string                           `json:"blueprintName,omitempty"`
	Spec          aiplatformv1alpha1.BlueprintSpec `json:"spec"`
}

func (h *BlueprintHandler) createBlueprint(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body blueprintCreateBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}

	if body.Spec.DisplayName == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: displayName is required", ErrInvalidInput))
		return
	}
	if body.Spec.Version == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: version is required", ErrInvalidInput))
		return
	}
	if err := validateBlueprintComponents(body.Spec.Components); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	// Determine the grouping family. A brand-new blueprint derives it from the
	// display name; "save as new version" passes the original blueprint-name label
	// explicitly so every version stays grouped under one tile even when that label
	// differs from slugify(displayName) (the case for several bundled blueprints).
	family := slugifyBlueprintName(body.Spec.DisplayName)
	if body.BlueprintName != "" {
		family = slugifyBlueprintName(body.BlueprintName)
		if family == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("%w: blueprintName must contain at least one alphanumeric character", ErrInvalidInput))
			return
		}
	}
	if family == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: displayName must contain at least one alphanumeric character", ErrInvalidInput))
		return
	}
	crName := naming.BlueprintCRName(family, body.Spec.Version)

	bp := &aiplatformv1alpha1.Blueprint{}
	bp.APIVersion = "ai-factory.suse.com/v1alpha1"
	bp.Kind = "Blueprint"
	bp.Name = crName
	bp.Labels = map[string]string{
		aiplatformv1alpha1.BlueprintNameLabel:    family,
		aiplatformv1alpha1.BlueprintVersionLabel: body.Spec.Version,
	}
	bp.Spec = body.Spec

	err := writeBlueprintWithCRDGuard(bp, func(obj *aiplatformv1alpha1.Blueprint, dryRun bool) error {
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
				"%w: blueprint with normalized name %q and version %q already exists — choose a different name or version",
				ErrConflict, family, body.Spec.Version,
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

	bp.ManagedFields = nil
	writeJSON(w, http.StatusCreated, bp)
}

func (h *BlueprintHandler) getBlueprint(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var bp aiplatformv1alpha1.Blueprint
	if err := h.client.Get(r.Context(), client.ObjectKey{Name: name}, &bp); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: blueprint %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	bp.ManagedFields = nil
	writeJSON(w, http.StatusOK, &bp)
}

func (h *BlueprintHandler) deleteBlueprint(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var bp aiplatformv1alpha1.Blueprint
	if err := h.client.Get(r.Context(), client.ObjectKey{Name: name}, &bp); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: blueprint %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if isManagedBlueprint(&bp) {
		writeError(w, http.StatusForbidden, fmt.Errorf(
			"%w: blueprint %q is managed by the product or a catalog and cannot be deleted here; "+
				"remove or edit the catalog in git (or disable defaultBlueprints for bundled ones)", ErrForbidden, name))
		return
	}

	if err := h.client.Delete(r.Context(), &bp); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: blueprint %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// slugifyBlueprintName converts a display name to a DNS-safe slug.
// Must stay in sync with the frontend slugifyBlueprintName in
// ui/pkg/aif-ui/utils/blueprint-api.ts — blueprint tile grouping and CR-name
// derivation depend on both producing identical output.
func slugifyBlueprintName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

type blueprintUpdateBody struct {
	Spec aiplatformv1alpha1.BlueprintSpec `json:"spec"`
}

func (h *BlueprintHandler) updateBlueprint(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, fmt.Errorf("%w: Content-Type must be application/json", ErrInvalidInput))
		return
	}

	var body blueprintUpdateBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%w: %v", ErrInvalidInput, err))
		return
	}
	if err := validateBlueprintComponents(body.Spec.Components); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}

	var bp aiplatformv1alpha1.Blueprint
	if err := h.client.Get(r.Context(), client.ObjectKey{Name: name}, &bp); err != nil {
		if errors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("%w: blueprint %q not found", ErrNotFound, name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	bp.Spec = body.Spec
	err := writeBlueprintWithCRDGuard(&bp, func(obj *aiplatformv1alpha1.Blueprint, dryRun bool) error {
		if dryRun {
			return h.client.Update(r.Context(), obj, client.DryRunAll)
		}
		return h.client.Update(r.Context(), obj)
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

	bp.ManagedFields = nil
	writeJSON(w, http.StatusOK, &bp)
}

// isManagedBlueprint reports whether a blueprint is re-created by Helm or Fleet
// if deleted, making a direct delete a confusing no-op. Bundled blueprints carry
// source=bundled (Helm); catalog blueprints carry Fleet's bundle-name label.
func isManagedBlueprint(bp *aiplatformv1alpha1.Blueprint) bool {
	if bp.Labels[aiplatformv1alpha1.BlueprintSourceLabel] == aiplatformv1alpha1.BlueprintSourceBundled {
		return true
	}
	if _, ok := bp.Labels["fleet.cattle.io/bundle-name"]; ok {
		return true
	}
	return false
}

// Compile-time guard: BlueprintHandler satisfies Handler.
var _ Handler = (*BlueprintHandler)(nil)
