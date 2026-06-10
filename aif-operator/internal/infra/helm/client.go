package helm

import (
	"context"
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
}

type HelmClient interface {
	EnsureRelease(ctx context.Context, spec ReleaseSpec) error
	DeleteRelease(ctx context.Context, name string) error
	GetRelease(ctx context.Context, name string) (*ReleaseInfo, error)
}
