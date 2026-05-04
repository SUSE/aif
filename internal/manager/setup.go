package manager

import (
	blueprintwh "github.com/SUSE/aif/internal/webhook"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// SetupWebhooks registers all admission webhooks with the manager's webhook server.
//
// Certificate reload behaviour:
// The webhook server (controller-runtime) watches CertDir for file modifications
// and hot-reloads cert+key without pod restart. This handles cert-manager rotation
// (default 30d duration, renewBefore 15d) transparently. For helm-hook mode, cert
// is generated once per helm install/upgrade. For manual mode, customer updates
// the Secret and reload happens automatically. See ARCHITECTURE.md §8.3.
func SetupWebhooks(mgr manager.Manager) error {
	// Register Blueprint immutability webhook
	// Path MUST match clientConfig.service.path in charts/aif-operator/templates/webhook.yaml
	mgr.GetWebhookServer().Register(
		"/validate-ai-suse-com-v1alpha1-blueprint",
		&webhook.Admission{Handler: &blueprintwh.BlueprintImmutabilityWebhook{}},
	)
	return nil
}
