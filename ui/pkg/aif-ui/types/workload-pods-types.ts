// Pod-level status surfaced in the AIWorkloads view while a workload isn't Running.

export interface PodContainerIssue {
  container: string;   // container name
  reason:    string;   // e.g. "ImagePullBackOff", "CrashLoopBackOff", "ErrImagePull"
  message?:  string;   // detail from the waiting/terminated state
  init:      boolean;  // true if from initContainerStatuses
}

export interface WorkloadPod {
  name:   string;
  phase:  string;              // pod status.phase
  issues: PodContainerIssue[];
}

export interface ClusterPodStatus {
  clusterId:    string;
  unavailable:  boolean;       // cluster unreachable / RBAC-denied
  pods:         WorkloadPod[]; // unhealthy pods attributed via the instance label
  unattributed: WorkloadPod[]; // fallback: label-less Helm pods (only when pods is empty for this cluster)
}

// keyed by `${namespace}/${name}` of the workload
export type WorkloadPodStatusMap = Record<string, ClusterPodStatus[]>;
