package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/SUSE/aif/internal/infra/rancher"
)

// deploymentStatus holds the readiness state and a human-readable message
// describing why the deployment is not ready (e.g. pod-level errors).
type deploymentStatus struct {
	Ready   bool
	Message string
}

// checkDeploymentReady checks if a Deployment with the given release name label
// is available. When not ready, it inspects pod conditions and container
// statuses to surface actionable detail (ImagePullBackOff, CrashLoopBackOff,
// pending scheduling, etc.).
func (r *InstallAIExtensionReconciler) checkDeploymentReady(ctx context.Context, releaseName string) (deploymentStatus, error) {
	var deploys appsv1.DeploymentList
	selector := labels.SelectorFromSet(map[string]string{
		"app.kubernetes.io/instance": releaseName,
	})
	if err := r.List(ctx, &deploys, &client.ListOptions{
		Namespace:     rancher.UIPluginNamespace,
		LabelSelector: selector,
	}); err != nil {
		return deploymentStatus{}, fmt.Errorf("list deployments: %w", err)
	}

	if len(deploys.Items) == 0 {
		return deploymentStatus{Message: "Deployment not found"}, nil
	}

	deploy := &deploys.Items[0]
	desired := int32(1)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}
	if deploy.Status.ReadyReplicas >= desired {
		return deploymentStatus{Ready: true, Message: "Deployment is available"}, nil
	}

	msg := r.diagnosePodFailures(ctx, selector)
	if msg == "" {
		msg = fmt.Sprintf("Deployment not yet ready (%d/%d replicas)", deploy.Status.ReadyReplicas, desired)
	}
	return deploymentStatus{Message: msg}, nil
}

// diagnosePodFailures inspects pods matching the selector and returns a
// message describing the first container-level or scheduling failure found.
func (r *InstallAIExtensionReconciler) diagnosePodFailures(ctx context.Context, selector labels.Selector) string {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, &client.ListOptions{
		Namespace:     rancher.UIPluginNamespace,
		LabelSelector: selector,
	}); err != nil {
		return ""
	}

	for i := range pods.Items {
		pod := &pods.Items[i]

		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				msg := fmt.Sprintf("Pod %s container %s: %s", pod.Name, cs.Name, cs.State.Waiting.Reason)
				if cs.State.Waiting.Message != "" {
					msg += " — " + cs.State.Waiting.Message
				}
				return msg
			}
			if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
				return fmt.Sprintf("Pod %s container %s: terminated with exit code %d (%s)",
					pod.Name, cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
			}
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
				return fmt.Sprintf("Pod %s: %s — %s", pod.Name, cond.Reason, cond.Message)
			}
		}
	}

	return ""
}

// discoverServiceURL finds the Service for a Helm release and returns its in-cluster URL.
func (r *InstallAIExtensionReconciler) discoverServiceURL(ctx context.Context, releaseName string) (string, error) {
	var services corev1.ServiceList
	selector := labels.SelectorFromSet(map[string]string{
		"app.kubernetes.io/instance": releaseName,
	})
	if err := r.List(ctx, &services, &client.ListOptions{
		Namespace:     rancher.UIPluginNamespace,
		LabelSelector: selector,
	}); err != nil {
		return "", fmt.Errorf("list services: %w", err)
	}

	if len(services.Items) == 0 {
		return "", fmt.Errorf("no service found for release %s", releaseName)
	}

	svc := &services.Items[0]
	if len(svc.Spec.Ports) == 0 {
		return "", fmt.Errorf("service %s/%s has no ports", svc.Namespace, svc.Name)
	}
	port := svc.Spec.Ports[0].Port

	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svc.Name, svc.Namespace, port), nil
}
