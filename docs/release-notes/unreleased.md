# Unreleased

## Operator API through Rancher

The operator API is now served through Rancher, at
`/v1/aif-operator/api/v1/...`. The chart registers it with a Rancher
`management.cattle.io/v3` `APIService`, and the operator opens an outbound
tunnel to Rancher. The operator no longer listens on port 8080, and the
`aif-operator` Service is removed. The Kubernetes service proxy URL
(`/api/v1/namespaces/<namespace>/services/http:aif-operator:8080/proxy/...`)
no longer works; integrators calling it must switch to `/v1/aif-operator`.

- **Rancher authenticates every request.** The operator takes the user from the
  identity Rancher forwards and returns HTTP 401 when there is none. Each route
  is checked with a Kubernetes SubjectAccessReview against the AI Factory
  objects, and returns HTTP 403 when the user lacks the permission:

  | Route | Required permission (`ai-factory.suse.com`) |
  | --- | --- |
  | `GET /api/v1/version`, `GET /api/v1/catalog` | `get` `settings` in the operator namespace |
  | `GET /api/v1/settings`, `GET /api/v1/settings/registry-status` | `get` `settings` in the operator namespace |
  | `PUT /api/v1/settings` | `update` `settings` in the operator namespace |
  | `POST /api/v1/settings/validate-credentials`, `POST /api/v1/settings/validate-chart-access`, `POST /api/v1/git/publish` | `update` `settings` in the operator namespace |
  | `GET /api/v1/blueprint-catalogs` | `list` `blueprintcatalogs` |
  | `GET /api/v1/blueprints` | `list` `blueprints` |
  | `GET /api/v1/blueprints/{name}` | `get` that `blueprint` |
  | `POST /api/v1/blueprints` | `create` `blueprints` |
  | `PUT`, `DELETE` on `/api/v1/blueprints/{name}` | `update`, `delete` that `blueprint` |
  | `GET /api/v1/aiworkloads` | `list` `aiworkloads` in all namespaces |
  | `POST /api/v1/namespaces/{namespace}/aiworkloads` | `create` `aiworkloads` in that namespace |
  | `DELETE` on `/api/v1/namespaces/{namespace}/aiworkloads/{name}` | `delete` that `aiworkload` |
  | `PATCH` on `/api/v1/namespaces/{namespace}/aiworkloads/{name}`; `POST .../aiworkloads/{name}/upgrade`, `/rollback`, `/retry` | `update` that `aiworkload` |

  Every modification of an existing object (saving Settings, editing a
  Blueprint or workload, and the workload upgrade, rollback and retry actions)
  needs `update`. Rancher global admins and owners of the cluster that runs the
  operator hold all of these permissions.
- **Writes need a JSON body.** `POST`, `PUT`, `PATCH` and `DELETE` requests
  without `Content-Type: application/json` get HTTP 415. Writes made with a
  browser session must also send the `X-Api-Csrf` header with the value of the
  `CSRF` cookie, or they get HTTP 403. The AI Factory UI extension sends both.
- **The operator API no longer returns registry passwords.** The
  `GET /api/v1/settings/registry-credentials` endpoint is removed. The new
  `GET /api/v1/settings/registry-status` returns only whether App Collection,
  SUSE Registry and NVIDIA credentials are configured. It answers 500 when
  Settings or a credential Secret cannot be read, rather than reporting the
  credentials as not configured.
- **The App wizard no longer fetches registry passwords from the operator.**
  Installed apps reference the pull secrets the operator delivers to target
  clusters: `suse-ai-pull-combined` for App Collection and SUSE Registry charts,
  and `ngc-secret` for NVIDIA charts. The wizard still creates per-registry
  `suse-ai-pull-secret-<host>` Secrets from the Rancher ClusterRepo
  credentials, using the signed-in user's own permissions.
- **Wildcard CORS removed.** The API no longer sends
  `Access-Control-Allow-Origin: *`.
- **The "operator is unreachable" banner follows the operator.** Overview,
  Blueprints and Workloads show it when their 10-second refresh cannot reach the
  operator, instead of silently keeping stale data, then re-check the connection
  every 10 seconds and reload once the operator answers. Any answer from the
  operator also clears the banner on pages opened later, so a brief outage (an
  operator rollout or a Rancher restart) no longer needs a **Retry** click.
- **New chart value `aggregation.enabled`** (default `true`) controls the
  `APIService`. Setting it to `false` disables the operator API, and the AI
  Factory UI extension does not work. Installing on a cluster without Rancher
  Manager fails unless it is set to `false`, because the `APIService` CRD is
  missing.
- **Connection Secret.** Rancher writes the tunnel token into the Secret
  `aif-operator-aggregation` (`<fullname>-aggregation`) in `cattle-system`,
  set by the new chart value `aggregation.secretNamespace` (default
  `cattle-system`). Keep it outside the operator namespace: the Settings
  credential validation can use any Secret in the operator namespace. The
  operator receives the Secret's name and namespace through the
  `AGGREGATION_SECRET_NAME` and `AGGREGATION_SECRET_NAMESPACE` environment
  variables. Keep the operator namespace admin-only for the other credential
  Secrets: leave it unassigned to a Rancher project, or move it to the
  **System** project.

### Upgrade notes

- **Don't upgrade with plain `helm upgrade --reuse-values`.** It keeps the
  previous release's pinned `manager.image.tag`, `aiExtension.source.helm.version`
  and `aiExtension.extension.version`, so the new chart would run the old
  operator image. Use `--reset-then-reuse-values` (Helm 3.14 or later), or pass
  the new versions explicitly. The chart refuses to render when one of these
  values is a version older (by major.minor) than the chart's `appVersion`, or
  older than `2.5.0-dev.2`, the first version that serves the operator API
  through Rancher's APIService (so an upgrade from `2.5.0-dev.1` with
  `--reuse-values` is refused too). The error names the value.
  In Rancher Apps & Marketplace an upgrade sends only the values that differ
  from the new chart's defaults; if the upgrade form shows a pinned
  `manager.image.tag` or `aiExtension` version from the previous release, edit
  it to the new version or remove it.
- **Upgrade the operator and the AI Factory UI extension together,** then
  reload the Rancher UI. An older extension can't reach the new operator and
  shows **Cannot connect to the SUSE AI operator** until the extension is
  upgraded. A newer extension against an older operator shows **AI Factory
  operator is unreachable. Make sure the operator and the AI Factory UI
  extension are the same version.** The operator upgrades the extension
  automatically unless `aiExtension.enabled=false`; those installs must upgrade
  the extension manually.
- **Rancher 2.13 or later is required.** The UI extension now declares
  `catalog.cattle.io/rancher-version: >= 2.13.0-0`. Tested on Rancher v2.13.1
  and v2.15.2, including Rancher with 3 replicas.
- **Removed chart value:** `api.port`, because the operator no longer has an
  API port. A value still set from an earlier release, for example with
  `helm upgrade --reset-then-reuse-values`, is ignored.
- **Readiness waits for the tunnel.** An operator pod is Ready only while its
  tunnel to Rancher is connected, so `helm upgrade --wait` fails, and the
  rollout keeps the previous pods, when the new pods cannot reach Rancher. See
  "HTTPS proxy" below for the most common cause.
- **HTTPS proxy.** If the operator uses an HTTPS proxy (`HTTPS_PROXY`),
  `NO_PROXY` must include the cluster's Service CIDR (for example
  `10.43.0.0/16`, the RKE2 and K3s default). Otherwise the tunnel to Rancher
  goes through the proxy. The tunnel connects to the address Rancher writes into
  the connection Secret, which on Rancher 2.15 is the ClusterIP of the
  `rancher-internal` Service in `cattle-system`; `NO_PROXY` never matches an IP
  address against a domain suffix such as `.svc`, so list the CIDR or that
  ClusterIP.
- **Automation** calling the API must use the `/v1/aif-operator` path through
  Rancher. Reads work with `kubectl get --raw /v1/aif-operator/api/v1/...` and a
  kubeconfig that goes through Rancher. `kubectl create --raw` sends no
  `Content-Type`, so its writes get HTTP 415; writes need a client that sends
  `Content-Type: application/json`, for example `curl` with a Rancher API token.
- **Rotate credentials after upgrading.** Earlier versions of the operator API
  did not check the caller's permissions and included endpoints that returned
  or used the stored credentials. As a precaution, rotate the App Collection,
  SUSE Registry, NVIDIA and Git credentials, then enter the new registry values
  in AI Factory **Settings** and update the Secret referenced by the Fleet Git
  source there.
- **`suse-ai-pull-secret-<host>` Secrets** are no longer created from operator
  credentials. Only the App Collection and chart repository Secrets are still
  refreshed, by the FleetBundle and GitOps install paths. After rotating
  credentials, upgrade apps installed by earlier versions through the App wizard,
  which switches them to `suse-ai-pull-combined`, before revoking the old
  credentials. Don't delete these Secrets.

## Blueprint deploy order and Kustomize overlays

Blueprint components accept two new optional fields:

- **`dependsOn`:** sibling component `chartName`s that must be ready before this
  component deploys (up to 32; a component can't depend on itself). The operator
  holds a component until its dependencies' Bundles are Ready or Modified at the
  version just rendered, so upgrades are ordered too. On Fleet 0.15 and later it
  also renders Fleet `dependsOn` with `acceptedStates: [Ready, Modified]`; older
  Fleet versions rely on the operator's hold alone. The hold waits for every
  target cluster.
- **`kustomize`:** a post-render overlay applied to the component's rendered Helm
  output, either inline (`files`, a map of path to content, plus `dir`) or from a
  directory of a git-backed custom repository (`gitRef`). Overlays are applied
  with Fleet's Kustomize support; Helm hooks are not post-rendered.

The Blueprint create wizard edits both on each component's **Advanced** tab,
with overlay examples and warnings. The install and Manage wizards can override
a component's deploy order and add an install-time overlay. Removing a
component from the install also removes it from the deploy order, and the
**Review** step shows the effective order.

Install-time overlays are applied by Fleet with its agent's permissions on the
target cluster, so anyone allowed to create AIWorkloads can add resources there.
Grant AIWorkload create access accordingly.

**Compatibility:** needs the Blueprint and AIWorkload CRDs shipped with this
release. Adding or removing `dependsOn` changes a workload's render digest once.
Upgrading Fleet to 0.15 also changes the digest of workloads with dependencies
once, as `acceptedStates` is added.

## Deploy-time values from ConfigMaps and Secrets

Blueprint components accept **`valuesFrom`**: up to 8 ConfigMap or Secret keys
(default key `values.yaml`) that Fleet reads on the target cluster at deploy time
and merges over the inline values. The namespace must be empty (the component's
release namespace) or a namespace that a component of the same Blueprint deploys
to; the API rejects other namespaces with HTTP 422 and the operator refuses to
render a workload that uses one (reason `InvalidValuesFrom`). This guards
against mistakes; it is not a security boundary, since a Blueprint chooses its
components' namespaces, so restrict who can create Blueprints. The create
wizard edits these on the **Advanced** tab and shows namespace errors inline;
the install wizard shows them read-only.

**Compatibility:** needs the Blueprint CRD shipped with this release. Workloads
of a Blueprint that already reads from another namespace stop rendering after
the upgrade until the Blueprint is fixed.

## Rollback policy

Blueprints accept **`rollback: Disabled`** for versions whose upgrade migrates
data that older versions can't read. Moving a workload to an older version is
blocked when either the deployed or the target version disables rollback,
whether by **Rollback**, by **Upgrade** with an older target, or by editing
`spec.source.blueprint.version`: the API returns
HTTP 400, and the operator keeps the deployed version, reports reason
`RollbackDisabled` (Degraded if the workload was running) and fails any
operation in progress. A version is older by semver order. Empty means rollback
is allowed. The create wizard sets this under **Basic Information**; the install
wizard shows it read-only.

Rollback now waits for the cluster to report the target version again instead of
succeeding immediately, and a held rollback or upgrade still times out.

## Automatic values

Blueprint values can use two placeholders that the operator fills in from the
target cluster:

- `aif:auto-domain:<label>` becomes `<label>.<ingress-ip>.sslip.io`.
- `aif:auto-ingress-class` becomes the cluster's default IngressClass. Imported
  RKE2 and K3s clusters fall back to their built-in ingress controller.

The resolved values are recorded in `status.resolvedValues` with the cluster
they came from, and are locked after install: the API rejects edits with HTTP
400, and the operator refuses to render a workload whose values change a locked
value (reason `LockedValueChanged`, Degraded if the workload was running).
Retargeting a workload to another cluster resolves automatic values again.
Resolving them needs a single target cluster (reason
`AutoValueNeedsSingleTarget`), and the API rejects changing a workload with
automatic values to several target clusters. On the local cluster, the operator
waits up to 5 minutes for the ingress LoadBalancer to get an address (reason
`AutoValuePending`), then falls back to a node IP with a warning event, or keeps
waiting if there is none; set the value explicitly to avoid this. A running
workload whose automatic values can't be resolved shows Degraded.

To read cluster facts, the operator now has read-only access to nodes,
IngressClasses, and Rancher's `management.cattle.io` nodes and clusters and
`provisioning.cattle.io` clusters.

**Compatibility:** needs the AIWorkload CRD shipped with this release. Values
recorded before this release adopt the current target cluster without being
resolved again. Workloads that already target several clusters keep rendering
with their recorded values and report an `AutoValueTarget` warning; set the
values explicitly or target one cluster.

## Outdated CRD detection

When the installed Blueprint or AIWorkload CRD is older than the operator, as can
happen with `crds.manageWithJob=false`, saving an object that uses a new field
now fails with HTTP 422 naming the dropped fields and the CRD file to apply,
instead of silently dropping them. The new fields covered are `rollback`,
`valuesFrom`, `dependsOn` and `kustomize`.

**Compatibility:** downgrading the operator past this release is not supported
once objects use these fields: older operators ignore them, render automatic-value
placeholders literally, and older CRDs drop the fields on the next write.

## Install wizard checks

The Blueprint install wizard now warns, on each component, when its chart is
already installed as a Helm release on a target cluster, naming the cluster,
release, chart version and namespace. The warning is advisory and only matches
by chart name. The missing-credentials check now also covers the gated
`nvidia-runai` repository.

## NVIDIA Run:ai Blueprints

Bundled NVIDIA Run:ai Blueprint deploys with its prerequisites on one target
cluster, with no install-time input:

- **NVIDIA RunAI** (`nvidia-runai`): NVIDIA's commercial Run:ai 2.26 (NVIDIA AI
  Enterprise). Its charts and images are gated: the NVIDIA NGC key in Settings
  must be entitled to Run:ai.

The Blueprint needs Application Collection credentials. It installs
local-path-provisioner, cert-manager, the Prometheus Operator and the NVIDIA GPU
Operator (each can be excluded if the cluster already has it), check cluster
capacity, register the cluster with the Run:ai control plane and issue its
certificates. The GPU Operator uses SUSE's precompiled driver images by default;
see the Blueprint description for driver branches per SLES version. When the
install finishes, the cluster shows as Connected in the Run:ai UI.
