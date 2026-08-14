# Proposed official air-gap documentation

The existing SUSE AI Factory air-gap requirements/workflow pages are a useful
starting point, but the supported guide should be executable and organized by
trust boundary rather than as a list of endpoints.

## Recommended chapter structure

1. **Scope and supported matrix**
   - tested AIF, Rancher, RKE2, Fleet, architecture, Harbor, and Git versions;
   - supported combined/separate UI installs and App/Blueprint strategies;
   - explicit GPU/model exclusions versus control-plane coverage.
2. **Plan the disconnected topology**
   - connected seed, transfer medium, private registry, private Git, management,
     downstream clusters, DNS/NTP, proxy/no-proxy, and browser boundary;
   - certificates, scoped robot accounts, capacity, and retention.
3. **Build the artifact manifest**
   - Rancher/RKE2 system images and bundled system charts;
   - AIF operator, UI, cleanup and CRD images;
   - exact AppCo/SUSE/NGC charts, dependencies, hooks/init containers, images,
     models/object artifacts, signatures/SBOMs, and architectures;
   - export, checksum, transfer, import, and destination digest verification.
4. **Configure every RKE2 node**
   - `/etc/rancher/rke2/registries.yaml`, auth and CA;
   - rewrite layout and `disable-default-registry-endpoint: true`;
   - restart order and a source-named `crictl pull` verification.
5. **Install Rancher for air-gap use**
   - `systemDefaultRegistry`, `useBundledSystemChart`, Rancher chart/system image
     list, private CA, and no-proxy/private DNS requirements.
6. **Install AI Factory**
   - combined operator+UI values from private OCI, including nested UI image and
     cleanup/CRD job overrides;
   - separate operator and standalone UI values/namespaces;
   - upgrade/rollback and CRD lifecycle.
7. **Configure AI Factory Settings**
   - distinct chart, image, Git, catalog, and vendor API endpoints;
   - secret and CA formats, rotation, NVIDIA chart-versus-image explanation;
   - dynamic mirrored discovery versus static catalog behavior.
8. **Configure Fleet GitOps**
   - private Gitea URL, branch, Basic/token/SSH contract, CA/known-hosts, Fleet
     workspace secrets, and GitRepo readiness.
9. **Validate user journeys**
   - App Helm/FleetBundle/GitOps, Blueprint supported strategies, local and
     downstream targets, expected resources/status, and a CPU-only smoke chart;
   - host, pod, and browser negative-egress tests.
10. **Troubleshoot and collect evidence**
    - missing image versus fallback attempt, x509, 401/403, ClusterRepo index,
      Fleet HelmOp/GitRepo, pull-secret host keys, static external assets, and
      runtime model downloads.

## Documentation rules

- Every command pins versions and uses placeholders that cannot be mistaken for
  usable credentials.
- Never recommend insecure TLS as the normal air-gap path.
- Say which process consumes each CA/secret; “install the CA on the node” is not
  sufficient for Rancher, Fleet, the operator, containerd, and the browser.
- Separate connected-stage commands from disconnected-stage commands and mark
  the exact point at which egress is removed.
- Include a negative public-egress assertion. Successful installation while an
  upstream fallback remains open is not proof.
- Generate artifact tables and values snippets from release-owned files when
  possible, with CI drift tests.
- Link the general Rancher and RKE2 air-gap procedures instead of duplicating
  their full lifecycle, but state the AIF-specific additions.

## Upstream references to align with

- [RKE2 private registry configuration](https://docs.rke2.io/install/private_registry)
- [RKE2 air-gap installation](https://docs.rke2.io/install/airgap)
- [Rancher air-gapped Helm installation](https://ranchermanager.docs.rancher.com/v2.14/getting-started/installation-and-upgrade/other-installation-methods/air-gapped-helm-cli-install/install-rancher-ha)
- [Fleet GitRepo authentication and CA options](https://fleet.rancher.io/next/how-tos-for-users/gitrepo-add)
- [Harbor Helm installation](https://goharbor.io/docs/edge/install-config/harbor-ha-helm/)
- [Harbor HTTPS configuration](https://goharbor.io/docs/main/install-config/configure-https/)
- [Gitea Kubernetes installation](https://docs.gitea.com/next/installation/install-on-kubernetes/)
- [NVIDIA AI Enterprise registry mirroring](https://docs.nvidia.com/aicr/user-guide/air-gapped-mirroring)

Before publishing, replace `next`/`edge` links with the versions qualified by
release QA, and link to the maintained SUSE AI Factory artifact manifest.
