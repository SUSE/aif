package webhook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func makeAdmissionRequest(t *testing.T, operation admissionv1.Operation, oldBp, newBp *aifv1.Blueprint) admission.Request {
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: operation,
		},
	}

	if oldBp != nil {
		oldBytes, err := json.Marshal(oldBp)
		if err != nil {
			t.Fatalf("failed to marshal old blueprint: %v", err)
		}
		req.OldObject = runtime.RawExtension{Raw: oldBytes}
	}

	if newBp != nil {
		newBytes, err := json.Marshal(newBp)
		if err != nil {
			t.Fatalf("failed to marshal new blueprint: %v", err)
		}
		req.Object = runtime.RawExtension{Raw: newBytes}
	}

	return req
}

func TestBlueprintImmutability_SpecMutationDenied(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	oldBp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nvidia-nim-llm.1.0.0",
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "nvidia-nim-llm",
			Version:       "1.0.0",
			UseCase:       "inference",
			Description:   "Original description",
			PublishedBy:   "admin",
			PublishedAt:   metav1.Now(),
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourceWrapsVendorChart,
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "nim",
					Kind: aifv1.ComponentKindApp,
				},
			},
		},
	}

	newBp := oldBp.DeepCopy()
	newBp.Spec.Description = "Changed description"

	req := makeAdmissionRequest(t, admissionv1.Update, oldBp, newBp)
	resp := webhook.Handle(context.Background(), req)

	if resp.Allowed {
		t.Errorf("expected spec mutation to be denied, but was allowed")
	}

	expectedMessage := "Blueprint spec is immutable; mint a new version instead"
	if resp.Result.Message != expectedMessage {
		t.Errorf("expected message %q, got %q", expectedMessage, resp.Result.Message)
	}
}

func TestBlueprintImmutability_StatusMutationAllowed(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	oldBp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nvidia-nim-llm.1.0.0",
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "nvidia-nim-llm",
			Version:       "1.0.0",
			UseCase:       "inference",
			PublishedBy:   "admin",
			PublishedAt:   metav1.Now(),
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourceWrapsVendorChart,
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "nim",
					Kind: aifv1.ComponentKindApp,
				},
			},
		},
		Status: aifv1.BlueprintStatus{
			Phase: aifv1.BlueprintPhaseActive,
		},
	}

	newBp := oldBp.DeepCopy()
	newBp.Status.Phase = aifv1.BlueprintPhaseDeprecated

	req := makeAdmissionRequest(t, admissionv1.Update, oldBp, newBp)
	resp := webhook.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Errorf("expected status mutation to be allowed, but was denied: %s", resp.Result.Message)
	}

	expectedMessage := "status / metadata change permitted"
	if resp.Result.Message != expectedMessage {
		t.Errorf("expected message %q, got %q", expectedMessage, resp.Result.Message)
	}
}

func TestBlueprintImmutability_MetadataMutationAllowed(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	oldBp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nvidia-nim-llm.1.0.0",
			Labels: map[string]string{
				"env": "prod",
			},
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "nvidia-nim-llm",
			Version:       "1.0.0",
			UseCase:       "inference",
			PublishedBy:   "admin",
			PublishedAt:   metav1.Now(),
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourceWrapsVendorChart,
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "nim",
					Kind: aifv1.ComponentKindApp,
				},
			},
		},
	}

	newBp := oldBp.DeepCopy()
	newBp.Labels["new-label"] = "value"

	req := makeAdmissionRequest(t, admissionv1.Update, oldBp, newBp)
	resp := webhook.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Errorf("expected metadata mutation to be allowed, but was denied: %s", resp.Result.Message)
	}
}

func TestBlueprintImmutability_StatusAndMetadataMutationAllowed(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	oldBp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nvidia-nim-llm.1.0.0",
			Labels: map[string]string{
				"env": "prod",
			},
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "nvidia-nim-llm",
			Version:       "1.0.0",
			UseCase:       "inference",
			PublishedBy:   "admin",
			PublishedAt:   metav1.Now(),
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourceWrapsVendorChart,
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "nim",
					Kind: aifv1.ComponentKindApp,
				},
			},
		},
		Status: aifv1.BlueprintStatus{
			Phase: aifv1.BlueprintPhaseActive,
		},
	}

	newBp := oldBp.DeepCopy()
	newBp.Status.Phase = aifv1.BlueprintPhaseDeprecated
	newBp.Labels["updated"] = "true"

	req := makeAdmissionRequest(t, admissionv1.Update, oldBp, newBp)
	resp := webhook.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Errorf("expected status+metadata mutation to be allowed, but was denied: %s", resp.Result.Message)
	}

	expectedMessage := "status / metadata change permitted"
	if resp.Result.Message != expectedMessage {
		t.Errorf("expected message %q, got %q", expectedMessage, resp.Result.Message)
	}
}

func TestBlueprintImmutability_CreateAllowed(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	newBp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nvidia-nim-llm.1.0.0",
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "nvidia-nim-llm",
			Version:       "1.0.0",
			UseCase:       "inference",
			PublishedBy:   "admin",
			PublishedAt:   metav1.Now(),
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourceWrapsVendorChart,
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "nim",
					Kind: aifv1.ComponentKindApp,
				},
			},
		},
	}

	req := makeAdmissionRequest(t, admissionv1.Create, nil, newBp)
	resp := webhook.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Errorf("expected CREATE to be allowed, but was denied: %s", resp.Result.Message)
	}

	expectedMessage := "non-update operations are not gated by this webhook"
	if resp.Result.Message != expectedMessage {
		t.Errorf("expected message %q, got %q", expectedMessage, resp.Result.Message)
	}
}

func TestBlueprintImmutability_DeleteAllowed(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	oldBp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nvidia-nim-llm.1.0.0",
		},
		Spec: aifv1.BlueprintSpec{
			BlueprintName: "nvidia-nim-llm",
			Version:       "1.0.0",
			UseCase:       "inference",
			PublishedBy:   "admin",
			PublishedAt:   metav1.Now(),
			Source: aifv1.BlueprintSource{
				Type: aifv1.BlueprintSourceWrapsVendorChart,
			},
			Components: []aifv1.ComponentRef{
				{
					Name: "nim",
					Kind: aifv1.ComponentKindApp,
				},
			},
		},
	}

	req := makeAdmissionRequest(t, admissionv1.Delete, oldBp, nil)
	resp := webhook.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Errorf("expected DELETE to be allowed, but was denied: %s", resp.Result.Message)
	}

	expectedMessage := "non-update operations are not gated by this webhook"
	if resp.Result.Message != expectedMessage {
		t.Errorf("expected message %q, got %q", expectedMessage, resp.Result.Message)
	}
}

func TestBlueprintImmutability_MalformedOldObject(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			OldObject: runtime.RawExtension{Raw: []byte("invalid json")},
			Object:    runtime.RawExtension{Raw: []byte(`{"metadata":{"name":"test"}}`)},
		},
	}

	resp := webhook.Handle(context.Background(), req)

	if resp.Allowed {
		t.Errorf("expected malformed old object to be errored, but was allowed")
	}

	// Response should be an error response, not a denial
	if resp.Result == nil || resp.Result.Code != 400 {
		t.Errorf("expected 400 error code, got %v", resp.Result)
	}

	if resp.Result.Message == "" || !strings.Contains(resp.Result.Message, "decode old object") {
		t.Errorf("expected decode error message, got %q", resp.Result.Message)
	}
}

func TestBlueprintImmutability_MalformedNewObject(t *testing.T) {
	webhook := &BlueprintImmutabilityWebhook{}

	oldBp := &aifv1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	oldBytes, _ := json.Marshal(oldBp)

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			OldObject: runtime.RawExtension{Raw: oldBytes},
			Object:    runtime.RawExtension{Raw: []byte("invalid json")},
		},
	}

	resp := webhook.Handle(context.Background(), req)

	if resp.Allowed {
		t.Errorf("expected malformed new object to be errored, but was allowed")
	}

	if resp.Result == nil || resp.Result.Code != 400 {
		t.Errorf("expected 400 error code, got %v", resp.Result)
	}

	if resp.Result.Message == "" || !strings.Contains(resp.Result.Message, "decode new object") {
		t.Errorf("expected decode error message, got %q", resp.Result.Message)
	}
}
