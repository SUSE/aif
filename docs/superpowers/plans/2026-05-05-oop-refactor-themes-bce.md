# OOP Refactor Plan — Themes B, C, E from the Code Review

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Source report:** `docs/superpowers/reviews/2026-05-05-oop-hexagonal-review.md` (the "report" hereafter).

**Pre-conditions (already landed in `chore/oop-foundations-and-dev-env`):**
- Theme A1 (`pkg/conditions.Set`)
- Theme A2 (Settings dead-cache drop)
- Theme A3 (`blueprint.Validate` free function)
- CLAUDE.md OOP conventions and "How to Add a New External Integration" recipe

**User directives (memory `feedback_oop_directives.md`, 2026-05-05):**
1. **Drop the Bundle in-memory cache.** New code must not assume it exists. `bundle.Repository` is K8s-backed, period. Touched by Tasks B2 + C4.
2. **`pkg/publish` MUST depend on Repository ports**, never an embedded `client.Client`. Touched by Task C3.
3. **Defer the `validateSpec`-vs-kubebuilder-marker dedup question** (report E2) until the team discusses it.

**Tech Stack:** Go 1.26 (GOTOOLCHAIN=auto), controller-runtime, mockgen.

**Out of scope:**
- C5 (`pkg/authz`) — pre-empt only if a REST handler ships before this plan completes (report's recommendation).
- E2 (validateSpec / CEL dedup) — open per user.
- Any change to `api/v1alpha1/*_types.go` — kubebuilder source of truth, untouchable here.
- Any change to controller-runtime patterns (`client.Client`, `controllerutil.AddFinalizer`, `record.EventRecorder` — wrap, don't reinvent).

---

## Execution order (small-first to maximize early wins)

1. **C3** Define `pkg/publish/interface.go` — locks REST surface for P1-10/P3-x
2. **C4** Per-CRD `Repository` interfaces in `pkg/{bundle,blueprint,workload,settings}/repository.go`
3. **B1** `pkg/blueprint/{types,conversions}.go` — domain types, fix leaky port
4. **B2** Lift `aifv1.*` leaf types out of `pkg/bundle/types.go`; **drop the in-memory cache** per directive 1
5. **B5** Split `pkg/nvidia` into `Discovery` + `Deployer` interfaces
6. **C6** `internal/webhook/registry.go` — slice-of-Validators
7. **B3** `pkg/workload/{types,interface}.go` — pre-place before P5-1
8. **C1** `pkg/helm/interface.go` — Engine + Releaser
9. **C2** `pkg/git/interface.go`
10. **B4** `pkg/source_collection/{types,interface,api_client,oci_fallback}.go`
11. **E1** Pull `client.Client` out of reconcilers (consumes C4)

Tasks 1-6 are S (≤1 day each) and produce immediate test wins. Tasks 7-10 are M (2-3 days). Task 11 is L (4+ days, multi-PR).

A reasonable first PR: Tasks 1+2 together (interfaces only, no behaviour change). Subsequent PRs land one task each.

---

## Task C3 — `pkg/publish/interface.go`

**Files:**
- Create: `pkg/publish/interface.go`
- Modify: `pkg/publish/workflow.go` (constructor signature)

**Spec source:** report §1C row 24 + ARCHITECTURE.md §6.2:1273-1282.

- [ ] **Step 1: Read the spec excerpt**

```bash
sed -n '1270,1290p' /home/thbertoldi/suse/aif/docs/spec/ARCHITECTURE.md
```
Expected: `Workflow` interface with Submit/Withdraw/Approve/RequestChanges signatures.

- [ ] **Step 2: Create `pkg/publish/interface.go`**

```go
package publish

import (
	"context"

	"github.com/SUSE/aif/pkg/blueprint"
	"github.com/SUSE/aif/pkg/bundle"
)

// Workflow orchestrates the Bundle publish-by-approval lifecycle. It depends
// only on Repository ports and an authorization checker — never on
// controller-runtime's client.Client directly. This keeps the workflow
// unit-testable without envtest.
type Workflow interface {
	Submit(ctx context.Context, ns, name string, req SubmitRequest) error
	Withdraw(ctx context.Context, ns, name string, user string) error
	RequestChanges(ctx context.Context, ns, name string, req ReviewRequest) error
	Approve(ctx context.Context, ns, name string, req ApproveRequest) (blueprint.Blueprint, error)
}

// SubmitRequest is the input to Workflow.Submit.
type SubmitRequest struct {
	User              string
	ProposedVersion   string
	ChangeDescription string
}

// ApproveRequest is the input to Workflow.Approve.
type ApproveRequest struct {
	User string
}

// ReviewRequest is the input to Workflow.RequestChanges.
type ReviewRequest struct {
	User    string
	Comment string
}

// Authorizer answers "may this user perform this action on this resource?".
// Implemented by a SubjectAccessReview-backed adapter in pkg/authz when that
// package lands; meanwhile a hand-rolled fake satisfies tests.
type Authorizer interface {
	Allowed(ctx context.Context, user, verb, resource string) (bool, error)
}

// Deps groups the constructor dependencies so that adding a new port doesn't
// churn every test. New entries here ARE allowed; renames are not.
type Deps struct {
	Bundles    bundle.Repository
	Blueprints blueprint.Repository
	Authz      Authorizer
	Logger     interface{ Info(string, ...any) } // *slog.Logger satisfies this
}
```

- [ ] **Step 3: Update `pkg/publish/workflow.go` to take `Deps`**

```go
package publish

// New returns a Workflow implementation bound to the supplied dependencies.
// The implementation lives in workflow_impl.go (P3-1).
func New(d Deps) Workflow {
	return &workflowImpl{deps: d}
}

type workflowImpl struct {
	deps Deps
}

// Method bodies left to P3-1; they MUST use d.Bundles / d.Blueprints / d.Authz
// only — never a raw client.Client.
func (w *workflowImpl) Submit(ctx context.Context, ns, name string, req SubmitRequest) error {
	return errNotImplemented("Submit")
}
func (w *workflowImpl) Withdraw(ctx context.Context, ns, name string, user string) error {
	return errNotImplemented("Withdraw")
}
func (w *workflowImpl) RequestChanges(ctx context.Context, ns, name string, req ReviewRequest) error {
	return errNotImplemented("RequestChanges")
}
func (w *workflowImpl) Approve(ctx context.Context, ns, name string, req ApproveRequest) (blueprint.Blueprint, error) {
	return blueprint.Blueprint{}, errNotImplemented("Approve")
}
```

(`errNotImplemented` and `blueprint.Blueprint` arrive in Tasks C4 and B1 respectively. Comment those imports out until they exist if landing C3 first.)

- [ ] **Step 4: Build**

Run: `go build ./pkg/publish/...`
Expected: clean (or errors blocked on B1/C4 deps — note them and proceed).

- [ ] **Step 5: Commit**

```bash
git add pkg/publish/
git commit -m "define publish.Workflow port; depend on Repository ports per user directive"
```

---

## Task C4 — Per-CRD `Repository` interfaces

**Files:**
- Create: `pkg/bundle/repository.go`
- Create: `pkg/blueprint/repository.go`
- Create: `pkg/workload/repository.go`
- Create: `pkg/settings/repository.go` (note: package may need creation)

For each file, define a 4-method `Repository` port and a `k8s_repository.go` adapter wrapping `client.Client`. The detailed shape:

- [ ] **Step 1: `pkg/bundle/repository.go`**

```go
package bundle

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
)

// Repository is the K8s-backed CRUD port for Bundle CRs. It supersedes the
// in-memory cache on Manager (which is being retired per user directive).
// Methods are intentionally limited to ≤4 (ISP) — split if you need more.
type Repository interface {
	Get(ctx context.Context, namespace, name string) (*aifv1.Bundle, error)
	List(ctx context.Context, namespace string, selector labels.Selector) ([]aifv1.Bundle, error)
	Update(ctx context.Context, b *aifv1.Bundle) error
	UpdateStatus(ctx context.Context, b *aifv1.Bundle) error
}
```

(Note: this Repository keeps `*aifv1.Bundle` in the signature deliberately — Repository is the *adapter boundary* between K8s and the domain. Domain-typed methods come in Task B2 via a separate `bundle.Service` port that wraps Repository.)

- [ ] **Step 2: `pkg/bundle/k8s_repository.go`** — implements `Repository` using `client.Client`. Constructor: `NewK8sRepository(c client.Client) Repository`.

- [ ] **Step 3: Repeat the pattern for `pkg/blueprint`, `pkg/workload`, `pkg/settings`.**

For workload, add a fifth method that violates ISP intentionally — split it:

```go
type Repository interface { Get/List/Update/UpdateStatus } // 4 methods
type DeploymentCounter interface {
	CountByBlueprint(ctx context.Context, name, version string) (int32, error)
}
```

This kills the cluster-wide `r.List(&workloadList)` in `internal/controller/blueprint_controller.go:120` — `BlueprintReconciler` depends on `workload.DeploymentCounter` instead.

- [ ] **Step 4: Hand-written fakes per package** — `fake_repository.go` with in-memory map, suitable for unit tests.

- [ ] **Step 5: Build + test**

Run: `go build ./... && go test ./pkg/bundle/... ./pkg/blueprint/... ./pkg/workload/... ./pkg/settings/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/bundle/ pkg/blueprint/ pkg/workload/ pkg/settings/
git commit -m "introduce Repository ports per CRD; pre-place K8s adapters and fakes"
```

---

## Task B1 — `pkg/blueprint/{types,conversions}.go`

**Files:**
- Create: `pkg/blueprint/types.go`
- Create: `pkg/blueprint/conversions.go`
- Modify: `pkg/blueprint/interface.go` — `Validate(Blueprint)`, not `*aifv1.Blueprint`

Mirror the `pkg/bundle/{types,conversions}.go` pattern. `Blueprint` domain struct fields: `Name`, `Version`, `Source`, `Components`, `PublishedAt`, `PublishedBy`, `UseCase`, `Description`, `Status`. No `metav1.*`, no `aifv1.*`.

`conversions.go` exposes `FromCR(*aifv1.Blueprint) Blueprint` and `ToCR(Blueprint) *aifv1.Blueprint`.

`interface.go`:

```go
type Validator interface {
	Validate(bp Blueprint) error
}
```

(Rename: `Manager` → `Validator`, since the report banned `Manager` and validation is the only behaviour here.)

`pkg/blueprint/manager.go.Validate` becomes `Validate(bp Blueprint) error` (still a free function, takes domain type now).

`internal/controller/blueprint_controller.go` calls `blueprint.Validate(blueprint.FromCR(&bp))`.

Tests in `pkg/blueprint/manager_test.go` get a small adjustment: build domain `Blueprint` literals instead of `*aifv1.Blueprint`. Lower the test's coupling to the CRD shape.

Commit: `decouple pkg/blueprint from api/v1alpha1; rename Manager to Validator`.

---

## Task B2 — Lift `aifv1.*` types out of `pkg/bundle/types.go`; drop the in-memory cache

**Files:**
- Modify: `pkg/bundle/types.go` (replace `aifv1.*` field types with domain types)
- Modify: `pkg/bundle/conversions.go` (expand)
- Delete: `pkg/bundle/manager.go` (or strip to just delegating to Repository)
- Delete: `pkg/bundle/interface.go.Manager` (replace with `Service`)
- Modify: `internal/controller/bundle_controller.go` (depend on `bundle.Service` + `bundle.Repository`)

This is the **directive #1** task — drop the in-memory cache.

- [ ] **Step 1: Add domain types for the leaf structs**

In `pkg/bundle/types.go`:

```go
type Submission struct { ProposedVersion, ChangeDescription, SubmittedBy string; SubmittedAt time.Time; GenerationAtSubmit int64 }
type Review struct { ReviewerComment, ReviewedBy string; ReviewedAt time.Time }
type ComponentRef struct { Name, Repo, Chart, Version string /* + values ref */ }
type PublishedVersionRef struct { BlueprintName, Version, PublishedBy string; PublishedAt time.Time }
```

Update the existing `Bundle` struct's `Submission`, `Review`, `Components`, `PublishedVersions` fields to use these domain types.

- [ ] **Step 2: Expand `conversions.go`** with `submissionFromCR`/`submissionToCR`, etc.

- [ ] **Step 3: Replace `bundle.Manager` with `bundle.Service`**

In `pkg/bundle/interface.go`:

```go
type Service interface {
	// Validate checks business rules NOT covered by kubebuilder markers.
	// (The kubebuilder-vs-Service dedup is open per user directive #3 —
	// duplicate is acceptable for now.)
	Validate(b Bundle) error
}
```

The implementation in `pkg/bundle/service.go` is a free function or stateless struct.

- [ ] **Step 4: Delete `pkg/bundle/manager.go`**

The cache + sync.RWMutex go away. The `validateSpec` body moves to `service.go`. Remove `New(logger)` constructor; replace with `NewService()`.

- [ ] **Step 5: Update `internal/controller/bundle_controller.go`**

Replace `Manager bundle.Manager` field with two fields: `Service bundle.Service` + `Bundles bundle.Repository`. Replace `r.Manager.Upsert(...)` with `r.Service.Validate(bundle.FromCR(&bundleCR))`.

- [ ] **Step 6: Update `cmd/operator/main.go`**

Constructor wiring: replace `bundle.New(logger)` with `bundle.NewService()`. Add `bundles := bundle.NewK8sRepository(mgrClient)`. Inject both into the reconciler.

- [ ] **Step 7: Update `pkg/bundle/manager_test.go`** to test `Service.Validate` against the domain `Bundle` type.

- [ ] **Step 8: Build + test**

```bash
go build ./...
go test ./...
```

- [ ] **Step 9: Commit**

```bash
git add pkg/bundle/ internal/controller/bundle_controller.go cmd/operator/main.go
git rm pkg/bundle/manager.go
git commit -m "drop bundle in-memory cache; introduce bundle.Service + bundle.Repository per user directive"
```

---

## Task B5 — Split `pkg/nvidia` into Discovery + Deployer

**Files:**
- Create: `pkg/nvidia/types.go`
- Create: `pkg/nvidia/interface.go`
- Modify: `pkg/nvidia/discovery.go`, `pkg/nvidia/deployer.go`

Per ARCHITECTURE.md §6.2:1377-1387 + report §1C row 20:

```go
// Discovery enumerates SUSE-mirrored NIM models from the SUSE Registry chart index.
type Discovery interface {
	Index(ctx context.Context) ([]NIMEntry, error)
	Refresh(ctx context.Context) error
	LookupModel(ctx context.Context, name string) (NIMEntry, error)
	UpdateSettings(ctx context.Context, s EngineSettings) error
}

// Deployer generates Helm values for a given NIM. Separate from Discovery
// because they have nothing in common.
type Deployer interface {
	GenerateValues(ctx context.Context, req GenerateRequest) (map[string]any, error)
}
```

`types.go` defines `NIMEntry`, `GenerateRequest`, `EngineSettings`. None of these import `api/v1alpha1` (per the new CLAUDE.md layering rule).

Commit: `split pkg/nvidia into Discovery + Deployer ports per ISP`.

---

## Task C6 — `internal/webhook/registry.go`

**Files:**
- Create: `internal/webhook/registry.go`
- Modify: `internal/manager/setup.go`

```go
package webhook

import "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

// Validator pairs a path with an admission validator.
type Validator struct {
	Path    string
	Handler admission.Handler
}

// Validators returns every webhook this operator serves. Add new entries
// here, not in setup.go.
func Validators() []Validator {
	return []Validator{
		{Path: "/validate-blueprints", Handler: NewBlueprintImmutability()},
		// future: {Path: "/validate-bundles", Handler: NewBundleValidator()},
	}
}
```

`setup.go` iterates the slice instead of hand-registering each path. Commit: `extract webhook registry slice; setup.go iterates instead of per-webhook wiring`.

---

## Task B3 — `pkg/workload/{types,interface}.go`

Pre-place these BEFORE P5-1 starts (gate S3 in `SCHEDULE_3DEV.md`).

```go
// pkg/workload/types.go
type Workload struct { ... }
type Source struct { Kind SourceKind; App *AppRef; Blueprint *BlueprintRef; BundleTest *BundleTestRef }
type Phase string  // mirrors aifv1.WorkloadPhase but lives in domain

// pkg/workload/interface.go
type Repository interface { Get/List/Update/UpdateStatus } // ≤4
type DeploymentCounter interface { CountByBlueprint(...) (int32, error) }
type StateMachine interface { Compute(ctx context.Context, w Workload, releases []ReleaseStatus) Phase }
type Deployer interface { Deploy(ctx context.Context, w Workload) error; Uninstall(ctx context.Context, w Workload) error }
```

Four small interfaces, each ≤4 methods. Test doubles live in `pkg/workload/fake_*.go`. Commit: `pre-place pkg/workload ports for P5-1 — types, Repository, DeploymentCounter, StateMachine, Deployer`.

---

## Task C1 — `pkg/helm/interface.go` (Engine + Releaser)

Per ARCHITECTURE.md §6.2:1284-1334. Split deliberately:

```go
type Engine interface {
	InstallChart(ctx context.Context, req InstallRequest) (ReleaseStatus, error)
	Uninstall(ctx context.Context, ns, name string) error
	Status(ctx context.Context, ns, name string) (ReleaseStatus, error)
	UpdateSettings(ctx context.Context, s EngineSettings) error
}

type Releaser interface {
	Rollback(ctx context.Context, ns, name string, revision int) error
	History(ctx context.Context, ns, name string) ([]ReleaseRevision, error)
}
```

Adapter file: `pkg/helm/sdk_engine.go` wrapping `helm.sh/helm/v3`. Hand-written fake in `pkg/helm/fake_engine.go`. Commit: `split pkg/helm into Engine + Releaser ports; sdk_engine adapter scaffolded`.

---

## Task C2 — `pkg/git/interface.go`

Per ARCHITECTURE.md §6.2:1339-1369. Single interface:

```go
type FleetEngine interface {
	Push(ctx context.Context, req PushRequest) (CommitRef, error)
	Remove(ctx context.Context, req RemoveRequest) error
	UpdateSettings(ctx context.Context, s EngineSettings) error
}
```

Adapter: `pkg/git/go_git_engine.go` using `go-git/v5`. Commit: `pkg/git.FleetEngine port; go-git adapter scaffolded`.

---

## Task B4 — `pkg/source_collection/*`

Per ARCHITECTURE.md §6.2:1410-1440 + report §1C row 19. Currently only `.gitkeep`. Author the full quartet:

- `types.go` — `App`, `ChartMetadata`
- `interface.go` — `Client` (List/GetChart/UpdateSettings) + `Source()` helper
- `api_client.go` — HTTP adapter (`api.apps.rancher.io`)
- `oci_fallback.go` — OCI catalog walk adapter (used when API is unreachable; air-gap path)
- `fake_client.go` — in-memory test double
- `errors.go` — sentinel errors (`ErrUnreachable`, `ErrNotFound`, `ErrUnauthorized`)

Commit: `pkg/source_collection: types + Client port + HTTP and OCI-fallback adapters + fake`.

---

## Task E1 — Pull `client.Client` out of reconcilers (LARGE — multi-PR)

Depends on C4. After C4 lands, every reconciler can replace `r.Get/r.List/r.Status().Update` with calls through the appropriate Repository.

End state:
```go
type BundleReconciler struct {
	Bundles  bundle.Repository
	Service  bundle.Service
	Recorder record.EventRecorder
	// no client.Client, no Scheme (those move into the Repository adapter)
}
```

Migration is one CRD at a time, in this order: Bundle → Settings → Workload → Blueprint. Each migration is a self-contained PR with green tests.

**Acceptance gate per CRD:**
1. Reconciler unit tests run without envtest (use the hand-written `Repository` fake).
2. `internal/controller/{x}_controller_test.go` compiles without importing `sigs.k8s.io/controller-runtime/pkg/client/fake`.

Commit pattern per migration: `migrate {CRD}Reconciler off client.Client onto {pkg}.Repository`.

---

## Self-review checklist

- [ ] Every task has a clear scope and commit message — no "TBD".
- [ ] User directives are visibly tied to the tasks that satisfy them (cache-drop → B2 + C4; publish→ports → C3; defer E2 → mentioned in scope).
- [ ] Every new `interface.go` is ≤4 methods or split deliberately.
- [ ] No new file under `pkg/{helm,git,nvidia,source_collection}` imports `api/v1alpha1`.
- [ ] No reconciler change in this plan removes the `+kubebuilder:rbac:` markers (RBAC is still generated via `make manifests`).
- [ ] The plan does not touch `api/v1alpha1/*_types.go`.

---

## Open questions

These are NOT blockers; they should be answered when convenient:

1. (Carried from review) Is the `validateSpec` business-rule duplication of kubebuilder markers intentional defence-in-depth? Until answered, leave duplicates in place per user directive #3.
2. Should `pkg/settings/repository.go` exist as its own package, or should Settings be a child of `pkg/conditions`? (Probably its own package; this plan assumes so. Re-evaluate if it ends up with only one method.)
3. Does `pkg/authz` need to land before P1-10's RequirePublisher middleware ships, or can the middleware's SAR call be inlined for one story and refactored later? (Report says inline-first; revisit when P1-10 is in flight.)
