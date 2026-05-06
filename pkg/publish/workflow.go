package publish

import "context"

// New returns a Workflow bound to the supplied dependencies. Until P3-1
// implements the methods, every operation returns ErrNotImplemented.
func New(d Deps) Workflow {
	return &workflowImpl{deps: d}
}

// workflowImpl is the production Workflow. P3-1 will fill in the bodies; this
// scaffold exists so REST handlers can compile against the interface today.
type workflowImpl struct {
	deps Deps
}

func (w *workflowImpl) Submit(_ context.Context, _, _ string, _ SubmitRequest) error {
	return ErrNotImplemented
}

func (w *workflowImpl) Withdraw(_ context.Context, _, _ string, _ string) error {
	return ErrNotImplemented
}

func (w *workflowImpl) RequestChanges(_ context.Context, _, _ string, _ ReviewRequest) error {
	return ErrNotImplemented
}

func (w *workflowImpl) Approve(_ context.Context, _, _ string, _ ApproveRequest) error {
	return ErrNotImplemented
}
