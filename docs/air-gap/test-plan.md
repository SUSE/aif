# Air-gap QA topology and test plan

## Topology

Use four logical roles. They may be VMs or small AWS instances; no role needs a
GPU.

```text
connected seed/controller
  |  checksummed media transfer (only before isolation)
  v
services RKE2: authenticated private-CA Harbor + authenticated Gitea
             ^                                      ^
             | private CIDRs                        | private CIDRs
management RKE2: Rancher + AIF  <------Fleet------ downstream RKE2
             ^
             |
optional isolated browser/test-runner
```

All schedulable management/downstream nodes must be listed in the lab inventory.
The gate is valid only when host-output and pod-forward negative probes pass on
every such node. Put a browser VM in `airgap_clients` for browser-side logo/link
testing; an unrestricted engineer laptop is not evidence of an air-gap journey.

The services node may remain connected during this first-pass lab because
Harbor is not a proxy cache and Gitea has no proxy. A higher-assurance variant
should isolate it after bootstrap and preload its own RKE2/Harbor/Gitea images.

## Environment implementation

The executable overlay is `/home/thbertoldi/suse/suse-ai-stack/airgap-lab`:

- `stack-cpu-only.example.yml` disables all GPU and optional AI workloads and
  prevents the base stack from installing AIF while connected;
- `artifacts.yml` is the explicit core/vendor transfer allow-list;
- `scripts/artifacts.sh` exports/imports charts and multi-arch images with
  source/destination digest checks and checksummed manifest/tool metadata;
- playbooks `00`–`02` create the service plane, trust CA, and configure RKE2;
- playbook `04` closes egress before playbook `03` installs AIF from Harbor in
  combined or separate mode; `99` removes only the lab-owned nftables table;
- playbook `05` runs a CPU-only Blueprint through FleetBundle or GitOps;
- playbook `06` proves internal availability, public host/pod rejection, mirror
  pulls, AIF readiness, and collects redacted evidence.

Follow the sibling lab README for variables and commands. The gate sets
`disable-default-registry-endpoint: true`; omitting a registry must fail closed.

## Qualification matrix

Run each case against one supported AIF/Rancher/RKE2/Fleet version tuple and
record it with the artifact manifest. “Control plane” for an NVIDIA case means
catalog discovery, values form, AIWorkload, HelmOp/Bundle or Git commit, chart
pull, secret shape, and a clear expected scheduling/runtime state. It does not
claim model serving without GPU/model artifacts.

| Source/journey | Helm | FleetBundle | GitOps | Targets |
|---|---|---|---|---|
| SUSE AppCo app | local install and status | local, downstream, mixed | local and downstream via Gitea | one then two clusters |
| SUSE Registry app | local install and status | local, downstream, mixed | local and downstream via Gitea | one then two clusters |
| NVIDIA app | control-plane only; no GPU readiness claim | control-plane only | control-plane + commit only | one then two clusters |
| Custom CPU Blueprint | currently unsupported: record AG-011 | automated and must become Running | automated and must become Running | local, downstream, mixed |
| Bundled NVIDIA Blueprint | currently blocked by AG-004/005/006 | expected-failure evidence | expected-failure evidence | control-plane only |

Cross the journey matrix with:

- AIF `combined` and `separate` operator/UI installation;
- dynamic mirrored discovery (recommended) and bundled static catalog;
- valid, missing, and rotated Harbor credentials;
- trusted and missing Harbor CA;
- Gitea authenticated HTTP baseline and private-CA HTTPS expected failure until
  AG-003 is fixed;
- x86_64 and, when supported, arm64 transfer manifests;
- local-only and one downstream Rancher cluster.

The App Helm/Fleet/GitOps cases must be driven from the Rancher UI (or a browser
automation suite) because App deployment resources are UI-owned. Directly
creating an AIWorkload does not exercise chart download, form rendering, or the
Rancher App action. The included playbook automates operator-owned Blueprint
paths and infrastructure assertions; it is not a substitute for those UI cases.

## Acceptance criteria

A positive case passes only if all of the following are true:

1. The transfer manifest and every file checksum pass; each imported image
   manifest digest equals its recorded source digest.
2. Harbor and Gitea require authentication where applicable and private CA
   verification remains enabled.
3. Every RKE2 node has all source registry mirrors plus
   `disable-default-registry-endpoint: true`.
4. AIF operator and UI images resolve directly to Harbor; cleanup/CRD jobs do
   not attempt a public registry.
5. Settings `observedGeneration` matches, all intended ClusterRepos are ready,
   and no unintended public ClusterRepo remains enabled.
6. The selected strategy creates the expected App/AIWorkload, HelmOp, Bundle,
   BundleDeployment, and/or Gitea commit in the correct Fleet workspace.
7. The CPU smoke Deployment is Ready on every selected target and its manifest
   retains `registry.suse.com/...`; Harbor/RKE2 evidence proves it was mirrored.
8. A public-IP TCP probe fails from every host and from a scheduled pod, while
   Harbor, Gitea, Rancher, Kubernetes, and internal DNS remain reachable.
9. An isolated browser renders AIF without successful public requests. External
   documentation links may be visibly unavailable; automatic logo/asset fetches
   are a defect.
10. Logs contain no successful public connection and failures identify a
    missing artifact/CA/credential rather than hanging indefinitely.

## Expected-failure cases

Keep known failures in the suite; do not weaken the lab to make them green:

- remove `cacerts` from a Fleet Helm auth secret and show the private-CA chart
  pull fails (AG-002);
- set `gitea_tls_enabled` and `smoke_expect_git_ca_failure`, then show AIF/Fleet
  cannot consume the Git CA through Settings (AG-003);
- deploy a bundled NVIDIA Blueprint with a mirrored endpoint and show the
  missing `nvidia-blueprints` identity (AG-005);
- use distinct Harbor and NGC credentials and show the single NVIDIA pair cannot
  represent both (AG-004);
- select Blueprint Helm and record that the UI disables it (AG-011);
- omit one RKE2 mirror and prove the pull fails without reaching upstream.

An expected failure passes only when it fails for the asserted reason and no
public egress succeeds.

## Evidence bundle

Retain the following per run:

- git revision of both repositories and exact version tuple;
- bundled `ARTIFACTS.yaml`, `SOURCE-DIGESTS.txt`, `SHA256SUMS`, and tool metadata;
- redacted RKE2 `registries.yaml`, fallback flag, containerd pull logs, node
  architecture, and nftables rules/counters;
- Harbor project/audit access and Gitea commit history;
- Helm releases, Settings, ClusterRepo/GitRepo/HelmOp/Bundle/AIWorkload status,
  pod images/image IDs, events, and operator/Fleet logs;
- browser HAR with secrets/cookies removed;
- result matrix with pass, expected fail, unexpected fail, and issue ID.

The playbooks fetch a redacted baseline into `airgap-lab/generated/evidence`.
Review it before sharing; browser and Harbor audit evidence are collected by the
test runner/environment, not automatically by the current playbook.
