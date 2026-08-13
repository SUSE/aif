/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SUSE/aif-operator/internal/logging"
	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
)

func (c *helmClient) install(
	ctx context.Context,
	cfg *action.Configuration,
	spec ReleaseSpec,
) error {
	log := logging.FromContext(ctx, "helm").WithValues(
		logging.KeyName, spec.Name,
		logging.KeyNamespace, spec.Namespace,
		logging.KeyVersion, spec.Version,
	)

	log.Info("Installing Helm release")

	install := action.NewInstall(cfg)
	install.ReleaseName = spec.Name
	install.Namespace = spec.Namespace
	install.Version = spec.Version
	if spec.RepoURL != "" {
		install.RepoURL = spec.RepoURL
	}

	ch, err := c.loadChart(install.SetRegistryClient, &install.ChartPathOptions, spec)
	if err != nil {
		log.Error(err, "Failed to load Helm chart")
		return err
	}

	_, err = install.RunWithContext(ctx, ch, spec.Values)
	if err != nil {
		log.Error(err, "Helm install failed")
		return err
	}

	log.Info("Helm release installed successfully")
	return nil
}

func (c *helmClient) upgrade(
	ctx context.Context,
	cfg *action.Configuration,
	spec ReleaseSpec,
) error {
	log := logging.FromContext(ctx, "helm").WithValues(
		logging.KeyName, spec.Name,
		logging.KeyNamespace, spec.Namespace,
		logging.KeyVersion, spec.Version,
	)

	log.Info("Upgrading Helm release")

	up := action.NewUpgrade(cfg)
	up.Namespace = spec.Namespace
	up.Version = spec.Version
	if spec.RepoURL != "" {
		up.RepoURL = spec.RepoURL
	}

	up.Wait = true
	up.Atomic = false
	up.Timeout = 10 * time.Minute

	ch, err := c.loadChart(up.SetRegistryClient, &up.ChartPathOptions, spec)
	if err != nil {
		log.Error(err, "Failed to load Helm chart")
		return err
	}
	_, err = up.RunWithContext(ctx, spec.Name, ch, spec.Values)
	if err != nil {
		log.Error(err, "Helm upgrade failed")
		return err
	}

	log.Info("Helm release upgraded successfully")
	return nil
}

// renderUpgrade dry-runs the upgrade and returns the manifest it would apply,
// along with the pulled chart's own version. The caller needs that version to
// tell a release that is genuinely up-to-date apart from a cosmetic version
// mismatch: Helm records the chart's version, not the requested one, so the two
// disagreeing means no upgrade will ever reconcile them.
func (c *helmClient) renderUpgrade(
	ctx context.Context,
	cfg *action.Configuration,
	spec ReleaseSpec,
) (manifest string, chartVersion string, err error) {
	up := action.NewUpgrade(cfg)
	up.Namespace = spec.Namespace
	up.Version = spec.Version
	up.DryRun = true
	up.Wait = false
	up.Atomic = false
	up.Timeout = 2 * time.Minute
	if spec.RepoURL != "" {
		up.RepoURL = spec.RepoURL
	}

	ch, err := c.loadChart(up.SetRegistryClient, &up.ChartPathOptions, spec)
	if err != nil {
		return "", "", err
	}

	rel, err := up.RunWithContext(ctx, spec.Name, ch, spec.Values)
	if err != nil {
		return "", "", err
	}

	if ch.Metadata != nil {
		chartVersion = ch.Metadata.Version
	}
	return rel.Manifest, chartVersion, nil
}

// deployedManifest returns the manifest of the revision actually running in the
// cluster. It deliberately does not use action.Get, which resolves to the last
// revision: a failed revision's manifest is what Helm *attempted*, so diffing
// against it reports "up-to-date" for an upgrade that never landed.
func deployedManifest(cfg *action.Configuration, name string) (string, error) {
	rel, err := cfg.Releases.Deployed(name)
	if err != nil {
		return "", err
	}
	return rel.Manifest, nil
}

func diffManifests(old, new string) bool {
	return old != new
}

func (c *helmClient) lockRelease(name string) func() {
	m, _ := c.locks.LoadOrStore(name, &sync.Mutex{})
	mtx := m.(*sync.Mutex)
	mtx.Lock()

	return func() {
		mtx.Unlock()
	}
}

func (c *helmClient) DeleteRelease(ctx context.Context, name string) error {
	log := logging.FromContext(ctx, "helm").WithValues(
		logging.KeyName, name,
	)

	cfg, err := c.actionConfig(ctx, c.settings.Namespace())
	if err != nil {
		return err
	}

	// Dropped up front, not on the success path: a release that is being removed
	// must not leave a verdict behind for a later release of the same name to
	// match against, and the uninstall below returns early when Helm has already
	// forgotten the release.
	c.dropConvergence(name)
	releaseUnconverged.DeleteLabelValues(name)

	uninstall := action.NewUninstall(cfg)
	uninstall.DeletionPropagation = "foreground"

	_, err = uninstall.Run(name)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			log.Info("Helm release already deleted")
			return nil
		}
		log.Error(err, "Failed to delete Helm release")
		return err
	}

	log.Info("Helm release deleted")
	return nil
}

func releaseInfoFrom(rel *release.Release) *ReleaseInfo {
	if rel == nil {
		return nil
	}

	info := &ReleaseInfo{
		Values:   rel.Config,
		Revision: rel.Version,
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		info.ChartName = rel.Chart.Metadata.Name
		info.Version = rel.Chart.Metadata.Version
	}
	if rel.Info != nil {
		info.Status = ReleaseStatus(rel.Info.Status)
	}
	return info
}

// lastRelease returns the newest revision of a release, or (nil, nil) if the
// release has never been installed.
//
// It reads storage directly instead of going through action.NewHistory. That
// action ignores its Max field and hands back the driver's raw query order,
// which for the Secret driver is the API server's name ordering
// (sh.helm.release.v1.<name>.v1 sorts first). Taking the head of that slice
// yields the OLDEST revision, not the newest.
//
// The helm CLI never trips over this because it sorts the result itself before
// printing (cmd/helm/history.go), which is why the defect is invisible upstream
// and worth naming here. Storage.Last does that sort for us, and is the same
// call action.Get relies on.
func lastRelease(cfg *action.Configuration, name string) (*ReleaseInfo, error) {
	rel, err := cfg.Releases.Last(name)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return releaseInfoFrom(rel), nil
}

// deployedRelease returns the newest revision that actually reached the cluster,
// or (nil, nil) if none has. A failed or pending revision sitting above it is
// skipped, so a half-applied upgrade doesn't read as the current state.
func deployedRelease(cfg *action.Configuration, name string) (*ReleaseInfo, error) {
	rel, err := cfg.Releases.Deployed(name)
	if err != nil {
		if errors.Is(err, driver.ErrNoDeployedReleases) || errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return releaseInfoFrom(rel), nil
}

func (c *helmClient) LastRelease(ctx context.Context, name string) (*ReleaseInfo, error) {
	cfg, err := c.actionConfig(ctx, c.settings.Namespace())
	if err != nil {
		return nil, err
	}
	return lastRelease(cfg, name)
}

func (c *helmClient) DeployedRelease(ctx context.Context, name string) (*ReleaseInfo, error) {
	cfg, err := c.actionConfig(ctx, c.settings.Namespace())
	if err != nil {
		return nil, err
	}
	return deployedRelease(cfg, name)
}

// releaseAction is the operation EnsureRelease selects from storage state alone,
// before any chart is pulled.
type releaseAction int

const (
	actionInstall releaseAction = iota
	actionUpgrade
	actionSkip
	actionPending
)

func (a releaseAction) String() string {
	switch a {
	case actionInstall:
		return "install"
	case actionUpgrade:
		return "upgrade"
	case actionSkip:
		return "skip"
	case actionPending:
		return "pending"
	default:
		return "unknown"
	}
}

// decideRelease picks the operation for a release from what storage reports.
//
// This is a pure function because it is where the bug lived. Not in either lookup
// — both are correct in isolation — but in which lookup fed which comparison.
// `last` answers "what did Helm most recently attempt"; `deployed` answers "what
// is actually running". Measuring drift against `last` makes a failed or
// superseded revision look applied, so the retry is skipped and the release never
// converges. Keeping that choice in one testable place is what stops the two from
// being swapped back.
//
// deployedFn is called lazily: install and pending are decided from `last` alone,
// and neither should pay for a storage read it cannot use. The returned
// ReleaseInfo is the deployed revision when one was consulted, nil otherwise.
func decideRelease(
	last *ReleaseInfo,
	deployedFn func() (*ReleaseInfo, error),
	spec ReleaseSpec,
) (releaseAction, *ReleaseInfo, error) {
	if last == nil {
		return actionInstall, nil, nil
	}

	// Helm rejects an upgrade over a release that is mid-operation, so stop here
	// rather than letting the upgrade fail opaquely further down.
	if last.Status.IsPending() {
		return actionPending, nil, nil
	}

	deployed, err := deployedFn()
	if err != nil {
		return actionUpgrade, nil, err
	}
	// A revision exists but nothing ever deployed — a first install that failed.
	// Retry as an upgrade: Helm's prepareUpgrade falls back to the last revision
	// when it is failed or superseded.
	if deployed == nil {
		return actionUpgrade, nil, nil
	}

	if !releaseNeedsUpgrade(deployed, spec) {
		return actionSkip, deployed, nil
	}
	return actionUpgrade, deployed, nil
}

func releaseNeedsUpgrade(info *ReleaseInfo, spec ReleaseSpec) bool {
	if versionDrift(info, spec) {
		return true
	}
	return !valuesEqual(info.Values, spec.Values)
}

// versionDrift reports whether an installed release's chart version differs from
// the requested version. It is the single version-difference predicate shared by
// releaseNeedsUpgrade (which upgrades on it) and the drift log in EnsureRelease
// (observability), so the decision and the log can't fall out of sync.
func versionDrift(info *ReleaseInfo, spec ReleaseSpec) bool {
	return info != nil && info.Version != spec.Version
}

func valuesEqual(a, b map[string]interface{}) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aj) == string(bj)
}

func (c *helmClient) EnsureRelease(ctx context.Context, spec ReleaseSpec) error {
	log := logging.FromContext(ctx, "helm").WithValues(
		logging.KeyName, spec.Name,
		logging.KeyNamespace, spec.Namespace,
	)

	unlock := c.lockRelease(spec.Name)
	defer unlock()

	cfg, err := c.actionConfig(ctx, spec.Namespace)
	if err != nil {
		return err
	}

	last, err := lastRelease(cfg, spec.Name)
	if err != nil {
		return err
	}

	decision, deployed, err := decideRelease(last, func() (*ReleaseInfo, error) {
		return deployedRelease(cfg, spec.Name)
	}, spec)
	if err != nil {
		return err
	}

	switch decision {
	case actionInstall:
		log.Info("Helm release not found, installing")
		return c.install(ctx, cfg, spec)

	case actionPending:
		log.Info("Helm release has a pending operation, skipping upgrade",
			"status", string(last.Status), "revision", last.Revision)
		return fmt.Errorf("%w: release %q is %s at revision %d",
			ErrReleasePending, spec.Name, last.Status, last.Revision)

	// Skipping here is a fast path only. The authoritative check is the manifest
	// diff below; this exists purely to avoid pulling a chart when neither the
	// version nor the values can have changed anything.
	case actionSkip:
		// Reaching here is convergence: version and values both agree. It is also
		// the only place that can observe a previously reported disagreement
		// having been fixed, because reportUnconverged below is unreachable once
		// the release compares equal to its spec. Without this the gauge only
		// ever rises, and goes on reporting a misconfiguration long after
		// someone corrected it.
		releaseUnconverged.WithLabelValues(spec.Name).Set(0)
		log.Info("Helm release version and values unchanged, skipping upgrade")
		return nil
	}

	if deployed == nil {
		log.Info("Helm release has no deployed revision, upgrading",
			"lastRevision", last.Revision, "lastStatus", string(last.Status))
		return c.upgrade(ctx, cfg, spec)
	}

	if versionDrift(deployed, spec) {
		log.Info("deployed Helm release version differs from requested version",
			"requestedVersion", spec.Version, "deployedVersion", deployed.Version)
	}

	// The manifest diff below is the only thing that can clear this disagreement,
	// and it costs a chart pull to compute. Once it has been computed for this
	// exact spec against this exact revision, its answer cannot change, so asking
	// again buys nothing and pulls the chart every reconcile.
	if c.convergenceHolds(spec, deployed) {
		// Reaching here means the spec and storage still disagree — the fast path
		// above would have returned otherwise — and that a render has already
		// proved no upgrade resolves it. The verdict is what is memoized; the
		// disagreement it describes is still live, so the gauge has to keep
		// saying so. Skipping this would let a latched release report 0 while
		// unconverged, which is a worse failure than the stale 1 it replaces: an
		// alert that is merely late is survivable, one that is silent is not.
		//
		// The cause was named in full when the latch was created and is not
		// repeated here, which is the point of the latch — at one reconcile a
		// minute for the life of the CR, re-logging it is how the signal gets
		// lost.
		releaseUnconverged.WithLabelValues(spec.Name).Set(1)
		log.Info("Helm release already verified up-to-date for this spec, skipping upgrade",
			"revision", deployed.Revision, "requestedVersion", spec.Version)
		return nil
	}

	// An error here leaves current empty, which forces the diff below to report a
	// change — erring towards attempting the upgrade rather than skipping it.
	current, _ := deployedManifest(cfg, spec.Name)
	rendered, chartVersion, err := c.renderUpgrade(ctx, cfg, spec)
	if err != nil {
		return err
	}

	if !diffManifests(current, rendered) {
		// Up-to-date, yet something in the spec still disagrees with storage or
		// this code would have taken the actionSkip fast path. Whatever it is, no
		// upgrade can resolve it, so record the verdict and report the cause
		// rather than rediscovering both on the next pass.
		c.latchConvergence(spec, deployed)
		reportUnconverged(log, spec, deployed, chartVersion)
		log.Info("Helm release is up-to-date, skipping upgrade")
		return nil
	}
	log.Info("Detected Helm manifest changes, upgrading")
	return c.upgrade(ctx, cfg, spec)
}

// reportUnconverged names why a release that needs no upgrade still does not
// compare equal to its spec, and raises a gauge for as long as that is true.
//
// Left unreported this is genuinely invisible: the release is healthy, the logs
// say "up-to-date", and the only outward sign used to be chart pulls on a loop —
// which the latch has just removed.
func reportUnconverged(log logr.Logger, spec ReleaseSpec, deployed *ReleaseInfo, chartVersion string) {
	switch {
	case chartVersion != "" && chartVersion != spec.Version:
		releaseUnconverged.WithLabelValues(spec.Name).Set(1)
		log.Info("Chart version does not match the requested version; "+
			"the release is up-to-date but will never compare equal to its spec. "+
			"Align the chart's Chart.yaml version with the tag the CR pins.",
			"requestedVersion", spec.Version, "chartVersion", chartVersion)

	case !valuesEqual(deployed.Values, spec.Values):
		releaseUnconverged.WithLabelValues(spec.Name).Set(1)
		onlyStored, onlyRequested := valuesKeyDiff(deployed.Values, spec.Values)
		log.Info("Requested values differ from the stored release values but change "+
			"nothing the chart renders; the release is up-to-date and cannot "+
			"converge on its own. Keys only in storage are ones Helm copies "+
			"forward on every upgrade because the CR requests no values at all — "+
			"declare them in the CR to make the two agree. Keys only in the CR "+
			"are ones this chart never reads.",
			"requestedVersion", spec.Version,
			"keysOnlyInStorage", onlyStored,
			"keysOnlyInRequest", onlyRequested)

	default:
		// Neither named cause applies, yet the release still did not take the
		// actionSkip fast path, so something disagrees. What is left is a stored
		// version the requested chart cannot update: the chart now pulled matches
		// the CR, but it renders exactly what is already deployed, so no upgrade
		// runs and storage keeps the version it was originally deployed from.
		// A version-only chart bump leaves precisely this behind.
		//
		// Raised, not lowered. This function is reached only on a disagreement,
		// so reporting convergence here would be wrong on its own terms — and it
		// would also contradict the latch path, which reports 1 for this same
		// release on every pass after this one. Lowering it would make the one
		// pass that can explain the cause the only one that denies there is one.
		releaseUnconverged.WithLabelValues(spec.Name).Set(1)
		log.Info("Stored release version differs from the requested version, and the "+
			"requested chart renders no change, so no upgrade will run to update "+
			"the record. The release is already running what the CR asks for; only "+
			"the version Helm stored lags behind, and it will keep lagging. Force "+
			"a new revision if the recorded version has to match.",
			"requestedVersion", spec.Version,
			"deployedVersion", deployed.Version,
			"chartVersion", chartVersion)
	}
}

// valuesKeyDiff names the top-level keys each side holds alone, so the log says
// which values disagree instead of leaving the reader to run `helm get values`
// and diff it against the CR by hand.
//
// Keys only, never the values under them: this is written to a log that outlives
// any credential in it, and a key name is what identifies the misconfiguration
// anyway. Top level only, for the same reason a deep diff would be worse to
// read than the two sources it is summarising.
func valuesKeyDiff(stored, requested map[string]interface{}) (onlyStored, onlyRequested []string) {
	for key := range stored {
		if _, ok := requested[key]; !ok {
			onlyStored = append(onlyStored, key)
		}
	}
	for key := range requested {
		if _, ok := stored[key]; !ok {
			onlyRequested = append(onlyRequested, key)
		}
	}
	sort.Strings(onlyStored)
	sort.Strings(onlyRequested)
	return onlyStored, onlyRequested
}
