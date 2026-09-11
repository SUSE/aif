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

Settings **Test** uses that same endpoint indirection and the current form's
credential and CA references. With a private endpoint configured, direct chart
checks target only that mirror; they do not fall back to a public chart source.
Select a **Chart to test** that exists in the mirror, since a deliberately partial
mirror may not contain the default sample. A missing sample is distinct from an
access denial. NVIDIA mirror mode checks the mirror instead of the public NGC
repositories, including when the mirror is intentionally anonymous.

Direct chart access and Rancher's saved repository readiness are separate results.
Reading a sample manifest or accessing a chart file does not verify the complete
mirrored chart inventory, full downloads, or container image availability.

## Blueprint and GitOps Git source

One Fleet repository has two reserved paths:

- `blueprints/` contains administrator-managed Blueprint resources consumed by
  Fleet on the management cluster.
- `workloads/` contains deployment resources written by AIF for the GitOps
  strategy.

Saving a non-empty `repoURL` immediately creates the Fleet `GitRepo` and starts
reconciliation. The selected branch must therefore already contain both
reserved paths with Fleet-readable content; otherwise the Fleet git job remains
failed until the paths are added. A `fleet.yaml` is one way to initialize or
customize a path, but it is optional when the path already contains deployable
manifests.

The minimal private-Git contract is authenticated HTTPS with optional private
CA trust:

```yaml
spec:
  fleet:
    repoURL: https://gitea.internal/platform/aif.git
    branch: main
    username: platform
    credSecretRef:
      name: git-credentials
      key: token
    caBundleSecretRef:
      name: private-ca
      key: ca.crt
```

Git over HTTPS has one credential model: a username plus the selected Secret
key as either a password or personal access token. The username is optional and
resolves in this order: `spec.fleet.username`, the `username` key in the same
Secret, then the generic value `token`. The default supplies the non-empty Basic
Auth username needed by the Git client and Fleet; servers that validate the
account name require an explicit value or Secret key. Leave both credential
fields unset for an anonymous repository. AIF mirrors authenticated credentials
to Fleet as a `kubernetes.io/basic-auth` Secret; a personal access token is the
password and is not sent as an HTTP Bearer token. The deprecated `authType`
values `token` and `basic` remain accepted only for compatibility with existing
Settings.

The operator uses the same credentials and PEM CA bundle for its Git writes and
for the generated Fleet `GitRepo`. Changing the repository URL or branch
requeues existing Blueprint workloads and republishes their GitOps manifests to
the new destination.

SSH is not part of this minimal contract. Supporting it safely requires an
explicit private-key and known-hosts model; TLS or SSH host verification must
not be disabled as a shortcut.

## Catalog logos

The extension ships a manifest of raster logos keyed by library and chart name.
It uses inline raster logos from catalog metadata when available, with the
bundled logo and then a placeholder as fallbacks. External or relative logo URLs
do not trigger browser requests. Image decode failures also advance through the
fallbacks. Private repository URLs do not affect the lookup, and the manifest
works on the first page load without a pre-populated browser cache.

Custom applications can supply inline raster logos or add a manifest entry;
see [catalog logo maintenance](../../ui/scripts/README.md).

## Container images

AIF does not rewrite image references in Blueprint values. The cluster must
provide transparent image routing, normally through RKE2/containerd registry
mirrors, and must prevent fallback to public registries when strict isolation is
required. The AIF charts expose their own image-registry overrides, but
cluster-wide mirroring and artifact population remain installation concerns.
