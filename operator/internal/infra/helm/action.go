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
	"sync"
	"time"

	"github.com/SUSE/aif-operator/internal/logging"
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

func (c *helmClient) renderUpgrade(
	ctx context.Context,
	cfg *action.Configuration,
	spec ReleaseSpec,
) (string, error) {
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
		return "", err
	}

	rel, err := up.RunWithContext(ctx, spec.Name, ch, spec.Values)
	if err != nil {
		return "", err
	}

	return rel.Manifest, nil
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
// yields the OLDEST revision, not the newest. Storage.Last sorts by revision,
// which is what `helm history` and action.Get already rely on.
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
	if last == nil {
		log.Info("Helm release not found, installing")
		return c.install(ctx, cfg, spec)
	}

	// Helm rejects an upgrade over a release that is mid-operation, so surface
	// that as its own error instead of letting the upgrade fail opaquely.
	if last.Status.IsPending() {
		log.Info("Helm release has a pending operation, skipping upgrade",
			"status", string(last.Status), "revision", last.Revision)
		return fmt.Errorf("%w: release %q is %s at revision %d",
			ErrReleasePending, spec.Name, last.Status, last.Revision)
	}

	// Drift is measured against what is deployed, never against the last
	// revision. A failed upgrade leaves a newer revision carrying the requested
	// version while the cluster still runs the previous one; comparing against
	// it would make the retry look unnecessary and skip it forever.
	deployed, err := deployedRelease(cfg, spec.Name)
	if err != nil {
		return err
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

	// Fast path only. The authoritative check is the manifest diff below; this
	// exists purely to skip a chart pull when nothing can possibly have changed.
	if !releaseNeedsUpgrade(deployed, spec) {
		log.Info("Helm release version and values unchanged, skipping upgrade")
		return nil
	}

	// An error here leaves current empty, which forces the diff below to report a
	// change — erring towards attempting the upgrade rather than skipping it.
	current, _ := deployedManifest(cfg, spec.Name)
	rendered, err := c.renderUpgrade(ctx, cfg, spec)
	if err != nil {
		return err
	}

	if !diffManifests(current, rendered) {
		log.Info("Helm release is up-to-date, skipping upgrade")
		return nil
	}
	log.Info("Detected Helm manifest changes, upgrading")
	return c.upgrade(ctx, cfg, spec)
}
