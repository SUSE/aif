package controller

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
)

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

	// FleetRepoURL/Branch/GitAuth carry the Fleet GitOps configuration to
	// both Fleet engines (FleetBundleEngine, FleetGitRepoEngine) via the
	// bus's projectFleet projection. Resolved values, not references: the
	// reconciler resolves spec.fleet.credSecretRef to bytes before calling
	// translateSettings, matching the SUSERegistry/AppCollection resolved-creds
	// pattern. Engines never touch the apiserver — required by CLAUDE.md
	// "credentials via UpdateSettings, never direct Secret reads".
	//
	// Auth method is encoded by which FleetGitAuth pointer is non-nil; the
	// CR's spec.fleet.authType string is consumed by the reconciler's switch
	// in translateSettings and not carried onto the snapshot (single source
	// of truth = the tagged-union pointers).
	FleetRepoURL string
	FleetBranch  string
	FleetGitAuth FleetGitAuth
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

// FleetGitAuth mirrors git.GitAuth as a tagged union over the three supported
// auth modes; exactly one pointer is non-nil when an auth method is in use, all
// nil for anonymous. Defined here so the snapshot doesn't import pkg/git — the
// bus translates to git.GitAuth at projection time, same approach as
// ImageRewriteRule above.
type FleetGitAuth struct {
	Token *FleetGitAuthToken
	Basic *FleetGitAuthBasic
	SSH   *FleetGitAuthSSH
}

type FleetGitAuthToken struct {
	Token string
}

type FleetGitAuthBasic struct {
	Username string // currently empty: aifv1.FleetConfig has no Username field; CRD follow-up tracked in PROJECT_PLAN.md
	Password string
}

type FleetGitAuthSSH struct {
	PrivateKeyPEM []byte
	// KnownHostsPEM intentionally omitted: aifv1.FleetConfig has no field for it
	// today, so the engine defaults to ssh.InsecureIgnoreHostKey (documented in
	// pkg/git/types.go::SSHAuth.KnownHostsPEM). CRD extension tracked in
	// PROJECT_PLAN.md follow-up.
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

// translateSettings derefs an aifv1.Settings into a SettingsSnapshot,
// applying §4.5 in-code defaults wherever a CR field is nil/empty. Pure
// function: never reads Secrets (caller resolves first and passes
// Credentials in), never mutates the input CR.
func translateSettings(s *aifv1.Settings, sc, ac, fc Credentials) SettingsSnapshot {
	out := SettingsSnapshot{
		SUSERegistry:          "registry.suse.com",
		AppCollectionRegistry: "dp.apps.rancher.io",
		AppCollectionAPI:      "https://api.apps.rancher.io",
		AppCollectionMode:     "api",
		ImageRewriteEnabled:   false,
		SUSERegistryUser:      sc.User,
		SUSERegistryToken:     sc.Token,
		AppCollectionUser:     ac.User,
		AppCollectionToken:    ac.Token,
	}
	if s == nil {
		return out
	}
	if re := s.Spec.RegistryEndpoints; re != nil {
		if re.SUSERegistry != "" {
			out.SUSERegistry = re.SUSERegistry
		}
		if re.ApplicationCollection != "" {
			out.AppCollectionRegistry = re.ApplicationCollection
		}
		// Symmetric with the other endpoints: empty == unset (Go's zero
		// value for omitempty fields can't distinguish unset from explicit
		// ""), so we keep the default. The explicit "disable HTTP discovery"
		// signal lives on CatalogDiscovery.ApplicationCollectionMode="disabled"
		// (handled below). Translation stops here; the bus's projection
		// stage is the right place for any mode→URL policy.
		if re.ApplicationCollectionAPI != "" {
			out.AppCollectionAPI = re.ApplicationCollectionAPI
		}
	}
	if ir := s.Spec.ImageRewrite; ir != nil {
		out.ImageRewriteEnabled = ir.Enabled
		for _, r := range ir.Rules {
			out.ImageRewriteRules = append(out.ImageRewriteRules, ImageRewriteRule{
				Match: r.Match, Replace: r.Replace,
			})
		}
	}
	if cd := s.Spec.CatalogDiscovery; cd != nil && cd.ApplicationCollectionMode != "" {
		out.AppCollectionMode = cd.ApplicationCollectionMode
	}
	if bc := s.Spec.BlueprintClassification; bc != nil {
		for _, c := range bc.ForceReferenceBlueprint {
			out.BlueprintForceReference = append(out.BlueprintForceReference, ChartRef{
				Repo: c.Repo, Chart: c.Chart,
			})
		}
		for _, c := range bc.ForceBuildingBlock {
			out.BlueprintForceBuildingBlock = append(out.BlueprintForceBuildingBlock, ChartRef{
				Repo: c.Repo, Chart: c.Chart,
			})
		}
	}
	if s.Spec.Fleet != nil {
		out.FleetRepoURL = s.Spec.Fleet.RepoURL
		out.FleetBranch = s.Spec.Fleet.Branch
		// Resolved credential bytes only when CredSecretRef is non-nil. Both
		// guards required: spec.Fleet AND spec.Fleet.CredSecretRef are pointers.
		// fc.Token carries the single resolved value (ssh PEM bytes, token, or
		// basic password) — the SecretKeySelector schema is one key per ref.
		if s.Spec.Fleet.CredSecretRef != nil {
			switch s.Spec.Fleet.AuthType {
			case aifv1.FleetAuthTypeToken:
				out.FleetGitAuth = FleetGitAuth{Token: &FleetGitAuthToken{Token: fc.Token}}
			case aifv1.FleetAuthTypeSSH:
				out.FleetGitAuth = FleetGitAuth{SSH: &FleetGitAuthSSH{PrivateKeyPEM: []byte(fc.Token)}}
			case aifv1.FleetAuthTypeBasic:
				// Username gap: aifv1.FleetConfig has no Username field; ship
				// empty for now and rely on the engine to surface the failure
				// as ErrAuth at Push time. Tracked in PROJECT_PLAN.md.
				out.FleetGitAuth = FleetGitAuth{Basic: &FleetGitAuthBasic{Password: fc.Token}}
			}
			// authType="" with credSecretRef set silently falls through to
			// anonymous — no cross-field admission validates this pair today.
			// PROJECT_PLAN P5-4b follow-up #3 tracks the validating webhook.
		}
	}
	return out
}
