# Private sources for the AIF operator and Rancher extension

## Scope

This proposal covers only the part of an air-gapped installation owned by the
AIF operator and Rancher extension:

- private Helm/OCI chart repositories;
- private HTTPS Git used for Blueprint input and GitOps output;
- repository authentication and private CA trust; and
- catalog UI behavior that does not require browser access to public assets.

Node image mirrors, registry fallback policy, host proxy configuration, DNS,
operating-system repositories, and the complete mirrored artifact inventory
belong to the cluster or installation environment. AIF documents those as
prerequisites but does not configure them.

## Chart sources

A Blueprint continues to identify a chart using `chartRepo`, `chartName`, and
`chartVersion`. `chartRepo` is a stable Rancher `ClusterRepo` resource name, not
an endpoint URL:

```yaml
components:
  - chartRepo: application-collection
    chartName: ollama
    chartVersion: 1.55.0
```

The Settings controller owns the endpoint indirection. In a connected
installation, the stable `ClusterRepo` points to the public source. In a private
installation, the same resource name points to the configured mirror:

```yaml
spec:
  registryEndpoints:
    applicationCollection: oci://harbor.internal/charts/application-collection
    suseRegistry: oci://harbor.internal/charts/suse-ai
    nvidia: oci://harbor.internal/charts/nvidia
```

The mirror must preserve the chart names and versions referenced by supported
Blueprints. Authentication and CA references remain in the corresponding
Application Collection, SUSE Registry, or NVIDIA Settings section. Changes to a
`ClusterRepo`, including a git-backed repository's indexed commit, requeue
chart-sourced workloads. Blueprint deployments rebuild their chart reference;
App workloads refresh the pull secrets managed by the operator.

The extension also classifies the well-known sources by their stable
`ClusterRepo` names rather than by public URL patterns. This keeps vendor-specific
pull-secret behavior intact after an endpoint moves to a private mirror, and
credentials are written only for the host of the effective `ClusterRepo` URL.

## Blueprint and GitOps Git source

One Fleet repository has two reserved paths:

- `blueprints/` contains administrator-managed Blueprint resources consumed by
  Fleet on the management cluster.
- `workloads/` contains deployment resources written by AIF for the GitOps
  strategy.

The minimal private-Git contract is authenticated HTTPS with optional private
CA trust:

```yaml
spec:
  fleet:
    repoURL: https://gitea.internal/platform/aif.git
    branch: main
    authType: token
    username: platform
    credSecretRef:
      name: git-credentials
      key: token
    caBundleSecretRef:
      name: private-ca
      key: ca.crt
```

`authType` supports `token` and `basic`. The operator uses the same credentials
and PEM CA bundle for its Git writes and for the generated Fleet `GitRepo`.
Changing the repository URL or branch requeues existing Blueprint workloads and
republishes their GitOps manifests to the new destination.

SSH is not part of this minimal contract. Supporting it safely requires an
explicit private-key and known-hosts model; TLS or SSH host verification must
not be disabled as a shortcut.

## Container images

AIF does not rewrite image references in Blueprint values. The cluster must
provide transparent image routing, normally through RKE2/containerd registry
mirrors, and must prevent fallback to public registries when strict isolation is
required. The AIF charts expose their own image-registry overrides, but
cluster-wide mirroring and artifact population remain installation concerns.
