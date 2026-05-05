package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// BlueprintImmutabilityWebhook validates that Blueprint spec fields are immutable.
// It allows CREATE, DELETE operations and UPDATE operations that only change status or metadata.
// Any UPDATE operation that modifies spec fields is denied.
type BlueprintImmutabilityWebhook struct{}

// NewBlueprintImmutability returns a new BlueprintImmutabilityWebhook handler.
// Used by webhook.Validators() in registry.go.
func NewBlueprintImmutability() *BlueprintImmutabilityWebhook {
	return &BlueprintImmutabilityWebhook{}
}

// Handle implements admission.Handler interface.
// It enforces Blueprint spec immutability by comparing old and new spec fields.
func (w *BlueprintImmutabilityWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Only UPDATE operations need validation
	// CREATE has no prior spec to compare against
	// DELETE is unrelated to spec mutation
	if req.Operation != admissionv1.Update {
		return admission.Allowed("non-update operations are not gated by this webhook")
	}

	// Deserialize old and new Blueprint objects
	var oldBp, newBp aifv1.Blueprint
	if err := json.Unmarshal(req.OldObject.Raw, &oldBp); err != nil {
		// Return 400 error for malformed JSON - not a policy denial
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode old object: %w", err))
	}
	if err := json.Unmarshal(req.Object.Raw, &newBp); err != nil {
		// Return 400 error for malformed JSON - not a policy denial
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("decode new object: %w", err))
	}

	// Compare spec fields using semantic deep equality
	// This ignores insignificant differences (field order, empty vs nil slices)
	if !equality.Semantic.DeepEqual(oldBp.Spec, newBp.Spec) {
		return admission.Denied("Blueprint spec is immutable; mint a new version instead")
	}

	// Specs are identical - allow status or metadata changes
	return admission.Allowed("status / metadata change permitted")
}
