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
	stderrors "errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The case ignoreGone exists for: client.IgnoreNotFound alone does not cover
// it. A Delete against a GVK whose CRD is gone fails resolving a REST mapping
// before it ever reaches the API server, coming back as a NoKindMatchError —
// a different error type than the NotFound a deleted object produces.
func TestIgnoreGone_SwallowsNoKindMatchError(t *testing.T) {
	err := &meta.NoKindMatchError{
		GroupKind:        schema.GroupKind{Group: "catalog.cattle.io", Kind: "ClusterRepo"},
		SearchedVersions: []string{"v1"},
	}
	if got := ignoreGone(err); got != nil {
		t.Errorf("ignoreGone(%v) = %v, want nil; the CRD being gone means there is nothing "+
			"left to delete, which is success, not failure", err, got)
	}
}

func TestIgnoreGone_SwallowsNotFound(t *testing.T) {
	err := errors.NewNotFound(schema.GroupResource{Group: "catalog.cattle.io", Resource: "clusterrepos"}, "my-plugin")
	if got := ignoreGone(err); got != nil {
		t.Errorf("ignoreGone(%v) = %v, want nil", err, got)
	}
}

func TestIgnoreGone_SwallowsNil(t *testing.T) {
	if got := ignoreGone(nil); got != nil {
		t.Errorf("ignoreGone(nil) = %v, want nil", got)
	}
}

// Anything else — a webhook rejection, a network error, Forbidden — is a real
// failure the caller still has to see and retry on. Swallowing it too would
// turn "cannot delete this" into a silent no-op.
func TestIgnoreGone_PassesThroughOtherErrors(t *testing.T) {
	err := stderrors.New("admission webhook denied the request")
	if got := ignoreGone(err); got != err {
		t.Errorf("ignoreGone(%v) = %v, want the original error unchanged", err, got)
	}
}
