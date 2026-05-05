# Three-Developer Domain-Decomposed Schedule

> **Status:** Proposal — discuss before adopting.
> **Audience:** Engineering leads picking an execution model for the AIF v1.1 build.
> **Relationship to PROJECT_PLAN.md:** This document is an **execution overlay**. PROJECT_PLAN.md remains the dependency-truth document; nothing here changes story IDs, acceptance criteria, or the dependency graph. This file decides *who* runs *which* story when more than one of them is unblocked.

---

## Why an overlay (and why not just follow PROJECT_PLAN.md)

PROJECT_PLAN.md is **layered**: Phase 1 = "all controllers", Phase 4 = "deployment engine", Phase 6 = "all UI". With one or two contributors that's fine — there's enough work in each layer to keep everyone busy. With **three** contributors who can each carry a stack vertically, layered execution leaves people waiting for cross-phase prerequisites and forces every UI story to land after Phase 5 closes.

Domain decomposition slices the same 76 stories along the four-noun model, so each developer can carry their domain end-to-end (CRD → controller → REST → UI) once the foundation is in place. The dependency graph in PROJECT_PLAN.md remains authoritative; this overlay simply assigns who owns each node.

---

## The three slices

| Dev | Domain | Owns end-to-end |
|---|---|---|
| **A — Authoring & Governance** | Bundle, Publish workflow, Publisher RBAC, Bundle UI | `api/v1alpha1/bundle_types.go` · `pkg/bundle/*` · `pkg/publish/*` · `internal/controller/bundle_controller.go` · `internal/api/bundles.go` · `ui/.../bundles/*` · l10n keys for bundles |
| **B — Deploy & Operate** | Blueprint, Workload, immutability webhook, Helm/Fleet engines, Workloads UI, install wizard | `api/v1alpha1/{blueprint,workload}_types.go` · `pkg/{blueprint,workload,helm,git}/*` · `internal/controller/{blueprint,workload}_controller.go` · `internal/webhook/blueprint_immutability.go` · `internal/api/{blueprints,workloads}.go` · `ui/.../{blueprints,workloads,install}/*` |
| **C — Platform & Plumbing** | Apps catalog, NIM discovery, Settings (incl. air-gap), operator chart, RBAC/Security, observability, CI, release | `pkg/{apps,nvidia,source_collection}/*` · `pkg/conditions/*` · `internal/controller/settings_controller.go` · `internal/api/{apps,settings,nvidia}.go` · `internal/manager/*` · `charts/*` · `ui/.../{apps,settings,overview}/*` · CI · all of Phases 7, 8, 9 |

Dev C is heavier (≈32 stories vs ≈13/16). That's deliberate: platform plumbing was always going to be wide. In practice Dev A and B finish their domain verticals earlier and pivot to help on Phases 7-9.

---

## Joint up-front work (week 1, the whole team)

These nine stories must land collaboratively before the slices diverge — they define the shapes everyone else builds on:

- **P0-0** Bootstrap CLAUDE.md (already landed)
- **P0-1** Go module + repo layout (already landed)
- **P0-2** Five CRD Go types — design sync needed; all three review schemas before merging
- **P0-4** Operator entry point — `cmd/operator/main.go` is append-only afterward
- **P1-7** Manager / setup.go wiring — append-only afterward
- **P1-9** CLAUDE.md controller-pattern recipe (append-only edit)
- **P3-7** CLAUDE.md publish-workflow recipe (append-only edit)
- **P6-0** CLAUDE.md UI-extension recipe (append-only edit)
- **P9-5** CLAUDE.md final polish — joint at end of project

**Sync rule:** anything that touches a documented "conflict hotspot" file from `PROJECT_PLAN.md §Coordination Notes` is append-only. No rewrites of existing lines.

---

## Interface-freeze sync points (one-hour design huddles)

When the dependency graph forces a contract that more than one slice will consume, freeze it in a 60-minute sync, then proceed. Don't ship to main without all three signing off.

| Sync | Trigger | What gets locked |
|---|---|---|
| **S1** | Before P3-1 | `pkg/bundle.Repository`, `pkg/publish.Workflow` interfaces (signatures + sentinel errors). Per user directive (memory `feedback_oop_directives.md`), `Workflow` takes Repository ports, never `client.Client`. |
| **S2** | Before P4-1 | `pkg/helm.Engine`, `pkg/helm.Releaser`, `pkg/git.FleetEngine` interfaces. ARCHITECTURE.md §6.2 already drafts most of these. |
| **S3** | Before P5-1 | `pkg/workload.Repository`, `pkg/workload.StateMachine`, the WorkloadPhase domain enum. Pre-place files even if the bodies come later. |
| **S4** | Before P1-10 | REST handler middleware contract: request_id, error envelope, RequirePublisher SAR call, structured logging. Each subsequent handler story plugs in via this. |

If a slice can't agree on an interface in 60 minutes, escalate — don't merge a half-baked port and refactor later.

---

## Phase 7 convergence

All five Phase 7 stories (P7-1..P7-5) are independently parallelizable per `PROJECT_PLAN.md §Parallelization Matrix` row 7. At Phase 7 the team **converges back to within-phase parallelism** — Dev A takes P7-5 (publisher SAR enforcement, in their domain), Dev C takes the rest, Dev B can either help Dev C or pull Phase 8 work forward. The layered plan shines here; keep it.

---

## Per-story assignment

Default reading: a row owned by **A**, **B**, or **C** is fully owned by that developer. **J** = joint. **B+C** etc. = primary/support; primary owner does the work, support reviews.

### Phase 0 — Foundation

| Story | Owner | Notes |
|---|---|---|
| P0-0 Bootstrap CLAUDE.md | J | Done |
| P0-1 Go module + layout | J | Done |
| P0-2 CRD Go types | J | Design sync first |
| P0-3 Operator Helm chart | C | DevOps |
| P0-4 Operator entry point | J | Append-only after first land |
| P0-5 aif-ui Helm chart | C | DevOps |
| P0-6 Air-gap chart values | C | |
| P0-7 Webhook TLS helm-hook | C | Needs P1-5 (B) to land first |

### Phase 1 — Controllers + Webhook

| Story | Owner | Notes |
|---|---|---|
| P1-1 BundleReconciler | A | (Largely landed) |
| P1-2 BlueprintReconciler | B | (Largely landed) |
| P1-3 WorkloadReconciler scaffold | B | (Largely landed) |
| P1-4 SettingsReconciler | C | (Largely landed) |
| P1-5 Blueprint immutability webhook | B | (Landed) |
| P1-6 InstallAIExtension reconciler | C | UIPlugin install plumbing |
| P1-7 manager/setup.go wiring | J | Append-only |
| P1-8 envtest harness | C | Tests |
| P1-9 CLAUDE.md controller pattern | J | Append-only |
| P1-10 HTTP API skeleton + middleware | C | **S4 sync first** |

### Phase 2 — Catalog (all Dev C)

| Story | Owner | Notes |
|---|---|---|
| P2-1 NIM discovery from SUSE Registry | C | |
| P2-2 SUSE Application Collection sync | C | |
| P2-3 Unified Apps catalog | C | Needs P2-1 |
| P2-4 `/api/v1/apps*` endpoints | C | Needs S4 |
| P2-5 Vendor Reference Blueprint wrapping | C+B | C drives; B reviews because it writes Blueprint CRs |
| P2-6 `/api/v1/nvidia/nims*` endpoints | C | Needs S4 |

### Phase 3 — Publish Workflow (Dev A heavy)

| Story | Owner | Notes |
|---|---|---|
| P3-1 Bundle.Manager + Publish.Workflow interfaces | A | **S1 sync first** |
| P3-2 Submit Bundle endpoint | A | |
| P3-3 Withdraw submission endpoint | A | |
| P3-4 Request Changes endpoint | A | |
| P3-5 Approve (mints Blueprint) | A+B | A drives the workflow; B reviews because it creates Blueprint CRs |
| P3-6 Pending review queue | A | |
| P3-7 CLAUDE.md publish recipe | J | Append-only |
| P3-8 Pre-flight check | A | Cross-domain — needs P5-7 (C) and P4-6 (B); A owns because it's a Bundle action |

### Phase 4 — Deployment Engine (Dev B heavy)

| Story | Owner | Notes |
|---|---|---|
| P4-1 Helm engine | B | **S2 sync first** |
| P4-2 Workload deploy logic | B | |
| P4-3 Fleet/Git engine | B | |
| P4-4 NIM resource sizing | B | |
| P4-5 Deploy / test-deploy endpoints | B | Needs S4 |
| P4-6 Image rewrite | B | Needs P5-7 (C) |

### Phase 5 — Workload Runtime + Settings

| Story | Owner | Notes |
|---|---|---|
| P5-1 Workload phase computation | B | **S3 sync first** |
| P5-2 Automatic recovery | B | Needs P5-1 |
| P5-3 Workload upgrade | B | |
| P5-4 Settings → engines push | C | Replaces dead cache (already removed); see `feedback_oop_directives.md` |
| P5-5 Pull-secret reconciler | C | |
| P5-6 Auth/publisher endpoint | A | Owned by A because it's the publisher gate |
| P5-7 Settings air-gap fields | C | **Promote to right after P0-2** per PROJECT_PLAN.md §Coordination Notes |
| P5-8 Catalog discovery fallback | C | Needs P5-7 |
| P5-9 Test-connection endpoint | C | Needs P5-7 |

### Phase 6 — UI Extension

| Story | Owner | Notes |
|---|---|---|
| P6-0 CLAUDE.md UI structure | J | Append-only |
| P6-1 Extension skeleton | C | C lands shared UI infra; A and B build on it |
| P6-2 Bundles list page | A | |
| P6-3 Bundle wizard | A | |
| P6-4 Submit/pending review UI | A | Needs P5-6 (A) |
| P6-5 Blueprints page | B | |
| P6-6 Workloads page | B | |
| P6-7 Apps catalog page | C | |
| P6-8 Install/Deploy wizard | B | Cross-cutting; B owns because it produces Workloads |
| P6-9 Settings page | C | Needs P5-7 (C) |
| P6-10 Overview page | J | Touches all four nouns; pick whoever has bandwidth |

### Phase 7 — Security (convergence; mostly C)

| Story | Owner | Notes |
|---|---|---|
| P7-1 ClusterRole audit | C | |
| P7-2 Pull-secret fail-closed | C | |
| P7-3 NetworkPolicy template | C | |
| P7-4 cert-manager webhook TLS | C | |
| P7-5 Publisher SAR enforcement | A | A's domain |

### Phase 8 — Observability + Tests

| Story | Owner | Notes |
|---|---|---|
| P8-1 Metrics endpoint | C | |
| P8-2 Structured logging audit | J | Each owner adds request_id/component to their handlers |
| P8-3 Controller events audit | J | Each owner adds events to their reconcilers |
| P8-4 Envtest coverage gate | C | Tests |
| P8-5 HTTP integration tests | J | Each owner tests their endpoints |
| P8-6 k3d E2E smoke (was kind) | C | Use k3d per `feedback_local_cluster.md` |

### Phase 9 — Production Readiness

| Story | Owner | Notes |
|---|---|---|
| P9-1 Multi-arch images | C | |
| P9-2 Cosign + SBOM | C | |
| P9-3 Production chart values | C | |
| P9-4 User guides | J | Split by domain — A writes Bundle/Publish guide, B writes Deploy/Operate guide, C writes Install/Settings guide |
| P9-5 CLAUDE.md polish | J | The only story allowed to delete content |
| P9-6 Air-gap release bundle | C | Needs P9-1, P9-2, P0-6, P0-7 |
| P9-7 Air-gap install guide | C | Needs P9-6, P5-7 |

---

## Story totals per slice

| Slice | Solo | Cross-slice (primary) | Joint | Total touched |
|---|---|---|---|---|
| A — Authoring | 13 | 1 (P3-5 with B) | 14 | 28 |
| B — Deploy/Operate | 14 | 2 (P3-5, P2-5 support) | 14 | 30 |
| C — Platform/Plumbing | 32 | 1 (P2-5) | 14 | 47 |

(Total touches > 76 because joint stories are counted once per slice.)

---

## Where domain decomposition wins (concretely)

- **Dev A doesn't wait for Phase 6 to start UI.** Once the UI scaffold (P6-1) lands, A picks up P6-2 in parallel with their P3-x backend work.
- **Dev B can develop the deployment engine and its UI concurrently.** P4-1 (Helm engine) and P6-5/P6-6 (Blueprint/Workload pages) only share `api/v1alpha1/{blueprint,workload}_types.go` — frozen up front.
- **Reviews stay within domain.** A reviews A's PRs. B reviews B's. Less context switching, faster review turnaround.
- **Conway's law works *for* you.** Each PR is contained in one slice's directories.

## Where it loses (be honest)

- **Interface-freeze syncs are gates.** If S1 isn't ready when P3-1 starts, A blocks B and C. Mitigation: schedule the sync ahead of when the next story actually starts.
- **PTO leaves a vertical idle.** A solo on PTO means no Bundle progress for the duration. Mitigation: cross-train at sync points; B and C can land small A-track stories like P3-3 (withdraw) if necessary.
- **Cross-domain stories need a designated owner.** P3-5 (approve), P3-8 (preflight), P2-5 (vendor wrap), P6-8 (install wizard) all touch multiple slices. The table above picks an owner; don't let them sit ownerless.

---

## When to abandon this overlay

Switch back to layered execution if:

1. **A slice falls more than two stories behind** the other two for two consecutive weeks — rebalance manually.
2. **Sync points are taking more than an hour** routinely — the design isn't ready; pause and resolve in PROJECT_PLAN/ARCHITECTURE before continuing.
3. **PRs from one slice are blocking PRs from another** more than 20% of the time — the ports aren't really decoupled; revisit interface design.

If two of these trip in the same iteration, fall back to the matrix in PROJECT_PLAN.md §Parallelization Matrix and go layer-by-layer.

---

## Next steps to make this real

1. Discuss this proposal with the team; resolve disagreements on the two cross-slice stories (P3-5, P2-5).
2. Schedule the four interface-freeze syncs in the calendar tied to the prerequisite stories.
3. Add `Owner: A | B | C | J` to each story in PROJECT_PLAN.md, OR keep this overlay as the canonical assignment doc.
4. After two weeks, retro: did slices stay independent? If not, what crossed the boundary, and is the boundary in the wrong place?
