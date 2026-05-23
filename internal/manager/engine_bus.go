package manager

import (
	"context"
	"log/slog"

	"github.com/SUSE/aif/internal/controller"
	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
	"github.com/SUSE/aif/pkg/source_collection"
)

// engineBus is the production SettingsApplier. Holds direct refs to all
// settings-aware engines and projects controller.SettingsSnapshot into each
// engine's own EngineSettings type. Bootstrapped in cmd/operator/main.go.
//
// The bus has no shared mutable state of its own (engine refs are constants
// after construction); each engine's UpdateSettings is thread-safe per its
// own pattern (sync.RWMutex sole-writer, mirroring helm.engine).
type engineBus struct {
	helm helm.Engine
	// fleetBundle is the FleetBundleEngine; receives FleetSettings via
	// projectFleet. Field is empty-settings today but wired through the
	// bus so P5-7 (downstream-cluster auth) can extend without touching
	// the bus contract.
	fleetBundle fleet.FleetBundleEngine
	nvidiaDisc  nvidia.Discovery
	nvidiaDepl  nvidia.Deployer
	appCollect  source_collection.Client
	logger      *slog.Logger
}

// NewEngineBus constructs the production SettingsApplier with refs to all
// settings-aware engines. Intended for cmd/operator/main.go after the
// engines themselves are constructed.
func NewEngineBus(
	h helm.Engine,
	fb fleet.FleetBundleEngine,
	nd nvidia.Discovery,
	nde nvidia.Deployer,
	ac source_collection.Client,
	logger *slog.Logger,
) controller.SettingsApplier {
	return &engineBus{
		helm:        h,
		fleetBundle: fb,
		nvidiaDisc:  nd,
		nvidiaDepl:  nde,
		appCollect:  ac,
		logger:      logger,
	}
}

// Apply projects the snapshot into per-engine EngineSettings and pushes via
// each engine's UpdateSettings. Returns nil today (all engines non-fallible).
//
// ctx is currently unused: each engine's UpdateSettings is a synchronous
// O(1) mutex-guarded swap with no IO and no cancellation point. The
// parameter is kept on the port signature so future cancellable engine
// pushes (and per-engine deadline propagation) plug in without a
// breaking-change. If any engine grows fallibility, aggregate via
// errors.Join here.
func (b *engineBus) Apply(_ context.Context, s controller.SettingsSnapshot) error {
	b.helm.UpdateSettings(b.projectHelm(s))
	b.fleetBundle.UpdateSettings(b.projectFleet(s))
	b.nvidiaDisc.UpdateSettings(b.projectNvidiaDiscovery(s))
	b.nvidiaDepl.UpdateSettings(b.projectNvidiaDeployer(s))
	b.appCollect.UpdateSettings(b.projectAppCo(s))
	b.logger.Info("settings applied to engines",
		slog.String("component", "manager.engine_bus"),
		slog.String("registry_endpoint", s.SUSERegistry),
		slog.String("app_collection_mode", s.AppCollectionMode),
		slog.Bool("image_rewrite_enabled", s.ImageRewriteEnabled))
	return nil
}

func (b *engineBus) projectHelm(s controller.SettingsSnapshot) helm.EngineSettings {
	rules := make([]helm.ImageRewriteRule, 0, len(s.ImageRewriteRules))
	for _, r := range s.ImageRewriteRules {
		rules = append(rules, helm.ImageRewriteRule{Match: r.Match, Replace: r.Replace})
	}
	return helm.EngineSettings{
		RegistryEndpoints: helm.RegistryEndpoints{
			SUSERegistry:             s.SUSERegistry,
			ApplicationCollection:    s.AppCollectionRegistry,
			ApplicationCollectionAPI: s.AppCollectionAPI,
		},
		ImageRewrite: helm.ImageRewriteConfig{
			Enabled: s.ImageRewriteEnabled,
			Rules:   rules,
		},
	}
}

func (b *engineBus) projectNvidiaDiscovery(s controller.SettingsSnapshot) nvidia.EngineSettings {
	return nvidia.EngineSettings{
		RegistryEndpoint: s.SUSERegistry,
		Username:         s.SUSERegistryUser,
		Token:            s.SUSERegistryToken,
	}
}

func (b *engineBus) projectNvidiaDeployer(s controller.SettingsSnapshot) nvidia.EngineSettings {
	// Deployer only needs the hostname for image.repository templating;
	// credentials and refresh interval are not used by Deployer.
	return nvidia.EngineSettings{RegistryEndpoint: s.SUSERegistry}
}

func (b *engineBus) projectAppCo(s controller.SettingsSnapshot) source_collection.EngineSettings {
	apiURL := s.AppCollectionAPI
	// §4.5: applicationCollectionMode=disabled means "skip API entirely."
	// Mechanical realization: pass APIURL=""; the AppCo client's
	// effectiveSettings() already treats empty APIURL as "not configured"
	// and returns ErrNotConfigured. Zero source_collection changes needed.
	//
	// mode=registry-fallback: pass URL through; AppCo client uses HTTP
	// normally. The "fall back to OCI on connection error" half is
	// unimplemented (no OCI walker exists); future story (P5-7 follow-up note 1).
	if s.AppCollectionMode == "disabled" {
		apiURL = ""
	}
	// OCIHost is consumed by source_collection.AnnotationReader (the
	// chart-annotation OCI walker that backs ReferenceBlueprint detection).
	// Without it, AnnotationReader.effectiveAnnotationSettings returns
	// ErrNotConfigured and pkg/apps.AppCoSource.enrichWithAnnotations
	// silently degrades on every Refresh.
	return source_collection.EngineSettings{
		APIURL:   apiURL,
		OCIHost:  s.AppCollectionRegistry,
		Username: s.AppCollectionUser,
		Token:    s.AppCollectionToken,
	}
}

// projectFleet is the FleetBundleEngine settings projector. FleetSettings
// is empty today — the engine talks to the local Rancher apiserver via the
// injected client.Client, so no per-cluster auth is needed yet. The
// projector exists so P5-7 (downstream-cluster auth, e.g. kubeconfig
// snippets per Cluster) can extend FleetSettings without changing the
// bus's contract with the engine.
func (b *engineBus) projectFleet(_ controller.SettingsSnapshot) fleet.FleetSettings {
	return fleet.FleetSettings{}
}
