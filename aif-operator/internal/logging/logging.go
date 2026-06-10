package logging

import (
	"context"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

func WithLogger(ctx context.Context, log logr.Logger) context.Context {
	return logr.NewContext(ctx, log)
}

func FromContext(ctx context.Context, component string) logr.Logger {
	return ctrl.LoggerFrom(ctx).WithName(component)
}

const (
	KeyExtension = "extension"
	KeyNamespace = "namespace"
	KeyComponent = "component"
	KeyPhase     = "phase"
	KeyResource  = "resource"
	KeyName      = "name"
	KeyVersion   = "version"
)

func Debug(log logr.Logger) logr.Logger {
	return log.V(1)
}

func Trace(log logr.Logger) logr.Logger {
	return log.V(2)
}
