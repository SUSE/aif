package bundle

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
)

// Repository is the K8s-backed CRUD port for Bundle CRs. It is the adapter
// boundary between controller-runtime's client.Client and the domain layer,
// so its signatures intentionally use *aifv1.Bundle. Domain-typed callers go
// through bundle.Service (added in plan task B2) which wraps Repository.
//
// Methods are kept ≤4 (ISP). Split into a separate port if you need more —
// don't grow this one.
type Repository interface {
	Get(ctx context.Context, namespace, name string) (*aifv1.Bundle, error)
	List(ctx context.Context, namespace string, selector labels.Selector) ([]aifv1.Bundle, error)
	Update(ctx context.Context, b *aifv1.Bundle) error
	UpdateStatus(ctx context.Context, b *aifv1.Bundle) error
}
