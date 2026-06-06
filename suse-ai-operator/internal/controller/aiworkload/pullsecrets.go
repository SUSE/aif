package aiworkload

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiplatformv1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
)

// reconcilePullSecrets ensures every named pull-secret is merged into every
// ServiceAccount in the workload's target namespace on the operator's own
// cluster. Returns settled=true when no SA needed patching this round.
// The caller decides whether to RequeueAfter.
func (r *AIWorkloadReconciler) reconcilePullSecrets(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	secretNames []string,
) (settled bool, err error) {
	l := log.FromContext(ctx)

	if w.Spec.TargetNamespace == "" || len(secretNames) == 0 {
		return true, nil
	}

	var sas corev1.ServiceAccountList
	if err := r.List(ctx, &sas, client.InNamespace(w.Spec.TargetNamespace)); err != nil {
		return false, fmt.Errorf("list ServiceAccounts in %s: %w", w.Spec.TargetNamespace, err)
	}

	settled = true
	for i := range sas.Items {
		sa := &sas.Items[i]
		if mergeImagePullSecrets(sa, secretNames) {
			if err := r.Update(ctx, sa); err != nil {
				return false, fmt.Errorf("update SA %s/%s: %w", sa.Namespace, sa.Name, err)
			}
			l.Info("merged pull secrets into ServiceAccount",
				"namespace", sa.Namespace, "name", sa.Name, "secrets", secretNames)
			settled = false
		}
	}

	// After SA mutations, bounce any pod stuck in ImagePullBackOff so the
	// kubelet re-reads the SA's imagePullSecrets at admission time.
	bounced, err := r.restartImagePullBackOffPods(ctx, w.Spec.TargetNamespace)
	if err != nil {
		return false, err
	}
	if bounced > 0 {
		settled = false
	}

	return settled, nil
}

// mergeImagePullSecrets adds each name to sa.ImagePullSecrets if not already
// present. Returns true if the SA was mutated. Order: existing entries first
// (preserved verbatim), then any new names in input order; duplicates in the
// input list are added once.
func mergeImagePullSecrets(sa *corev1.ServiceAccount, names []string) bool {
	have := make(map[string]struct{}, len(sa.ImagePullSecrets))
	for _, ref := range sa.ImagePullSecrets {
		have[ref.Name] = struct{}{}
	}
	mutated := false
	for _, name := range names {
		if _, ok := have[name]; ok {
			continue
		}
		sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		have[name] = struct{}{}
		mutated = true
	}
	return mutated
}

// restartImagePullBackOffPods deletes pods in `namespace` whose container
// statuses report ImagePullBackOff or ErrImagePull. The pod's controller
// (Deployment, StatefulSet, ReplicaSet, DaemonSet, Job) recreates it; the
// recreated pod picks up its ServiceAccount's current .imagePullSecrets at
// admission time. Returns the count of pods deleted.
func (r *AIWorkloadReconciler) restartImagePullBackOffPods(ctx context.Context, namespace string) (int, error) {
	l := log.FromContext(ctx)

	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(namespace)); err != nil {
		return 0, fmt.Errorf("list pods in %s: %w", namespace, err)
	}

	bounced := 0
	for i := range pods.Items {
		p := &pods.Items[i]
		if !isPodImagePullBackOff(p) {
			continue
		}
		if err := r.Delete(ctx, p); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue
			}
			return bounced, fmt.Errorf("delete pod %s/%s: %w", p.Namespace, p.Name, err)
		}
		l.Info("bounced ImagePullBackOff pod", "namespace", p.Namespace, "name", p.Name)
		bounced++
	}
	return bounced, nil
}

func isPodImagePullBackOff(p *corev1.Pod) bool {
	for _, cs := range p.Status.InitContainerStatuses {
		if waitingIsImagePullFailure(cs.State.Waiting) {
			return true
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if waitingIsImagePullFailure(cs.State.Waiting) {
			return true
		}
	}
	return false
}

func waitingIsImagePullFailure(w *corev1.ContainerStateWaiting) bool {
	if w == nil {
		return false
	}
	switch w.Reason {
	case "ImagePullBackOff", "ErrImagePull":
		return true
	}
	return false
}

// mergePullSecretNames adds each name from add to existing if not already
// present. Used to accumulate secret names from per-component injector runs
// onto AIWorkload.Status.PullSecretNames.
func mergePullSecretNames(existing, add []string) []string {
	if len(add) == 0 {
		return existing
	}
	have := make(map[string]struct{}, len(existing))
	for _, n := range existing {
		have[n] = struct{}{}
	}
	out := existing
	for _, n := range add {
		if _, ok := have[n]; ok {
			continue
		}
		out = append(out, n)
		have[n] = struct{}{}
	}
	return out
}
