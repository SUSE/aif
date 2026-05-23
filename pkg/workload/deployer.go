package workload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/fleet"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"

	"sigs.k8s.io/yaml"
)

// deployer is the production Deployer. Pure orchestrator: holds
// constant refs to its dependency ports; no mutable state.
type deployer struct {
	log         *slog.Logger
	render      helm.ValueRenderer
	fleetBundle fleet.FleetBundleEngine
	bpRepo      blueprint.Repository
	bundleRepo  bundle.Repository
	nvDisc      nvidia.Discovery
	nvDepl      nvidia.Deployer
}

// NewDeployer constructs the production Deployer.
//
// Dependencies are pushed via constructor (not via UpdateSettings) because
// the deployer doesn't carry settings of its own — image-rewrite and
// pull-secret policy live inside helm.Engine via P5-7's bus, NIM sizing
// formulas live inside nvidia.Deployer.
func NewDeployer(
	log *slog.Logger,
	render helm.ValueRenderer,
	fleetBundle fleet.FleetBundleEngine,
	bpRepo blueprint.Repository,
	bundleRepo bundle.Repository,
	nvDisc nvidia.Discovery,
	nvDepl nvidia.Deployer,
) Deployer {
	return &deployer{
		log:         log,
		render:      render,
		fleetBundle: fleetBundle,
		bpRepo:      bpRepo,
		bundleRepo:  bundleRepo,
		nvDisc:      nvDisc,
		nvDepl:      nvDepl,
	}
}

// Deploy resolves the workload's source into components, renders values for
// each via helm.ValueRenderer, assembles a single Fleet Bundle, and dispatches
// via FleetBundleEngine.Apply. Returns a typed result the reconciler can apply
// to status. Idempotent: re-invocation with the same DeployRequest converges
// to the same cluster state.
func (d *deployer) Deploy(ctx context.Context, req DeployRequest) (DeployResult, error) {
	desired, observedGen, err := d.resolveSource(ctx, req)
	if err != nil {
		return DeployResult{ObservedBundleGeneration: observedGen}, err
	}

	componentsForFleet := make([]fleet.ComponentBundle, 0, len(desired))
	releaseRecords := make([]ComponentRelease, 0, len(desired))

	for _, c := range desired {
		ov := helm.Overrides{
			Blueprint:    parseOverride(c.blueprintOverride),
			Workload:     parseOverride(req.Overrides[c.name]),
			NIMGenerated: d.nimGenerated(ctx, req, c),
		}
		values, err := d.render.Render(ctx, c.repo, c.chart, c.version, ov)
		if err != nil {
			return DeployResult{}, fmt.Errorf("render %q: %w", c.name, err)
		}
		chartRef := composeChartRef(c.repo, c.chart, c.version)
		componentsForFleet = append(componentsForFleet, fleet.ComponentBundle{
			Name:     c.name,
			ChartRef: chartRef,
			Values:   values,
		})
		releaseRecords = append(releaseRecords, ComponentRelease{
			Name:        c.name,
			ReleaseName: ComposeReleaseName(req.ID, c.name),
			ChartRef:    chartRef,
			Status:      "fleet-managed",
		})
	}

	spec := fleet.BundleDeploymentSpec{
		WorkloadID:     req.ID,
		WorkloadNS:     req.Namespace,
		TargetClusters: req.TargetClusters,
		Components:     componentsForFleet,
		PullSecretData: req.PullSecretData,
		Owner:          req.Owner,
	}
	obs, err := d.fleetBundle.Apply(ctx, spec)
	if err != nil {
		return DeployResult{
			Components:               releaseRecords,
			ObservedBundleGeneration: observedGen,
		}, err
	}

	// Phase is NOT set on DeployResult: post-P5-1 the controller owns
	// phase via RecomputePhase(PhaseInputFromCR(w)). The per-cluster
	// projection below feeds PhaseInput.PerClusterPhases so Rule 0 of
	// RecomputePhase aggregates per-cluster phases for the Fleet path.
	return DeployResult{
		Components:               releaseRecords,
		PerCluster:               translateObserved(obs),
		ObservedBundleGeneration: observedGen,
	}, nil
}

// translateObserved maps fleet.BundleObservedStatus → workload-domain
// per-cluster status. Aggregate Phase is computed downstream by
// RecomputePhase (which calls AggregateClusterPhases on
// PhaseInput.PerClusterPhases — Rule 0 in phase.go).
func translateObserved(obs fleet.BundleObservedStatus) []ClusterDeploymentStatusDomain {
	if len(obs.PerCluster) == 0 {
		return nil
	}
	out := make([]ClusterDeploymentStatusDomain, 0, len(obs.PerCluster))
	for _, e := range obs.PerCluster {
		var p ClusterPhase
		if e.ConnectionError {
			p = ClusterFailed
		} else {
			p = MapFleetStateToPhase(e.FleetState)
		}
		out = append(out, ClusterDeploymentStatusDomain{
			ClusterName: e.ClusterName,
			Phase:       p,
			FleetState:  e.FleetState,
		})
	}
	return out
}

// nimGenerated performs NIM detection for a single component and returns
// the generated values layer if NIM, or nil if non-NIM or error.
// Carves out the discovery+deployer calls from the old installComponent.
func (d *deployer) nimGenerated(ctx context.Context, req DeployRequest, c desiredComponent) map[string]any {
	entry, derr := d.nvDisc.Get(ctx, fmt.Sprintf("%s:%s", c.chart, c.version))
	if derr != nil {
		if !errors.Is(derr, nvidia.ErrNIMNotFound) {
			d.log.Warn("nvidia.Discovery.Get returned non-NotFound error; treating component as non-NIM",
				slog.String("component", c.name),
				slog.String("chart", c.chart),
				slog.String("version", c.version),
				slog.String("err", derr.Error()))
		}
		return nil
	}

	// gpuCount is a deployer-protocol field, read ONLY from workloadOverrides
	// per P4-4 follow-up note 2. Blueprint overrides cannot influence NIM
	// sizing (their job is Helm-native chart values).
	wlOverrides := parseOverride(req.Overrides[c.name])
	gpuCount := extractGPUCount(wlOverrides)
	generated, gerr := d.nvDepl.GenerateValues(ctx, nvidia.GenerateRequest{
		Entry:    entry,
		Replicas: req.Replicas,
		GPUs:     gpuCount,
	})
	if gerr != nil {
		d.log.Warn("nvidia.Deployer.GenerateValues failed; skipping NIM layer",
			slog.String("component", c.name),
			slog.String("err", gerr.Error()))
		return nil
	}
	return generated
}

// parseOverride parses a YAML string from the user CR's
// valueOverrides map into a Go map. Empty/whitespace input → nil map.
// Invalid YAML → nil (silently dropped — Fleet surfaces the resulting
// chart-render failure if the override was load-bearing).
func parseOverride(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]any
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// composeChartRef constructs an OCI reference from the App-ref shape.
// Replaces what InstallChartFromRepo used to build internally.
func composeChartRef(repo, chart, version string) string {
	// Normalize: repo may or may not carry the oci:// prefix.
	r := repo
	if !strings.HasPrefix(r, "oci://") {
		r = "oci://" + strings.TrimPrefix(r, "https://")
	}
	r = strings.TrimSuffix(r, "/")
	return r + "/" + chart + ":" + version
}

// Teardown deletes the Fleet Bundle for the workload. Called by
// WorkloadReconciler's finalizer block on Workload deletion.
// Fleet handles per-cluster uninstall and orphan cleanup declaratively.
func (d *deployer) Teardown(ctx context.Context, namespace, workloadID string, _ []ComponentRelease) error {
	return d.fleetBundle.Teardown(ctx, namespace, workloadID)
}

// extractGPUCount looks for an int-typed "gpuCount" key in the parsed
// Workload override map. Returns nil if absent or not numeric.
//
// Per P4-4 follow-up note 2, gpuCount is read ONLY from workloadOverrides
// (NOT blueprintOverrides) — it's a deployer-protocol field, not a Helm
// chart value. Blueprint authors who want to influence NIM sizing do so
// via Helm-native fields (resources.limits.nvidia.com/gpu etc).
//
// YAML decoding via sigs.k8s.io/yaml routes through JSON, so integer
// literals decode as float64; handle that case too.
func extractGPUCount(wlOverrides map[string]any) *int32 {
	v, ok := wlOverrides["gpuCount"]
	if !ok {
		return nil
	}
	// In practice only the float64 arm fires (sigs.k8s.io/yaml decodes numeric
	// literals via JSON). The int/int32/int64 arms are forward-compat for
	// callers that pre-parse YAML using a different library.
	switch n := v.(type) {
	case int:
		out := int32(n)
		return &out
	case int32:
		return &n
	case int64:
		out := int32(n)
		return &out
	case float64:
		out := int32(n)
		return &out
	}
	return nil
}

// desiredComponent is the deployer-internal projection of a component
// to install. Carries everything needed to assemble an InstallRequest.
type desiredComponent struct {
	name              string // componentName (release-name suffix; valueOverrides key)
	repo              string // OCI host + path (e.g. "oci://registry.suse.com/ai/charts")
	chart             string // chart name (e.g. "nim-llm")
	version           string // chart version
	blueprintOverride string // YAML string from Blueprint.spec.valueOverrides[name]; "" for App/BundleTest
}

// resolveSource translates req.Source into the ordered list of components
// to install plus the observed bundle generation (BundleTest only).
//
// Returns ErrSourceNotResolved if the source CR is not found.
// Returns ErrNestedBlueprintNotSupported if any child component has Kind=Blueprint.
func (d *deployer) resolveSource(ctx context.Context, req DeployRequest) ([]desiredComponent, int64, error) {
	switch req.Source.Kind {
	case SourceKindApp:
		if req.Source.App == nil {
			return nil, 0, ErrSourceNotResolved
		}
		return []desiredComponent{{
			name:    req.SpecName,
			repo:    req.Source.App.Repo,
			chart:   req.Source.App.Chart,
			version: req.Source.App.Version,
		}}, 0, nil

	case SourceKindBlueprint:
		if req.Source.Blueprint == nil {
			return nil, 0, ErrSourceNotResolved
		}
		bp, err := d.bpRepo.Get(ctx, blueprintCRName(req.Source.Blueprint.Name, req.Source.Blueprint.Version))
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %v", ErrSourceNotResolved, err)
		}
		return ComponentsFromBlueprintCR(bp)

	case SourceKindBundleTest:
		if req.Source.BundleTest == nil {
			return nil, 0, ErrSourceNotResolved
		}
		b, err := d.bundleRepo.Get(ctx, req.Source.BundleTest.Namespace, req.Source.BundleTest.Name)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %v", ErrSourceNotResolved, err)
		}
		return ComponentsFromBundleCR(b)
	}
	return nil, 0, ErrSourceNotResolved
}
