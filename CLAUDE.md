# CLAUDE.md — SUSE AI Factory Developer Reference

This is the living documentation for the SUSE AI Factory (AIF) codebase. Read this before opening files.

**Links:** [SOFTWARE_SPEC.md](docs/spec/SOFTWARE_SPEC.md) | [ARCHITECTURE.md](docs/spec/ARCHITECTURE.md) | [PROJECT_PLAN.md](docs/spec/PROJECT_PLAN.md)

---

**Note on Repository State:** This repository is in active development. Many paths and components described below represent the target-state architecture from ARCHITECTURE.md and will be populated by stories across the project plan. Not all directories, packages, or features exist yet. Refer to PROJECT_PLAN.md to see which stories implement each component.

---

## Project Overview

SUSE AI Factory is the AI platform management layer built into Rancher. It gives platform engineers, AI/ML practitioners, and operations teams a single place to discover AI applications, compose them into validated AI stacks, publish those stacks as reusable blueprints, and deploy and monitor AI workloads on any Rancher-managed Kubernetes cluster.

The product uses a four-noun conceptual model:
- **App** — Building-block AI application packaged as a Helm chart (catalog-wide, immutable)
- **Bundle** — Mutable workshop where authors compose Apps and Blueprints (namespaced, Draft → Submitted → Approved)
- **Blueprint** — Published, immutable, versioned AI stack (cluster-scoped, governed)
- **Workload** — Running instance of an App or Blueprint (namespaced, tracks deployment history)

Governance: A Bundle becomes a Blueprint via publish-by-approval workflow. Blueprint versions are immutable. Workloads record provenance in `spec.source`.

**Air-gap first-class:** Every capability works in air-gapped clusters. AIF does not host an internal registry or reach NVIDIA NGC directly—assets flow through SUSE Registry.

---

## Repository Layout

```
aif/
├── api/v1alpha1/          # CRD Go types (Bundle, Blueprint, Workload, Settings, InstallAIExtension)
├── charts/                # Helm charts (aif-operator, aif-ui, generic-container, nim-llm, nim-vlm)
├── cmd/operator/          # Main entry point (≤250 lines)
├── docs/spec/             # SOFTWARE_SPEC.md, ARCHITECTURE.md, PROJECT_PLAN.md
├── internal/
│   ├── api/               # HTTP handlers (one per resource group)
│   ├── controller/        # Kubernetes controllers (Bundle, Blueprint, Workload, Settings)
│   └── manager/           # Route registration, manager wiring
├── pkg/                   # Business logic (apps, bundle, blueprint, publish, workload, helm, git, nvidia)
├── ui/ai-factory/         # Vue 3 Rancher Dashboard extension
└── Makefile               # Build, test, lint, manifests, generate
```

**Key directories:**
- `api/v1alpha1/` — All five CRD Go types (group `ai-platform.suse.com`); run `make manifests generate` after edits
- `charts/aif-operator/` — Operator Helm chart; CRDs in `crds/`, templates in `templates/`
- `internal/controller/` — Reconcilers; each touches exactly one CRD; imports only from `pkg/`
- `pkg/` — Business logic; interfaces in `interface.go` per package
- `ui/ai-factory/pkg/ai-factory/` — UI extension root; imports only from `@rancher/shell` and `@components/`

---

## Build & Test Commands

```bash
# Build operator binary
make build                        # Outputs to bin/aif-operator

# Run tests
make test                         # Unit tests (go test)
make test-integration             # Integration tests (envtest, planned for P8-5)

# Lint
make lint                         # golangci-lint
helm lint charts/aif-operator     # Helm chart lint
helm lint charts/aif-ui

# Generate CRDs and deepcopy
make manifests                    # controller-gen → charts/aif-operator/crds/
make generate                     # deepcopy → zz_generated.deepcopy.go

# Install build tools
make install-tools                # Installs controller-gen, kubebuilder, golangci-lint, mockgen, ginkgo

# Docker build
make docker-build                 # Two-stage Dockerfile, runs as UID 1000

# Helm operations
make helm-install                 # Install aif-operator chart to current cluster
make helm-uninstall
make charts-package               # Package all charts to .tgz

# UI (requires Node 18+ and yarn)
cd ui/ai-factory && yarn install
yarn build                        # Production build
yarn test                         # Vue component tests
```

---

## Code Conventions

### Go

- **Logging:** Use `log/slog` exclusively. JSON in production (`--log-format=json`), text in development. Include `request_id`, `component` in every entry. Never log credentials or PII.
- **Errors:** Wrap with context using `fmt.Errorf("operation: %w", err)`. Use `errors.Is` and `errors.As` for comparison. No `panic` except in `init()`.
- **No print statements:** Never use `fmt.Println`, `log.Println`. Use `slog` or return errors.
- **Interfaces:** Define in `interface.go` per package. Concrete implementations in separate files. Tests use fakes or mocks, never real K8s clients.
- **Context:** Every I/O function accepts `context.Context` as first argument. Respect cancellation.
- **Condition constants:** Import condition Type/Reason from `pkg/conditions/types.go`. Never use raw strings in controllers. CI enforces this with grep guards.
- **Phase enums:** Typed Go string constants (e.g., `type BundlePhase string; const BundlePhaseDraft BundlePhase = "Draft"`).
- **HTTP handlers:** Never surface raw internal errors. Use `writeError(w, code, err)` helper to translate to structured JSON response.
- **Import order:** Standard library → third-party → internal packages. Group with blank lines.

**Forbidden patterns:**
- `cluster-admin` in RBAC
- Wildcard verbs (`"*"`) or resources in RBAC
- Direct NVIDIA NGC calls (`nvcr.io`, `helm.ngc.nvidia.com`, `integrate.api.nvidia.com`)
- Hardcoded registry hostnames (use `Settings.spec.registryEndpoints`)
- Raw condition strings in controllers (use constants from `pkg/conditions/types.go`)

### UI (Vue 3 / Rancher Shell)

- **Imports:** Only from `@rancher/shell` and `@components/`. See `ARCHITECTURE.md §3.1` Imports Reference Table for allowed paths.
- **l10n:** All user-facing strings via `labelKey` from `pkg/ai-factory/l10n/en-us.yaml`. No hardcoded English text.
- **Steve store:** Use `this.$store.dispatch('ai-factory/findAll', {type})` for resource lists. Never raw API calls in components.
- **Component structure:** List pages use `ResourceList` wrapper, detail pages use `ResourceDetail`, edit pages use `CruResource`.
- **Validators:** Custom validation in `pkg/ai-factory/validators/index.js`, registered via `validators.js` DSL.
- **Routing:** All routes registered in `pkg/ai-factory/routing/aif-routing.js`.

**Reference files from Harvester:**
- Entry point: `harvester-ui-extension/pkg/harvester/index.ts`
- Product registration: `pkg/harvester/config/harvester-cluster.js`
- List page: `pkg/harvester/pages/c/_cluster/_resource/index.vue`
- Detail page: `pkg/harvester/detail/harvesterhci.io.host/index.vue`

---

## How to Add...

### A New CRD

1. Define Go types in `api/v1alpha1/{kind}_types.go`
2. Add `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`, `+kubebuilder:printcolumn` markers
3. Use `[]metav1.Condition` for status, typed enums for phases
4. Run `make manifests generate` to produce CRD YAML and deepcopy
5. Create controller in `internal/controller/{kind}_controller.go`
6. Register controller in `cmd/operator/main.go` manager setup
7. Add REST endpoints in `internal/api/{resource}.go`
8. Register routes in `internal/manager/routes.go`

**Checklist:**
- [ ] CRD YAML generated in `charts/aif-operator/crds/`
- [ ] Deepcopy methods generated
- [ ] Controller reconciles with finalizer `ai-platform.suse.com/cleanup`
- [ ] Condition Types use constants from `pkg/conditions/types.go`
- [ ] REST endpoints registered in routes.go (not main.go)

### A New REST Endpoint

1. Add handler function in `internal/api/{resource}.go`
2. Register route in `internal/manager/routes.go` via `mux.HandleFunc`
3. Use middleware: CORS, request ID, error translation
4. Extract calling user from `Impersonate-User` header for audit fields
5. Return structured JSON error envelope on failure

**Checklist:**
- [ ] Handler uses `slog` with `request_id` field
- [ ] Errors translated via `writeError(w, code, err)`
- [ ] RBAC enforced via K8s (operator SA has permissions; per-user via Rancher RBAC)
- [ ] Route registered in routes.go, not main.go

### A UI List Page

1. Create `ui/ai-factory/pkg/ai-factory/list/{crd}.vue`
2. Import `ResourceList` from `@shell/components/ResourceList`
3. Define table headers in `config/table-headers.js`
4. Register route in `routing/aif-routing.js`
5. Add resource type constant in `config/types.ts`
6. Create l10n keys in `l10n/en-us.yaml`

**Checklist:**
- [ ] Uses ResourceList wrapper
- [ ] All strings from labelKey
- [ ] Registered in routing
- [ ] Type constant exported from types.ts

### A UI Detail Page

1. Create `ui/ai-factory/pkg/ai-factory/detail/{crd}/index.vue`
2. Import `ResourceDetail` from `@shell/components/ResourceDetail`
3. Use `Tabbed` and `Tab` for multi-tab layout if needed
4. Fetch resource via Steve store: `this.$store.dispatch('ai-factory/find', {type, id})`
5. Register route in routing (detail routes auto-match via resource type)

**Checklist:**
- [ ] Uses ResourceDetail wrapper
- [ ] Tabs use Tabbed/Tab components
- [ ] Data fetched via Steve store
- [ ] All actions call REST endpoints, not direct CRD mutations

### A UI Edit Page

1. Create `ui/ai-factory/pkg/ai-factory/edit/{crd}.vue`
2. Import `CruResource` from `@shell/components/CruResource`
3. Use form components from `@components/Form/*` and `@shell/components/form/*`
4. Add validation in `validators/index.js`
5. Save via `this.value.save()` (Steve model method)

**Checklist:**
- [ ] Uses CruResource wrapper
- [ ] Form inputs use LabeledInput, LabeledSelect, etc.
- [ ] Validation registered in validators.js
- [ ] Save calls Steve model save, not raw API

### A Publish-Workflow Action

1. Add REST endpoint in `internal/api/bundles.go` (e.g., `/bundles/{ns}/{name}/submit`)
2. Implement business logic in `pkg/publish/workflow.go`
3. Update `BundleStatus.phase` and `status.conditions`
4. Emit Kubernetes Event for audit trail
5. Add UI button in Bundle detail page, gated by phase

**Checklist:**
- [ ] Endpoint checks RBAC (publisher actions check `aif-blueprint-publisher` role via SAR)
- [ ] Phase transition validated per `ARCHITECTURE.md §4.2`
- [ ] Event emitted with structured reason
- [ ] UI button disabled when action invalid for current phase

---

## Where to Look

| Concept | Architecture Section | Project Plan Stories | Customer Spec |
|---------|---------------------|----------------------|---------------|
| Four-noun model (App/Bundle/Blueprint/Workload) | §2.3 Conceptual Model | P0-2, P1-1, P1-2, P1-3 | SOFTWARE_SPEC §1 |
| CRD schemas | §4 Custom Resource Definitions | P0-2, P1-1, P1-2, P1-3, P1-4 | — |
| REST API contracts | §5 REST API Contract | P1-10, P3-2..P3-6, P4-2, P5-3 | — |
| Go package architecture | §6 Go Package Architecture | P4-1, P5-1, P5-2 | — |
| UI extension structure | §7 UI Extension Architecture | P6-0..P6-10 | — |
| Controller design | §8 Controller Design | P1-6, P1-7, P1-8 | — |
| Helm charts | §9 Helm Chart Specifications | P0-3, P0-6, P0-7 | — |
| Security & RBAC | §10 Security Architecture | P7-1, P7-2, P7-3, P7-4, P7-5 | SOFTWARE_SPEC §2 Personas |
| Observability | §11 Observability | P8-1, P8-2, P8-3 | — |
| Testing strategy | §12 Testing Strategy | P8-4, P8-5, P8-6 | — |
| External integrations | §13 External Integration Contracts | P2-1, P2-2, P2-3, P2-4, P2-5 | SOFTWARE_SPEC §9 Settings |
| Air-gap deployment | §13.4 Image Pull Secrets, §4.5 Settings CRD | P0-6, P0-7, P5-7, P9-6, P9-7 | SOFTWARE_SPEC §1 Vision (air-gap bullet) |
| Vendor Reference Blueprint wrapping | §13.1 Reference Blueprint Detection | P2-5 | SOFTWARE_SPEC §5, §6 |
| Publish-by-approval workflow | §5 Bundles endpoints, §8.5 Publisher Role | P3-1, P3-2, P3-3, P3-4, P3-5, P3-6 | SOFTWARE_SPEC §7 Bundles |
| Workload phase state machine | §4.4 Workload CRD, Status Fields | P5-1, P5-2 | SOFTWARE_SPEC §8 Workloads |
| NIM resource sizing | §4.4 NIM Resource Sizing Formulas | P4-4 | — |
| Helm values merge | §6.6 Helm Values Merge Precedence | P4-1, P4-6 | — |
| Blueprint immutability webhook | §8.3 Immutability Webhook | P1-5 | SOFTWARE_SPEC §6 Blueprints |

**Imports Reference Table:** See `ARCHITECTURE.md §3.1` for the complete UI imports reference (which `@shell/*` and `@components/*` paths are allowed).

---

## Living-Doc Stories

This file is maintained via five stories across the project:

1. **P0-0** — Bootstrap CLAUDE.md (this story)
2. **P1-9** — Expand "How to Add a Controller" with reconciler pattern, finalizer, status conditions
3. **P3-7** — Expand "How to Add a Publish-Workflow Action" with Bundle phase transitions and publisher RBAC
4. **P6-0** — Expand "How to Add a UI Page" with Steve store patterns, routing, validators, l10n
5. **P9-5** — Final polish: prune outdated content, consolidate examples, ensure <400 lines

Between P0-0 and P9-5, all edits are append-only. P9-5 is the only story allowed to delete content.

---

## Critical Constraints

1. **No internal OCI registry.** AIF does not host, proxy, or mirror images. No `pkg/registry/`, no `:5000` port, no registry metrics.
2. **No direct NVIDIA NGC access.** AIF does not reach `nvcr.io`, `helm.ngc.nvidia.com`, or `integrate.api.nvidia.com`. NIMs flow through SUSE Registry.
3. **Mirror path convention:** NVIDIA charts at `oci://registry.suse.com/ai/charts/nvidia/{nim-llm|nim-vlm}:{version}`, images at `registry.suse.com/ai/containers/nvidia/{model}:{version}`.
4. **Blueprint immutability:** Spec fields are immutable per version. Status fields (Active/Deprecated/Withdrawn) are mutable. Webhook enforces this.
5. **Workload provenance:** Every Workload records `spec.source` (App|Blueprint|BundleTest). Operate actions check `source.kind`.
6. **Air-gap parameterization:** No hardcoded registry hostnames. Use `Settings.spec.registryEndpoints` for overrides. Image rewrite layer (§6.6 layer 5) handles prefix substitution.
7. **Publish-by-approval governance:** Bundle → Submitted → Approved → mints Blueprint version. Blueprint Publisher role via RBAC (`aif-blueprint-publisher` ClusterRole).
8. **Single pull-secret pattern:** Workload pods use one docker-config Secret (`suse-registry-creds`) reconciled by the operator into each workload namespace.

---

**End of CLAUDE.md** — 274 lines
