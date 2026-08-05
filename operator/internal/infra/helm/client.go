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
	"crypto/tls"
	"errors"
)

// ErrReleasePending is returned when a release is stuck mid-operation. Helm
// refuses to upgrade over a pending release, so there is nothing useful to
// attempt until it settles or is rolled back.
var ErrReleasePending = errors.New("helm release has a pending operation in progress")

type ReleaseStatus string

const (
	StatusDeployed        ReleaseStatus = "deployed"
	StatusFailed          ReleaseStatus = "failed"
	StatusSuperseded      ReleaseStatus = "superseded"
	StatusPendingInstall  ReleaseStatus = "pending-install"
	StatusPendingUpgrade  ReleaseStatus = "pending-upgrade"
	StatusPendingRollback ReleaseStatus = "pending-rollback"
)

// IsPending reports whether an operation is still in flight on the release.
// Helm's own upgrade path rejects these states, so callers should back off
// rather than layer another operation on top.
func (s ReleaseStatus) IsPending() bool {
	switch s {
	case StatusPendingInstall, StatusPendingUpgrade, StatusPendingRollback:
		return true
	default:
		return false
	}
}

type ReleaseInfo struct {
	ChartName string
	Version   string
	Values    map[string]interface{}
	Status    ReleaseStatus
	Revision  int
}

type ReleaseSpec struct {
	Name      string
	Namespace string
	ChartRef  string
	RepoURL   string
	Version   string
	Values    map[string]interface{}
	// RegistryAuth optionally authenticates the chart pull. In-memory only.
	RegistryAuth *RegistryAuth
	// TLSConfig optionally supplies registry TLS trust (private CA / mTLS / skip-verify). In-memory only.
	TLSConfig *tls.Config
}

// RegistryAuth carries resolved chart-pull credentials. Never logged or persisted.
type RegistryAuth struct {
	Username string
	Password string
}

type HelmClient interface {
	EnsureRelease(ctx context.Context, spec ReleaseSpec) error
	DeleteRelease(ctx context.Context, name string) error
	// LastRelease returns the newest revision, whatever its status — the same
	// thing `helm history` shows at the top. Use it to report what Helm last
	// attempted. Returns (nil, nil) if the release was never installed.
	LastRelease(ctx context.Context, name string) (*ReleaseInfo, error)
	// DeployedRelease returns the newest revision that actually reached the
	// cluster, skipping failed and pending revisions above it. Use it to decide
	// whether the cluster matches the desired state. Returns (nil, nil) if
	// nothing has ever deployed.
	DeployedRelease(ctx context.Context, name string) (*ReleaseInfo, error)
}
