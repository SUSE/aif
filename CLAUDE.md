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
- `api/v1alpha1/` — All five CRD Go types (group `ai.suse.com`); run `make manifests generate` after edits
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
make test-controllers             # Controller integration tests (envtest, Ginkgo)

# Lint
make lint                         # golangci-lint
helm lint charts/aif-operator     # Helm chart lint
helm lint charts/aif-ui

# Generate CRDs and deepcopy
make manifests                    # controller-gen → charts/aif-operator/crds/
make generate                     # deepcopy → zz_generated.deepcopy.go

# Install build tools
make install-tools                # Installs controller-gen@v0.20.1, golangci-lint@v2.11.4, mockgen@v0.6.0, ginkgo@v2.28.1, setup-envtest@v0.24.0
make envtest                      # Downloads envtest binaries (etcd + kube-apiserver)

# Docker build
make docker-build                 # Two-stage Dockerfile, runs as UID 1000

# Helm operations
make helm-install                 # Install aif-operator chart to current cluster
make helm-uninstall
make charts-package               # Package all charts to .tgz

# UI (requires Node 18+ and yarn)
cd ui/ai-factory && yarn install
yarn build                        # Production build
yarn test                         # Scaffold smoke tests (Node --test runner, pkg/ai-factory/test/)
yarn test:unit                    # Vitest unit tests (tests/unit/)
yarn test:unit:ui                 # Vitest UI — interactive browser test runner
yarn test:coverage                # Vitest with coverage report
```

### Local Dev Loop

```bash
# One-time setup
make install-tools                # Go tools (controller-gen, golangci-lint, mockgen, ginkgo)
make dev-cluster                  # k3d cluster 'aif-dev' (requires k3d)
make dev-install                  # kubectl apply -f charts/aif-operator/crds/

# Iterate
make run                          # operator out-of-cluster (uses your kubeconfig)
make examples                     # apply minimal sample CRs in another shell
kubectl get bundles,blueprints,workloads -A

# Teardown
make dev-cluster-down
```

Sample CRs live in `examples/`. Each is the minimal valid CR for its CRD.

**Gotchas:**

- The k3d loadbalancer container (`k3d-aif-dev-serverlb`) sometimes exits on its own (`Exited (137)`); kubectl then refuses with `connection refused`. Recover with `k3d cluster start aif-dev` (no need to recreate). The k3s server stays up across these exits.
- The Blueprint immutability webhook is REGISTERED inside the operator process, but `make run` does NOT install the `ValidatingWebhookConfiguration` (that ships in `charts/aif-operator/templates/webhook.yaml` and is created only by `helm install`). To exercise immutability end-to-end, install the chart instead of running out-of-cluster.

---

## Code Conventions

### Go

- **Layering rule (hexagonal):** Imports flow ONE direction:
  `cmd/` → `internal/{controller,api,manager,webhook}/` → `pkg/<domain>/` → `pkg/conditions/` → stdlib.
  `pkg/*` packages MUST NOT import `internal/*`.
  `pkg/{helm,git,nvidia,source_collection,authz}` MUST NOT import `api/v1alpha1` — they speak in their own domain types. Translation lives in the controller.
  `pkg/{bundle,blueprint,workload,settings}` import `api/v1alpha1` from two file roles only:
    - `conversions.go` — CR↔domain translation (the canonical home).
    - `repository.go` — K8s adapter ports (Repository / Reader / Writer). These DO take `*aifv1.X` by design because they are the K8s adapter boundary.
  Other files (`interface.go`, `types.go`, business-logic adapters like `manager.go` / `service.go`) SHOULD be aifv1-free so domain logic stays framework-agnostic. Two pre-existing exceptions are tracked legacy and exempt: `pkg/blueprint/interface.go`'s `Manager` interface (P1-2 acceptance) and `pkg/bundle/types.go`'s reuse of `aifv1` leaf types (P1-1 acceptance). Don't add new violations; don't unilaterally rename or refactor the legacy without amending PROJECT_PLAN.md.
- **When to add `interface.go`:** A package gets one when (a) it has at least one I/O dependency (K8s, HTTP, Helm SDK, git), OR (b) it is consumed by a controller/handler that needs a test double. Pure-data packages (`pkg/conditions`) do not need one.
- **Per-package file layout:** `interface.go` (ports, ≤4 methods each), `types.go` (domain structs, no `aifv1`), `<adapter>.go` (one concrete impl per adapter), `conversions.go` (CR↔domain, only file allowed to import `aifv1`), `<adapter>_test.go`.
- **Interface size (ISP):** Target ≤4 methods per interface. If you reach 5, split by role (Reader/Writer, Discoverer/Deployer, Engine/Releaser). One interface, one reason to mock.
- **Naming — prefer role-revealing names over `Manager` for new types.** "Manager" tells you nothing about the responsibility; pick the role: `Repository` (CRUD over a store), `Service` (orchestrates Repository + Validator), `Validator` (pure spec checks), `StateMachine` (phase transitions), `Resolver` (dereferences a ref), `Engine` (wraps an external SDK like Helm/git), `Provider` (reads from an external API like SUSE App Collection), `UseCase`/`Workflow` (multi-step business operation like publish-by-approval). The legacy `pkg/bundle.Manager` and `pkg/blueprint.Manager` interfaces are kept to satisfy P1-1 / P1-2 acceptance criteria and are exempt — do not rename them without amending PROJECT_PLAN.md.
- **Domain types:** Domain types (the structs in `pkg/<x>/types.go`) carry behaviour. Free functions over domain structs are preferred to methods on a `Manager` struct that only holds a logger. If a struct's only field is `*slog.Logger` and the methods don't access state, those are functions, not methods.
- **Where invariants live:**
  - Shape (required, length, enum, regex) — `+kubebuilder:validation:*` on the CRD type. Enforced by the API server.
  - Cross-field (discriminated unions, "exactly one of") — admission webhook in `internal/webhook/`.
  - Business rules (e.g. "you may not publish if a Blueprint with this name+version already exists") — `pkg/<x>.Validator` called by the controller and REST handlers.
  Do NOT duplicate kubebuilder constraints in Go validators — the API server already rejects them. Only encode what kubebuilder cannot express.
- **Test doubles:** Hand-written fakes go in `pkg/<x>/fake_<role>.go` (e.g. `pkg/bundle/fake_repository.go`) and implement the port. Use `mockgen` only when a stable, frequently-rebuilt mock is needed (rare); generated mocks live in `pkg/<x>/mock/`. Tests in controllers MUST use the fake/mock, never `client.Client` against a real apiserver — for that, add an envtest case under `test/integration/`.
- **Logging:** Use `log/slog` exclusively. JSON in production (`--log-format=json`), text in development. Include `request_id`, `component` in every entry. Never log credentials or PII.
- **Errors:** Wrap with context using `fmt.Errorf("operation: %w", err)`. Use `errors.Is` and `errors.As` for comparison. No `panic` except in `init()`.
  - Define sentinel errors per package (`pkg/<x>/errors.go`) when callers need to distinguish failure modes (e.g. `ErrSecretNotFound`, `ErrBundleNotFound`). Never let consumers `strings.Contains` on error messages.
- **No print statements:** Never use `fmt.Println`, `log.Println`. Use `slog` or return errors.
- **Interfaces:** Define in `interface.go` per package per the layering rule above. Concrete implementations in separate files. Tests use fakes or mocks, never real K8s clients.
- **Context:** Every I/O function accepts `context.Context` as first argument. Respect cancellation.
- **Condition constants:** Import condition Type/Reason from `pkg/conditions/types.go`. Never use raw strings in controllers. CI enforces this with grep guards.
- **Condition setting:** Use `conditions.Set(&status.Conditions, cond)` (built on `meta.SetStatusCondition`). Do not hand-roll condition merging in controllers; `meta.SetStatusCondition` correctly preserves `LastTransitionTime` when status hasn't changed.
- **Phase enums:** Typed Go string constants (e.g., `type BundlePhase string; const BundlePhaseDraft BundlePhase = "Draft"`).
- **HTTP handlers:** Never surface raw internal errors. Use `writeError(w, code, err)` helper to translate to structured JSON response.
- **Import order:** Standard library → third-party → internal packages. Group with blank lines.

**Forbidden patterns:**
- `cluster-admin` in RBAC
- Wildcard verbs (`"*"`) or resources in RBAC
- Direct NVIDIA NGC calls (`nvcr.io`, `helm.ngc.nvidia.com`, `integrate.api.nvidia.com`)
- Hardcoded registry hostnames (use `Settings.spec.registryEndpoints`)
- Raw condition strings in controllers (use constants from `pkg/conditions/types.go`)
- NEW top-level type named `Manager` in `pkg/*` (use a role-revealing name; see naming rule above). The pre-existing `pkg/bundle.Manager` and `pkg/blueprint.Manager` are exempt.
- NEW domain-logic port (Validator, Service, UseCase) in `interface.go` importing `api/v1alpha1`. K8s adapter ports in `repository.go` MAY import `aifv1` — that is the legitimate adapter boundary. The pre-existing `pkg/blueprint/interface.go` aifv1 import is exempt as legacy.
- `pkg/{helm,git,nvidia,source_collection,authz}` importing `api/v1alpha1` at all
- `strings.Contains(err.Error(), ...)` for error classification (use sentinel errors + `errors.Is`)

### UI (Vue 3 / Rancher Shell)

- **Imports:** Only from `@rancher/shell` and `@components/`. See `ARCHITECTURE.md §3.1` Imports Reference Table for allowed paths.
- **l10n:** All user-facing strings via `labelKey` from `pkg/ai-factory/l10n/en-us.yaml`. No hardcoded English text.
- **Steve store:** Use Steve store for Rancher CRDs. For operator-specific reads and writes (Settings, Apps, NIM index, workflow lifecycle actions), use the typed functions in `utils/operator-api.ts` instead. Never call `fetch()` directly in component code — route all operator calls through `operator-api.ts`.
- **Component structure:** List pages use `ResourceList` wrapper, detail pages use `ResourceDetail`, edit pages use `CruResource`.
- **Validators:** Custom validation in `pkg/ai-factory/validators/index.js`, registered via `validators.js` DSL.
- **Routing:** All routes registered in `pkg/ai-factory/routing/index.ts`.

**Reference files from Harvester:**
- Entry point: `harvester-ui-extension/pkg/harvester/index.ts`
- Product registration: `pkg/harvester/config/harvester-cluster.js`
- List page: `pkg/harvester/pages/c/_cluster/_resource/index.vue`
- Detail page: `pkg/harvester/detail/harvesterhci.io.host/index.vue`

---

## Workflow & contribution rules

These rules apply to every PR regardless of which CRD or layer you touch.

### Branch naming

- **Story work:** `{StoryID}-{kebab-description}` taken from `PROJECT_PLAN.md`. Example: `P2-3-apps-catalog`, `P1-5-blueprint-immutability-webhook`.
- **Cross-cutting / non-story work:** prefix with `fix/`, `chore/`, or `docs/` and a short kebab description. Examples: `fix/register-builtin-scheme-types`, `docs/claude-md-formalize-patterns`.
- One branch per logically distinct change; don't bundle unrelated work to save a round-trip.

### Commit & PR hygiene

- Plain commit messages — **no `Co-Authored-By` trailer, no AI attribution**, no automated footers.
- Subject ≤72 chars, imperative mood (`add X`, `fix Y`, `refactor Z` — not `added X`). Body wraps at ~72 cols and explains *why*, not just *what*.
- One concept per commit. If a refactor and a feature land together, split them.
- PR descriptions describe what changed, why, and how to validate — not how to read the diff.

### Verify before claiming

- Before claiming a target/test/feature works, **run the command and read the output**. No "expected to pass" without evidence; no "this should work" in lieu of `make test` output.
- For UI / integration / live-upstream behavior, run the actual flow once — type-checking and unit tests verify code correctness, not feature correctness.
- When you can't run something locally (missing creds, missing cluster, restricted environment), say so explicitly and ask the user to verify rather than asserting it works.

### Manager-owned docs

The following files are owned by the project manager and **must not be edited unilaterally**:

- `docs/spec/PROJECT_PLAN.md`
- `docs/spec/ARCHITECTURE.md`
- `docs/spec/SOFTWARE_SPEC.md`
- `CLAUDE.md` (this file)

If you spot a factual error, naming inconsistency, broken example, or stale claim, **flag it to the user and ask for confirmation** before patching. Embedding learnings into PROJECT_PLAN follow-up notes (`> **Follow-up (post-merge):** …`) is encouraged once the user agrees, and is the canonical channel for "I learned X while implementing Y".

Between P0-0 and P9-5, edits are append-only — never delete existing prose; add new sections / extend existing checklists.

### Writing rules — don't propagate behavioral claims

The spec sometimes carries assertions about how operators / customers / publishers tend to behave (e.g. "customers often reuse the same value", "publishers typically …"). These are historical context, **not verified facts**. Do NOT propagate such claims into user-facing surfaces:

- READMEs
- `.env.example` and other config templates
- Error messages and CLI output
- Code comments
- Commit messages and PR descriptions

Stick to what is structurally true (field names, schema relationships, observable behavior). If the spec text needs to be cited, link to it; don't paraphrase the behavioral claim.

---

## How to Add...

### A New CRD

1. Define Go types in `api/v1alpha1/{kind}_types.go`
2. Add `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`, `+kubebuilder:printcolumn` markers
3. Use `[]metav1.Condition` for status, typed enums for phases
4. Run `make manifests generate` to produce CRD YAML and deepcopy
5. Create controller in `internal/controller/{kind}_controller.go` following [docs/dev/controller-guide.md](docs/dev/controller-guide.md)
6. Register controller in `cmd/operator/main.go` manager setup
7. Add REST endpoints in `internal/api/{resource}.go`
8. Register routes in `internal/manager/routes.go`

**Checklist:**
- [ ] CRD YAML generated in `charts/aif-operator/crds/`
- [ ] Deepcopy methods generated
- [ ] Controller reconciles with finalizer `ai.suse.com/cleanup`
- [ ] Condition Types use constants from `pkg/conditions/types.go`
- [ ] REST endpoints registered in routes.go (not main.go)
- [ ] Controller follows reconciler skeleton from [controller-guide.md](docs/dev/controller-guide.md)
- [ ] RBAC markers present (resource, status, finalizers, events)
- [ ] Test file with envtest scaffold (see controller-guide.md §7)

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
4. Register route in `routing/index.ts`
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

### A New External Integration (HTTP API, OCI registry, Git, Helm SDK, etc.)

1. Create `pkg/<integration>/types.go` — pure-Go value objects. Do NOT import `api/v1alpha1`.
2. Create `pkg/<integration>/interface.go` — port with ≤4 methods. Name it by role (`Provider`, `Engine`, `Client`).
3. Create `pkg/<integration>/<adapter>.go` — concrete implementation (one adapter = one external system + protocol). Examples: `pkg/source_collection/api_client.go` (HTTP), `pkg/source_collection/oci_fallback.go` (registry walk).
4. Create `pkg/<integration>/fake_<role>.go` — in-memory fake implementing the port for unit tests.
5. Create `pkg/<integration>/errors.go` — sentinel errors (`ErrUnreachable`, `ErrNotFound`, `ErrUnauthorized`).
6. Wire credentials: integration receives a `Settings(s EngineSettings)` push from `SettingsReconciler.applySettingsToEngines` — never reads K8s Secrets directly. See `ARCHITECTURE.md §6.2 EngineSettings` and §8.2.1.
7. Wire instance in `cmd/operator/main.go` and inject into the consuming controller(s) via the port type, never the concrete struct.
8. **Verification ergonomics** — ship the same trio every external integration ships, so every package can be probed with one command:
   - `pkg/<integration>/example_test.go` — `Example_<X>` with a deterministic `// Output:` block exercising the happy path against an in-process stub. Doubles as `make verify-<integration>-mock`.
   - `pkg/<integration>/live_test.go` — `//go:build live`, calls the real upstream when env vars are set, skips otherwise. Asserts only that the auth handshake + happy path complete; entry counts are informational. Doubles as `make verify-<integration>-live`. Caught a real auth-scheme bug on P2-1 (HTTP Basic vs OCI Bearer) — this isn't optional.
   - Makefile targets `test-<integration>` / `verify-<integration>-mock` / `verify-<integration>-live` mirroring the existing `nim` and `appco` trios.
   - `.env.example` entry for each new env var the live target consumes (Makefile auto-loads `.env`; see top of `Makefile`). Use **dedicated** env-var names per upstream service (don't reuse `SUSE_REG_*` for an unrelated upstream just because the spec hints customers might).
9. Forbidden: importing `api/v1alpha1`, calling `client.Client` directly, reading Secrets directly, hardcoding hostnames (read from `EngineSettings.RegistryEndpoints`).

**Checklist:**
- [ ] `interface.go` is free of `api/v1alpha1` imports
- [ ] Port has ≤4 methods (split if larger)
- [ ] Adapter file is named for the protocol/source (`api_client.go`, `oci_fallback.go`, `sdk_engine.go`)
- [ ] `fake_<role>.go` exists and is used by at least one consumer's test
- [ ] Sentinel errors defined; no `strings.Contains` on error messages downstream
- [ ] Credentials arrive via `UpdateSettings`, not via direct Secret reads
- [ ] Hostnames sourced from `EngineSettings.RegistryEndpoints`, never literals
- [ ] Consuming controller depends on the interface, not the struct
- [ ] `Example_<X>` exists with a deterministic `// Output:` check (verify-mock target works)
- [ ] `live_test.go` exists with `//go:build live` and skips cleanly without creds (verify-live target works)
- [ ] Makefile `test-<integration>` / `verify-<integration>-mock` / `verify-<integration>-live` targets added to `.PHONY` and present in the file
- [ ] `.env.example` lists the live target's env vars with a comment pointing at the upstream service

---

## Where to Look

| Concept | Architecture Section | Project Plan Stories | Customer Spec |
|---------|---------------------|----------------------|---------------|
| Four-noun model (App/Bundle/Blueprint/Workload) | §2.3 Conceptual Model | P0-2, P1-1, P1-2, P1-3 | SOFTWARE_SPEC §1 |
| CRD schemas | §4 Custom Resource Definitions | P0-2, P1-1, P1-2, P1-3, P1-4 | — |
| REST API contracts | §5 REST API Contract | P1-10, P3-2..P3-6, P4-2, P5-3 | — |
| Go package architecture | §6 Go Package Architecture | P4-1, P5-1, P5-2 | — |
| UI extension structure | §7 UI Extension Architecture | P6-0..P6-10 | — |
| Controller design | §8 Controller Design, [controller-guide.md](docs/dev/controller-guide.md) | P1-6, P1-7, P1-8, P1-9 | — |
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

**End of CLAUDE.md** — ~447 lines (P9-5 will prune to <400)

<!-- code-review-graph MCP tools -->
## MCP Tools: code-review-graph

**IMPORTANT: This project has a knowledge graph. ALWAYS use the
code-review-graph MCP tools BEFORE using Grep/Glob/Read to explore
the codebase.** The graph is faster, cheaper (fewer tokens), and gives
you structural context (callers, dependents, test coverage) that file
scanning cannot.

### When to use graph tools FIRST

- **Exploring code**: `semantic_search_nodes` or `query_graph` instead of Grep
- **Understanding impact**: `get_impact_radius` instead of manually tracing imports
- **Code review**: `detect_changes` + `get_review_context` instead of reading entire files
- **Finding relationships**: `query_graph` with callers_of/callees_of/imports_of/tests_for
- **Architecture questions**: `get_architecture_overview` + `list_communities`

Fall back to Grep/Glob/Read **only** when the graph doesn't cover what you need.

### Key Tools

| Tool | Use when |
|------|----------|
| `detect_changes` | Reviewing code changes — gives risk-scored analysis |
| `get_review_context` | Need source snippets for review — token-efficient |
| `get_impact_radius` | Understanding blast radius of a change |
| `get_affected_flows` | Finding which execution paths are impacted |
| `query_graph` | Tracing callers, callees, imports, tests, dependencies |
| `semantic_search_nodes` | Finding functions/classes by name or keyword |
| `get_architecture_overview` | Understanding high-level codebase structure |
| `refactor_tool` | Planning renames, finding dead code |

### Workflow

1. The graph auto-updates on file changes (via hooks).
2. Use `detect_changes` for code review.
3. Use `get_affected_flows` to understand impact.
4. Use `query_graph` pattern="tests_for" to check coverage.
