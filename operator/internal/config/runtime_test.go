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

// An unset name disables the operator API; there is no default name, so the
// operator never guesses which Secret holds the tunnel token.
func TestGetAggregationSecretName(t *testing.T) {
	t.Setenv("AGGREGATION_SECRET_NAME", "")
	if got := GetAggregationSecretName(); got != "" {
		t.Errorf("unset: got %q, want empty", got)
	}
	t.Setenv("AGGREGATION_SECRET_NAME", "aif-operator-aggregation")
	if got := GetAggregationSecretName(); got != "aif-operator-aggregation" {
		t.Errorf("set: got %q, want aif-operator-aggregation", got)
	}
}

// The namespace defaults to cattle-system, the chart's default, which keeps the
// tunnel token out of the operator namespace.
func TestGetAggregationSecretNamespace(t *testing.T) {
	t.Setenv("AGGREGATION_SECRET_NAMESPACE", "")
	if got := GetAggregationSecretNamespace(); got != DefaultAggregationSecretNamespace {
		t.Errorf("unset: got %q, want %q", got, DefaultAggregationSecretNamespace)
	}
	if DefaultAggregationSecretNamespace != "cattle-system" {
		t.Errorf("DefaultAggregationSecretNamespace = %q, want cattle-system", DefaultAggregationSecretNamespace)
	}
	t.Setenv("AGGREGATION_SECRET_NAMESPACE", "custom-ns")
	if got := GetAggregationSecretNamespace(); got != "custom-ns" {
		t.Errorf("set: got %q, want custom-ns", got)
	}
}
