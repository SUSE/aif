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

const DefaultWorkloadNamespace = "aif-workloads"

// GetWorkloadNamespace returns the namespace on the control cluster where
// AIWorkload CRs are stored. It is intentionally distinct from the operator
// release namespace (GetOperatorNamespace) so control-plane config resources and
// workload records keep independent RBAC and quota boundaries. The deployment
// target namespace of a workload is carried separately by Spec.TargetNamespace.
func GetWorkloadNamespace() string {
	if ns := os.Getenv("WORKLOAD_NAMESPACE"); ns != "" {
		return ns
	}
	return DefaultWorkloadNamespace
}

const DefaultOperatorService = "aif-operator"

func GetOperatorService() string {
	if svc := os.Getenv("OPERATOR_SERVICE"); svc != "" {
		return svc
	}
	return DefaultOperatorService
}
