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

package rancher

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
)

type DependencyNotReadyError struct {
	Dependency string
}

func (e *DependencyNotReadyError) Error() string {
	return fmt.Sprintf("dependency %q is not ready", e.Dependency)
}

// ignoreGone treats "nothing to delete" as success, whether that is because
// the object was never there (NotFound) or because the CRD backing it is gone
// (NoKindMatchError). client.IgnoreNotFound alone misses the second case: a
// Delete against an unregistered GVK fails to resolve a REST mapping before it
// ever reaches the API server, so it comes back as a meta.NoKindMatchError, not
// a NotFound. Without this, deleting ClusterRepo/UIPlugin after Rancher's CRDs
// are gone returns an error forever, and the finalizer that calls it can never
// clear — the exact "resources dangling" failure mode for a CR, not just for
// the object it was trying to remove.
//
// Deliberately does NOT also swallow Forbidden. A permissions gap the operator
// cannot resolve on its own (its ClusterRole missing a verb, a webhook denying
// the request) should surface and eventually stop the finalizer via
// cleanupTimeout's bounded give-up, not retry forever pretending to succeed —
// that would hide a real misconfiguration behind an object that looks deleted
// but never was. The one Forbidden this can plausibly still hit is transient:
// a freshly created ClusterRoleBinding whose grant hasn't propagated to the
// API server's authorizer cache yet, which the bounded retry above this
// resolves within its normal interval, not by ignoreGone treating it as success.
func ignoreGone(err error) error {
	if err == nil || errors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil
	}
	return err
}
