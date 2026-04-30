# Air-Gap Installation and Operation

> **Audience:** Cluster administrators installing or operating SUSE AI Factory in an air-gapped Kubernetes cluster (no internet access, all dependencies served from a customer-internal OCI registry).
> **Scope:** End-to-end procedure: from "tarball downloaded" to "first NIM workload running."

---

## 1. What air-gap means in AIF

An **air-gapped cluster** has no internet access. It cannot reach `registry.suse.com`, `dp.apps.rancher.io`, `api.apps.rancher.io`, `nvcr.io`, `helm.ngc.nvidia.com`, `ghcr.io`, `quay.io`, `docker.io`, or any other public host. Everything AIF needs — operator image, UI extension, Helm charts, NIM containers, vendor Reference Blueprint charts — must come from a **customer-internal OCI registry** (e.g., Harbor, Quay, Nexus, Artifactory).

**AIF supports air-gap as a first-class deployment scenario.** There is no separate "Air-Gap Edition" of AIF; the same operator binary, the same UI extension, and the same workflows run in both connected and air-gapped clusters. **Air-gap is configuration**, exposed through:

- The operator Helm chart values: `image.registry`, `imagePullSecrets`, `webhook.tlsMode`
- The Settings CRD: `spec.registryEndpoints.*`, `spec.imageRewrite.*`, `spec.catalogDiscovery.applicationCollectionMode`, `spec.blueprintClassification.*`
- The customer's mirror process: every required image and chart pre-mirrored from SUSE's hosted registries into the customer's local registry

This document walks you through the end-to-end procedure. See `docs/spec/SOFTWARE_SPEC.md` §1 (Vision) and §9 Section 5 (Settings — Advanced) for the customer-facing description, `docs/spec/ARCHITECTURE.md` §4.5 (Settings CRD) and §16 (Air-Gap Release Bundle) for the engineering contract.

### What's air-gap-friendly out of the box

- **Single pull-secret pattern** — `suse-registry-creds` works against any docker-config-compliant registry; just point it at your local registry and adjust the auth.
- **No NGC at runtime** — AIF never reaches NVIDIA NGC; vendor assets come from your local registry only.
- **No internal OCI registry** — AIF doesn't host or proxy a registry; it consumes from the configured one. Your existing Harbor / Quay / Nexus is the registry.
- **Reference Blueprint detection via chart annotation** — the annotation `ai-platform.suse.com/role: reference-blueprint` is preserved through any digest-aware mirror tool; auto-wrapping just works.

### What isn't air-gap-friendly out of the box (and how AIF handles it)

| Concern | How AIF handles it |
|---|---|
| Operator image at `ghcr.io/suse/aif-operator` | Override via `--set image.registry=harbor.example.com/suse` (per `ARCHITECTURE.md §9.1`) |
| Cert-manager dependency for the validating webhook | Use `--set webhook.tlsMode=helm-hook` (no cert-manager required) |
| Hardcoded `registry.suse.com` in NIM-generated values | Settings.spec.registryEndpoints.suseRegistry override + Helm values merge layer 5 image rewrite |
| Hardcoded SUSE App Collection HTTP API | Settings.spec.catalogDiscovery.applicationCollectionMode = `registry-fallback` or `disabled` |

---

## 2. Pre-requisites

Before you begin:

1. **A reachable, customer-internal OCI registry** with push and pull credentials. Harbor, Quay, Nexus, Artifactory, AWS ECR, Azure ACR — any OCI-compliant registry works.
2. **A bastion host with internet access** to download the AIF release bundle and (separately) to mirror vendor assets from SUSE Registry. The bastion does not need to talk to the air-gapped cluster directly.
3. **A mirror tool**: `skopeo` (recommended; ships with the AIF release bundle's `mirror.sh`), or Harbor's built-in replication, or `oras`, or any OCI-aware copy tool that **preserves manifest digests and chart annotations**.
4. **A Rancher-managed Kubernetes cluster** in the air-gap perimeter (Rancher 2.10+).
5. **Either cert-manager pre-installed** in the cluster, **or** plans to use `webhook.tlsMode=helm-hook` (no extra dependency).
6. **Helm 3.13+** and **kubectl** on whatever workstation will run the install commands (typically the bastion or a workstation that can reach the cluster's kube-apiserver).

---

## 3. Step 1 — Download and mirror the AIF release bundle

The AIF release bundle is a self-contained `tar.gz` containing every image, chart, mirror script, and example values needed to install AIF. Per `docs/spec/ARCHITECTURE.md §16`, it includes:

```
aif-airgap-bundle-<version>.tar.gz
├── manifest.yaml                # every image and chart with sha256 digests
├── images/                      # OCI tarballs (multi-arch)
├── charts/                      # all Helm charts
├── values-airgap-example.yaml   # commented helm install example
├── mirror.sh                    # skopeo-based wrapper
└── README.md                    # one-page quick-start
```

### 3.1 Download on the bastion

```bash
# Replace <version> with the AIF release version (e.g., 1.0.0)
wget https://github.com/SUSE/aif/releases/download/v<version>/aif-airgap-bundle-<version>.tar.gz
wget https://github.com/SUSE/aif/releases/download/v<version>/aif-airgap-bundle-<version>.tar.gz.sig
wget https://github.com/SUSE/aif/releases/download/v<version>/aif-airgap-bundle-<version>.tar.gz.crt

# Verify the signature
cosign verify-blob \
  --certificate aif-airgap-bundle-<version>.tar.gz.crt \
  --signature aif-airgap-bundle-<version>.tar.gz.sig \
  aif-airgap-bundle-<version>.tar.gz

# Extract
tar xzf aif-airgap-bundle-<version>.tar.gz
cd aif-airgap-bundle-<version>/
```

### 3.2 Authenticate to your local registry

```bash
# For Harbor / generic OCI:
docker login harbor.example.com
helm registry login harbor.example.com

# For ECR/ACR/etc., use the appropriate auth flow.
```

### 3.3 Mirror everything

```bash
# Dry-run first (lists what will be pushed; no actual push)
./mirror.sh harbor.example.com/suse --dry-run

# Real push
./mirror.sh harbor.example.com/suse --parallel 4
```

`mirror.sh` uses `skopeo copy --all --preserve-digests` for images and `helm push` for charts. It verifies each pushed digest against `manifest.yaml` automatically. Re-running is idempotent (digest-aware no-op).

After completion, your registry will contain (under `harbor.example.com/suse/`):
- `aif-operator:<version>` (multi-arch)
- `aif-ui:<version>`
- `nim-llm:<version>`, `nim-vlm:<version>`, `generic-container:<version>`
- The five Helm charts as OCI artifacts

---

## 4. Step 2 — Mirror the AI workload assets

The release bundle ships AIF itself but **does not** include vendor Reference Blueprint charts (NVIDIA RAG, NVIDIA AIQ) or NIM model containers — those have separate licensing terms and release cadences. You mirror them independently from SUSE Registry.

> **Cadence note:** Re-mirror on a cadence aligned with NIM and Reference Blueprint release notifications. A weekly cadence plus event-driven re-mirroring on NVIDIA release announcements is a reasonable baseline. AIF doesn't enforce a cadence — this is operational judgment. See `docs/spec/ARCHITECTURE.md §13.1` Customer-side re-mirroring contract.

### 4.1 Authenticate to SUSE Registry on the bastion

```bash
# Use the SUSE Registry credentials provided by your SUSE subscription
skopeo login --username '<your-suse-user>' registry.suse.com
helm registry login --username '<your-suse-user>' registry.suse.com
```

### 4.2 Mirror NIM container images

```bash
# Example: mirror Llama 3.1 8B Instruct (one tag at a time, or use --all to mirror everything)
skopeo copy --all --preserve-digests \
  docker://registry.suse.com/ai/containers/nvidia/llama-3.1-8b-instruct:1.0.0 \
  docker://harbor.example.com/suse/ai/containers/nvidia/llama-3.1-8b-instruct:1.0.0

# Repeat for each NIM you want to make available. List available NIMs:
curl -u '<user>:<token>' https://registry.suse.com/v2/_catalog | \
  jq '.repositories[] | select(startswith("ai/containers/nvidia/"))'
```

### 4.3 Mirror NIM Helm charts

```bash
skopeo copy --all --preserve-digests \
  docker://registry.suse.com/ai/charts/nvidia/nim-llm:1.0.0 \
  docker://harbor.example.com/suse/ai/charts/nvidia/nim-llm:1.0.0
# Repeat for nim-vlm and any additional NVIDIA charts you use.
```

### 4.4 Mirror vendor Reference Blueprint charts

```bash
skopeo copy --all --preserve-digests \
  docker://registry.suse.com/ai/charts/nvidia/rag:1.2.0 \
  docker://harbor.example.com/suse/ai/charts/nvidia/rag:1.2.0
# Repeat for nvidia/aiq and any other Reference Blueprints you want to make available.
```

> **Critical:** `skopeo copy --all --preserve-digests` preserves the `ai-platform.suse.com/role: reference-blueprint` chart annotation. AIF relies on this annotation to detect Reference Blueprints and auto-wrap them as AIF Blueprints. If your mirror tool strips chart annotations, vendor-chart wrapping won't work. See `docs/spec/ARCHITECTURE.md §13.1`.

### 4.5 Mirror SUSE Application Collection charts (optional)

If you want SUSE Application Collection apps (Milvus, Ollama, vLLM, etc.) available:

```bash
skopeo copy --all --preserve-digests \
  docker://dp.apps.rancher.io/charts/milvus:4.1.2 \
  docker://harbor.example.com/suse/charts/milvus:4.1.2
# Repeat for each SUSE App Collection chart you want.

# To enumerate what's available in SUSE App Collection:
curl -u '<user>:<token>' https://api.apps.rancher.io/v1/applications?packaging_format=HELM_CHART | \
  jq '.data[].slug_name'
```

---

## 5. Step 3 — Install AIF

### 5.1 Create the operator pull-secret

```bash
kubectl create namespace aif

kubectl create secret docker-registry harbor-pull-secret \
  --namespace=aif \
  --docker-server=harbor.example.com \
  --docker-username='<harbor-user>' \
  --docker-password='<harbor-token>'
```

### 5.2 Install the operator

Use the example values from the release bundle as a starting point:

```bash
# Inspect the example
cat values-airgap-example.yaml

# Customize for your environment, then install
helm install aif-operator harbor.example.com/suse/aif-operator-<version>.tgz \
  --namespace aif \
  --values values-airgap-example.yaml \
  --set image.registry=harbor.example.com/suse \
  --set 'imagePullSecrets[0].name=harbor-pull-secret' \
  --set webhook.tlsMode=helm-hook
```

The minimal air-gap-relevant values:
- `image.registry=harbor.example.com/suse` — pull the operator image from your registry
- `imagePullSecrets[0].name=harbor-pull-secret` — auth for the operator pod
- `webhook.tlsMode=helm-hook` — generate self-signed TLS without cert-manager (skip if you have cert-manager pre-mirrored)

### 5.3 Install the UI extension

```bash
helm install aif-ui harbor.example.com/suse/aif-ui-<version>.tgz \
  --namespace aif \
  --set image.registry=harbor.example.com/suse \
  --set 'imagePullSecrets[0].name=harbor-pull-secret'
```

### 5.4 Verify the operator is running

```bash
kubectl get pods -n aif
# Expect: aif-operator-... Running 1/1
kubectl get validatingwebhookconfiguration aif-blueprint-immutability
kubectl get clusterrole aif-blueprint-publisher
```

---

## 6. Step 4 — Configure Settings

Open Rancher → AI Factory in the sidebar → Settings.

### 6.1 Toggle Advanced

In the Settings page header, flip the **Advanced** toggle on. Section 5 — Registry Endpoints (Advanced) appears below Section 4.

### 6.2 Configure registry endpoints

| Field | Air-gap value |
|---|---|
| SUSE Registry endpoint | `harbor.example.com` |
| SUSE Application Collection endpoint | `harbor.example.com` |
| SUSE Application Collection API | leave blank (disable HTTP catalog) **or** point at your internal mirror's API if you have one |
| Catalog Discovery Mode | `Registry Fallback` (recommended) or `Disabled` |
| Image Rewrite Rules | **Add two rules:**<br>1. Match: `registry.suse.com/` → Replace: `harbor.example.com/suse/`<br>2. Match: `dp.apps.rancher.io/` → Replace: `harbor.example.com/suse/` |

### 6.3 Configure SUSE Registry credentials (Section 3)

Enter your Harbor credentials in Section 3 — SUSE Registry. AIF reuses this Secret as the workload pull secret, automatically reconciled into every workload namespace.

### 6.4 Test connection

Click **Test Connection** in Section 5. You should see green checkmarks for every endpoint (with millisecond latencies). Red X means the endpoint isn't reachable from the operator pod — fix before saving.

### 6.5 Save

Click **Save Settings**. The "Custom registry endpoints active" chip appears in the page header (it stays visible regardless of the Advanced toggle state, so anyone glancing at Settings knows the cluster is in a non-default configuration).

### 6.6 Bind a Blueprint Publisher

Air-gap doesn't change the Blueprint Publisher designation flow. See `PUBLISHERs.md` for binding the `aif-blueprint-publisher` ClusterRole.

---

## 7. Step 5 — Verify

### 7.1 Apps catalog populates

Navigate to AI Factory → Apps. Expect to see your mirrored NIMs and SUSE App Collection charts. If empty:
- Check Settings → Advanced → Test Connection
- Check the operator pod logs: `kubectl logs -n aif deploy/aif-operator | grep -i discovery`

### 7.2 Blueprints populates

Navigate to AI Factory → Blueprints. Expect to see auto-wrapped vendor Reference Blueprints (e.g., NVIDIA RAG) — one card per Blueprint name, with a version selector showing each mirrored chart version.

### 7.3 Smoke deploy a NIM

From the Apps page, click Install on a NIM (e.g., Llama 3.1 8B). Walk the Install & Deploy wizard, confirm a target cluster, click Install. Then on the Workloads page:

```bash
# Check the resulting pod is pulling from your registry, not registry.suse.com
kubectl get pod -n aif-workloads -o jsonpath='{.items[*].spec.containers[*].image}' | tr ' ' '\n'
# Expect: harbor.example.com/suse/ai/containers/nvidia/llama-3.1-8b-instruct:1.0.0
```

If the pod's `image:` field shows `registry.suse.com/...` instead of `harbor.example.com/...`, your image rewrite rules aren't being applied. Check Settings → Advanced → Image Rewrite Rules.

### 7.4 Compose, test-deploy, and publish a Bundle

1. Navigate to Bundles → New Bundle. Compose a small Bundle with one or two components.
2. Click **Test Deploy** to verify it runs end-to-end with the rewritten image references.
3. Click **Pre-flight Check** to confirm all charts and images are resolvable in your registry. Green checkmark expected.
4. Click **Submit for Review**, then have your designated publisher Approve. The Bundle becomes a published AIF Blueprint version.

---

## 8. Day-2 ops

### 8.1 Re-mirror on each AIF release

When SUSE releases a new AIF version:

```bash
wget https://github.com/SUSE/aif/releases/download/v<new-version>/aif-airgap-bundle-<new-version>.tar.gz
tar xzf aif-airgap-bundle-<new-version>.tar.gz
./mirror.sh harbor.example.com/suse
helm upgrade aif-operator harbor.example.com/suse/aif-operator-<new-version>.tgz --namespace aif --reuse-values
helm upgrade aif-ui harbor.example.com/suse/aif-ui-<new-version>.tgz --namespace aif --reuse-values
```

### 8.2 Re-mirror on NIM / Reference Blueprint release notifications

Subscribe to NVIDIA NIM release notifications. When a new model or Blueprint version is announced and SUSE has mirrored it into SUSE Registry:

```bash
# Mirror the new NIM
skopeo copy --all --preserve-digests \
  docker://registry.suse.com/ai/containers/nvidia/<new-model>:<version> \
  docker://harbor.example.com/suse/ai/containers/nvidia/<new-model>:<version>

# Mirror the new Reference Blueprint chart (annotation preserved automatically)
skopeo copy --all --preserve-digests \
  docker://registry.suse.com/ai/charts/nvidia/<chart>:<version> \
  docker://harbor.example.com/suse/ai/charts/nvidia/<chart>:<version>

# AIF auto-wraps the new chart as a Blueprint version on its next refresh cycle (default 10m).
# Force an immediate refresh from Settings → Section 3 → Refresh NIM Index Now.
```

### 8.3 Rotate Harbor credentials

Update the `suse-registry-creds-source` Secret in the `aif` namespace; the SettingsReconciler propagates the change into every workload namespace within one reconcile cycle.

```bash
kubectl create secret generic suse-registry-creds-source \
  --namespace=aif \
  --from-literal=username='<new-user>' \
  --from-literal=token='<new-token>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 8.4 Troubleshooting

**Apps page shows "Cannot reach SUSE Registry":**
- Settings → Advanced → Test Connection. Red X identifies the unreachable endpoint.
- Check the operator pod logs for the actual error: `kubectl logs -n aif deploy/aif-operator | grep -i 'registry\|discovery'`
- Verify your NetworkPolicy allows egress from the `aif` namespace to your local registry (see §9 below).

**Blueprints page shows "Cannot refresh wrapped Reference Blueprints":**
- Same root cause as above; Apps and Blueprints share the same registry-discovery code path.

**Workload pods show `ErrImagePull` referencing `registry.suse.com/...`:**
- Image rewrite rules aren't being applied. Verify Settings → Advanced → Image Rewrite Rules contains `registry.suse.com/` → `harbor.example.com/suse/`.
- Verify `imageRewrite.enabled` is implicitly true (it's true when rules are present).

**Bundle Pre-flight Check shows "missing items":**
- Some referenced charts or images aren't mirrored yet. The pre-flight result includes copy-pasteable `skopeo copy` commands; run them on the bastion, then re-run pre-flight.

**Webhook denies a Blueprint patch with "x509: certificate signed by unknown authority":**
- The `webhook.tlsMode=helm-hook` cert wasn't propagated correctly. `helm upgrade aif-operator ... --reuse-values --set webhook.tlsMode=helm-hook` to re-trigger the cert-generation hook.

---

## 9. Reference: every URL/host AIF tries to reach

By component, when running with **default** Settings:

| Component | Reaches | Settings field that overrides it |
|---|---|---|
| `pkg/nvidia/discovery` | `https://registry.suse.com/v2/_catalog` and `/v2/<chart>/tags/list` | `Settings.spec.registryEndpoints.suseRegistry` |
| `pkg/blueprint/wrapper` | `oci://registry.suse.com/ai/charts/nvidia/<chart>:<version>` (HEAD on each chart's `Chart.yaml` for annotation read) | `Settings.spec.registryEndpoints.suseRegistry` |
| `pkg/source_collection/client` | `https://api.apps.rancher.io/v1/applications` | `Settings.spec.registryEndpoints.applicationCollectionAPI` + `catalogDiscovery.applicationCollectionMode` |
| Helm engine (`pkg/helm`) on chart pull | `oci://registry.suse.com/...`, `oci://dp.apps.rancher.io/...` | `Settings.spec.registryEndpoints.{suseRegistry, applicationCollection}` + `imageRewrite.rules` |
| Workload pods (image pulls) | Whatever `image.repository` resolves to after Helm values merge layer 5 (`pkg/helm.ApplyImageRewrites`) | `Settings.spec.imageRewrite.rules` |
| Fleet engine (`pkg/git/fleet`) | Configured Git repo URL (e.g., `git@gitlab.example.com:aif-config.git`) | `Settings.spec.fleet.repoURL` (already customer-controlled) |
| Validating webhook | kube-apiserver only (intra-cluster) | n/a |
| Operator pod itself | kube-apiserver, the configured registries, the Fleet Git host | (same as above) |

Hosts AIF **never** reaches: `nvcr.io`, `helm.ngc.nvidia.com`, `integrate.api.nvidia.com`. These are accessed only by the SUSE-managed mirror process upstream of you (per `docs/spec/ARCHITECTURE.md §13.1`).

### Recommended NetworkPolicy egress allow-list

For an `aif` namespace NetworkPolicy:

```yaml
egress:
  - to:
    - namespaceSelector: { matchLabels: { name: kube-system } }   # kube-apiserver
  - to:
    - ipBlock: { cidr: <your-harbor-CIDR> }                       # local registry
    ports: [{ protocol: TCP, port: 443 }]
  - to:
    - ipBlock: { cidr: <your-internal-git-CIDR> }                 # Fleet Git (if used)
    ports: [{ protocol: TCP, port: 443 }, { protocol: TCP, port: 22 }]
  # NOTE: NGC hosts are intentionally NOT allow-listed.
```

---

## See also

- `docs/spec/SOFTWARE_SPEC.md` §1 (Vision — air-gap as first-class), §9 Section 5 (Settings — Advanced)
- `docs/spec/ARCHITECTURE.md` §4.5 (Settings CRD additions), §6.6 layer 5 (image rewrite), §9.1 (chart values), §13.1 (customer-side re-mirroring), §16 (Air-Gap Release Bundle)
- `PUBLISHERs.md` — Blueprint Publisher designation (independent of air-gap, but typically configured in the same install session)
