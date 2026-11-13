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

import "os"

const DefaultExtensionNamespace = "cattle-ui-plugin-system"

func GetExtensionNamespace() string {
	if ns := os.Getenv("EXTENSION_NAMESPACE"); ns != "" {
		return ns
	}
	return DefaultExtensionNamespace
}

const DefaultOperatorNamespace = "aif-operator"

func GetOperatorNamespace() string {
	if ns := os.Getenv("OPERATOR_NAMESPACE"); ns != "" {
		return ns
	}
	return DefaultOperatorNamespace
}

// GetAggregationSecretName returns the name of the Secret Rancher's APIService
// controller writes the aggregation tunnel connection (url, token, ca.crt)
// into. Empty disables the operator API; there is no default.
func GetAggregationSecretName() string {
	return os.Getenv("AGGREGATION_SECRET_NAME")
}

// DefaultAggregationSecretNamespace matches the chart's
// aggregation.secretNamespace default.
const DefaultAggregationSecretNamespace = "cattle-system"

// GetAggregationSecretNamespace returns the namespace of the aggregation
// Secret. It must not be the operator namespace: the Settings API's
// validate-credentials overrides resolve Secret refs in the operator
// namespace and send their values as basic auth to a caller-chosen URL, so a
// tunnel token stored there could be sent out.
func GetAggregationSecretNamespace() string {
	if ns := os.Getenv("AGGREGATION_SECRET_NAMESPACE"); ns != "" {
		return ns
	}
	return DefaultAggregationSecretNamespace
}
