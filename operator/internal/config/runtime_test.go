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

package config

import "testing"

func TestGetWorkloadNamespace(t *testing.T) {
	t.Run("defaults when unset", func(t *testing.T) {
		t.Setenv("WORKLOAD_NAMESPACE", "")
		if got := GetWorkloadNamespace(); got != DefaultWorkloadNamespace {
			t.Fatalf("GetWorkloadNamespace() = %q, want %q", got, DefaultWorkloadNamespace)
		}
	})

	t.Run("honors env override", func(t *testing.T) {
		t.Setenv("WORKLOAD_NAMESPACE", "custom-workloads")
		if got := GetWorkloadNamespace(); got != "custom-workloads" {
			t.Fatalf("GetWorkloadNamespace() = %q, want %q", got, "custom-workloads")
		}
	})
}
