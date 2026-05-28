package conditions

// Action constants for Kubernetes event recording.
// These describe what action the controller took when the event occurred.
// Per Kubernetes conventions, actions are UpperCamelCase and describe
// the controller's activity at the time of the event.
const (
	ActionValidating  = "Validating"
	ActionReconciling = "Reconciling"
	ActionDeleting    = "Deleting"
	ActionInstalling  = "Installing"
	ActionChecking    = "Checking"
	ActionWaiting     = "Waiting"
	ActionApplying    = "Applying"
	ActionResolving   = "Resolving"
	ActionCreating    = "Creating"
	ActionVerifying   = "Verifying"
)
