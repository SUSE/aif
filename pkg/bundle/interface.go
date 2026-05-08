package bundle

import "context"

type Manager interface {
	Create(ctx context.Context, b Bundle) (Bundle, error)
	Get(ctx context.Context, ns, name string) (Bundle, error)
	List(ctx context.Context, opts ListOptions) ([]Bundle, error)
	Update(ctx context.Context, b Bundle) (Bundle, error)
	Delete(ctx context.Context, ns, name string) error
	ListPendingReview(ctx context.Context) ([]Bundle, error)
}

type ListOptions struct {
	Namespace string
}
