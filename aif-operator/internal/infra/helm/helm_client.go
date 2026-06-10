package helm

import (
	"context"
	"fmt"
	"sync"

	"github.com/SUSE/aif-operator/internal/logging"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
)

type helmClient struct {
	settings *cli.EnvSettings
	registry *registry.Client
	locks    sync.Map
}

func New(settings *cli.EnvSettings) (HelmClient, error) {
	reg, err := registry.NewClient(
		registry.ClientOptDebug(settings.Debug),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	)
	if err != nil {
		return nil, err
	}

	return &helmClient{
		settings: settings,
		registry: reg,
	}, nil
}

func (c *helmClient) actionConfig(ctx context.Context, namespace string) (*action.Configuration, error) {
	log := logging.FromContext(ctx, "helm")

	logging.Trace(log).Info(
		"Initializing Helm action configuration",
		logging.KeyNamespace, namespace,
	)

	cfg := new(action.Configuration)
	if err := cfg.Init(
		c.settings.RESTClientGetter(),
		namespace,
		"",
		func(format string, v ...interface{}) {
			logging.Trace(log).Info("helm", "msg", fmt.Sprintf(format, v...))
		},
	); err != nil {
		return nil, err
	}
	return cfg, nil
}
