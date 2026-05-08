package publish

import "errors"

var (
	ErrNotImplemented              = errors.New("publish.Workflow: method not implemented")
	ErrInvalidTransition           = errors.New("invalid phase transition")
	ErrBundleNotFound              = errors.New("bundle not found")
	ErrPublishConflict             = errors.New("blueprint version already exists")
	ErrPublishNotPending           = errors.New("bundle is not in submitted state")
	ErrPublishVersionNotIncreasing = errors.New("proposed version must be greater than existing")
	ErrPublisherRequired           = errors.New("publisher role required")
)
