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
| `image.registry` | string | `ghcr.io` | Image registry hostname (with optional project prefix) |
| `image.repository` | string | `suse/aif-operator` | Image repository path |
| `image.tag` | string | `""` | Image tag (empty defaults to `.Chart.AppVersion`) |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `imagePullSecrets` | list | `[]` | Image pull secrets for private registries |
| `serviceAccount.name` | string | `""` | Service account name (empty defaults to chart fullname) |
| `replicaCount` | integer | `1` | Number of operator replicas |
| `resources.requests.cpu` | string | `100m` | CPU request |
| `resources.requests.memory` | string | `256Mi` | Memory request |
| `resources.limits.cpu` | string | `1000m` | CPU limit |
| `resources.limits.memory` | string | `512Mi` | Memory limit |
| `persistence.enabled` | boolean | `true` | Enable persistent volume |
| `persistence.size` | string | `10Gi` | Persistent volume size |
| `operator.logLevel` | string | `info` | Log level (debug, info, warn, error) |
| `operator.logFormat` | string | `json` | Log format (json, text) |
| `operator.catalogRefresh` | string | `10m` | Catalog refresh interval |
| `webhook.enabled` | boolean | `true` | Enable admission webhooks |
| `webhook.tlsMode` | string | `cert-manager` | TLS certificate mode |
