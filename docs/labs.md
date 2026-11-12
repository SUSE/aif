# SUSE AI Factory Labs (proof of concept)

**Status:** proof of concept on branch `poc/aif-labs`. Not production code. Use a test
cluster only.

A **lab** is an optional extension to SUSE AI Factory (AIF). Each lab is its own Helm
chart. An admin turns it on in **Settings → Labs**, and it then appears in the
navigation under **SUSE AI Factory → Labs**. The example lab is **Token Factory**, a
UI mock-up with no real backend.

Contents:

1. [Try it](#1-try-it)
2. [Write a lab](#2-write-a-lab)
3. [How it works](#3-how-it-works)
4. [Security](#4-security)
5. [Troubleshooting](#5-troubleshooting)
6. [Status and next steps](#6-status-and-next-steps)

---

## 1. Try it

**You need**

- A test Rancher with **AIF already installed** (Rancher Desktop works), and `kubectl`
  pointing at its local cluster, with admin rights.
- On your machine:
  - `docker` with BuildKit. To build for a different CPU type it also needs QEMU,
    which Rancher Desktop and Docker Desktop include.
  - `helm` 3.14 or newer, `yarn` 1.x (`brew install yarn`), `jq`, `yq`, and `node` 20 or
    newer.
- Internet access from your machine and the cluster to `ttl.sh`.

**Run**

```bash
git clone -b poc/aif-labs <repo-url> aif && cd aif
(cd ui && yarn install --ignore-engines)
hack/labs-poc.sh
```

The script shows the kube context, the AIF release and the target platform, and asks
before it changes anything. Then it:

1. Builds the operator, the AIF UI and the Token Factory lab for your cluster's CPU
   architecture.
2. Pushes them to `ttl.sh/aif-labs-<random>`, with images pinned by digest. ttl.sh is
   public and anonymous, and everything in it expires after **24h**. Rerun the script
   to republish.
3. Runs `helm upgrade` on your AIF release and keeps your existing values. The chart's
   CRD hook Job adds the new CRDs.

The first run takes about 10–20 minutes. `DEPLOY=0` builds and publishes without
deploying.

**Play**

1. Reload the browser. If you developer-loaded `aif-ui` earlier, remove that copy
   first.
2. In **Settings → Labs**, tick **Token Factory**. Watch it with
   `kubectl get aiflab -w`. It goes Pending → Installing → Ready.
3. Reload the browser and open **SUSE AI Factory → Labs → Token Factory**.
4. Untick it. The lab is uninstalled, and its navigation entry is gone after a reload.
5. Under **Lab catalogs**, add a public https URL to a catalog file (format in
   [§2.5](#25-make-it-available)). A GitHub gist "raw" URL works.

**Revert** (the script prints the exact commands at the end)

```bash
kubectl delete aiflab --all --wait=true
kubectl delete aiflabcat --all
helm rollback <release> <previous-revision> -n <namespace>
```

The added CRDs stay. They are harmless.

---

## 2. Write a lab

Copy Token Factory. The steps below use `my-lab`; the package and chart names are then
`aif-lab-my-lab`.

| Part | Token Factory reference |
|---|---|
| UI extension | `ui/pkg/aif-lab-token-factory/` |
| Chart (extension server + optional backend) | `charts/aif-lab-token-factory/` |
| Lab-owned CRDs (admin installs) | `charts/aif-lab-token-factory-crds/` |
| Image | `build/Dockerfile.lab-token-factory` |

### 2.1 UI extension

```bash
cp -R ui/pkg/aif-lab-token-factory ui/pkg/aif-lab-my-lab
```

- `package.json`: set `name`, `version`, `rancher.annotations` and `icon`.
- `product.ts`: add pages to the **existing** AIF product. Don't call `product()`;
  AIF owns the product.
  ```ts
  const { virtualType, basicType } = $plugin.DSL(store, 'suseai');
  virtualType({ name: 'lab-my-lab', label: 'My Lab', route: { name: 'c-cluster-suseai-lab-my-lab', params: { cluster: '_' } } });
  basicType(['lab-my-lab'], 'Labs');
  ```
- `routing.ts`: use the route `c-cluster-suseai-lab-my-lab`, the path
  `/c/:cluster/suseai/labs/my-lab`, and `meta: { product: 'suseai' }`.
- Iterate with `yarn build-pkg aif-lab-my-lab && yarn serve-pkgs`, then in Rancher go to
  Extensions → ⋮ → Developer Load.

### 2.2 Backend (optional)

- Add Deployments and Services to the chart (see `templates/mock-api.yaml`).
- Put your CRDs in the API group `my-lab.labs.ai-factory.suse.com`. The AIF operator
  never reads them.
- The browser reaches the backend through Rancher's service proxy:
  `/k8s/clusters/local/api/v1/namespaces/<ns>/services/http:<svc>:<port>/proxy/...`

### 2.3 Chart rules

```bash
cp -R charts/aif-lab-token-factory charts/aif-lab-my-lab
```

1. **Names start with `aif-lab-`.** Both the chart name (the last segment of
   `chartURL`) and `extension.name` need this prefix. The operator rejects other names
   (see [§3.3](#33-naming-rule)).
2. **The first Service port serves the extension catalog:** `index.yaml`, the
   extension chart and the plugin assets. The image from §2.4 does this. The operator
   installs the chart as release `aif-lab-my-lab-server` in `cattle-ui-plugin-system`.
3. **No `crds/` directory.** The operator can't create CRDs, and Helm fails with
   Forbidden even when the CRD already exists. Ship CRDs in a separate
   `aif-lab-my-lab-crds` chart that an admin installs first.
4. **Image values** go under `image.registry`, `image.repository` and `image.tag`.

### 2.4 Build and publish

Create `build/Dockerfile.lab-my-lab` from the Token Factory one, then:

```bash
LAB=aif-lab-my-lab DEPLOY=0 hack/labs-poc.sh     # to ttl.sh
```

To publish by hand to your own registry:

```bash
cd ui && yarn publish-pkgs -c -i '' -r local -o aif-labs aif-lab-my-lab && cd ..
# publish-pkgs names the image after ui/package.json (aif-ui), so retag it:
docker build -f build/Dockerfile.lab-my-lab --build-arg BASE=local/aif-labs/aif-ui:<ui version> \
  -t <registry>/aif-lab-my-lab:<version> .
docker push <registry>/aif-lab-my-lab:<version>
helm package charts/aif-lab-my-lab && helm push aif-lab-my-lab-<version>.tgz oci://<registry>/chart
```

Push the chart under `/chart`: if it shares a ref with the image, the chart push
overwrites the image. If `manager.allowedRegistryHosts` is set, it must include your
registry.

### 2.5 Make it available

**External catalog (recommended).** Host a JSON array on a public https URL:

```json
[
  {
    "name": "my-lab",
    "displayName": "My Lab",
    "description": "What it does.",
    "maturity": "experimental",
    "source": { "kind": "Helm", "helm": { "chartURL": "oci://<registry>/chart/aif-lab-my-lab", "version": "0.1.0" } },
    "extension": { "name": "aif-lab-my-lab", "version": "0.1.0" },
    "ui": { "label": "My Lab", "icon": "icon-gear", "routeName": "c-cluster-suseai-lab-my-lab" }
  }
]
```

Add it under **Settings → Labs → Lab catalogs**, or as an object:

```yaml
apiVersion: ai-factory.suse.com/v1alpha1
kind: LabCatalog
metadata: { name: my-team }
spec: { url: https://example.com/aif-labs/catalog.json }
```

Labs from this catalog show a "Catalog: my-team" badge. Ticking one creates a Lab named
`my-team-<entry>` that follows the catalog, so a new version in the catalog upgrades
it. The lab must be pullable anonymously, because `auth`/`tls` in external catalogs
are ignored (see [§4](#4-security)).

**Hand-made Lab.** A Lab without a catalog (shows as "Custom"):

```yaml
apiVersion: ai-factory.suse.com/v1alpha1
kind: Lab
metadata: { name: my-lab }
spec:
  enabled: true
  displayName: My Lab
  source: { kind: Helm, helm: { chartURL: oci://<registry>/chart/aif-lab-my-lab, version: 0.1.0 } }
  extension: { name: aif-lab-my-lab, version: 0.1.0 }
```

**Built-in.** Add the entry to `operator/internal/labs/catalog.json` and rebuild the
operator. This is reserved for labs that SUSE ships.

---

## 3. How it works

```
Settings → Labs (UI) ──writes as the logged-in user──▶ Lab / LabCatalog objects
LabCatalog controller ── fetches catalogs ──▶ LabCatalog.status.entries
Lab controller ── resolves entry ──▶ child InstallAIExtension "lab-<name>"
                                       ├▶ Helm release <chart>-server (extension server)
                                       └▶ ClusterRepo + UIPlugin (Rancher loads the UI)
```

### 3.1 Objects (`ai-factory.suse.com/v1alpha1`, cluster-scoped)

**LabCatalog** (`aiflabcat`):

- `spec.url`: https. It is empty only for `builtin`, which the operator creates from
  the embedded `catalog.json` and recreates if deleted.
- `status`:
  - `phase`: Ready or Error
  - `message`, `lastFetched`
  - `skipped`: the number of entries dropped
  - `entries[]`: the validated entries

**Lab** (`aiflab`):

- `spec.enabled`
- **Either** `spec.catalogRef: {catalog, entry}` **or** an inline `spec.source` plus
  `spec.extension`. A CEL rule enforces this.
- `status`:
  - `phase`: Disabled, Pending, Installing, Ready or Failed
  - `message`, `extensionRef`, `resolvedChartURL`, `resolvedVersion`

### 3.2 Controllers

- **LabCatalog**:
  - fetches external catalogs with `safehttp`, which allows only public addresses,
    with a 10s timeout and a 1 MiB cap
  - refetches when the spec changes and every 10 minutes
  - drops invalid entries, strips `auth`/`tls`, and keeps the previous entries if a
    fetch fails
- **Lab**:
  - resolves the catalog entry or the inline spec, and applies the naming rule
  - when enabled, creates the child `InstallAIExtension` (owned by the Lab) and copies
    its phase
  - when disabled, deletes the child, whose finalizer uninstalls the release and the
    UIPlugin
  - watches LabCatalogs, so catalog updates reach the Labs that use them

### 3.3 Naming rule

The chart name and `extension.name` must start with `aif-lab-`. This keeps labs in
their own namespace of Helm release and UIPlugin names, so a lab never collides with
the AIF UI (`aif-ui`) or with another extension. The code is in
`operator/internal/labs/security.go`.

### 3.4 UI

- **Settings → Labs** uses Rancher's `management` store (steve) for Lab and LabCatalog.
  Requests run as the logged-in user, so Kubernetes RBAC decides, and the lists update
  live.
- When the user lacks permission, the controls are disabled.
- Labs settings take effect immediately. The page's **Apply** button covers only the
  other settings.
- A lab extension's pages appear because the extension adds itself to the `suseai`
  product (see §2.1).

### 3.5 Code map

| Area | Path |
|---|---|
| CRD types | `operator/api/v1alpha1/lab_types.go`, `labcatalog_types.go` |
| Controllers | `operator/internal/controller/lab/`, `operator/internal/controller/labcatalog/` |
| Built-in catalog, fetch, validation | `operator/internal/labs/` |
| Settings → Labs | `ui/pkg/aif-ui/pages/components/LabsSettings.vue`, `services/labs.ts`, `types/lab-types.ts` |
| RBAC / GlobalRole | `charts/aif-operator/templates/rbac/manager-role.yaml`, `labs-admin-globalrole.yaml` |
| Build, publish, deploy | `hack/labs-poc.sh` |

---

## 4. Security

- **Who can turn on labs:** only Rancher global admins by default. The chart's Rancher
  GlobalRole `aif-labs-admin` ("AI Factory Labs Admin", `labs.adminGlobalRole.enabled`)
  grants it to others.
- **A lab is fully trusted.** Rancher does not isolate UI extensions: a lab's
  JavaScript runs in every user's browser session. Being able to turn on labs or add
  catalogs is therefore equivalent to Rancher admin.
- **Catalogs can't use the operator's credentials.** `auth`/`tls` are stripped from
  external entries. Otherwise an entry could name a Secret in the operator namespace
  together with an attacker's `chartURL`.
- **Lab CRDs** are installed by an admin, because the operator can't create CRDs.
- **Labs are plain Kubernetes objects.** The UI reads and writes them through Rancher
  as the logged-in user, so Kubernetes RBAC applies to every change.

**Open:**

1. **Catalog trust.** Whoever controls a catalog URL controls its labs. Needed:
   - pin charts by digest and verify signatures (cosign)
   - require `manager.allowedRegistryHosts` when external catalogs exist
   - ask the admin to confirm before installing
2. **Silent upgrades.** Catalog labs follow updates with no confirmation. Consider
   pinning versions.
3. **ttl.sh is for throwaway clusters only.** Anyone who knows the path can push to
   it. Images are pinned by digest, but charts are pulled by version.

---

## 5. Troubleshooting

```bash
kubectl get aiflab,aiflabcat
kubectl get aifext lab-<name> -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
kubectl -n cattle-ui-plugin-system get pods,uiplugins
```

| Symptom | Cause / fix |
|---|---|
| `403 denied` from a registry | Wrong `chartURL`, or a private registry (external catalogs can't use credentials) |
| `cannot create resource "customresourcedefinitions"` | The chart has a `crds/` directory. Move it to a `-crds` chart |
| Pod `ImagePullBackOff`, and the image is a Helm chart | The chart and the image were pushed to the same OCI ref |
| `exec format error` in pods | Wrong CPU architecture. The script uses the first node's architecture |
| Registry host refused | `manager.allowedRegistryHosts` doesn't include it |
| Catalog `Error` | See `kubectl get aiflabcat <name> -o yaml` → `status.message` |
| Ready but no menu entry | Browser not reloaded, or the extension's `product.ts` doesn't target `suseai` |
| AIF UI not updated | `kubectl describe aifext aif-ui`, then a hard reload |
| Script warns about `global.imageRegistry` | Unset it for the test. It overrides the ttl.sh images |

---

## 6. Status and next steps

**Tested on Rancher Desktop with Rancher Prime v2.15.2:**

- A Lab installs from ttl.sh, and Token Factory appears under Labs next to the AIF UI.
  The order in which extensions load didn't matter.
- The Settings → Labs list with source badges.
- An external catalog on a GitHub gist: duplicates are skipped, and errors show in the
  row.

**Not yet tested:**

- the object-based version (LabCatalog and `catalogRef`) on a cluster: the CEL rules,
  store saves and removes, permission checks, and recreating `builtin`
- the full disable → re-enable cycle
- the mock backend (`mockApi.enabled`)

**Next:**

1. Close the catalog trust gaps (§4).
2. A shared UI SDK for labs and a scaffolding command.
3. `aifVersionRange` compatibility checks.
4. A Lab status that reports missing lab-owned CRDs.
