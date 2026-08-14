# AI Factory air-gap spike

This first pass separates three conclusions that are easy to blur together:

1. AI Factory has useful disconnected primitives: private chart endpoints,
   credential references, authenticated/TLS-aware UI chart installation, Fleet
   deployment, and a bundled catalog.
2. The repository does not yet provide a complete, safe air-gap workflow. The
   most serious blockers are private-CA propagation to Fleet, NVIDIA endpoint
   and credential semantics, and Git TLS/auth support.
3. An executable, CPU-only lab now exists in the sibling `suse-ai-stack`
   checkout under `airgap-lab/`. It makes those gaps reproducible without
   pretending that NVIDIA models or GPU runtimes were tested.

No end-to-end environment was provisioned during this code-only pass: actual
VM/AWS addresses, SUSE/NGC/AppCo entitlements, and Rancher cluster IDs are not
available in the repository. A green syntax/render check is therefore not an
air-gap certification. The lab's `verify` phase is designed to produce the
evidence needed for that decision on real infrastructure.

## Documents

- [Code assessment and prioritized gaps](assessment.md)
- [QA topology, matrix, and execution playbook](test-plan.md)
- [Proposed official documentation structure](official-docs-plan.md)

## Working definition

For this spike, “air-gapped” means that management nodes, downstream workload
nodes, their pods, and the test browser cannot open a connection to a public IP.
They may reach explicitly allow-listed private networks containing Rancher,
Kubernetes, Harbor, Gitea, DNS, and the test controller. DNS resolution alone is
not treated as egress. Both host `output` and pod `forward` paths are tested.

The artifact seed is connected only before the gate closes. It exports a
checksummed transfer bundle. Harbor is authenticated, private, and protected by
a private CA; it is not a pull-through proxy. Gitea is authenticated and has no
upstream proxy. This prevents either service from silently masking missing
artifacts.

## First-pass verdict

The implementation is **promising but not currently supportable as a complete
air-gap journey without workarounds**. Combined and separate AIF installation
can be expressed from a private OCI registry, and node-level image redirection
is viable. Fleet chart pulls from a private-CA Harbor need a manual `cacerts`
patch, secure private-CA GitOps is not representable end to end, and the NVIDIA
air-gap model conflates credentials and repository identities. These are release
blocking for a documented, typical-customer workflow even if a permissive lab
can be made to pass.
