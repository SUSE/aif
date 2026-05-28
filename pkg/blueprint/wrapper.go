package blueprint

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/SUSE/aif/pkg/apps"
)

type wrapper struct {
	catalog apps.Catalog
	store   WrappedBlueprintStore
	emitter EventEmitter
	logger  *slog.Logger
}

// NewWrapper creates a Wrapper that consumes the apps.Catalog to
// detect Reference Blueprint charts and persists wrapping Blueprint CRs
// via the WrappedBlueprintStore port.
func NewWrapper(catalog apps.Catalog, store WrappedBlueprintStore, emitter EventEmitter, logger *slog.Logger) Wrapper {
	return &wrapper{
		catalog: catalog,
		store:   store,
		emitter: emitter,
		logger:  logger,
	}
}

func (w *wrapper) WrapDetectedCharts(ctx context.Context) error {
	allApps, err := w.catalog.List(ctx, apps.ListOpts{IncludeReferenceBlueprints: true})
	if err != nil {
		return fmt.Errorf("listing catalog: %w", err)
	}

	rbSet := make(map[string]apps.App)
	for _, a := range allApps {
		if !a.ReferenceBlueprint {
			continue
		}
		name, err := wrappedName(a)
		if err != nil {
			w.logger.Warn("skipping non-semver chart version", "app", a.ID, "version", a.ChartRef.Version)
			continue
		}
		rbSet[name] = a
	}

	for name, a := range rbSet {
		bp := blueprintFromApp(a, name)
		created, err := w.store.CreateWrapped(ctx, bp)
		if err != nil {
			return fmt.Errorf("creating blueprint %s: %w", name, err)
		}
		if created {
			w.emitter.BlueprintWrappedFromVendorChart(bp)
		}
	}

	existing, err := w.store.ListWrapped(ctx)
	if err != nil {
		return fmt.Errorf("listing wrapped blueprints: %w", err)
	}
	for _, bp := range existing {
		if _, stillPresent := rbSet[bp.Name]; stillPresent {
			continue
		}
		if bp.Status.Phase == PhaseWithdrawn {
			continue
		}
		if err := w.store.Withdraw(ctx, bp.Name); err != nil {
			return fmt.Errorf("withdrawing blueprint %s: %w", bp.Name, err)
		}
		w.emitter.BlueprintWithdrawn(bp)
	}

	return nil
}

func wrappedName(a apps.App) (string, error) {
	if err := validateSemVer(a.ChartRef.Version); err != nil {
		return "", err
	}
	return a.Source + "-" + a.ChartRef.Chart + "." + a.ChartRef.Version, nil
}

func blueprintFromApp(a apps.App, name string) Blueprint {
	lineage := a.Source + "-" + a.ChartRef.Chart
	useCase := a.UseCase
	if useCase == "" {
		useCase = "other"
	}
	now := time.Now().UTC()

	return Blueprint{
		Name:    name,
		Lineage: lineage,
		Version: a.ChartRef.Version,
		UseCase: useCase,
		ChangeDescription: fmt.Sprintf("Auto-wrapped from %s/%s:%s at %s",
			a.ChartRef.Repo, a.ChartRef.Chart, a.ChartRef.Version,
			now.Format(time.RFC3339)),
		Source: Source{
			Type: SourceTypeWrapsVendorChart,
			Vendor: &VendorChartRef{
				Provider: a.Source,
				Repo:     a.ChartRef.Repo,
				Chart:    a.ChartRef.Chart,
				Version:  a.ChartRef.Version,
			},
		},
		Components: []ComponentRef{{
			Name: lineage,
			Kind: ComponentKindApp,
			App: &AppRef{
				Repo:    a.ChartRef.Repo,
				Chart:   a.ChartRef.Chart,
				Version: a.ChartRef.Version,
			},
		}},
		PublishedBy: "aif-system",
		PublishedAt: now,
		Status:      Status{Phase: PhaseActive},
	}
}

func validateSemVer(version string) error {
	if !strictVersionPattern.MatchString(version) {
		return fmt.Errorf("%w: %s", ErrSkippedNonSemVer, version)
	}
	return nil
}
