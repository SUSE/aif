package manager

import (
	"github.com/SUSE/aif/internal/webhook"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhooks registers all admission webhooks with the manager.
func SetupWebhooks(mgr ctrl.Manager) error {
	// Register Blueprint immutability webhook
	mgr.GetWebhookServer().Register(
		"/validate-ai-suse-com-v1alpha1-blueprint",
		&admission.Webhook{
			Handler: &webhook.BlueprintImmutabilityWebhook{},
		},
	)
	return nil
}
