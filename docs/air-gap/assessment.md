# Air-gap code assessment

Reviewed areas include the operator and UI charts, Settings API/controller,
InstallAIExtension controller, App and Blueprint journeys, Fleet HelmOp/GitRepo
generation, credential delivery, static/dynamic catalog behavior, bundled
Blueprints, and the existing `suse-ai-stack/air-gapped-install` scripts.

## What is already usable

- `Settings.spec.registryEndpoints` redirects AppCo, SUSE, and NVIDIA chart
  catalogs to private endpoints. The NVIDIA field is explicitly chart-only;
  image redirection remains a node concern.
- The Settings controller creates authenticated Rancher `ClusterRepo` objects
  and copies basic-auth secrets into both Fleet workspaces.
- Helm-sourced `InstallAIExtension` supports Basic/Bearer/docker-config auth,
  private CA, mTLS, and a gated insecure mode. This is sufficient for a combined
  operator+UI install from authenticated Harbor.
- The UI chart supports standalone installation, and both charts expose their
  own image registry/repository/tag controls.
- Blueprint FleetBundle and GitOps paths can target local and downstream Fleet
  workspaces. App paths support direct Helm locally and Fleet strategies across
  clusters.
- The operator can serve a bundled application catalog without fetching the
  catalog JSON at runtime.

These primitives are necessary, but they do not cover artifact acquisition,
node configuration, all TLS trust domains, or the complete user journey.

## Prioritized gaps

| ID | Priority | Confirmed gap and impact | Suggested implementation |
|---|---|---|---|
| AG-001 | P0 | There is no authoritative, versioned bill of materials for AIF plus selected apps, chart dependencies, hooks/init containers, multiple architectures, model artifacts, and Rancher system images. The old stack scripts scrape default Helm output, skip failures, flatten image names, and do not verify destination digests. A disconnected install can therefore fail late or silently use stale content. | Publish a release-owned artifact manifest and supported mirroring CLI. Resolve exact charts with the customer values, vendor dependencies, record source manifests/digests/signatures/SBOMs, preserve multi-arch indexes, verify the destination, and emit the matching RKE2/Rancher configuration. Treat “artifact missing” as fatal. |
| AG-002 | P0 | AIF-generated Fleet Helm auth secrets contain only `username/password`; Fleet reads a `cacerts` key for a private chart CA. RKE2 containerd trust and Rancher's `additionalTrustedCAs` do not configure Fleet's Helm client. Consequently a normal private-CA Harbor HelmOp can fail after its ClusterRepo appears healthy. See [settings controller](../../operator/internal/controller/settings/settings_controller.go#L469). | Add a registry CA secret reference to each Settings registry. Mirror `cacerts` into `cattle-system`, `fleet-local`, and `fleet-default` auth secrets, reconcile rotation, and cover OCI/HTTPS ClusterRepo plus HelmOp with integration tests. Avoid global insecure-skip workarounds. |
| AG-003 | P0 | `FleetSettings.authType` advertises `ssh`, `token`, and `basic`, but the operator Git client reads one secret key and always sends HTTP Basic with username `token`; it has no custom CA or SSH host-key support. The generated Fleet GitRepo also omits its supported `caBundle` field. A typical authenticated, private-CA Gitea cannot work consistently. See [Settings type](../../operator/api/v1alpha1/settings_types.go#L31), [Git client](../../operator/internal/git/client.go#L52), and [GitRepo generation](../../operator/internal/controller/settings/settings_controller.go#L371). | Make Git auth a validated discriminated union: basic username/password, token with configurable username, or SSH key plus known-hosts. Add `caBundleSecretRef`, load it into go-git TLS roots, populate Fleet `spec.caBundle`, and test Gitea over private-CA HTTPS. Do not offer UI options the backend ignores. |
| AG-004 | P0 | NVIDIA uses one username/token pair for three different trust domains: private mirrored chart auth, `nvcr.io` image auth, and NGC API secrets. With Harbor credentials configured for the chart endpoint, the operator places the Harbor password into `ngc-api`; with an NGC token, Harbor chart pull auth fails. | Split NVIDIA settings into chart-repository credentials, image-registry credentials, and API/model credentials. Add an explicit image-registry endpoint/host. Validate combinations and never infer a Docker auth key from an OCI chart URL. |
| AG-005 | P0 | In air-gap mode the Settings controller deletes `nvidia-blueprints` and creates only `nvidia`, while shipped NVIDIA Blueprint components name `nvidia-blueprints`. Those Blueprints cannot resolve their charts. See [controller branch](../../operator/internal/controller/settings/settings_controller.go#L681) and [bundled Blueprint](../../charts/aif-operator/files/blueprints/nvidia-rag-minimal-2-6-0.yaml#L16). | Preserve both logical ClusterRepo names and allow each to point at a separate mirrored path, or rewrite bundled Blueprint references through a stable alias. Add a test that every bundled Blueprint component resolves against the Settings-produced repos in connected and mirrored modes. |
| AG-006 | P0 | NVIDIA `buildNGCDockerConfig` uses the full `registryEndpoints.nvidia` value as a Docker auth-map key, even though that setting is documented as an OCI chart URL; the SUSE combined-secret path correctly keeps NVIDIA images at `nvcr.io`. The two injectors disagree. See [NVIDIA injector](../../operator/internal/controller/aiworkload/blueprint.go#L470) and [SUSE injector](../../operator/internal/controller/aiworkload/blueprint.go#L592). | Introduce typed chart and image endpoints and normalize to a registry host before producing docker config. Use one shared endpoint resolver and contract tests for connected, mirror-host, and `oci://host/path` inputs. |
| AG-007 | P1 | The operator chart cannot initialize `registryEndpoints`, Git settings, registry CAs, catalog discovery, or image rewrite values. Operators must install, then mutate Settings through UI/API; the first reconciliation can create public ClusterRepos. | Add Settings bootstrap values (prefer secret refs, never inline production tokens) and render them into the chart-owned Settings CR on first install. Define merge/ownership semantics so later UI changes are not reverted on Helm upgrade. |
| AG-008 | P1 | `global.imageRegistry` on the operator chart changes the operator image but does not cascade into the nested UI chart installed by InstallAIExtension. Cleanup and CRD jobs also have independent image settings. An apparently complete override can still pull GHCR or SUSE Registry. | Provide a single documented air-gap image block and programmatically propagate it to manager, UI extension values, cleanup job, and CRD job. Add a Helm unit test that rejects public image references under an air-gap profile. |
| AG-009 | P1 | Static catalog entries contain public repository metadata and browser-loaded public logo URLs. It can show charts not present in the customer's mirror; logos generate browser egress attempts. A remote catalog cannot be hosted on internal Gitea/HTTP because the SSRF client deliberately rejects private addresses. See [catalog data](../../operator/internal/catalog/default-catalog.json#L1), [browser image](../../ui/pkg/aif-ui/pages/Apps.vue#L140), and [safe HTTP policy](../../operator/internal/infra/safehttp/safehttp.go#L17). | Ship logos as UI assets or proxy/cache an allow-listed catalog asset bundle. In air-gap mode, make live mirrored discovery authoritative and curated data enrichment-only. For remote catalogs, support an admin allow-list plus private CA rather than globally permitting private SSRF destinations. |
| AG-010 | P1 | `applicationCollectionAPI`, `catalogDiscovery`, and `imageRewrite` are persisted schema/UI fields but have no runtime consumer; the latter two controls are hidden with `v-if=false`. The `OFFLINE_MODE` feature flag is declared but unused. These imply protections that do not exist. | Either implement them end to end with tests or remove/hide them from the public contract and docs. Prefer node-level registry redirection as the baseline; chart-value rewrite can be an explicit complementary mechanism with a typed image-reference walker. |
| AG-011 | P1 | The Blueprint wizard deliberately disables direct Helm, although the CRD enum includes Helm and the requested qualification matrix includes Blueprint Helm. The Blueprint controller handles only FleetBundle and GitOps creation. See [wizard](../../ui/pkg/aif-ui/pages/components/BlueprintInstallWizard.vue#L255) and [controller switch](../../operator/internal/controller/aiworkload/blueprint.go#L118). | Decide the supported contract. Either implement local Blueprint Helm with transactional multi-chart lifecycle/status/rollback or document Blueprint Helm as unsupported and remove it from the qualification requirement/API choices for Blueprint sources. |
| AG-012 | P1 | Git-sourced InstallAIExtension has only repo/branch; it cannot express private repository authentication, CA trust, or SSH host verification. | Reuse the hardened Git settings/auth types for extension Git sources, or explicitly support only Helm-sourced extensions in disconnected installs. |
| AG-013 | P1 | There is no air-gap readiness/preflight view. Settings tests do not prove node mirror coverage, default-endpoint disablement, chart dependency completeness, browser assets, model downloads, or public egress rejection. | Add a read-only readiness API/UI: resolve every catalog/chart/image host, report CA/auth state, compare against approved mirrors, inspect RKE2 mirror/fallback state where available, and run opt-in pull probes. Display “disconnected-ready” only with evidence and explain chart-only versus image endpoints. |
| AG-014 | P1 | No product NetworkPolicy or egress policy bounds the operator, Fleet, or workload namespaces. A missed image/runtime URL will time out ambiguously and may leak when infrastructure egress is accidentally open. | Publish optional deny-by-default NetworkPolicies/Cilium policies with explicit Rancher, Kubernetes, DNS, Harbor, Gitea, and required internal service destinations. Keep infrastructure firewall enforcement as the primary boundary and expose denied-egress diagnostics. |
| AG-015 | P2 | AIF's bundled catalog and Blueprints include documentation links and workloads that can download models/configuration at runtime. Mirroring charts and container images alone is not enough for those workloads. | Add workload-specific “offline contract” metadata: required registries, model/object artifacts, licenses, initialization network calls, storage, and GPU needs. The mirror tool should consume the contract; the catalog should filter or warn when the profile is incomplete. |
| AG-016 | P2 | The installation docs do not make combined versus separate UI namespace/values, registry fallback behavior, Fleet/Git CA trust, transfer integrity, or browser isolation one coherent journey. | Adopt the proposed structure in [official-docs-plan.md](official-docs-plan.md), publish tested version matrices, and generate command snippets from maintained values/examples to prevent drift. |

## Required product semantics

The most robust design is to separate endpoints by function instead of by
vendor name:

| Function | Example | Consumer | Required trust/auth |
|---|---|---|---|
| Chart catalog | `oci://harbor/ac-charts` | Rancher ClusterRepo, Fleet Helm | Basic/robot auth + CA |
| Container images | original names redirected to `harbor/images/...` | containerd on every node | RKE2 mirror auth + CA, fallback disabled |
| Git write/read | `https://gitea/...git` | AIF go-git + Fleet GitRepo | Basic/token/SSH + CA/known-hosts |
| Vendor API/model store | internal object/model service or none | workload init/runtime | distinct API credential + CA |
| Catalog metadata/assets | bundled or internal approved URL | AIF API + browser | bundled assets or allow-listed CA-aware fetch |

This avoids the current NVIDIA ambiguity and prevents a chart-registry password
from being reused as an application API key.

## Suggested implementation order

1. Fix AG-002 through AG-006 and add private-CA Harbor/Gitea integration tests.
2. Publish the artifact contract/tooling (AG-001), including AIF/Rancher images.
3. Add install-time Settings and a single air-gap values profile (AG-007/008).
4. Make catalog/readiness behavior honest and deterministic (AG-009/010/013).
5. Resolve the Blueprint Helm support decision and document the supported
   matrix before qualification begins.
