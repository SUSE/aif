package controller

import "context"

// SettingsSnapshot is the deref'd, defaults-applied view of an aifv1.Settings
// CR that the reconciler hands to the SettingsApplier bus.
//
// Each field is a value type (no pointers, no aifv1 imports). Defaults are
// applied at translation time so downstream engines see the in-code default
// when a CR field is nil/empty (per ARCHITECTURE.md §4.5 final paragraph
// "Defaults handled in code, not schema").
type SettingsSnapshot struct {
	SUSERegistry          string // §4.5 default "registry.suse.com"
	SUSERegistryUser      string // resolved from spec.suseRegistry.userSecretRef
	SUSERegistryToken     string // resolved from spec.suseRegistry.tokenSecretRef
	AppCollectionRegistry string // §4.5 default "dp.apps.rancher.io"
	AppCollectionAPI      string // §4.5 default "https://api.apps.rancher.io"
	AppCollectionUser     string
	AppCollectionToken    string
	AppCollectionMode     string // "api" (default) | "registry-fallback" | "disabled"

	ImageRewriteEnabled bool
	ImageRewriteRules   []ImageRewriteRule

	// BlueprintClassification stored for future engine consumption (P2-7
	// wrapper). Not pushed by the bus today; see follow-up note 4.
	BlueprintForceReference     []ChartRef
	BlueprintForceBuildingBlock []ChartRef
}

// ImageRewriteRule mirrors aifv1.ImageRewriteRule and helm.ImageRewriteRule.
// Defined here so the snapshot doesn't import aifv1 OR pkg/helm — the bus
// translates to the engine's own type at projection time.
type ImageRewriteRule struct {
	Match   string
	Replace string
}

// ChartRef mirrors aifv1.ChartRef. Defined here for snapshot independence.
type ChartRef struct {
	Repo  string
	Chart string
}

// Credentials is the resolved (user, token) pair the reconciler assembles
// from Secret resolution before calling translateSettings. Keeps the
// translation function pure (no Secret reads).
type Credentials struct {
	User  string
	Token string
}

// SettingsApplier is the bus port the reconciler calls on every reconcile.
// Production implementation projects SettingsSnapshot into per-engine
// EngineSettings types and pushes via each engine's UpdateSettings.
//
// Returns error even though no engine fails today: forward-looking. If any
// engine grows fallibility (validation rejecting a snapshot, persistence
// errors, etc.), the port already has the return shape and the reconciler
// can wrap into Ready=False.
type SettingsApplier interface {
	Apply(ctx context.Context, s SettingsSnapshot) error
}
