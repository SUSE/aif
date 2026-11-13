# Importing NVIDIA AICR bundles as a blueprint catalog

Status: implemented by `operator/cmd/aicr-import` · Target: AI Factory `main`

## 1. Goal

Make the output of NVIDIA AI Cluster Runtime (AICR) consumable by the SUSE AI
Factory (AIF) extension as Blueprints, so an AIF administrator can install an
NVIDIA-validated GPU stack from the Blueprints page instead of hand-writing
blueprints (today's "GPU Operator for RKE2" blueprints carry ~300 lines of
pasted chart defaults around two real decisions).

Delivery is staged:

1. **SUSE-side importer** (this spec): a tool that reads an AICR bundle and
   writes an AIF Git catalog. No AICR change required.
2. **Upstream** (later): propose `aicr bundle --deployer aif` to NVIDIA/aicr
   once the Blueprint schema settles (ordering, prerequisites).

## Usage

```bash
# 1. Generate an AICR bundle (https://github.com/NVIDIA/aicr)
aicr recipe --service rke2 --os sles --intent training -o recipe.yaml
aicr bundle -r recipe.yaml -o ./bundle

# 2. Convert it to a blueprint catalog
cd operator
go run ./cmd/aicr-import --bundle ../bundle --out ../catalog \
  --name nvidia-gpu-platform --display-name "NVIDIA GPU Platform" --version 1.0.0

# Optional: export a subset, reuse an existing repo, or emit one blueprint
#   --only gpu-operator,nfd
#   --repo https://helm.ngc.nvidia.com/nvidia=nvidia
#   --single
```

Then register `catalog/` as described in its generated `README.md`.

## 2. Input contract: the AICR bundle

The importer reads an AICR bundle produced by `aicr bundle` (default `helm`
deployer). The per-folder layout is frozen by AICR
(`pkg/bundler/testdata/layout/manifests/*.txt`), so it is a stable contract:

| File | Meaning |
|---|---|
| `recipe.yaml` | Resolved recipe: criteria, `componentRefs` (name, namespace, version, `dependencyRefs`), `deploymentOrder`, applied overlays, AICR version |
| `NNN-<release>/upstream.env` | `CHART`, `REPO`, `VERSION`. For OCI charts `REPO` is empty and `CHART` is the full `oci://…/<chart>` reference |
| `NNN-<release>/values.yaml` | AICR's validated values (static) |
| `NNN-<release>/cluster-values.yaml` | Per-cluster values (dynamic paths); merged over `values.yaml` |
| `NNN-<release>/Chart.yaml` | Present only for **local charts** (raw manifests wrapped by AICR, e.g. `-pre`/`-post` folders) |

`NNN` is install order. AICR's own deployers honour this order, either strictly
(helm) or by dependency level (argocd/flux/helmfile).

## 3. Output: an AIF Git catalog

```
<catalog-repo>/
  blueprints/
    catalog.yaml                 # BlueprintCatalog
    <name>-<version>.yaml        # one file per Blueprint
  settings-customrepos.yaml      # Settings.spec.customRepos entries (admin applies once)
  README.md                      # import + install order + caveats
```

Registration (admin, once):

```yaml
# AIF Settings
spec:
  customRepos:   [ ...entries from settings-customrepos.yaml... ]
  blueprintCatalogs:
    - name: nvidia-aicr
      repoURL: https://git.example.com/org/aicr-catalog.git
      branch: main
      paths: [blueprints]
```

Why chart sources go through `Settings.spec.customRepos` rather than raw
ClusterRepo manifests:

- AIF resolves `chartRepo` by ClusterRepo **name**, so any ClusterRepo works at
  install time, but AIF **prunes** any ClusterRepo labelled
  `ai-factory.suse.com/custom-repo` that is not declared in `Settings`
  (`pruneCustomRepos`). Declaring the sources is the supported path.
- AIF uses the **no-op pull-secret injector** for custom repos, so AICR's
  validated values reach Helm unmodified (see §6.1).
- The catalog GitRepo applies *any* YAML under `paths` to the Rancher local
  cluster with no kind filter. Shipping ClusterRepos there would work, but it
  relies on undocumented behaviour, so the catalog carries Blueprint and
  BlueprintCatalog objects only.

## 4. Mapping rules

### 4.1 Blueprint

| Field | Value |
|---|---|
| `metadata.name` | `<slug>-<version with dots→dashes>` (AIF convention) |
| labels | `ai-factory.suse.com/blueprint-name`, `ai-factory.suse.com/blueprint-version`, plus `aicr.nvidia.com/recipe` (criteria slug) and `aicr.nvidia.com/version` |
| `spec.displayName` | e.g. `NVIDIA GPU Platform – RKE2 / SLES / training (AICR)` |
| `spec.version` | SemVer chosen by the publisher; bump on any AICR or values change (Blueprint versions are immutable in practice) |
| `spec.description` | criteria, AICR version, recipe overlays, link to validation.aicr.run when the recipe is validated |
| `spec.source` | `Nvidia` (NVIDIA-validated content) |

### 4.2 Component (one per upstream-chart folder)

| Field | Value |
|---|---|
| `chartRepo` | name of the custom repo for the chart's source (§4.3) |
| `chartName` | `CHART` (for OCI: last path segment) |
| `chartVersion` | `VERSION`, unchanged (AICR may use a `v` prefix) |
| `targetNamespace` | the component's namespace from `recipe.yaml` |
| `releaseName` | the folder's release name (`NNN-` stripped); keeps release names identical to every other AICR deployer |
| `values` | deep-merge of `values.yaml` then `cluster-values.yaml` |
| `vendor` | **omitted** (§6.1) |

### 4.3 Custom repos

| Source | Entry |
|---|---|
| HTTP(S) repo | `type: helm`, `url: <REPO>`; one entry per distinct URL |
| OCI chart `oci://<registry>/<ns>/<chart>` | `type: oci`, `url: oci://<registry>/<ns>`; one entry per distinct namespace. AIF appends the chart name (`ociChartRef`) |

Names are deterministic DNS-1123 labels: `aicr-<host-and-path slug>`,
truncated to 63 characters with a hash suffix on collision. A mapping option
lets the importer reuse an existing AIF repo instead (e.g. AIF's operator-managed
`nvidia` repo for `https://helm.ngc.nvidia.com/nvidia`). Reusing an
operator-managed repo re-enables value injection, so it is opt-in.

## 5. Composition: what goes in one Blueprint

AIF creates one Fleet HelmOp per component, **all at once, with no ordering**.
An AICR stack is ordered, so the importer must decide Blueprint boundaries.

### 5.1 Tiered Blueprints (default)

Split the recipe by AICR dependency level (the `dependencyRefs` DAG in
`recipe.yaml`, i.e. AICR's `ComponentRefsTopologicalLevels`) into one Blueprint
per tier:

```
nvidia-gpu-platform-1-foundation   cert-manager, nfd, prometheus-operator-crds, …
nvidia-gpu-platform-2-operators    gpu-operator, kube-prometheus-stack, …
nvidia-gpu-platform-3-scheduling   kai-scheduler, nvidia-dra-driver-gpu, …
```

Each Blueprint lists its predecessor in `description` and the README. The admin
installs tiers in order and waits for each to reach `Running` before the next.

### 5.2 Single Blueprint

Allowed only when every component tolerates parallel installation (no
component's templates use CRDs or webhooks from another component in the same
Blueprint). The importer refuses otherwise.

### 5.3 Subset export (e.g. "GPU Operator only")

A subset must be closed under `dependencyRefs` and under **companion rules**:
settings in one component that assume another component is present. Known
companion rules:

| Component | Assumes | If exported without it |
|---|---|---|
| `gpu-operator` | standalone `nfd` (AICR sets `nfd.enabled: false`) | set `nfd.enabled: true`, or include `nfd` |

Observed on a test cluster: a GPU-Operator-only blueprint built from AICR values
silently removed NFD. Existing node labels hid the problem; a new node would not
have been labelled.

## 6. Constraints and known issues

### 6.1 Pull-secret injection rewrites values (AIF bug)

With `vendor: nvidia` (and an operator-managed repo) AIF injects NGC
pull-secret keys. One of them, `operator.image.pullSecrets` (the k8s-nim-operator
shape), turns gpu-operator's **string** `operator.image` into a map. The image
then renders as `nvcr.io/nvidia/map[pullSecrets:[ngc-secret]]:v26.7.1` and the
upgrade hangs on the pre-upgrade CRD job. Hand-written blueprints avoid this
only because they paste `operator.image: gpu-operator`. AICR's images are
public, so the importer omits `vendor` and uses custom repos (no-op injector).
Upstream fix: inject `operator.image.pullSecrets` only when `operator.image` is
already a map, or only for the NIM operator chart.

### 6.2 Charts that create their own CRDs and use `lookup`

Fleet runs a dry-run install before every install. A chart that uses Helm
`lookup` is dry-run server-side, so its templated CRs must already map. A chart
that ships CRDs in `crds/` **and** templates CRs of them **and** uses `lookup`
fails on a fresh cluster and cannot recover by itself. Example: kai-scheduler
(`Queue`, `SchedulingShard`; `lookup` for OpenShift detection). gpu-operator has
no `lookup` and installs fine. Mitigation: put such charts in their own later
tier and document the CRD pre-install, until AIF supports prerequisites (§8).

### 6.3 Local charts (raw manifests)

Folders with `Chart.yaml` have no chart repository. The importer fails and
lists them. Future work: publish them to an OCI registry and reference them as
an `oci` custom repo. The RKE2 training recipe has none; inference has two.

### 6.4 Coexistence with what the cluster already runs

AICR's full stack overlaps components common on Rancher clusters:

| AICR component | Common existing component |
|---|---|
| `kube-prometheus-stack`, `prometheus-operator-crds` | `rancher-monitoring` |
| `kai-scheduler` | Run:AI (ships the KAI CRDs) |

Exclude these before export (`aicr bundle --set <component>:enabled=false`). The
tiered layout also lets an admin skip a tier.

### 6.5 Ownership and removal

Blueprint HelmOps use `takeOwnership`, so installing over an existing release
(for example a GPU Operator installed with `helm`) **adopts and upgrades** it.
Deleting the AIWorkload uninstalls every release it owns. The README must say
so.

## 7. OS and platform values

The importer copies AICR's values verbatim, so OS correctness comes from the
AICR recipe. On SUSE these recipes must follow the RKE2 GPU Operator
documentation (https://docs.rke2.io/add-ons/gpu_operators), which SUSE AI
Factory and NVIDIA both reference:

- non-NRI toolkit path: `toolkit.env CONTAINERD_SOCKET=/run/k3s/containerd/containerd.sock`
- SLES: SUSE precompiled driver `registry.suse.com/third-party/nvidia`,
  `usePrecompiled: true`, driver branch (e.g. `595`); `kernelModuleType: open`
  with Secure Boot
- SL Micro: host-installed driver, `driver.enabled: false`
- no kernel version pinned in recipe constraints: precompiled image tags follow
  `uname -r`

AICR's generic `rke2` base currently enables the experimental NRI path, which
deletes the `nvidia` RuntimeClass. That breaks nvsentinel's metadata-collector.
SUSE recipes override this to the documented path.

## 8. Requested AIF changes (SUSE/aif)

1. Fix §6.1 (injector shape check).
2. `BlueprintComponent.dependsOn: [<chartName>]`, mapped to HelmOp `spec.dependsOn`.
   This makes a whole AICR stack one Blueprint and removes tiering.
3. Prerequisites: either a CRD-only component type or "apply `crds/` first"
   for a component (§6.2).
4. Document what a catalog may contain, or restrict catalog GitRepos to
   Blueprint/BlueprintCatalog kinds (§3).

## 9. Validation plan

1. Schema: server-side dry-run of the generated Blueprints, BlueprintCatalog and
   Settings patch against AIF `main` CRDs.
2. Import: register the catalog, then check that Blueprints and the catalog card
   appear and custom repos reconcile.
3. Install tiered Blueprints on a test cluster in order; verify every
   component's health (AICR health checks) and GPU allocatable.
4. Upgrade: publish version N+1 and bump the AIWorkload; verify a clean Helm
   upgrade, and that driver changes regenerate CDI.
5. Removal: delete the AIWorkloads in reverse tier order; verify a clean
   uninstall.

## 10. Out of scope (v1)

Air-gapped mirroring, per-cluster value overrides (AIWorkload
`componentValues` covers this), network-operator / RDMA recipes, and SL Micro
recipes (these depend on AICR overlays that do not exist yet).
