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

package installaiextension

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

func ServiceEndpoint(svc *corev1.Service) (name, namespace string, port int32, error error) {
	if svc == nil {
		return "", "", 0, fmt.Errorf("service is nil")
	}

	if len(svc.Spec.Ports) == 0 {
		return "", "", 0, fmt.Errorf("service %s has no ports", svc.Name)
	}

	return svc.Name, svc.Namespace, svc.Spec.Ports[0].Port, nil
}
