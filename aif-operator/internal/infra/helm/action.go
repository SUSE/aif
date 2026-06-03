package helm

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/SUSE/aif-operator/internal/logging"
	"helm.sh/helm/v3/pkg/action"
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
	install.SetRegistryClient(c.registry)
	if spec.RepoURL != "" {
		install.RepoURL = spec.RepoURL
	}

	ch, _, err := resolveChart(&install.ChartPathOptions, c.settings, spec.ChartRef)
	if err != nil {
		log.Error(err, "Failed to resolve Helm chart")
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
	up.SetRegistryClient(c.registry)
	if spec.RepoURL != "" {
		up.RepoURL = spec.RepoURL
	}

	up.Wait = true
	up.Atomic = false
	up.Timeout = 10 * time.Minute

	ch, _, err := resolveChart(&up.ChartPathOptions, c.settings, spec.ChartRef)
	if err != nil {
		log.Error(err, "Failed to resolve Helm chart")
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
	up.SetRegistryClient(c.registry)
	if spec.RepoURL != "" {
		up.RepoURL = spec.RepoURL
	}

	ch, _, err := resolveChart(&up.ChartPathOptions, c.settings, spec.ChartRef)
	if err != nil {
		return "", err
	}

	rel, err := up.RunWithContext(ctx, spec.Name, ch, spec.Values)
	if err != nil {
		return "", err
	}

	return rel.Manifest, nil
}

func currentManifest(cfg *action.Configuration, name string) (string, error) {
	get := action.NewGet(cfg)
	rel, err := get.Run(name)
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
		if strings.Contains(err.Error(), "release: not found") {
			log.Info("Helm release already deleted")
			return nil
		}
		log.Error(err, "Failed to delete Helm release")
		return err
	}

	log.Info("Helm release deleted")
	return nil
}

func (c *helmClient) GetRelease(ctx context.Context, name string) (*ReleaseInfo, error) {
	cfg, err := c.actionConfig(ctx, c.settings.Namespace())
	if err != nil {
		return nil, err
	}

	hist := action.NewHistory(cfg)
	hist.Max = 1

	rels, err := hist.Run(name)
	if err != nil {
		if strings.Contains(err.Error(), "release: not found") {
			return nil, nil
		}
		return nil, err
	}
	if len(rels) == 0 {
		return nil, nil
	}

	rel := rels[0]

	return &ReleaseInfo{
		ChartName: rel.Chart.Name(),
		Version:   rel.Chart.Metadata.Version,
		Values:    rel.Config,
		Status:    ReleaseStatus(rel.Info.Status),
		Revision:  rel.Version,
	}, nil
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

	info, err := c.GetRelease(ctx, spec.Name)
	if err != nil {
		return err
	}
	if info == nil {
		log.Info("Helm release not found, installing")
		return c.install(ctx, cfg, spec)
	}

	current, _ := currentManifest(cfg, spec.Name)
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
