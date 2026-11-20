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

package api

import (
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// writeGuardedByCRD runs write, preceded by a server-side dry run of the same
// write when guarded (the request sets fields an outdated CRD would silently
// prune; Helm never upgrades CRDs). If dropped reports any of them missing from
// the dry-run result, nothing is persisted and an ErrInvalidInput naming the
// CRD file to apply is returned. kind names the CRD in the message and crdFile
// is its file under charts/aif-operator/crds.
func writeGuardedByCRD[T client.Object](obj T, kind, crdFile string, guarded bool, dropped func(probe T) []string, write func(obj T, dryRun bool) error) error {
	if guarded {
		probe := obj.DeepCopyObject().(T)
		if err := write(probe, true); err != nil {
			return err
		}
		if d := dropped(probe); len(d) > 0 {
			return fmt.Errorf("%w: %s CRD is outdated (%s were dropped); apply charts/aif-operator/crds/%s",
				ErrInvalidInput, kind, strings.Join(d, ", "), crdFile)
		}
	}
	return write(obj, false)
}

// guardedField is an optional list-item field an outdated CRD would prune.
type guardedField[T any] struct {
	name  string
	isSet func(T) bool
}

func anyGuardedFieldSet[T any](items []T, fields []guardedField[T]) bool {
	for _, it := range items {
		for _, f := range fields {
			if f.isSet(it) {
				return true
			}
		}
	}
	return false
}

// droppedListFields lists, as list[key].field, every guarded field set on a
// sent item but unset on the stored item at the same index.
func droppedListFields[T any](list string, sent, stored []T, key func(T) string, fields []guardedField[T]) []string {
	var dropped []string
	for i, it := range sent {
		var got T
		if i < len(stored) {
			got = stored[i]
		}
		for _, f := range fields {
			if f.isSet(it) && !f.isSet(got) {
				dropped = append(dropped, fmt.Sprintf("%s[%s].%s", list, key(it), f.name))
			}
		}
	}
	return dropped
}
