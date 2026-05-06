package manager

import (
	aifwh "github.com/SUSE/aif/internal/webhook"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhooks registers every admission webhook listed in
// internal/webhook.Validators() with the manager's webhook server.
//
// Adding a new webhook is a one-line edit to webhook.Validators() — this
// function does not change.
//
// Certificate reload behaviour:
// The webhook server (controller-runtime) watches CertDir for file modifications
// and hot-reloads cert+key without pod restart. This handles cert-manager rotation
// (default 30d duration, renewBefore 15d) transparently. For helm-hook mode, cert
// is generated once per helm install/upgrade. For manual mode, customer updates
// the Secret and reload happens automatically. See ARCHITECTURE.md §8.3.
func SetupWebhooks(mgr ctrl.Manager) error {
	server := mgr.GetWebhookServer()
	for _, v := range aifwh.Validators() {
		server.Register(v.Path, &admission.Webhook{Handler: v.Handler})
	}
	return nil
}
