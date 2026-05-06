package blueprint

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
)

// Repository is the K8s-backed CRUD port for Blueprint CRs. Blueprint is
// cluster-scoped, so List takes only a label selector — no namespace argument.
//
// Methods are kept ≤4 (ISP). Split into a separate port if you need more.
type Repository interface {
	Get(ctx context.Context, name string) (*aifv1.Blueprint, error)
	List(ctx context.Context, selector labels.Selector) ([]aifv1.Blueprint, error)
	Update(ctx context.Context, bp *aifv1.Blueprint) error
	UpdateStatus(ctx context.Context, bp *aifv1.Blueprint) error
}
