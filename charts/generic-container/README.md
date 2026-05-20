# Generic Container Helm Chart

Deploys a single-container workload with optional GPU support. Used by the AIF operator as a template for user-defined container workloads.

## GPU Support

Set `gpu.enabled=true` to request GPU resources. The chart injects `gpu.type` (default `nvidia.com/gpu`) into `resources.limits` with the specified `gpu.count`.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `global.imageRegistry` | string | `""` | Global image registry override |
| `global.imagePullSecrets` | list | `[]` | Global image pull secrets |
| `image.registry` | string | `""` | Image registry (overridden by global.imageRegistry) |
| `image.repository` | string | `nginx` | Image repository |
| `image.tag` | string | `latest` | Image tag (defaults to appVersion) |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `replicaCount` | integer | `1` | Number of replicas |
| `service.port` | integer | `80` | Service port |
| `service.type` | string | `ClusterIP` | Kubernetes Service type |
| `gpu.enabled` | boolean | `false` | Enable GPU resource requests |
| `gpu.count` | string | `"1"` | Number of GPUs to request |
| `gpu.type` | string | `nvidia.com/gpu` | GPU resource type |
| `resources.requests.cpu` | string | `100m` | CPU request |
| `resources.requests.memory` | string | `128Mi` | Memory request |
| `resources.limits.cpu` | string | `500m` | CPU limit |
| `resources.limits.memory` | string | `256Mi` | Memory limit |
| `podSecurityContext.runAsNonRoot` | boolean | `true` | Run as non-root |
| `podSecurityContext.runAsUser` | integer | `1000` | Container UID |
| `podSecurityContext.seccompProfile.type` | string | `RuntimeDefault` | Seccomp profile type |
| `containerSecurityContext.allowPrivilegeEscalation` | boolean | `false` | Disallow privilege escalation |
| `containerSecurityContext.capabilities.drop` | list | `[ALL]` | Linux capabilities to drop |
| `containerSecurityContext.readOnlyRootFilesystem` | boolean | `false` | Mount root filesystem as read-only |
| `nodeSelector` | object | `{}` | Node selector labels |
| `tolerations` | list | `[]` | Pod tolerations |
| `affinity` | object | `{}` | Pod affinity and anti-affinity rules |
| `podAnnotations` | object | `{}` | Additional annotations for pods |
| `extraVolumes` | list | `[]` | Additional volumes to add to the pod |
| `extraVolumeMounts` | list | `[]` | Additional volume mounts to add to the container |
| `priorityClassName` | string | `""` | PriorityClass name for scheduling priority |
| `livenessProbe` | object | `{}` | Kubernetes liveness probe spec |
| `readinessProbe` | object | `{}` | Kubernetes readiness probe spec |
| `env` | list | `[]` | Environment variables |
| `ports` | list | `[]` | Additional container ports |
| `imagePullSecrets` | list | `[]` | Image pull secrets for private registries |

## Labels

All resources include the standard `app.kubernetes.io/component: workload` label for filtering.
