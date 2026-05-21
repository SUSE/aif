# NIM VLM Helm Chart

Deploys an NVIDIA NIM vision-language model inference service. Default model: LLaVA v1.6 Mistral 7B.

## Requirements

- NVIDIA GPU node with driver and device plugin installed
- Node labeled `nvidia.com/gpu.present=true`
- Default: 2 GPUs, 16 CPU, 64Gi memory

## Health Checks

The chart configures liveness and readiness probes against `/v1/health/ready` (port 8000).
VLM containers require 120+ seconds to load model weights before becoming ready.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `nameOverride` | string | `""` | Override the chart name |
| `fullnameOverride` | string | `""` | Override the full release name |
| `global.imageRegistry` | string | `""` | Global image registry override |
| `global.imagePullSecrets` | list | `[]` | Global image pull secrets |
| `image.registry` | string | `registry.suse.com` | Image registry (overridden by global.imageRegistry) |
| `image.repository` | string | `ai/containers/nvidia/llava-v1.6-mistral-7b` | NIM container image repository |
| `image.tag` | string | `""` | Image tag (defaults to Chart.AppVersion) |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `replicaCount` | integer | `1` | Number of replicas |
| `strategy.type` | string | `Recreate` | Deployment strategy (Recreate avoids double GPU allocation) |
| `service.port` | integer | `8000` | Service port |
| `service.type` | string | `ClusterIP` | Kubernetes Service type |
| `resources.requests.cpu` | string | `16` | CPU request |
| `resources.requests.memory` | string | `64Gi` | Memory request |
| `resources.limits.cpu` | string | `16` | CPU limit |
| `resources.limits.memory` | string | `64Gi` | Memory limit |
| `resources.limits.nvidia.com/gpu` | string | `"2"` | GPU limit |
| `tolerations` | list | GPU NoSchedule toleration | Pod tolerations |
| `nodeSelector` | object | `{nvidia.com/gpu.present: "true"}` | Node selector labels |
| `imagePullSecrets` | list | `[{name: suse-registry-creds}]` | Image pull secrets |
| `podSecurityContext.runAsNonRoot` | boolean | `true` | Run as non-root |
| `podSecurityContext.runAsUser` | integer | `1000` | Container UID; override if your mirrored NIM image sets a different non-root USER |
| `podSecurityContext.seccompProfile.type` | string | `RuntimeDefault` | Seccomp profile type |
| `containerSecurityContext.allowPrivilegeEscalation` | boolean | `false` | Disallow privilege escalation |
| `containerSecurityContext.capabilities.drop` | list | `[ALL]` | Linux capabilities to drop |
| `containerSecurityContext.readOnlyRootFilesystem` | boolean | `false` | Mount root filesystem as read-only |
| `affinity` | object | `{}` | Pod affinity and anti-affinity rules |
| `podAnnotations` | object | `{}` | Additional annotations for pods |
| `extraVolumes` | list | `[]` | Additional volumes to add to the pod |
| `extraVolumeMounts` | list | `[]` | Additional volume mounts to add to the container |
| `env` | list | `[]` | Environment variables |
| `priorityClassName` | string | `""` | PriorityClass name for GPU workload scheduling |
| `podDisruptionBudget.enabled` | boolean | `false` | Enable PodDisruptionBudget |
| `podDisruptionBudget.minAvailable` | integer | `1` | Minimum available pods during disruption |

## Labels

All resources include the standard `app.kubernetes.io/component: inference` label for filtering.
