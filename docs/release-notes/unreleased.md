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

Creating or updating a Blueprint through the API now returns HTTP 422 when a
component's `dependsOn` names an unknown component or itself, lists the same
dependency twice, or forms a cycle, instead of saving it and failing every install
later. A Blueprint that is already stored with such an error can be read and
deleted, but saving it again needs the dependency fixed. Writes made directly with
`kubectl` skip this check; the operator still reports `InvalidDependsOn` when the
Blueprint is installed.

A component can also set **`additionalPullSecretVendors`** (`suse` or `nvidia`,
up to 2) to have the operator deliver another vendor's pull secrets to the
component's namespace without changing the component's Helm values. The API
rejects unknown values with HTTP 422, and an outdated Blueprint CRD that would
drop the field is reported like the other new fields.

The Blueprint create wizard edits both on each component's **Advanced** tab,
with overlay examples and warnings. The install and Manage wizards can override
a component's deploy order and add an install-time overlay. Removing a
component from the install also removes it from the deploy order, and the
**Review** step shows the effective order.

Install-time overlays are applied by Fleet with its agent's permissions on the
target cluster, so they can create any resource there, including on the Rancher
management cluster when it is a target. These changes therefore require
permission to **update Blueprints**, which in practice means Rancher
administrators:

- adding or changing an install-time overlay or a deploy-order override;
- for a workload that carries an overlay: adding a target cluster, changing the
  target namespace, or switching to another Blueprint or source type. Changing
  only the Blueprint version does not need the permission.

Without it the API returns HTTP 403 and the install and Manage wizards show the
Advanced tab read-only. Editing values, changing the Blueprint version, removing
target clusters, and removing an overlay or custom order are not affected, and
dependencies on components that a newer Blueprint version no longer has are
removed when the workload is saved. A workload may hold at most 64
`componentValues` entries.

The rule is enforced in two places. The API checks it for the wizards and API
clients. On Kubernetes 1.30 and later the chart also installs a
`ValidatingAdmissionPolicy` named `<release>-advanced-customization` that applies
the same rule to writes that bypass the API, such as `kubectl` and GitOps. Turn it
off with `advancedCustomizationPolicy.enabled=false`. The operator's own service
account is exempt because the API has already checked its writes. A GitOps agent
that writes overlays or `dependsOn` overrides needs `update` on `blueprints`. The
policy is stricter than the API for directly applied manifests that drop stale
`dependsOn` names: those need the permission. Offline renders (`helm template`,
Argo CD) must pass `--api-versions admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy`,
or the policy is not rendered; install and upgrade print a warning. On older
Kubernetes only the API check applies, so grant create or update on AIWorkloads
only to trusted users there. Publishing to the GitOps repository
(`POST /api/v1/git/publish`) is governed by permission to update Settings. The
new `GET /api/v1/capabilities` endpoint reports the caller's permission; against
an older operator that lacks it, the wizards show the Advanced tab read-only.

Overlays are rendered more strictly and more completely than before:

- Fleet only reads a root file named `kustomization.yaml`. Overlays rooted in
  `kustomization.yml` or `Kustomization` were silently ignored; they are now
  renamed to `kustomization.yaml`, and an overlay with more than one root file is
  rejected. Overlays that Fleet used to skip take effect after the upgrade, and
  the component's render digest changes once.
- A `gitRef` overlay is read at the commit recorded in the component's
  fingerprint (the commit Rancher last indexed). If the repository index is behind
  the branch, the reconcile retries. Git operations now have timeouts: 2 minutes
  to read and 5 minutes to publish.
- A `gitRef` overlay needs an `https://` repository; `http://` and other schemes
  are refused so that credentials are never sent in clear text.
- Components with an overlay that come from an HTTP or OCI repository are pulled
  by the operator itself. It trusts the registry Secret's `cacerts` as well as the
  repository's `caBundle`, and OCI downloads are capped in size while streaming.
- Permission is checked when an overlay is added or its scope widens. Overlays
  that already exist keep applying after the permission of the user who created
  them changes.

When a component is held, the workload condition and the component's status
cell name the dependency that is blocking it and, if that dependency failed, say
so and why, instead of only reporting that it is waiting. A hold has no timeout:
it ends when the dependency becomes ready or is removed from the order.

A switch between install methods (for example HelmOp to embedded Bundle) that
does not settle is retried with increasing delay (5s up to 60s). After 10 minutes
the workload reports reason `SwitchStalled` and the operator emits one Warning
event; it keeps retrying and never aborts the switch on its own.

Deleting a workload now waits for Fleet to release each component's resources
before removing the Fleet objects, and retries on transient errors (Warning
event `KeepResourcesReleaseRetrying`). If Fleet has not released them 10 minutes
after the deletion, in either install mode, the deletion proceeds and the
operator emits a Warning event with reason `KeepResourcesNotReleased` naming the
clusters; resources may then remain there and need manual cleanup. If Fleet is
no longer installed, there is nothing to release and deletion is not delayed.

These events are recorded in the workload's namespace, which needs `create` and
`patch` on `events` for the operator's service account. The chart grants that
only in the extensions namespace, so until it is granted cluster-wide the events
are rejected and appear only in the operator log. A stalled switch still shows
the `SwitchStalled` reason on the workload.

Holds and certification now also require every target cluster's
BundleDeployment to have applied the current revision. Fleet can report a Bundle
as Ready for a few hundred milliseconds after a spec change while its
deployments are still on the previous revision, which used to release the next
component early. Expect a workload to turn Ready slightly later.

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
data that older versions can't read. Before the operator first writes anything
for a version whose Blueprint disables rollback, it records
`status.minimumSafeVersion` (stored as `<blueprint>@<version>`). A workload can
then no longer be moved below that version, whether by **Rollback**, by
**Upgrade** with an older target, by a `PUT`, or by editing
`spec.source.blueprint.version`. Moving down from a deployed rollback-disabled
version is blocked too. The API returns HTTP 400 ("use X or newer"); the operator
keeps the deployed version, reports reason `RollbackDisabled` (Degraded if the
workload was running) and fails any operation in progress. Versions are ordered
by semver, and empty means rollback is allowed. A version that is refused before
it renders (for example an invalid `dependsOn`, a locked-value change, or a chart
that is too large) does not raise the floor. The Manage page shows the minimum
safe version and disables Roll Back and upgrade targets the operator would refuse.
The create wizard sets the policy under **Basic Information**; the install wizard
shows it read-only.

If a failed upgrade leaves the floor higher than you want, an administrator can
clear it. First set `spec.source.blueprint.version` back with `kubectl edit`; do
this first, because while the spec still names the rollback-disabled version the
next render raises the floor again. Then run:

```
kubectl patch aiworkload <name> -n <namespace> --subresource=status --type=merge \
  -p '{"status":{"minimumSafeVersion":null}}'
```

This needs `update` on `aiworkloads` and `patch` on `aiworkloads/status`
(kubectl 1.24 or later). Clearing the floor does not bypass a `rollback: Disabled`
policy on the deployed version's own Blueprint.

Rollback now waits for the cluster to report the target version again instead of
succeeding immediately, and a held rollback or upgrade still times out.

## Automatic values

Blueprint values can use two placeholders that the operator fills in from the
target cluster:

- `aif:auto-domain:<label>` becomes `<label>.<ingress-ip>.sslip.io`. This needs
  public DNS; on air-gapped clusters set the value explicitly, for example
  `global.domain`.
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
`valuesFrom`, `dependsOn` and `kustomize`. If an outdated AIWorkload CRD drops
`status.minimumSafeVersion`, the operator refuses to render with reason
`CRDOutdated` until the CRD is upgraded, and fails any operation in progress.

**Compatibility:** downgrading the operator past this release is not supported
once objects use these fields: older operators ignore them, render automatic-value
placeholders literally, and older CRDs drop the fields on the next write.

## GitOps and upgrade fixes

- In GitOps mode, removing one of a workload's target workspaces (for example
  `local`) no longer deletes the published Bundle file that another workspace
  still needs, which could have uninstalled the component from a cluster that was
  still selected.
- Embedded Bundles written by the previous release are adopted on the first
  reconcile (the owner label is added), so excluding such a component removes it
  and it can switch to a HelmOp instead of being refused. Workloads whose Bundles
  are published to a Git repository are republished once with the label.

## Install wizard checks

The Blueprint install wizard now warns, on each component, when its chart is
already installed as a Helm release on a target cluster, naming the cluster,
release, chart version and namespace. The warning is advisory and only matches
by chart name. The missing-credentials check now also covers the gated
`nvidia-runai` repository. `GET /api/v1/settings/registry-status` gains a
`usable` object that is true when credentials resolve or, for NVIDIA, an
anonymous mirror is configured, so an anonymous NVIDIA mirror no longer disables
Install; the existing fields keep their meaning, and the wizard falls back to them
against an older operator.

## NVIDIA Run:ai Blueprints

The bundled NVIDIA Run:ai Blueprint deploys Run:ai with its prerequisites on one
target cluster, with no required install-time input:

- **NVIDIA RunAI** (`nvidia-runai`): NVIDIA's commercial Run:ai 2.26 (NVIDIA AI
  Enterprise). Its charts and images are gated: the NVIDIA NGC key in Settings
  must be entitled to Run:ai, or an NVIDIA mirror configured in Settings must
  serve them.

The Blueprint needs Application Collection credentials. It installs
local-path-provisioner, cert-manager, the Prometheus Operator and the NVIDIA GPU
Operator (each can be excluded if the cluster already has it), checks cluster
capacity, registers the cluster with the Run:ai control plane and issues its
certificates. When the install finishes, the cluster shows as Connected in the
Run:ai UI.

- **GPU Operator** v26.3.3, inside NVIDIA's supported range for Run:ai 2.26
  (25.10 to 26.3), with SUSE's precompiled driver images by default; see the
  Blueprint description for driver branches per SLES version.
- **Registration** needs the Run:ai permission sets "Cluster edit" and "Service
  accounts edit". If Run:ai refuses a grant, the install fails and names the
  missing permission set; it no longer falls back to the System administrator
  role. A pre-provisioned credential Secret
  (`runai-backend/runai-registrar-credentials`) can be supplied instead.
- **Credentials** are sent only to the control-plane Ingress address, never to a
  host chosen at install time. When the Ingress certificate chain includes a
  `ca.crt`, only that CA is trusted. Install only on clusters where untrusted
  users cannot create Ingresses, because anyone who can create an Ingress for the
  Run:ai host on a shared ingress controller could intercept them.
- **Wildcard certificate:** a certificate that cannot be issued yet (for example
  by an ACME HTTP-01 issuer, which cannot issue wildcards) no longer blocks the
  install; its state is reported under `wildcardCertificate` in
  `runai-backend/runai-install-info`. A Kubernetes API error while creating it
  still fails the run.
- **Credential failures say what they mean.** When a chart download is refused
  with HTTP 401 or 403, the workload condition now explains it: the credentials
  were rejected (401), or they authenticate but are not allowed to download the
  chart (403), which for NVIDIA gated repositories (NIM, cuOpt, NVIDIA Run:ai)
  usually means the NGC key's organization lacks the entitlement; with no
  credentials configured it says so. Settings **Test** shows the same kind of
  explanation for an NGC key that authenticates but is denied gated charts. A key
  can read the index and public charts without being entitled to gated ones, so
  two environments can behave differently with keys that look alike. The extra
  explanation is not added for OCI repositories.
- **Preflight failures are now visible.** The pod log lists each finding and
  `kubectl describe pod` shows it in the readiness event, with the full result in
  `runai-backend/runai-preflight-result`. Earlier builds only logged "preflight
  failed", while the workload waited on the setup component. The Blueprint
  description has a verified install-time overlay for lowering a threshold
  (administrators only). NVIDIA does not support Run:ai below its documented
  minimum hardware.
- **Preflight** thresholds can be overridden with `PREFLIGHT_MIN_CORES`,
  `PREFLIGHT_MIN_MEMORY_GB` and `PREFLIGHT_MIN_STORAGE_GB` (defaults 20, 32 and
  160). Storage is compared against ephemeral-storage capacity with a 5%
  tolerance, and NotReady and cordoned nodes are skipped.
- **Prometheus pod no longer fails to pull its image.** The Prometheus Operator
  from Application Collection starts the Run:ai Prometheus pod (`runai`
  namespace) with a config-reloader image from Application Collection. The
  `runai` namespace now also receives the Application Collection pull secret
  (`suse-ai-pull-combined`); before, the pod stayed in `ImagePullBackOff` on any
  node that had not already pulled the image. Overriding
  `global.additionalImagePullSecrets` on `runai-cluster` replaces the default list,
  so keep `suse-ai-pull-combined` unless that image is mirrored.
- **A pull secret you choose is kept.** The operator fills an empty
  `imagePullSecrets` or `global.imagePullSecrets` in a component's values with
  `suse-ai-pull-combined`, which holds every configured registry credential. It no
  longer replaces a secret set there by the Blueprint or in the Values tab, so a value
  that was silently overwritten before now takes effect on the next reconcile.
- **The community NVIDIA RunAI OSS Blueprint hands no pull secret to the Run:ai
  cluster.** Its control plane hands the first `global.imagePullSecrets` entry to the
  cluster as its registry credential, which would be `suse-ai-pull-combined`, so the
  Blueprint sets `clusterService.config.imagePullSecret.enabled: false`. For a private
  registry, set `global.imagePullSecrets` and `keycloakx.imagePullSecrets` on
  `control-plane`, and that setting to `true`.
- **Known limitation:** on downstream clusters the `prometheus-statsd-exporter`
  bundle can show `Modified` with "namespace.v1 runai is not owned by us". The
  Blueprint creates the `runai` namespace with its Pod Security labels, and the
  operator's pull-secret release later adopts it. The namespace, its labels and
  the install are not affected, and dependency holds count `Modified` as ready.
- **GitOps is not supported for this Blueprint.** Fleet installs the whole
  `workloads/` folder as one Helm release, and the Run:ai charts embedded in
  Bundles exceed Helm's 1 MiB release Secret limit (the install ends in
  `ErrApplied: Secret ... too long`). Use the default Fleet HelmOp install mode.
- The Blueprint's generator sources live in `charts/aif-operator/blueprint-src`
  and are not packaged with the chart; CI regenerates the Blueprint and fails if
  the committed file differs.
- **Community variant (not bundled).** The same sources build an open-source
  Run:ai 3.0.20 variant, `nvidia-runai-oss`, that is published in the
  SUSE/community-blueprints repository (`community/suse-ai-factory/`) instead of
  the chart. That repository also provides the `nvidia-runai-oss` ClusterRepo,
  which Fleet applies; the operator no longer creates it. A variant with
  `bundled: false` is written to `dist/` for copying and `build.py --check` only
  verifies that it builds. Its Run:ai charts and images are public, so no NVIDIA
  NGC key is needed; Application Collection credentials are still required. GitOps install mode is not supported for it, for the
  same Helm release size reason as above.
