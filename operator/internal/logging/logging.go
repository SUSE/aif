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
