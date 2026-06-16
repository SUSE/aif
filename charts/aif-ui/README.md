# AIF UI Helm Chart

Deploys the SUSE AI Factory UI extension as a container-based Rancher Dashboard extension.

The chart creates a Deployment and Service that serve the built extension assets (including `index.yaml`). The InstallAIExtension controller then creates a ClusterRepo pointing to the Service and a UIPlugin referencing the chart metadata.

## Prerequisites

- Rancher 2.10+ with UI Extensions support (`catalog.cattle.io/v1` API)
- Target namespace: `cattle-ui-plugin-system`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `replicaCount` | int | `1` | Number of replicas |
| `image.registry` | string | `ghcr.io` | Container image registry |
| `image.repository` | string | `suse/aif-ui` | Container image repository |
| `image.tag` | string | `""` (uses `appVersion`) | Container image tag |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `global.imageRegistry` | string | `""` | Global registry override (air-gap) |
| `global.imagePullSecrets` | list | `[]` | Global image pull secrets |
| `imagePullSecrets` | list | `[]` | Image pull secrets |
| `nameOverride` | string | `""` | Override chart name |
| `fullnameOverride` | string | `""` | Override full release name |
| `service.type` | string | `ClusterIP` | Service type |
| `service.port` | int | `8080` | Service port |
| `podAnnotations` | object | `{}` | Pod annotations |
| `podLabels` | object | `{}` | Additional pod labels |
| `podSecurityContext` | object | See `values.yaml` | Pod-level security context |
| `containerSecurityContext` | object | See `values.yaml` | Container-level security context |
| `resources` | object | See `values.yaml` | Container resource requests/limits |
| `probes.liveness.enabled` | bool | `true` | Enable liveness probe |
| `probes.liveness.initialDelaySeconds` | int | `10` | Liveness probe initial delay |
| `probes.readiness.enabled` | bool | `true` | Enable readiness probe |
| `probes.readiness.initialDelaySeconds` | int | `5` | Readiness probe initial delay |
| `nodeSelector` | object | `{}` | Node selector |
| `tolerations` | list | `[]` | Tolerations |
| `affinity` | object | `{}` | Affinity rules |
| `rollingUpdate.maxSurge` | string | `25%` | Rolling update max surge |
| `rollingUpdate.maxUnavailable` | string | `25%` | Rolling update max unavailable |
| `operator.namespace` | string | `aif-operator` | Namespace where the SUSE AI operator is installed. Written to the `aif-ui-config` ConfigMap and read by the UI extension at runtime to build the operator API URL. |
| `operator.service` | string | `aif-operator` | Service name of the SUSE AI operator. |
