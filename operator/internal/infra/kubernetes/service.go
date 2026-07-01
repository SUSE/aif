package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ServiceForHelmRelease(
	ctx context.Context,
	c client.Client,
	namespace, releaseName string,
) (*corev1.Service, error) {

	var list corev1.ServiceList
	if err := c.List(
		ctx,
		&list,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"app.kubernetes.io/instance": releaseName,
		},
	); err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no service found for release %q", releaseName)
	}

	return &list.Items[0], nil
}
