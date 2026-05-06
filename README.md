# SUSE AI Factory (AIF)

SUSE AI Factory is a Rancher Dashboard extension and a Kubernetes operator that
turn Rancher into an AI-platform management plane: discover AI building blocks,
compose them into approved stacks, publish those stacks under governance, and
deploy and operate them on any Rancher-managed cluster — including air-gapped
ones.

---

## What it does

Today, getting production AI onto Kubernetes usually means stitching together
Helm charts (NIM models, vector databases, vLLM, observability), copy-pasting
YAML between teams, and trusting that nothing has drifted. There is no shared
notion of "this is the RAG-with-Llama stack we approved last quarter."
Reproducing what someone else deployed is detective work, and governance is
informal.

AIF makes Rancher itself the catalog, workshop, governed publishing pipeline,
and deploy/operate console for AI workloads:

- **Catalog.** A single screen showing every approved AI building block —
  NVIDIA NIM models, vector databases, runtimes — sourced from SUSE Registry
  and SUSE Application Collection.
- **Workshop.** An ML practitioner picks components, tweaks Helm values, test-
  deploys to a sandbox, and iterates. Their work-in-progress lives privately
  in their own namespace.
- **Governed publishing.** When ready, the author submits the bundle. A
  designated Blueprint Publisher approves it, which mints an immutable,
  versioned reference stack that any team in the cluster can deploy with one
  click.
- **Deploy and operate.** Anyone can deploy that approved stack to any
  Rancher-managed cluster. Each running workload remembers what it came
  from, so the platform can offer "Upgrade to v2" or "this version is now
  Deprecated" without spreadsheets.
- **Air-gap parity.** The same workflow works disconnected. There is no
  separate "online edition" — air-gap is configuration, not a different
  product.

The business value is straightforward: AIF compresses
"discover → compose → review → publish → deploy → upgrade" for AI stacks from
a multi-team, multi-tool, mostly-manual process into a single Rancher-native
flow with an audit trail.

---

## The four-noun model

Everything in AIF is one of four things:

| Noun       | What it is                                                                         | Mutability                       | Scope             |
|------------|------------------------------------------------------------------------------------|----------------------------------|-------------------|
| **App**       | Building-block AI application packaged as a Helm chart in the SUSE catalog.     | Immutable                        | Catalog-wide      |
| **Bundle**    | Mutable workshop where an author composes Apps and existing Blueprints.         | Mutable; Draft → Submitted → Approved (or Changes Requested → Draft) | Namespaced |
| **Blueprint** | Published, immutable, versioned AI stack tied to a use case (e.g. RAG).         | Immutable per version            | Cluster-scoped    |
| **Workload**  | A running instance of an App or Blueprint on a target cluster.                  | Status-only                      | Workload namespace |

A Bundle becomes a Blueprint version through the publish-by-approval workflow.
Each Workload records its `spec.source` (App, Blueprint, or BundleTest), so
provenance is always recoverable.

> Note: NVIDIA and other vendors publish their own "Reference Blueprints" —
> these are Helm charts. AIF treats each as an App in the catalog and wraps it
> as a single-component AIF Blueprint so it shows up on the Blueprints page
> with the same versioning and governance. A vendor Reference Blueprint is
> not an AIF Blueprint; it's a chart that AIF wraps. See
> [`docs/spec/SOFTWARE_SPEC.md`](docs/spec/SOFTWARE_SPEC.md) §6.

---

## Architecture at a glance

AIF ships two cooperating pieces:

- A **Kubernetes operator** (`cmd/operator`) that owns the AIF CRDs in the
  `ai.suse.com` API group — `App`, `Bundle`, `Blueprint`, `Workload`,
  `Settings`, `InstallAIExtension` — and runs reconcilers, an admission
  webhook (Blueprint immutability), and a small REST API that the UI calls.
- A **Rancher Dashboard extension** (`ui/ai-factory`) built against
  `@rancher/shell`, surfacing the four nouns as a first-class product inside
  the Rancher Dashboard sidebar.

External integrations are intentionally narrow: AIF reads charts and images
from SUSE Registry (`registry.suse.com`) and SUSE Application Collection
(`dp.apps.rancher.io`), and optionally drives Rancher Fleet for GitOps
deployments. AIF does **not** host an internal registry and does **not** call
NVIDIA NGC directly — NIMs flow through SUSE Registry via an out-of-band
mirror process.

For the full design, see [`docs/spec/ARCHITECTURE.md`](docs/spec/ARCHITECTURE.md).

---

## Prerequisites

- **Go ≥ 1.26.** `go.mod` declares `go 1.26.0`. If your installed toolchain is
  older, set `GOTOOLCHAIN=auto` so the Go command can fetch a matching
  toolchain on demand.
- **Helm 3.13+** for chart operations.
- **Rancher 2.10+** (the UI extension is built against the Rancher Dashboard
  Shell of that era).
- **Kubernetes 1.24+** for the target cluster running the operator.
- **Node 18+ and yarn** if you intend to build the UI extension.
- A local Kubernetes cluster — the project standardises on **k3d** (k3s in
  Docker) for local dev. `make dev-cluster` will create one for you.

---

## Quick start — local dev

Bring up a local cluster, install the CRDs, run the operator from source, and
apply a sample Bundle.

```bash
# 1. Start a local k3d cluster
make dev-cluster

# 2. Install the CRDs
make dev-install

# 3. Build and run the operator against your kubeconfig
make build && ./bin/aif-operator

# 4. In another terminal, apply the sample CRs
make examples
kubectl get bundles,blueprints,workloads -A
```

The sample manifests live in [`examples/`](examples/) — minimal valid CRs
intended only to verify the controller reconciles cleanly. To tear the cluster
down: `make dev-cluster-down`.

To work on the UI extension:

```bash
cd ui/ai-factory
yarn install
yarn build
```

---

## Repository layout

```
aif/
├── api/v1alpha1/          # CRD Go types (Bundle, Blueprint, Workload, Settings, InstallAIExtension)
├── charts/                # Helm charts (aif-operator, aif-ui, generic-container, nim-llm, nim-vlm)
├── cmd/operator/          # Operator main entry point
├── docs/spec/             # SOFTWARE_SPEC.md, ARCHITECTURE.md, PROJECT_PLAN.md
├── internal/
│   ├── api/               # REST handlers (one per resource group)
│   ├── controller/        # Kubernetes controllers
│   └── manager/           # Route registration, manager wiring
├── pkg/                   # Business logic packages
├── ui/ai-factory/         # Vue 3 Rancher Dashboard extension
└── Makefile
```

For a deeper tour of conventions and where things go, see
[`CLAUDE.md`](CLAUDE.md).

---

## Build, test, lint

The Makefile wraps the common loops. Target reality reflects the current repo
state — some operations are still stubs.

| Target                | What it does                                                    |
|-----------------------|-----------------------------------------------------------------|
| `make build`          | Build the operator binary to `./bin/aif-operator`.              |
| `make run`            | Run the operator from source (`go run ./cmd/operator`).         |
| `make test`           | Run unit tests (`go test -v ./...`).                            |
| `make lint`           | Run `golangci-lint`.                                            |
| `make manifests`      | Regenerate CRD YAML into `charts/aif-operator/crds/`.           |
| `make generate`       | Regenerate deepcopy methods.                                    |
| `make docker-build`   | Build the operator container image.                             |
| `make docker-push`    | Push the operator container image.                              |
| `make install-tools`  | Install pinned dev tools (controller-gen, golangci-lint, mockgen, ginkgo). |

The following targets exist but currently print "Not implemented yet" — treat
them as placeholders until later phases land:

- `make helm-install`
- `make helm-uninstall`
- `make charts-package`

For the UI:

```bash
cd ui/ai-factory
yarn install
yarn build      # production build
yarn test       # component tests
```

Helm charts can be lint-checked with `helm lint charts/aif-operator` and
`helm lint charts/aif-ui`.

---

## Where to learn more

- [`docs/spec/SOFTWARE_SPEC.md`](docs/spec/SOFTWARE_SPEC.md) — what users see
  and can do (product manager / customer-success view).
- [`docs/spec/ARCHITECTURE.md`](docs/spec/ARCHITECTURE.md) — how it's built
  (CRDs, REST API, Go packages, controllers, security, observability).
- [`docs/spec/PROJECT_PLAN.md`](docs/spec/PROJECT_PLAN.md) — engineering
  roadmap with story-level acceptance criteria.
- [`CLAUDE.md`](CLAUDE.md) — developer reference: code conventions, "how to
  add X" recipes, directory pointers.

There is no `CONTRIBUTING.md` in the tree yet. Until one lands, the spec
documents above are the authoritative guide for contribution scope and code
conventions.

---

## License

No `LICENSE` file is present in this repository at the time of writing.
Licensing is **TBD**; consult the repository owner before redistribution.
