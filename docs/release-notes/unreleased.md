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
