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
