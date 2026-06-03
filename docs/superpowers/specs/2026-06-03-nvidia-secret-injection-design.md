# NVIDIA Blueprint Component Secret Injection — Design

**Date:** 2026-06-03
**Branch:** `inject-nvidia-auth`
**Status:** Draft — pending review
**Scope:** `suse-ai-operator` (Go controller) + minimal frontend touch-up

## Problem

The operator's existing secret-injection for Blueprint components assumes the
"SUSE shape": a single combined `kubernetes.io/dockerconfigjson` secret called
`suse-ai-pull-combined`, written into both `imagePullSecrets` and
`global.imagePullSecrets` of the rendered Helm values. This works for SUSE AI
charts because they honor `global.imagePullSecrets` via shared library
templates.

NVIDIA charts do not. The k8s-nim-operator chart pulls images from `nvcr.io`
and needs `image.pullSecrets: [ngc-secret]` in its values; standalone NIM
charts need `imagePullSecrets: [{name: ngc-secret}]` at the top level, plus an
Opaque secret named `ngc-api` containing `NGC_API_KEY` for runtime model
downloads. Today, deploying any NVIDIA blueprint component through the
operator produces `ImagePullBackOff` (and, for NIM workloads, missing-API-key
errors) because the SUSE-shaped injection does not touch the paths NVIDIA
charts read.

This design adds a per-component vendor signal and routes secret creation and
values-injection through vendor-specific profiles.

## Goals

- Deploy NVIDIA blueprint components (e.g., `k8s-nim-operator`) end-to-end
  through the operator without manual `kubectl create secret` steps.
- Keep the existing SUSE flow byte-for-byte unchanged (regression-guarded
  by existing tests).
- Make vendor routing explicit and deterministic — no URL-host heuristics.
- Cover NVIDIA chart shape variability robustly enough to support all NIM
  charts surveyed (k8s-nim-operator, standalone NIM LLM/VLM/Riva/Cosmos/Safety,
  NeMo-Retriever) without per-chart configuration.

## Non-goals

- App-sourced (non-Blueprint) workloads. Those install through the Rancher
  catalog UI and do not flow through `ensureCombinedPullSecret`.
- The NVIDIA RAG umbrella chart's `imagePullSecret.password` /
  `ngcApiSecret.password` setters. That chart creates secrets itself
  (Option 2 of the original task). Blueprint authors deploying it set those
  via the component's `values:` block; the vendor injection coexists
  harmlessly.
- Mutating `image.repository` to rewrite image hostnames. `ImageRewriteSettings`
  already exists for that and is orthogonal.
- Per-component target namespaces. `AIWorkload.Spec.TargetNamespace` is
  per-workload today; the NVIDIA secrets live there.
- Adding configurability for the `ngc-secret` / `ngc-api` / `NGC_API_KEY`
  names. Those are conventions hardcoded by NVIDIA's own templates and chart
  defaults; introducing knobs is YAGNI.

## Design

### 1. Vendor signal on `BlueprintComponent`

Add an enum field to the CRD:

```go
// ComponentVendor selects the secret-injection profile for a Blueprint
// component. "suse" keeps the historical combined-secret + global.imagePullSecrets
// behavior. "nvidia" creates ngc-secret + ngc-api in the target namespace and
// writes both common pull-secret value paths.
// +kubebuilder:validation:Enum=suse;nvidia
type ComponentVendor string

const (
    ComponentVendorSUSE   ComponentVendor = "suse"
    ComponentVendorNvidia ComponentVendor = "nvidia"
)

type BlueprintComponent struct {
    ChartRepo    string          `json:"chartRepo"`
    ChartName    string          `json:"chartName"`
    ChartVersion string          `json:"chartVersion"`
    // Vendor selects the secret-injection profile. Defaults to "suse" so
    // existing blueprints behave identically after upgrade.
    // +kubebuilder:default=suse
    // +optional
    Vendor       ComponentVendor `json:"vendor,omitempty"`
    Values       *apixv1.JSON    `json:"values,omitempty"`
}
```

CRD defaulting (`+kubebuilder:default=suse`) ensures any pre-existing Blueprint
CR without the field reads back as `"suse"` — full backward compatibility.

The operator dispatches purely on `component.Vendor`. There is no URL-host
heuristic fallback; behavior is fully explicit and predictable.

### 2. Frontend auto-fill

In `pkg/suse-ai-lifecycle-manager/pages/components/wizard/BlueprintAppSelectorStep.vue`,
the `addApp(app: AppCollectionItem)` function builds the component object that
ends up in the Blueprint CR. `AppCollectionItem` already carries
`library: 'suse-ai' | 'nvidia'` (set by `services/app-collection.ts`). The
wizard maps that to the new `vendor` field at component-construction time:

```ts
emit('update:components', [
  ...props.components,
  {
    chartRepo,
    chartName:    app.slug_name,
    chartVersion: versions[0] || '1.0.0',
    vendor:       app.library === 'nvidia' ? 'nvidia' : 'suse',
  },
]);
```

`BlueprintComponent` in `types/blueprint-types.ts` gains a matching optional
`vendor?: 'suse' | 'nvidia'` field. No UI control is exposed for it — the
mapping is automatic and not user-editable in v1.

### 3. Secret-injection profiles in the operator

Refactor the per-vendor logic into a small interface in `blueprint.go`:

```go
// secretInjector configures Helm values for a blueprint component so its
// rendered workloads can pull images and access vendor APIs. Each implementation
// owns the namespace-scoped Secret objects it requires and the Helm-values
// paths it writes.
type secretInjector interface {
    // Apply ensures the required secrets exist in targetNamespace and mutates
    // vals to reference them. A nil or no-op Apply (e.g., missing credentials)
    // is acceptable; Helm will surface the resulting ImagePullBackOff downstream.
    Apply(ctx context.Context, targetNamespace string, repoInfo clusterRepoInfo, vals map[string]any) error
}

func (r *AIWorkloadReconciler) injectorFor(vendor aiplatformv1alpha1.ComponentVendor) secretInjector {
    switch vendor {
    case aiplatformv1alpha1.ComponentVendorNvidia:
        return &nvidiaInjector{r: r}
    default:
        return &suseInjector{r: r}
    }
}
```

Both `ensureBlueprintHelmOp` and `ensureBlueprintGitFile` are refactored to:
1. Build `vals` from `c.Values` as today.
2. `injectorFor(c.Vendor).Apply(ctx, w.Spec.TargetNamespace, repoInfo, vals)`.
3. Continue with HelmOp patching / git file publishing.

#### 3.1 `suseInjector` — current behavior, unchanged

Encapsulates the existing logic verbatim:
- Calls the (renamed-but-otherwise-unchanged) `ensureCombinedPullSecret`
  which writes `suse-ai-pull-combined` covering chart-repo + App Collection
  + SUSE Registry + Nvidia auths.
- Writes `imagePullSecrets: [{name: suse-ai-pull-combined}]` and
  `global.imagePullSecrets: [{name: suse-ai-pull-combined}]`.

Regression guarded by the existing `TestEnsureCombinedPullSecret_*` tests,
which we keep passing untouched.

#### 3.2 `nvidiaInjector` — new

```go
type nvidiaInjector struct{ r *AIWorkloadReconciler }

const (
    nvidiaImagePullSecretName = "ngc-secret"
    nvidiaAPISecretName       = "ngc-api"
    nvidiaAPISecretKey        = "NGC_API_KEY"
)
```

`Apply` steps:

1. Read `Settings.Spec.Nvidia.UserSecretRef` + `Settings.Spec.Nvidia.TokenSecretRef`
   from the operator namespace. If either is missing or unreadable, log a
   warning and return nil (no-op, Helm will surface ImagePullBackOff).
2. Resolve registry host: `Settings.Spec.RegistryEndpoints.Nvidia` if set,
   else `defaultNvidiaHost` (`nvcr.io`). Reuse the same constant the existing
   combined-secret code uses.
3. Patch `ngc-secret` (kubernetes.io/dockerconfigjson) in `targetNamespace`:
   ```json
   { "auths": { "<host>": { "auth": "<base64(user:token)>", "username": "<user>", "password": "<token>" } } }
   ```
4. Patch `ngc-api` (Opaque) in `targetNamespace` with
   `data["NGC_API_KEY"] = <token bytes>`.
5. Mutate `vals` (belt-and-suspenders — see Section 4 for shape variability
   reasoning). The two paths use deliberately different shapes to match the
   conventions of each chart family:
   - `vals["imagePullSecrets"]` ← list of objects `[{name: "ngc-secret"}]`.
     This is the standard k8s pod-spec shape consumed by NIM LLM, NIM VLM,
     Riva, Cosmos, Multimodal Safety, and similar charts.
   - `vals["image"]["pullSecrets"]` ← flat list of strings `["ngc-secret"]`.
     This is the k8s-nim-operator chart's shape; its template iterates the
     list as strings, not objects.
   - Do **not** touch `global.*`.

Both `Patch` calls use `client.Apply` with `client.FieldOwner("suse-ai-operator")`
matching the existing pattern, so re-reconcile is idempotent.

### 4. Values merge semantics

NVIDIA charts use at least three pull-secret shapes:

| Chart family | Path |
|---|---|
| `k8s-nim-operator` | `image.pullSecrets: [string]` |
| Standalone NIM (LLM, VLM, Riva, Cosmos, Safety) | `imagePullSecrets: [{name}]` |
| NeMo-Retriever | `service.imagePullSecrets[].name` + `nimOperator.<key>.image.pullSecrets` |

The injector writes the two top-level shapes unconditionally. Charts that
read only one ignore the other harmlessly. Charts whose shape is none of the
above (e.g., NeMo-Retriever's nested paths, or the NVIDIA RAG umbrella
chart's `imagePullSecret.password` setter) are handled by the blueprint
author in the component's `values:` block — our injection coexists.

Merge rules per path:

- **Path absent** — create it with `[ngc-secret]` / `[{name: ngc-secret}]`.
- **Path present and our secret already in it** — leave unchanged
  (idempotency under re-reconcile after author edits).
- **Path present with other entries** — prepend our entry. Author intent is
  preserved; pull from `nvcr.io` is guaranteed.
- **Path present but wrong type** (e.g., author wrote a string where we
  expect objects) — leave unchanged and log a warning. The author has
  effectively expressed "don't manage this path"; we don't fight them.

`ngc-api` has no values path written. Every NIM chart surveyed either
defaults `*.ngcAPISecret` to the literal string `ngc-api` or hardcodes the
name in templates. If a future chart requires an explicit reference, the
blueprint author sets it in `values:`.

### 5. Error handling

- **Missing NVIDIA credentials** (no `Settings.Nvidia.UserSecretRef` or
  `TokenSecretRef`, or referenced secret missing): log a warning with the
  target namespace and skip secret creation. Reconcile continues; Helm
  surfaces `ImagePullBackOff` for the missing secrets in cluster status.
  Matches current `ensureCombinedPullSecret` posture.
- **Secret patch failure**: return error from injector. The reconcile loop
  retries. Matches current pattern (`ensureCombinedPullSecret` errors bubble
  up).
- **NGC token secret references a non-existent key**: log a warning, return
  nil (same as missing creds). The user fixes Settings and the next
  reconcile picks it up.
- **Vendor enum mismatch** (e.g., CRD validation bypassed via raw API):
  CRD enum validation rejects unknown values at admission time. In-controller
  switch defaults to `suseInjector` if a value somehow slips through.

### 6. RBAC

The reconciler already has
```
+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
```
but `ensureCombinedPullSecret` already issues `Patch` calls today, which
implies the verb is granted somewhere (Helm/ClusterRoleBinding or an
omission in the marker comments). We extend the existing marker to include
`create;patch;update` if the runtime RBAC also needs it. Audit during
implementation; if the existing combined-secret patch works in-cluster, no
change is needed.

### 7. Testing

All tests in `suse-ai-operator/internal/controller/aiworkload/`. Most extend
the existing `blueprint_pullsecret_test.go`; a new `nvidia_injector_test.go`
file is acceptable if the suite grows beyond ~250 lines.

**Regression guards (existing tests, must continue passing):**
- `TestEnsureCombinedPullSecret_IncludesNvidia`
- `TestEnsureCombinedPullSecret_NvidiaHostOverride`

**New tests for `nvidiaInjector`:**
- `TestNvidiaInjector_CreatesBothSecrets` — both `ngc-secret` (correct type
  + dockerconfigjson contents with base64 auth) and `ngc-api` (Opaque +
  `NGC_API_KEY` data key) exist in target namespace after Apply.
- `TestNvidiaInjector_HostOverride` — `Settings.RegistryEndpoints.Nvidia="registry.example.com"`
  → dockerconfigjson `auths` map keys on that host, no `nvcr.io` entry.
- `TestNvidiaInjector_NoCreds_NoOp` — Settings without Nvidia refs → Apply
  returns nil, no secrets created, values untouched.
- `TestNvidiaInjector_MissingTokenSecret` — Settings references a token
  secret that doesn't exist → no-op + warning, returns nil.
- `TestNvidiaInjector_WritesBothPathShapes` — after Apply, `vals` has both
  `imagePullSecrets: [{name: "ngc-secret"}]` and
  `image.pullSecrets: ["ngc-secret"]`.
- `TestNvidiaInjector_PreservesAuthorPullSecrets` — author pre-populated
  `image.pullSecrets: ["custom"]` → result is `["ngc-secret", "custom"]`.
- `TestNvidiaInjector_IdempotentSelfEntry` — Apply twice → secret entries
  not duplicated.
- `TestNvidiaInjector_DoesNotTouchGlobal` — `global` key absent from `vals`
  after Apply (unless the author already set it for other reasons; in that
  case left untouched).

**Dispatcher tests:**
- `TestInjectorFor_VendorNvidia` — returns `*nvidiaInjector`.
- `TestInjectorFor_VendorSUSE` — returns `*suseInjector`.
- `TestInjectorFor_VendorEmpty` — returns `*suseInjector` (defensive default;
  in practice the CRD default fills this in).

**End-to-end via the existing envtest suite:** the `suite_test.go` envtest
already loads CRDs from `config/crd/bases/`. After regenerating CRDs, add
or extend a Blueprint-reconciler test that drives a 2-component blueprint
(one `vendor: suse`, one `vendor: nvidia`) and asserts the resulting
HelmOp `spec.helm.values` contains the right keys for each.

## Migration / rollout

- New `Vendor` field defaults to `"suse"` via CRD default. Existing Blueprint
  CRs read back as `"suse"` after CRD upgrade; reconcile loop produces
  byte-identical HelmOps for them.
- Frontend wizard auto-fills `vendor` for newly added components. Existing
  blueprints loaded for edit retain whatever was persisted; editing a
  component does not change its vendor.
- No data migration required.
- CRD update needs `make manifests` + `make generate` (controller-gen
  regenerates deepcopy and CRD YAML in both `config/crd/bases/` and the
  chart's `crds/` directory). Both updated CRD YAMLs must land in the
  PR together.

## Files touched

**`suse-ai-operator/`:**
- `api/v1alpha1/blueprint_types.go` — add `ComponentVendor` + field.
- `api/v1alpha1/zz_generated.deepcopy.go` — regenerated.
- `config/crd/bases/ai-platform.suse.com_blueprints.yaml` — regenerated.
- `internal/controller/aiworkload/blueprint.go` — refactor: extract
  `secretInjector` interface, `suseInjector` (wrapping existing logic),
  `nvidiaInjector` (new), `injectorFor` dispatch. Both `ensureBlueprintHelmOp`
  and `ensureBlueprintGitFile` call the dispatcher instead of inlining.
- `internal/controller/aiworkload/blueprint_pullsecret_test.go` — new tests
  per Section 7.

**`charts/suse-ai-operator/`:**
- `crds/ai-platform.suse.com_blueprints.yaml` — regenerated.

**`pkg/suse-ai-lifecycle-manager/`:**
- `types/blueprint-types.ts` — add optional `vendor?: 'suse' | 'nvidia'`.
- `pages/components/wizard/BlueprintAppSelectorStep.vue` — set `vendor`
  from `app.library` in `addApp`.

## Open questions

None at write time; all decisions captured. Future refinements (per-chart
values path overrides, per-component namespaces, additional vendors) are
deferred until a concrete need arises.
