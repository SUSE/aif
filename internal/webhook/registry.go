package webhook

import "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

// Validator pairs a URL path with an admission Handler. Adding a new webhook
// to the operator means appending an entry to the slice returned by
// Validators(), not editing internal/manager/setup.go.
//
// Path MUST match clientConfig.service.path in
// charts/aif-operator/templates/webhook.yaml for the corresponding webhook.
type Validator struct {
	Path    string
	Handler admission.Handler
}

// Validators returns every admission webhook this operator serves. The slice
// is the authoritative registry; SetupWebhooks iterates it.
func Validators() []Validator {
	return []Validator{
		{
			Path:    "/validate-ai-suse-com-v1alpha1-blueprint",
			Handler: NewBlueprintImmutability(),
		},
	}
}
