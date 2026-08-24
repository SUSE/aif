# SUSE AI Operator

Helm chart to deploy the SUSE AI Operator on Kubernetes.

The SUSE AI Operator manages the lifecycle of the AI extension in a Rancher-managed cluster using the `InstallAIExtension` custom resource.
It integrates with Rancher catalogs and UI plugins to enable declarative installation and management of the AI extension.

**Homepage:** <https://github.com/SUSE/aif/aif-operator>

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

This chart ships CRDs under the standard Helm `crds/` directory and additionally
upgrades them via a `pre-install`/`pre-upgrade` hook Job using server-side apply
(field manager `aif-operator-crds`). The two paths are complementary, not
competing:

| Phase | Who writes the CRDs |
|---|---|
| `helm install` | Helm's native `crds/` path, then the hook Job re-applies and takes field ownership |
| `helm upgrade` | The hook Job only — Helm never touches `crds/` after install (Helm 3 **and** Helm 4) |
| `helm uninstall` | Nobody touches the CRDs — they (and any custom resources you keep) are left intact. See the note below on which custom resources persist |

**Why both paths are needed**
- Helm installs `crds/` *before* it builds the release manifest. This chart's own
  `Blueprint`, `Settings`, and `InstallAIExtension` resources are release objects,
  so their kinds must already be registered at that point. Without `crds/`, a
  fresh `helm install` fails with `no matches for kind "Blueprint"` before any
  pre-install hook has a chance to run.
- Helm never upgrades CRDs from `crds/` — that is unchanged in Helm 4 — so the
  hook Job carries schema changes forward on every `helm upgrade`.

**How It Works**
- The hook Job (`crds.manageWithJob`, enabled by default) server-side-applies the
  chart's CRDs before the release's resources are created. No manual step needed.
- On install the Job re-applies what Helm just created; `--force-conflicts`
  transfers field ownership to `aif-operator-crds` once. Helm does not write the
  CRDs again, so the two managers never contend on subsequent upgrades.
- The Job only applies CRDs — it never deletes them — and CRDs from `crds/` are
  not release-manifest objects, so `helm uninstall` always leaves the CRDs
  themselves intact.
- Custom *resources* follow their own policy on uninstall, not the CRDs':
  - `Settings` carries `helm.sh/resource-policy: keep`, so it survives uninstall.
  - The bundled `Blueprint` defaults are ordinary release objects and are
    removed on uninstall (set `defaultBlueprints.enabled=false` to manage them
    yourself). Any Blueprints you create outside the chart are unaffected.
  - The `InstallAIExtension` is deliberately removed on uninstall by the
    `pre-delete` cleanup hook, which deletes the CR so the operator's finalizer
    can tear the extension down before the operator itself goes away.

The hook Job's ClusterRole is least-privilege: `create`/`list` on
`customresourcedefinitions` cluster-wide (unavoidable), and `get`/`update`/`patch`
scoped via `resourceNames` to exactly this chart's five CRDs — so the hook can
never mutate unrelated CRDs. It has no `delete`.

When introducing a brand-new CRD, do not add custom resources of that kind to
the same chart release. On upgrade, Helm resolves the target manifest before its
`pre-upgrade` hooks run, so those resources fail discovery before the CRD Job can
install the new kind. Stage such resources in a later release or apply them
out-of-band after the CRD upgrade.

**Restricted environments**
If the installer may not create cluster-scoped RBAC, you have two options:

1. **Pre-provision the RBAC out-of-band.** A cluster admin creates the
   ServiceAccount + ClusterRole + ClusterRoleBinding once (see
   `templates/crds/crd-apply-rbac.yaml` for the exact rules), then install with:

   ```bash
   helm install ... \
     --set crds.rbac.create=false \
     --set crds.serviceAccountName=<pre-created-sa>
   ```

   The pre-created ServiceAccount must live in the release namespace. The Job still
   applies CRDs on every install/upgrade — no manual CRD step needed.

2. **Disable the Job** with `crds.manageWithJob=false`. CRDs are still installed
   natively from `crds/` on first install, and no `kubectl` image or cluster-scoped
   hook RBAC is required. **Note:** in this mode CRD *schema upgrades* are your
   responsibility — apply them out-of-band before each `helm upgrade`:

   ```bash
   kubectl apply --server-side -f crds/
   ```

**Air-gapped clusters**
The hook Job needs the `kubectl` image (`crds.image.*`, default
`registry.suse.com/suse/kubectl`); the same image is used by the `pre-delete`
extension-cleanup Job, so it must be mirrored regardless. Mirror it and override
`crds.image.registry`, or set the chart-wide `global.imageRegistry`. Sites that
cannot mirror it can install with `crds.manageWithJob=false` (see above) — the
native `crds/` path needs no image at all. CRDs themselves ship inside the chart;
nothing is fetched at apply time.

**GitOps (Argo CD / Flux)**
CRDs are delivered by `crds/` plus a Helm hook, so they are **not** rendered as
release objects: they do not appear in `helm diff`, and Argo CD/Flux track only
the hook, not the CRD schema. Two consequences to plan for:
- Argo CD runs the hook as a PreSync job; if you see it stall, it is usually the
  known hook SA/ClusterRole teardown ordering issue — the CRDs still apply.
- To review CRD schema changes in your GitOps pipeline, manage the manifests under
  `crds/` in a **separate** Argo Application / Flux Kustomization with
  server-side apply (and pruning disabled), and install this chart with
  `crds.manageWithJob=false`.

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
 `kubectl delete crd installaiextensions.ai-factory.suse.com`

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

### Default blueprints

When `defaultBlueprints.enabled=true`, the chart renders the curated `Blueprint` CRs bundled under `files/blueprints/` so they appear on the Blueprints page immediately after install — no git connectivity required.

| Name                         | Description                                         | Default |
| ---------------------------- | --------------------------------------------------- | ------- |
| `defaultBlueprints.enabled`  | Create the bundled default `Blueprint` CRs on install | `true`  |

The defaults are Helm-managed: `helm upgrade` reconciles them to the chart's current set and `helm uninstall` removes them. Each rendered Blueprint carries the marker label `ai-factory.suse.com/source: bundled`. Set `defaultBlueprints.enabled=false` to manage blueprints exclusively by other means.

## Bundled blueprints

The chart ships a curated set of `Blueprint` CRs as plain YAML data files. A single template (`templates/default-blueprints.yaml`) discovers every file via `.Files.Glob`, injects the `ai-factory.suse.com/source: bundled` marker label, and renders each one — all gated by `defaultBlueprints.enabled`.

### Adding a blueprint

1. Create one YAML file per blueprint **version** under `charts/aif-operator/files/blueprints/`. Adding a file is the only step — no template edits are needed.

2. Each file must be a complete, single-document `Blueprint` CR. Set `metadata.name` and the two grouping labels following the operator's naming convention:

   - **slug** = `spec.displayName` lowercased, with each run of non-`[a-z0-9]` characters replaced by a single `-`, trimmed of leading/trailing `-`.
   - `metadata.name` = `<slug>-<version>` with any `+build` metadata stripped and dots replaced by hyphens (e.g. *RAG Chatbot* `1.2.0` → `rag-chatbot-1-2-0`).
   - label `ai-factory.suse.com/blueprint-name` = `<slug>`
   - label `ai-factory.suse.com/blueprint-version` = the full `spec.version` (keeps dots).

   The UI groups versions that share the `blueprint-name` label into one card with a version selector. Do **not** set the `ai-factory.suse.com/source` label — the chart injects it.

   Example (`files/blueprints/rag-chatbot-1.1.0.yaml`):

   ```yaml
   apiVersion: ai-factory.suse.com/v1alpha1
   kind: Blueprint
   metadata:
     name: rag-chatbot-1-1-0
     labels:
       ai-factory.suse.com/blueprint-name: rag-chatbot
       ai-factory.suse.com/blueprint-version: 1.1.0
   spec:
     displayName: RAG Chatbot
     version: 1.1.0
     description: Retrieval-augmented chatbot stack.
     components:
       - chartRepo: my-repo
         chartName: my-chart
         chartVersion: 1.0.0
   ```

### Validating

Run both checks from the repository root before committing. They are offline (`helm` + `yq` only — no cluster needed):

```bash
# Naming convention (metadata.name + grouping labels) and name uniqueness
bash charts/aif-operator/tests/default-blueprints-convention.sh

# Renders one CR per file, toggle on/off works, source=bundled label injected
bash charts/aif-operator/tests/default-blueprints-render.sh
```

Both print `PASS` on success. The convention check enforces the rules above; the render check confirms the files render and carry the marker label when `defaultBlueprints.enabled=true` (and nothing when `false`).

For full CRD schema conformance (required fields, the semver pattern, `components` having at least one entry with non-empty `chartRepo`/`chartName`/`chartVersion`), validate against a cluster that has the Blueprint CRD installed:

```bash
helm template t charts/aif-operator --set defaultBlueprints.enabled=true \
  | yq 'select(.kind == "Blueprint")' \
  | kubectl apply --dry-run=server -f -
```

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
kubectl get crd installaiextensions.ai-factory.suse.com
```
* Re-apply CRDs manually if required
