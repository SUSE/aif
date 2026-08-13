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
	// converged memoizes "this spec needs no upgrade" verdicts by release name.
	// See convergenceHolds for why re-deriving them is what pulled the chart on
	// a loop.
	converged sync.Map
	// charts maps chartCacheKey to the archive a pull already downloaded, so the
	// render and the upgrade of one reconcile share a single fetch.
	charts sync.Map

	// Test seams, nil in production. Between them they cover everything
	// EnsureRelease needs from outside the process — a cluster and a registry —
	// so its decision paths can be driven end to end, and the chart pulls they
	// do or do not perform counted.
	actionConfigFn func(ctx context.Context, namespace string) (*action.Configuration, error)
	fetchChartFn   chartFetcher
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
	if c.actionConfigFn != nil {
		return c.actionConfigFn(ctx, namespace)
	}
	return c.initActionConfig(ctx, namespace)
}

func (c *helmClient) initActionConfig(ctx context.Context, namespace string) (*action.Configuration, error) {
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
