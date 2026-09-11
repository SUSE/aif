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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// The asymmetry this closes: catalogCRDBecameReady already lets a reconcile
// blocked on RancherUnavailable wake up the instant Rancher's CRDs appear,
// but a reconcile blocked on ExtensionNamespaceMissing — the other thing
// checkPreconditions gates on, and just as capable of being the slow one to
// appear during the same Rancher bootstrap race — still had to wait out the
// full backoff. extensionNamespaceCreated is the same idea applied to the
// namespace.

func namespaceNamed(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestExtensionNamespaceCreated_FiresOnTheExtensionNamespace(t *testing.T) {
	r := &InstallAIExtensionReconciler{ExtensionNamespace: wiringNamespace}

	if !r.extensionNamespaceCreated().Create(event.CreateEvent{Object: namespaceNamed(wiringNamespace)}) {
		t.Error("want Create to fire for the extension namespace")
	}
}

func TestExtensionNamespaceCreated_IgnoresOtherNamespaces(t *testing.T) {
	r := &InstallAIExtensionReconciler{ExtensionNamespace: wiringNamespace}

	if r.extensionNamespaceCreated().Create(event.CreateEvent{Object: namespaceNamed("kube-system")}) {
		t.Error("want Create to ignore a namespace this operator has no stake in")
	}
}

// Unlike a CRD, a Namespace has no readiness gate — it is usable the instant
// it exists — so there is no Established-style transition to also react to,
// and nothing else (a label update, a deletion, a poll) is this predicate's
// concern.
func TestExtensionNamespaceCreated_IgnoresUpdateDeleteAndGeneric(t *testing.T) {
	r := &InstallAIExtensionReconciler{ExtensionNamespace: wiringNamespace}
	p := r.extensionNamespaceCreated()

	ns := namespaceNamed(wiringNamespace)
	if p.Update(event.UpdateEvent{ObjectOld: ns, ObjectNew: ns}) {
		t.Error("want Update to never fire")
	}
	if p.Delete(event.DeleteEvent{Object: ns}) {
		t.Error("want Delete to never fire")
	}
	if p.Generic(event.GenericEvent{Object: ns}) {
		t.Error("want Generic to never fire")
	}
}
