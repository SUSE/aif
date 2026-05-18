package workload

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
	"github.com/SUSE/aif/pkg/helm"
	"github.com/SUSE/aif/pkg/nvidia"
)

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

// Deploy is implemented incrementally in tasks 15-25.
func (d *deployer) Deploy(_ context.Context, _ DeployRequest) (DeployResult, error) {
	// Tasks 15-25 fill this in.
	return DeployResult{}, nil
}

// Teardown is implemented in task 24.
func (d *deployer) Teardown(_ context.Context, _ string, _ []ComponentRelease) error {
	// Task 24 fills this in.
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
		return componentsFromCRComponents(bp.Spec.Components, bp.Spec.ValueOverrides)

	case SourceKindBundleTest:
		// Task 17 implements this branch.
		return nil, 0, ErrSourceNotResolved
	}
	return nil, 0, ErrSourceNotResolved
}
