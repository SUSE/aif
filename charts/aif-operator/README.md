# SUSE AI Operator

Helm chart to deploy the SUSE AI Operator on Kubernetes.

The SUSE AI Operator manages the lifecycle of the AI extension in a Rancher-managed cluster using the `InstallAIExtension` custom resource.
It integrates with Rancher catalogs and UI plugins to enable declarative installation and management of the AI extension.

**Homepage:** <https://github.com/SUSE/suse-ai-lifecycle-manager/aif-operator>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| SUSE LLC |  | <https://www.suse.com> |

## Prerequisites

- Kubernetes 1.24+
- Helm 3.x
- Rancher installed (for UIPlugin and ClusterRepo integration)

The following CRDs must exist before adding the operator:
  - `uiplugins.catalog.cattle.io`
  - `clusterrepos.catalog.cattle.io`

You can verify with:
```bash
kubectl get crd uiplugins.catalog.cattle.io
kubectl get crd clusterrepos.catalog.cattle.io
```

## CRD Management

This chart ships CRDs in the standard Helm crds/ directory.

**How It Works**
- CRDs are installed automatically by Helm on first install
- CRDs are not upgraded automatically on `helm upgrade` (Helm default behavior)
- CRDs must be updated manually if the schema changes
- CRDs are not deleted automatically on `helm uninstall` (Helm default behavior)

**Manual CRD Installation**
If CRDs are not installed automatically (for example, in restricted environments or using --skip-crds in helm install), you can apply them manually:

`kubectl apply -f crds/installaiextension.yaml`

## Installing the Chart

This chart is distributed as an OCI Helm chart. Install the chart with the release name `aif-operator`:

```bash
helm install aif-operator \
  -n aif-operator \
  --create-namespace \
  oci://ghcr.io/suse/chart/aif-operator
```

The command deploys the SUSE AI Operator using the default configuration. See the [Parameters](#parameters) section for configurable options.

## Uninstalling the Chart

To uninstall the operator:

```bash
helm uninstall aif-operator -n aif-operator
```

This removes all Kubernetes resources created by the chart **except CRDs**, which must be removed manually if desired.
For example:
 `kubectl delete crd installaiextensions.ai-platform.suse.com`

## Parameters

### Global parameters

| Name                      | Description                        | Value |
| ------------------------- | ---------------------------------- | ----- |
| `global.imageRegistry`    | Global override for image registry | `""`  |
| `global.imagePullSecrets` | Global image pull secrets          | `[]`  |
| `nameOverride`            | Partially override chart name      | `""`  |
| `fullnameOverride`        | Fully override resource names      | `""`  |

### Manager parameters

#### General

| Name                       | Description                       | Default              |
| -------------------------- | --------------------------------- | -------------------- |
| `manager.replicaCount`     | Number of operator replicas       | `1`                  |
| `manager.args`             | Additional command-line arguments | `["--leader-elect"]` |
| `manager.env`              | Extra environment variables       | `[]`                 |
| `manager.imagePullSecrets` | Image pull secrets                | `[]`                 |
| `manager.podAnnotations`   | Pod annotations                   | `{}`                 |

#### Image

| Name                       | Description               | Default                 |
| -------------------------- | ------------------------- | ----------------------- |
| `manager.image.registry`   | Operator image registry   | `ghcr.io`               |
| `manager.image.repository` | Operator image repository | `suse/aif-operator` |
| `manager.image.tag`        | Operator image tag        | `""`                    |
| `manager.image.pullPolicy` | Image pull policy         | `IfNotPresent`          |

#### Pod Security Context

| Name                                             | Description               | Default          |
| ------------------------------------------------ | ------------------------- | ---------------- |
| `manager.podSecurityContext.runAsNonRoot`        | Run container as non-root | `true`           |
| `manager.podSecurityContext.seccompProfile.type` | Seccomp profile type      | `RuntimeDefault` |

#### Container Security Context

| Name                                               | Description                | Default   |
| -------------------------------------------------- | -------------------------- | --------- |
| `manager.securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false`   |
| `manager.securityContext.readOnlyRootFilesystem`   | Read-only root filesystem  | `true`    |
| `manager.securityContext.capabilities.drop`        | Linux capabilities to drop | `["ALL"]` |

#### Resources

| Name                                | Description    | Default |
| ----------------------------------- | -------------- | ------- |
| `manager.resources.requests.cpu`    | CPU request    | `10m`   |
| `manager.resources.requests.memory` | Memory request | `64Mi`  |
| `manager.resources.limits.cpu`      | CPU limit      | `500m`  |
| `manager.resources.limits.memory`   | Memory limit   | `128Mi` |

#### Probes

| Name                                          | Description           | Default    |
| --------------------------------------------- | --------------------- | ---------- |
| `manager.probes.liveness.enabled`             | Enable liveness probe | `true`     |
| `manager.probes.liveness.httpGet.path`        | Liveness probe path   | `/healthz` |
| `manager.probes.liveness.httpGet.port`        | Liveness probe port   | `8081`     |
| `manager.probes.liveness.periodSeconds`       | Probe period          | `20`       |
| `manager.probes.liveness.initialDelaySeconds` | Initial delay         | `15`       |
| `manager.probes.readiness.enabled`             | Enable readiness probe | `true`    |
| `manager.probes.readiness.httpGet.path`        | Readiness probe path   | `/readyz` |
| `manager.probes.readiness.httpGet.port`        | Readiness probe port   | `8081`    |
| `manager.probes.readiness.periodSeconds`       | Probe period           | `10`      |
| `manager.probes.readiness.initialDelaySeconds` | Initial delay          | `5`       |

#### Scheduling

| Name                   | Description        | Default |
| ---------------------- | ------------------ | ------- |
| `manager.nodeSelector` | Node selector      | `{}`    |
| `manager.tolerations`  | Pod tolerations    | `[]`    |
| `manager.affinity`     | Pod affinity rules | `{}`    |

### Metrics parameters

| Name             | Description             | Default |
| ---------------- | ----------------------- | ------- |
| `metrics.enable` | Enable metrics endpoint | `true`  |
| `metrics.port`   | Metrics HTTPS port      | `8443`  |

> When enabled, a metrics Service and RBAC rules are created to support authenticated scraping.

### AI Extension bundling

When `aiExtension.enabled=true`, the chart creates an `InstallAIExtension` CR that the operator reconciles to install the UI extension automatically.

| Name                                           | Description                                | Default                                  |
| ---------------------------------------------- | ------------------------------------------ | ---------------------------------------- |
| `aiExtension.enabled`                          | Create InstallAIExtension CR on install    | `true`                                   |
| `aiExtension.source.kind`                      | Source type                                | `Helm`                                   |
| `aiExtension.source.helm.chartURL`             | Helm chart URL (OCI or HTTPS)              | `oci://ghcr.io/suse/chart/aif-ui`       |
| `aiExtension.source.helm.version`              | Helm chart version                         | `0.1.0`                                  |
| `aiExtension.extension.name`                   | Extension name (UIPlugin name)             | `aif-ui`                                 |
| `aiExtension.extension.version`                | Extension version                          | `0.1.0`                                  |
| `aiExtension.cleanup.image.registry`           | kubectl image registry for cleanup job     | `registry.suse.com`                      |
| `aiExtension.cleanup.image.repository`         | kubectl image repository                   | `suse/kubectl`                           |
| `aiExtension.cleanup.image.tag`                | kubectl image tag                          | `1.35`                                   |

#### Source types

**Helm** (`aiExtension.source.kind=Helm`): The operator installs a Helm chart that deploys a container serving extension assets. It then creates a ClusterRepo pointing to the in-cluster Service and a UIPlugin CR for Rancher to load the extension.

### RBAC helper roles

| Name                 | Description                                      | Default |
| -------------------- | ------------------------------------------------ | ------- |
| `rbacHelpers.enable` | Create helper ClusterRoles (admin/editor/viewer) | `false` |

## Troubleshooting

### Check pod status

```bash
kubectl get pods -l app.kubernetes.io/name=aif-operator -n aif-operator
```

### Check logs

```bash
kubectl logs deploy/aif-operator -n aif-operator -f
```

### Metrics endpoint not reachable

* Ensure `metrics.enable=true`
* Verify the metrics Service exists:
``` bash
kubectl get svc -n aif-operator
```
* Confirm RBAC permissions allow access to `/metrics`

### CRD not found errors

* Ensure the CRD exists:
``` bash
kubectl get crd installaiextensions.ai-platform.suse.com
```
* Re-apply CRDs manually if required
