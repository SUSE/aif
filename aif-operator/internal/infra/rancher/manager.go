package rancher

import (
	"github.com/SUSE/aif-operator/internal/infra/helm"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Manager struct {
	client     client.Client
	indexCache *helm.IndexCache
}

func NewManager(c client.Client) *Manager {
	return &Manager{client: c, indexCache: helm.NewIndexCache()}
}
