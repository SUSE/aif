package kubernetes

import (
	"context"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentStatus struct {
	Ready   bool
	Message string
}

func IsDeploymentReady(
	ctx context.Context,
	c client.Client,
	namespace, releaseName string,
	log logr.Logger,
) (DeploymentStatus, error) {

	var list appsv1.DeploymentList
	if err := c.List(
		ctx,
		&list,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"app.kubernetes.io/instance": releaseName,
		},
	); err != nil {
		return DeploymentStatus{}, err
	}

	if len(list.Items) == 0 {
		return DeploymentStatus{
			Ready:   false,
			Message: "No deployments found for release " + releaseName,
		}, nil
	}

	for _, d := range list.Items {
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		if d.Status.ReadyReplicas < desired {
			log.Info("Deployment not ready",
				"deployment", d.Name,
				"readyReplicas", d.Status.ReadyReplicas,
				"desiredReplicas", desired,
			)
			return DeploymentStatus{
				Ready:   false,
				Message: "Deployment " + d.Name + " not ready",
			}, nil
		}
	}

	return DeploymentStatus{
		Ready:   true,
		Message: "All deployments ready",
	}, nil
}
