# AIF Operator Helm Chart

This chart deploys the SUSE AI Factory Operator, which manages AI workload lifecycles and integrates with Rancher AI.

## Air-Gap Install Modes

The chart supports three image registry modes:

| Mode | `--set` flags | Resulting image reference | Notes |
|------|---------------|---------------------------|-------|
| Connected (default) | None | `ghcr.io/suse/aif-operator:<tag>` | Uses default public registry |
| Air-gap hostname-only | `--set image.registry=harbor.example.com` | `harbor.example.com/suse/aif-operator:<tag>` | Registry hostname without project prefix |
| Air-gap with project | `--set image.registry=harbor.example.com/suse --set image.repository=aif-operator` | `harbor.example.com/suse/aif-operator:<tag>` | Full registry path with project/namespace (path-collapse pattern: also override repository to match mirrored path) |
| No registry (fallback) | `--set image.registry=''` | `suse/aif-operator:<tag>` | Falls back to Docker Hub convention |

## Example Air-Gap Install

```bash
# When images are mirrored to harbor.example.com/suse/aif-operator:<tag>
# (mirror.sh replaces ghcr.io with harbor.example.com/suse and strips the suse/ prefix)
helm install aif-operator charts/aif-operator \
  --set image.registry=harbor.example.com/suse \
  --set image.repository=aif-operator \
  --set 'imagePullSecrets[0].name=harbor-pull-secret'
```

This produces a Deployment with:
- Container image: `harbor.example.com/suse/aif-operator:<tag>`
- ImagePullSecrets: Reference to the `harbor-pull-secret` Secret

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `global.imageRegistry` | string | `""` | Global image registry override |
| `global.imagePullSecrets` | list | `[]` | Global image pull secrets |
| `image.registry` | string | `ghcr.io` | Image registry hostname (with optional project prefix) |
| `image.repository` | string | `suse/aif-operator` | Image repository path |
| `image.tag` | string | `""` | Image tag (empty defaults to `.Chart.AppVersion`) |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `imagePullSecrets` | list | `[]` | Image pull secrets for private registries |
| `serviceAccount.name` | string | `""` | Service account name (empty defaults to chart fullname) |
| `replicaCount` | integer | `1` | Number of operator replicas |
| `service.type` | string | `ClusterIP` | Kubernetes Service type |
| `service.ports.api` | integer | `8080` | API server port |
| `service.ports.health` | integer | `8081` | Health check port |
| `service.ports.metrics` | integer | `8082` | Metrics port (conditional on `metrics.enabled`) |
| `service.ports.webhook` | integer | `9443` | Webhook server port (conditional on `webhook.enabled`) |
| `resources.requests.cpu` | string | `100m` | CPU request |
| `resources.requests.memory` | string | `256Mi` | Memory request |
| `resources.limits.cpu` | string | `1000m` | CPU limit |
| `resources.limits.memory` | string | `512Mi` | Memory limit |
| `persistence.enabled` | boolean | `true` | Enable persistent volume |
| `persistence.size` | string | `10Gi` | Persistent volume size |
| `persistence.storageClass` | string | `""` | Storage class (empty uses cluster default) |
| `persistence.accessMode` | string | `ReadWriteOnce` | PVC access mode |
| `persistence.mountPath` | string | `/data` | Mount path for persistent volume |
| `podSecurityContext.runAsNonRoot` | boolean | `true` | Run as non-root |
| `podSecurityContext.runAsUser` | integer | `1000` | Container UID |
| `podSecurityContext.seccompProfile.type` | string | `RuntimeDefault` | Seccomp profile type |
| `containerSecurityContext.allowPrivilegeEscalation` | boolean | `false` | Disallow privilege escalation |
| `containerSecurityContext.capabilities.drop` | list | `[ALL]` | Linux capabilities to drop |
| `containerSecurityContext.readOnlyRootFilesystem` | boolean | `true` | Read-only root filesystem |
| `podAnnotations` | object | `{}` | Additional annotations for pods |
| `extraEmptyDirs` | list | `[/data/charts, /data/git, /tmp]` | Additional emptyDir volume mounts |
| `operator.logLevel` | string | `info` | Log level (debug, info, warn, error) |
| `operator.logFormat` | string | `json` | Log format (json, text) |
| `operator.catalogRefresh` | string | `10m` | Catalog refresh interval |
| `metrics.enabled` | boolean | `true` | Enable metrics port |
| `webhook.enabled` | boolean | `true` | Enable admission webhooks |
| `webhook.tlsMode` | string | `cert-manager` | TLS certificate mode |
| `webhook.certManager.enabled` | boolean | `true` | Enable cert-manager for webhook TLS |
| `nodeSelector` | object | `{}` | Node selector labels |
| `tolerations` | list | `[]` | Pod tolerations |
| `affinity` | object | `{}` | Pod affinity and anti-affinity rules |
| `podDisruptionBudget.enabled` | boolean | `false` | Enable PodDisruptionBudget |
| `podDisruptionBudget.minAvailable` | integer | `1` | Minimum available pods during disruption |

> **Note:** NetworkPolicy support (P7-3) and additional webhook TLS modes (P7-4) are deferred and will be added post-MVP.
