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
)

type ReleaseStatus string

const (
	StatusDeployed ReleaseStatus = "deployed"
	StatusFailed   ReleaseStatus = "failed"
)

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
	// PlainHTTP pulls from an OCI registry over plain http:// (credentials sent
	// in cleartext). Opt-in; OCI only.
	PlainHTTP bool
}

// RegistryAuth carries resolved chart-pull credentials. Never logged or persisted.
type RegistryAuth struct {
	Username string
	Password string
}

type HelmClient interface {
	EnsureRelease(ctx context.Context, spec ReleaseSpec) error
	DeleteRelease(ctx context.Context, name string) error
	GetRelease(ctx context.Context, name string) (*ReleaseInfo, error)
}
