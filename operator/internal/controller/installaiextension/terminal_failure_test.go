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

package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestSetTerminalFailure_MirrorsOntoReady(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	ext.Generation = 7

	setTerminalFailure(ext, conditionTypeHelmInstalled, "InstallFailed", "boom")

	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
		t.Fatalf("phase = %q, want Failed", ext.Status.Phase)
	}

	hi := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeHelmInstalled)
	if hi == nil || hi.Status != metav1.ConditionFalse || hi.Reason != "InstallFailed" {
		t.Fatalf("HelmInstalled condition = %+v, want False/InstallFailed", hi)
	}

	// Ready must be mirrored to False with the same reason/message and generation,
	// so it never shows a stale success while phase is Failed.
	rd := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
	if rd == nil || rd.Status != metav1.ConditionFalse || rd.Reason != "InstallFailed" || rd.Message != "boom" {
		t.Fatalf("Ready condition = %+v, want False/InstallFailed/boom", rd)
	}
	if rd.ObservedGeneration != 7 {
		t.Fatalf("Ready observedGeneration = %d, want 7", rd.ObservedGeneration)
	}
}

func TestSetTerminalFailure_ReadyCondTypeNotDuplicated(t *testing.T) {
	ext := &v1alpha1.InstallAIExtension{}
	ext.Generation = 1

	setTerminalFailure(ext, conditionTypeReady, "InvalidSpec", "bad spec")

	// When the sub-condition IS Ready, there must be exactly one Ready condition.
	count := 0
	for _, c := range ext.Status.Conditions {
		if c.Type == conditionTypeReady {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Ready condition count = %d, want 1", count)
	}
	rd := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
	if rd == nil || rd.Status != metav1.ConditionFalse || rd.Reason != "InvalidSpec" {
		t.Fatalf("Ready condition = %+v, want False/InvalidSpec", rd)
	}
}
