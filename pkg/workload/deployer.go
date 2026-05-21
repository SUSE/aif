package workload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"

	"sigs.k8s.io/yaml"
)

// componentInstallTimeout matches helm.defaultInstallTimeout (5min). Kept
// here so the deployer doesn't import an unexported helm constant; if the
// helm engine ever exposes a public DefaultInstallTimeout, prefer that.
const componentInstallTimeout = 5 * time.Minute

// deployer is the production Deployer. Pure orchestrator: holds
// constant refs to its dependency ports; no mutable state.
type deployer struct {
	helm           helm.Engine
	blueprintRepo  blueprint.Repository
	bundleRepo     bundle.Repository
	nvidiaDisc     nvidia.Discovery
	nvidiaDeployer nvidia.Deployer
	logger         *slog.Logger
}

// NewDeployer constructs the production Deployer.
//
// Dependencies are pushed via constructor (not via UpdateSettings) because
// the deployer doesn't carry settings of its own — image-rewrite and
// pull-secret policy live inside helm.Engine via P5-7's bus, NIM sizing
// formulas live inside nvidia.Deployer.
//
// req.Overrides is read-only — the implementation MUST NOT mutate the
// map or its string values (it's shared with the caller's Workload CR
// per pkg/workload/conversions.go.WorkloadToDeployRequest).
func NewDeployer(
	h helm.Engine,
	br blueprint.Repository,
	bnr bundle.Repository,
	nd nvidia.Discovery,
	nde nvidia.Deployer,
	logger *slog.Logger,
) Deployer {
	return &deployer{
		helm:           h,
		blueprintRepo:  br,
		bundleRepo:     bnr,
		nvidiaDisc:     nd,
		nvidiaDeployer: nde,
		logger:         logger,
	}
}

// Deploy resolves the workload's source into components, helm-installs each
// with layered overrides, cleans up orphans from drift, and returns a typed
// result the reconciler can apply to status. Idempotent: re-invocation with
// the same DeployRequest converges to the same cluster state.
func (d *deployer) Deploy(ctx context.Context, req DeployRequest) (DeployResult, error) {
	desired, observedGen, err := d.resolveSource(ctx, req)
	if err != nil {
		return DeployResult{ObservedBundleGeneration: observedGen}, err
	}

	var (
		components []ComponentRelease
		errs       []error
	)
	for _, dc := range desired {
		release, derr := d.installComponent(ctx, req, dc)
		components = append(components, release)
		if derr != nil {
			errs = append(errs, derr)
		}
	}

	// Orphan cleanup: uninstall releases that were previously installed but
	// are no longer in the desired set. Failures keep the orphan visible in
	// status with marker status; phase aggregation (Task 22) will see
	// ComponentStatusOrphanUninstallFailed and surface phase=Deploying until clean.
	orphans := detectOrphans(req.Previous, desired)
	for _, orphan := range orphans {
		if uerr := d.helm.Uninstall(ctx, req.Namespace, orphan.ReleaseName); uerr != nil {
			orphan.Status = ComponentStatusOrphanUninstallFailed
			components = append(components, orphan)
			errs = append(errs, errors.Join(ErrComponentUninstallFailed,
				fmt.Errorf("orphan %q: %w", orphan.Name, uerr)))
		}
		// Successful uninstall: orphan is implicitly dropped (not appended to components).
	}

	return DeployResult{
		Components:               components,
		ObservedBundleGeneration: observedGen,
	}, errors.Join(errs...)
}

// installComponent runs a single component install: parses overrides,
// composes release name, builds the helm.InstallRequest, calls the engine,
// and translates the result into a ComponentRelease.
//
// NIM detection (layer 4) is added in Task 20.
func (d *deployer) installComponent(ctx context.Context, req DeployRequest, dc desiredComponent) (ComponentRelease, error) {
	bpOverrides, err := parseYAMLOverrides(dc.blueprintOverride)
	if err != nil {
		return ComponentRelease{Name: dc.name, Status: "failed"},
			errors.Join(ErrComponentInstallFailed,
				fmt.Errorf("parse blueprint override for %q: %w", dc.name, err))
	}
	wlOverrides, err := parseYAMLOverrides(req.Overrides[dc.name])
	if err != nil {
		return ComponentRelease{Name: dc.name, Status: "failed"},
			errors.Join(ErrComponentInstallFailed,
				fmt.Errorf("parse workload override for %q: %w", dc.name, err))
	}

	// NIM detection: ask Discovery whether this chart:version is a known NIM.
	// Found → call GenerateValues with resolved GPU count; layer 4 = result.
	// Not found (ErrNIMNotFound) → silently skip (expected for non-NIMs).
	// Any other error → log warning and treat as non-NIM.
	var nimGenerated map[string]any
	if entry, derr := d.nvidiaDisc.Get(ctx, fmt.Sprintf("%s:%s", dc.chart, dc.version)); derr == nil {
		// gpuCount is a deployer-protocol field, read ONLY from workloadOverrides
		// per P4-4 follow-up note 2. Blueprint overrides cannot influence NIM
		// sizing (their job is Helm-native chart values).
		gpuCount := extractGPUCount(wlOverrides)
		generated, gerr := d.nvidiaDeployer.GenerateValues(ctx, nvidia.GenerateRequest{
			Entry:    entry,
			Replicas: req.Replicas,
			GPUs:     gpuCount,
		})
		if gerr != nil {
			return ComponentRelease{Name: dc.name, Status: "failed"},
				errors.Join(ErrComponentInstallFailed, fmt.Errorf("nvidia.GenerateValues for %q: %w", dc.name, gerr))
		}
		nimGenerated = generated
	} else if !errors.Is(derr, nvidia.ErrNIMNotFound) {
		d.logger.Warn("nvidia.Discovery.Get returned non-NotFound error; treating component as non-NIM",
			slog.String("component", dc.name),
			slog.String("chart", dc.chart),
			slog.String("version", dc.version),
			slog.String("err", derr.Error()))
	}

	chartRef := composeChartRef(dc.repo, dc.chart, dc.version)
	releaseName := ComposeReleaseName(req.ID, dc.name)

	status, ierr := d.helm.InstallChartFromRepo(ctx, helm.InstallRequest{
		Namespace:   req.Namespace,
		ReleaseName: releaseName,
		ChartRef:    chartRef,
		Overrides: helm.Overrides{
			Blueprint:    bpOverrides,
			Workload:     wlOverrides,
			NIMGenerated: nimGenerated,
		},
		Wait:                   false,
		Timeout:                componentInstallTimeout,
		RequireImageRepository: true,
	})
	rel := ComponentRelease{
		Name:        dc.name,
		ReleaseName: releaseName,
		ChartRef:    chartRef,
		Status:      status.Status,
		Revision:    int32(status.Revision),
	}
	if ierr != nil {
		rel.Status = "failed"
		return rel, errors.Join(ErrComponentInstallFailed, fmt.Errorf("helm install %q: %w", dc.name, ierr))
	}
	return rel, nil
}

// parseYAMLOverrides parses a YAML string from the user CR's
// valueOverrides map into a Go map. Empty/whitespace input → nil map
// (treated as "no overrides"). Invalid YAML → error.
func parseYAMLOverrides(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out map[string]any
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// composeChartRef builds the OCI chart ref string from {repo, chart, version}.
// Trims trailing slash on repo to avoid `oci://r//chart:1.0`.
func composeChartRef(repo, chart, version string) string {
	repo = strings.TrimRight(repo, "/")
	return fmt.Sprintf("%s/%s:%s", repo, chart, version)
}

// Teardown uninstalls all component releases in the provided slice.
// Called by WorkloadReconciler's finalizer block on Workload deletion.
//
// Continues past errors, accumulating failures via errors.Join — gives full
// attempt coverage before surfacing failures to the caller (mirroring Deploy's
// install loop semantics from Task 19).
func (d *deployer) Teardown(ctx context.Context, namespace string, releases []ComponentRelease) error {
	if len(releases) == 0 {
		return nil
	}
	var errs []error
	for _, r := range releases {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if err := d.helm.Uninstall(ctx, namespace, r.ReleaseName); err != nil {
			errs = append(errs, fmt.Errorf("uninstall %q: %w", r.ReleaseName, err))
		}
	}
	return errors.Join(errs...)
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
		bp, err := d.blueprintRepo.Get(ctx, blueprintCRName(req.Source.Blueprint.Name, req.Source.Blueprint.Version))
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

// detectOrphans returns previous-component entries whose Name is not in
// desired. Used for drift cleanup: caller invokes helm.Engine.Uninstall
// on each returned entry.
func detectOrphans(previous []ComponentRelease, desired []desiredComponent) []ComponentRelease {
	desiredNames := make(map[string]struct{}, len(desired))
	for _, d := range desired {
		desiredNames[d.name] = struct{}{}
	}
	var orphans []ComponentRelease
	for _, p := range previous {
		if _, kept := desiredNames[p.Name]; !kept {
			orphans = append(orphans, p)
		}
	}
	return orphans
}

