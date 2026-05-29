# SUSE AI Lifecycle Manager vs SUSE AI Factory — Feature Comparison

> **Purpose:** Track which features from `suse-ai-lifecycle-manager` (LCM) have been ported to `suse-ai-factory` (AIF), which are new in AIF, and which gaps remain.  

---

## Table of Contents

1. [CRD & Resource Type Comparison](#1-crd--resource-type-comparison)
2. [Backend Comparison](#2-backend-comparison)
   - 2.1 [Controllers](#21-controllers)
   - 2.2 [REST API Endpoints](#22-rest-api-endpoints)
   - 2.3 [Business Logic Packages](#23-business-logic-packages)
3. [UI / Frontend Comparison](#3-ui--frontend-comparison)
   - 3.1 [Pages](#31-pages)
   - 3.2 [Components](#32-components)
   - 3.3 [Services & Utilities](#33-services--utilities)
4. [Feature-by-Feature Gap Analysis](#4-feature-by-feature-gap-analysis)
   - 4.1 [Features Ported to AIF](#41-features-successfully-ported-to-aif)
   - 4.2 [Features New in AIF](#42-features-new-in-aif-not-in-lcm)
   - 4.3 [Gaps: LCM Features Not Yet in AIF](#43-gaps-lcm-features-not-yet-in-aif)
5. [Helm Charts Comparison](#5-helm-charts-comparison)
6. [Infrastructure & Tooling Comparison](#6-infrastructure--tooling-comparison)
7. [Known Bugs in AIF](#7-known-bugs-in-aif)
8. [Recommendations & Prioritization](#8-recommendations--prioritization)

---
## 1. CRD & Resource Type Comparison

### 1.1 CRD Mapping

| CRD                       | LCM                                                                                                              | AIF                                                                                                                                                                                                         | Delta                                                                                                                      |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| **AIWorkload / Workload** | `AIWorkload` — source (App/Blueprint), targetClusters, deployStrategy (Helm/FleetBundle/GitOps), componentValues | `Workload` — source (App/Blueprint/BundleTest), deployStrategy (helm/gitops), replicas, update strategy (Rolling/BlueGreen/Canary/AutoRecovery), scaling (HPA/VPA), deploymentHistory, recoveryFailureCount | AIF adds BundleTest source, update strategies, scaling, deployment history, recovery state machine                         |
| **Blueprint**             | Namespaced; displayName, version, description, deprecated, components[]                                          | Cluster-scoped; blueprintName (lineage), version, useCase, source discriminator (Published/WrapsVendorChart), components[], valueOverrides, publishedBy/At, deprecationStatus, deploymentCount              | AIF adds governance (immutability webhook), lineage tracking, vendor-chart wrapping, deployment counting, use-case tagging |
| **Bundle**                | Not present                                                                                                      | Namespaced; Draft/Submitted/ChangesRequested phases, title, description, useCase, authors, components, valueOverrides, testDeploys, publishedVersions, submission/review status                             | **New in AIF** — publish-by-approval workflow                                                                              |
| **Settings**              | Fleet config, AppCo creds, SUSE Registry creds, registry endpoints, image rewrite, catalog discovery             | Same + BlueprintClassification (force reference blueprint / building block overrides)                                                                                                                       | Nearly equivalent; AIF adds classification overrides                                                                       |
| **InstallAIExtension**    | Helm config, extension metadata, phase                                                                           | Same structure                                                                                                                                                                                              | Equivalent                                                                                                                 |

### 1.2 CRD Field Detail — AIWorkload (LCM) vs Workload (AIF)

| Field                             | LCM AIWorkload                     | AIF Workload                                                          |
| --------------------------------- | ---------------------------------- | --------------------------------------------------------------------- |
| `spec.displayName`                | Yes                                | Yes (`spec.name`)                                                     |
| `spec.source.sourceType`          | App, Blueprint                     | App, Blueprint, **BundleTest**                                        |
| `spec.targetClusters`             | Yes (cluster IDs)                  | Yes (informational)                                                   |
| `spec.deployStrategy`             | Helm, FleetBundle, GitOps          | helm, **gitops** (no FleetBundle enum)                                |
| `spec.componentValues`            | Yes                                | Yes (`spec.valueOverrides`)                                           |
| `spec.replicas`                   | No                                 | **Yes** (default 1)                                                   |
| `spec.strategy`                   | No                                 | **Yes** (RollingUpdate, BlueGreen, Canary, AutomaticRecovery)         |
| `spec.scaling`                    | No                                 | **Yes** (HPA min/max/targetCPU, VPA updateMode)                       |
| `spec.paused`                     | No                                 | **Yes**                                                               |
| `status.phase`                    | Pending, Running, Degraded, Failed | Pending, Deploying, Running, Degraded, Failed, **RecoveryInProgress** |
| `status.clusterStatuses`          | Per-cluster phase + message        | **Per-component release status**                                      |
| `status.deploymentHistory`        | No                                 | **Yes** (ordered revision records)                                    |
| `status.recoveryFailureCount`     | No                                 | **Yes**                                                               |
| `status.observedBundleGeneration` | No                                 | **Yes** (drift detection for BundleTest)                              |

### 1.3 CRD Field Detail — Blueprint

| Field | LCM Blueprint | AIF Blueprint |
|---|---|---|
| **Scope** | Namespaced | **Cluster-scoped** |
| `spec.displayName` | Yes | Yes |
| `spec.version` | Yes (semver) | Yes (semver, immutable) |
| `spec.description` | Yes | Yes |
| `spec.deprecated` | Yes (boolean) | **Phase-based** (Active/Deprecated/Withdrawn) |
| `spec.components[]` | chartRepo, chartName, chartVersion, values | Same + componentRef discriminated union (App/Blueprint) |
| `spec.useCase` | No | **Yes** (rag/vision/fine-tuning/inference/other) |
| `spec.source` | No | **Yes** (Published/WrapsVendorChart discriminator) |
| `spec.valueOverrides` | Via components[].values | **Separate field** (per-component YAML strings) |
| `status.deploymentCount` | No | **Yes** (active workload count) |
| `status.deprecationStatus` | No | **Yes** (reason, actionedBy, actionedAt) |
| **Immutability** | Not enforced | **Webhook-enforced** (spec changes rejected after creation) |

---

## 2. Backend Comparison

### 2.1 Controllers

| Controller | LCM | AIF | Notes |
|---|---|---|---|
| **AIWorkload / Workload** | Reconciles via Helm/Fleet/GitOps, derives phase from cluster statuses | Reconciles via Deployer port, formal phase state machine, recovery counter, requeue cadence by phase | AIF more robust |
| **Blueprint** | Basic validation and deprecation | Validates spec, computes deploymentCount from Workloads, deletion protection | AIF adds governance |
| **Bundle** | Not present | Draft/Submitted/ChangesRequested reconciliation, partial-approval self-healing | **New in AIF** |
| **Settings** | Watches Settings CR, updates operator config | Resolves Secret refs, translates to SettingsSnapshot, fans out to all engines via bus | AIF more decoupled |
| **InstallAIExtension** | Helm install + UIPlugin + ClusterRepo management | Helm install + UIPlugin CRD check | Equivalent core; LCM has more ClusterRepo logic |

### 2.2 REST API Endpoints

| Endpoint | LCM | AIF | Notes |
|---|---|---|---|
| **List/Get Apps** | Via Rancher ClusterRepo + App Collection service (frontend-side) | `GET /api/v1/apps`, `/apps/categories`, `/apps/{id}` — server-side catalog | AIF has dedicated catalog API |
| **List/CRUD AIWorkloads** | `GET/POST/PATCH/DELETE /api/v1/aiworkloads` | `GET /api/v1/workloads`, `POST .../upgrade` | LCM has full CRUD; AIF has list + upgrade only (CRUD via Steve store) |
| **List/CRUD Blueprints** | `GET/POST/PATCH/DELETE /api/v1/blueprints` | Via Steve store (K8s API directly) | LCM has custom REST; AIF uses native K8s API |
| **Publish Workflow** | Not present | `POST /bundles/{ns}/{name}/submit\|withdraw\|approve\|request-changes` | **New in AIF** |
| **NVIDIA NIM** | Not present | `GET /api/v1/nvidia/nims`, `/nims/{id}`, `POST /nvidia/refresh` | **New in AIF** |
| **Settings** | `GET/PUT /api/v1/settings` | `GET/PUT /api/v1/settings` | Equivalent |
| **Health/Metrics** | Basic healthz | healthz + readyz + Prometheus metrics | AIF more complete |

### 2.3 Business Logic Packages

| Package | LCM | AIF | Notes |
|---|---|---|---|
| **apps (catalog)** | `services/app-collection.ts` (frontend) | `pkg/apps/` — Aggregator, NVIDIASource, AppCoSource, stale-but-good cache | AIF has server-side catalog with multi-source aggregation |
| **bundle** | Not present | `pkg/bundle/` — Manager, Repository, Conversions | **New in AIF** |
| **blueprint** | `utils/blueprint-api.ts` (frontend helpers) | `pkg/blueprint/` — Manager, Wrapper (vendor-chart auto-wrapping), Repository | AIF much richer |
| **workload** | `services/app-lifecycle-service.ts` + `services/rancher-apps.ts` | `pkg/workload/` — Deployer, Upgrader, Phase state machine, Repository | AIF more structured |
| **publish** | Not present | `pkg/publish/` — Submit/Withdraw/Approve/RequestChanges workflow | **New in AIF** |
| **helm** | `infra/helm/` — action, chart, values, index | `pkg/helm/` — Engine (install/uninstall/rollback/status/history), 6-layer values merge, image rewrite | AIF more complete |
| **nvidia** | Not present | `pkg/nvidia/` — Discovery, Deployer, AnnotationReader, RegistryClient, Classifier | **New in AIF** |
| **source_collection** | Not present (uses Rancher ClusterRepo) | `pkg/source_collection/` — API client, OCI fallback, AnnotationReader | **New in AIF** |
| **git (Fleet)** | `services/fleet-bundle.ts`, `services/git-publish.ts` (frontend) | `pkg/git/` — **Stub** (single placeholder file) | LCM is more complete |
| **helm_oci** | Not present | `pkg/helm_oci/` — Chart.yaml parsing, OCI manifest, blob I/O | **New in AIF** |
| **conditions** | Not present | `pkg/conditions/` — Type/Reason constants, set helpers | **New in AIF** |
| **Fleet bundle creation** | `services/fleet-bundle.ts` — builds Fleet Bundle CRs | Not implemented in backend | **Gap in AIF** |

---

## 3. UI / Frontend Comparison

### 3.1 Pages

| Page | LCM Status | LCM Lines | AIF Status | AIF Lines | Gap |
|---|---|---|---|---|---|
| **Overview / Dashboard** | Complete (summary cards, recent workloads, blueprints, quick actions, 10s auto-refresh) | 469 | **Stub** ("Coming Soon") | 22 | HIGH |
| **Apps Catalog** | Complete (search, repo filter, installed filter, tile/list views, external links, install actions) | 1,230 | Complete (search, source filter, category filter, ref-blueprint toggle, tile view, refresh) | 535 | Partial parity — LCM has list view mode, installed filter, per-cluster status |
| **Install Wizard** | Complete (delegates to AppWizard — 5 steps, multi-cluster, progress tracking) | 13 + wizard | Not present | — | HIGH |
| **App Instances** | Complete (per-app deployments across clusters, search, filter by cluster/status, delete/manage actions) | 1,302 | Not present | — | HIGH |
| **Manage / Edit** | Complete (delegates to AppWizard in manage mode — edit values, upgrade version) | 13 + wizard | Not present | — | HIGH |
| **Blueprints** | Complete (search, deprecated toggle, tile grid, version selector, install/edit/copy/delete actions, active workload warnings) | 577 | Complete (search, use-case filter, version picker, lineage cards, versions panel, show-withdrawn toggle) | 260 | Partial parity — LCM has create/edit/copy/delete actions; AIF has use-case filter, versions panel |
| **Blueprint Create** | Complete (4-step wizard: BasicInfo, SelectApps, Config, Review) | 78 + wizard | Not present | — | MEDIUM |
| **Blueprint Install** | Complete (deploy blueprint to clusters with per-component config) | 17 + wizard | Not present | — | HIGH |
| **AI Workloads** | Complete (search, phase/source filter, bulk delete, upgrade blueprint version, per-cluster drill-down, 10s auto-refresh) | 634 | **Stub** ("Coming Soon") | 22 | HIGH |
| **Bundles** | Not present | — | **Stub** ("Coming Soon") | 22 | N/A (new in AIF) |
| **Pending Reviews** | Not present | — | **Stub** ("Coming Soon") | 22 | N/A (new in AIF) |
| **Settings** | Complete (Fleet, AppCo, SUSE Registry, registry endpoints, catalog discovery, image rewrite — expandable sections) | 700 | Complete (same sections, form-based editing) | 637 | Near parity |

### 3.2 Components

| Component | LCM | AIF | Gap |
|---|---|---|---|
| **AppWizard** (multi-step install/manage) | Complete — orchestrates 5 steps, multi-cluster, 3 deploy strategies, progress tracking | Not present | HIGH |
| **BlueprintCreateWizard** (4-step blueprint authoring) | Complete — BasicInfo, AppSelector, Config, Review | Not present | MEDIUM |
| **BlueprintInstallWizard** (blueprint deployment) | Complete — target selection, component config | Not present | HIGH |
| **BasicInfoStep** (release name, namespace) | Complete | Not present | Part of wizard gap |
| **TargetStep** (multi-cluster picker) | Complete | Not present | Part of wizard gap |
| **ValuesStep** (YAML editor) | Complete | Not present | Part of wizard gap |
| **ReviewStep** (pre-install confirmation) | Complete | Not present | Part of wizard gap |
| **InstallProgressModal** (real-time multi-cluster progress) | Complete | Not present | HIGH |
| **ClusterSelect** (multi-select cluster picker) | Complete | Not present | Part of wizard gap |
| **ClusterResourceTable** (K8s resource listing) | Complete | Not present | MEDIUM |
| **ValuesYaml** (YAML editor component) | Complete | Not present | Part of wizard gap |
| **AppCard** | Complete (logo, name, description, status, links) | Complete (similar) | Near parity |
| **BlueprintCard** | Not present | Complete (lineage card with version history) | AIF ahead |
| **BlueprintVersionPicker** | Not present | Complete | AIF ahead |
| **BlueprintVersionsPanel** | Not present | Complete | AIF ahead |
| **BlueprintPhasePill** | Not present | Complete (Active/Deprecated/Withdrawn) | AIF ahead |
| **AddToBundleDialog** | Not present | Complete | AIF ahead |
| **AppStatusBadge** | Complete | Not present (uses inline badges) | Minor |
| **AppHealthIndicator** | Complete | Not present | MEDIUM |
| **ClusterChips** | Complete | Not present | LOW |
| **InstallationProgress** | Complete | Not present | Part of wizard gap |
| **ResourceUsage** | Complete | Not present | LOW |

### 3.3 Services & Utilities

| Service/Utility | LCM | AIF | Gap |
|---|---|---|---|
| **operator-api.ts** (REST client) | Complete — AIWorkload CRUD, Blueprint CRUD, Settings, Git publish | Complete — Apps, Publish workflow, NVIDIA, Settings, Workload upgrade | Different scope; both complete for their API surfaces |
| **blueprint-api.ts** | Complete — groupByFamily, latestVersion, semverCompare, slugify, CR name generation | Complete — `utils/blueprint.ts` with similar helpers | Parity |
| **rancher-apps.ts** (Helm/cluster operations) | Complete — 15+ methods for cluster, namespace, chart, repo, secret management | Not present (backend handles these) | Architecture difference — AIF delegates to backend |
| **app-collection.ts** | Complete — fetch catalog, repo lookup | Not present (backend `pkg/apps/` handles this) | Architecture difference |
| **fleet-bundle.ts** | Complete — create Fleet bundles for multi-cluster | Not present | Gap (backend stub too) |
| **git-publish.ts** | Complete — commit and push to GitOps repo | Not present | Gap (backend stub too) |
| **app-lifecycle-service.ts** | Complete — install, upgrade, delete, wait, rollback | Not present (backend `pkg/workload/` handles this) | Architecture difference |
| **chart-service.ts** | Complete — chart metadata, versions | Not present | Backend handles this |
| **chart-values.ts** | Complete — YAML parsing, merging | Not present | Backend handles this |
| **cluster-resources.ts** | Complete — K8s resource CRUD | Not present | Gap — no resource inspection |
| **cluster-service.ts** | Complete — connectivity, health | Not present | Gap — no cluster monitoring |
| **ui-persist.ts** | Complete — localStorage with TTL | Not present | LOW gap |
| **repo-auth.ts** | Complete — credential management | Not present | Backend handles this |
| **mock-api.ts** | Not present | Complete — mock fallback for development | AIF ahead |
| **date.ts** | Not present | Complete — date formatting helpers | AIF ahead |

---

## 4. Feature-by-Feature Gap Analysis

### 4.1 Features Successfully Ported to AIF

| # | Feature | LCM Implementation | AIF Implementation | Parity |
|---|---|---|---|---|
| 1 | App catalog browsing | `pages/Apps.vue` + `services/app-collection.ts` | `pages/apps.vue` + `pkg/apps/` server-side catalog | Full |
| 2 | App search (text) | Frontend filter on name/description | Frontend filter on name/displayName/description | Full |
| 3 | App category filtering | Via repository filter | `GET /api/v1/apps/categories` + dropdown | Full |
| 4 | SUSE Application Collection integration | `services/app-collection.ts` | `pkg/source_collection/` (HTTP API + OCI fallback) | Full (AIF richer) |
| 5 | SUSE Registry / NIM integration | Via catalog | `pkg/nvidia/` — dedicated Discovery, Deployer, AnnotationReader | Full (AIF richer) |
| 6 | Helm-based workload deployment | `services/rancher-apps.ts` | `pkg/helm/engine.go` (direct Helm SDK) | Full |
| 7 | Helm values merging | `services/chart-values.ts` (2 layers) | `pkg/helm/values.go` (6 layers) | Full (AIF richer) |
| 8 | Settings page (credentials, Fleet, registry) | `pages/Settings.vue` | `pages/settings.vue` | Full |
| 9 | Settings CRD (singleton config) | Settings CRD | Settings CRD | Full |
| 10 | Air-gap support (image rewrite, endpoints, discovery mode) | Settings CRD fields | Settings CRD fields + image rewrite engine layer | Full |
| 11 | InstallAIExtension CRD | CRD + controller | CRD + controller | Full |
| 12 | Blueprint listing & version management | `pages/Blueprints.vue` | `pages/blueprints.vue` | Partial (see gaps) |
| 13 | Blueprint CRD | CRD (namespaced) | CRD (cluster-scoped, immutable) | Full (AIF richer) |
| 14 | Workload CRD & lifecycle | AIWorkload CRD | Workload CRD (richer state machine) | Full (AIF richer) |
| 15 | Internationalization (i18n) | `l10n/en-us.json` | `l10n/en-us.yaml` | Full |

### 4.2 Features New in AIF (Not in LCM)

| # | Feature | AIF Implementation | Status |
|---|---|---|---|
| 1 | **Bundle CRD & composition workflow** | `api/v1alpha1/bundle_types.go`, `pkg/bundle/` | Complete (backend) |
| 2 | **Publish-by-approval governance** | `pkg/publish/workflow.go`, `internal/api/publish.go` | Complete (backend) |
| 3 | **Blueprint immutability webhook** | `internal/webhook/blueprint_immutability.go` | Complete |
| 4 | **Blueprint vendor-chart auto-wrapping** | `pkg/blueprint/wrapper.go` | Complete |
| 5 | **Blueprint use-case tagging** | `spec.useCase` (rag/vision/fine-tuning/inference/other) | Complete |
| 6 | **Blueprint deployment counting** | `status.deploymentCount` computed from Workloads | Complete |
| 7 | **Workload update strategies** | RollingUpdate, BlueGreen, Canary, AutomaticRecovery | Complete (CRD + phase machine) |
| 8 | **Workload scaling (HPA/VPA)** | `spec.scaling` with minReplicas, maxReplicas, targetCPU, VPA mode | Complete (CRD) |
| 9 | **Workload recovery state machine** | RecoveryInProgress phase, recoveryFailureCount, threshold | Complete |
| 10 | **Workload deployment history** | `status.deploymentHistory` with revision records | Complete |
| 11 | **Workload upgrade API** | `POST /api/v1/workloads/{ns}/{name}/upgrade` with 5-rule validation | Complete |
| 12 | **NVIDIA NIM first-class discovery** | `pkg/nvidia/` — OCI index walking, resource profiles, sizing | Complete |
| 13 | **NVIDIA NIM REST API** | `GET /api/v1/nvidia/nims`, `POST /nvidia/refresh` | Complete |
| 14 | **Server-side app catalog** | `pkg/apps/` aggregator with multi-source, stale-but-good cache | Complete |
| 15 | **OCI manifest parsing** | `pkg/helm_oci/` — Chart.yaml extraction, manifest parsing | Complete |
| 16 | **Hexagonal architecture** | Clean ports/adapters, ISP (<=4 methods), layering rule | Complete |
| 17 | **Condition constants library** | `pkg/conditions/` — 20+ typed reasons, CI-enforced | Complete |
| 18 | **Settings bus propagation** | `internal/manager/engine_bus.go` — fan-out to all engines | Complete |
| 19 | **Comprehensive test infrastructure** | envtest, Ginkgo, example tests, live tests, Makefile targets | Complete |
| 20 | **AddToBundleDialog** (UI) | `components/apps/AddToBundleDialog.vue` | Complete |
| 21 | **Blueprint version components** (UI) | BlueprintCard, VersionPicker, VersionsPanel, PhasePill | Complete |
| 22 | **Overview dashboard** (UI) | `pages/overview.vue` | Stub |
| 23 | **Bundles page** (UI) | `pages/bundles.vue` | Stub |
| 24 | **Pending Reviews page** (UI) | `pages/pending-reviews.vue` | Stub |

### 4.3 Gaps: LCM Features Not Yet in AIF

#### Priority: HIGH — Core user workflows

| # | Feature | LCM Implementation | AIF Status | Impact |
|---|---|---|---|---|
| G1 | **Installation Wizard (multi-step)** | `AppWizard.vue` — BasicInfo → Target → Values → Review → Progress (5 steps, 3 deploy strategies) | Not present | Users cannot install apps/blueprints through the UI |
| G2 | **Multi-cluster target selection UI** | `TargetStep.vue`, `ClusterSelect.vue` — pick clusters, assign namespaces | Not present | No cluster targeting in UI |
| G3 | **Real-time installation progress** | `InstallProgressModal.vue` — per-cluster progress with retry | Not present | No visibility into deployment status from UI |
| G4 | **AI Workloads page (full)** | `AIWorkloads.vue` — table with search, phase/source filter, bulk delete, upgrade, 10s refresh (634 lines) | Stub (22 lines, "Coming Soon") | Users cannot view or manage workloads from UI |
| G5 | **App Instances page (per-app drilldown)** | `AppInstances.vue` — all deployments of specific app, filter by cluster/status, delete/manage actions (1,302 lines) | Not present | No per-app deployment view |
| G6 | **Manage/Edit existing workloads UI** | `Manage.vue` — re-enter wizard to edit values, upgrade version | Not present | Users cannot modify running workloads from UI |
| G7 | **Blueprint Install wizard** | `BlueprintInstallWizard.vue` — deploy blueprint to clusters with per-component config | Not present | Users cannot deploy blueprints from UI |
| G8 | **Overview Dashboard (full)** | `Overview.vue` — summary cards (workload counts, running, issues, blueprints), recent workloads, active blueprints, quick actions (469 lines) | Stub (22 lines, "Coming Soon") | No platform health summary |

#### Priority: MEDIUM — Operational features

| # | Feature | LCM Implementation | AIF Status | Impact |
|---|---|---|---|---|
| G9 | **Blueprint Create wizard** | `BlueprintCreateWizard.vue` — 4-step wizard with app selector, per-component config | Not present | Users must create Blueprints only via publish-by-approval (no direct creation) |
| G10 | **Rollback support** | `app-lifecycle-service.ts` → `rollbackApp()` | Backend has `helm.Engine.Rollback()` but no API endpoint or UI action | Users cannot roll back deployments |
| G11 | **Uninstall workflow (UI)** | Delete with confirmation + progress tracking | Backend has `helm.Engine.Uninstall()` but no frontend delete action for workloads | Users cannot delete workloads from UI |
| G12 | **Blueprint deprecation/undeprecation UI** | `Blueprints.vue` — toggle deprecated, active workload warnings | AIF CRD supports Deprecated/Withdrawn but UI has no action buttons | Users cannot manage blueprint lifecycle from UI |
| G13 | **Cluster resource inspection** | `ClusterResourceTable.vue` — view Deployments/StatefulSets/DaemonSets | Not present | Users cannot inspect K8s resources backing a workload |
| G14 | **Fleet GitOps integration (backend)** | `services/fleet-bundle.ts`, `services/git-publish.ts` — full implementation | `pkg/git/` is a single stub file | GitOps deploy strategy non-functional |
| G15 | **Image pull secret management per workload** | `ensureRegistrySecretSimple()`, `ensureServiceAccountPullSecret()` | Credentials at Settings level; no per-workload secret UI | Limited credential management |
| G16 | **Chart version browsing** | `chart-service.ts` → `listChartVersions()`, `fetchChartDefaultValues()` | Not present in UI (handled via Blueprint versioning) | No chart-level version exploration |
| G17 | **App health indicators** | `AppHealthIndicator.vue` — visual health status | Phase badges only, no health component | Less visibility into app health |

#### Priority: LOW — Polish & infrastructure

| # | Feature | LCM Implementation | AIF Status | Impact |
|---|---|---|---|---|
| G18 | **Grid/List view toggle** | Apps page: tile grid + table list modes | Card view only | Minor UX limitation |
| G19 | **Resource usage visualization** | `ResourceUsage.vue` — CPU/memory display | Not present | No resource consumption visibility |
| G20 | **Bulk operations** | Feature flag for multi-app install/upgrade/uninstall | Not present | Single-item operations only |
| G21 | **UI state persistence** | `ui-persist.ts` — localStorage with TTL for filters, form state | Not present | State lost on page reload |
| G22 | **Feature flags system** | `config/feature-flags.ts` — 10 flags with categories and dependency resolution | Not present | No runtime feature toggling |
| G23 | **Notification configuration** | Settings for install/upgrade/error notifications with duration | Not present | No configurable notifications |
| G24 | **Cluster health & capability monitoring** | `ClusterCapabilities`, health checks, node info | Not present | No downstream cluster modeling |
| G25 | **Custom chart repository management** | `store/modules/repositories.ts` — add/sync/manage repos | Not present (pre-configured sources only) | Cannot add custom repos |
| G26 | **Installed apps filter** | Checkbox to show only installed apps | Not present | Minor filtering gap |
| G27 | **Per-cluster status chips** | `ClusterChips.vue` — badges showing deployed clusters | Not present | Less multi-cluster visibility |

---

## 5. Helm Charts Comparison

| Chart | LCM | AIF | Notes |
|---|---|---|---|
| **Operator Chart** | `charts/suse-ai-operator/` — Deployment, RBAC, CRDs, Settings, metrics, webhooks | `charts/aif-operator/` — Deployment, RBAC, CRDs, Settings, PVC, PDB, webhook (cert-manager), metrics | AIF adds PVC, PDB, cert-manager TLS |
| **UI Extension Chart** | `charts/suse-ai-lifecycle-manager/` — UIPlugin deployment | `charts/aif-ui/` — UIPlugin manifest | Equivalent |
| **Reference Blueprint Charts** | Not present | `charts/nim-llm/`, `charts/nim-vlm/`, `charts/generic-container/` | **New in AIF** |
| **CRD Count in Chart** | 4 YAMLs | 5 YAMLs (includes Bundle) | AIF adds Bundle CRD |

---

## 6. Infrastructure & Tooling Comparison

| Capability | LCM | AIF |
|---|---|---|
| **Build system** | `package.json` (yarn) + Go build | `Makefile` (Go) + `package.json` (yarn) |
| **Unit tests** | Go tests | Go tests + Vitest (UI) |
| **Integration tests** | Not visible | envtest + Ginkgo controller suite |
| **Live/example tests** | Not visible | Per-package example_test.go + live_test.go (gated by env/build tags) |
| **Linting** | Not visible | golangci-lint + forbidden pattern grep guards |
| **CRD generation** | Manual | `make manifests` (controller-gen) |
| **Docker build** | Not visible | Two-stage Dockerfile, UID 1000 |
| **Dev cluster** | Not visible | `make dev-cluster` (k3d), `make dev-install`, `make examples` |
| **Documentation** | README.md only | CLAUDE.md, SOFTWARE_SPEC.md, ARCHITECTURE.md, PROJECT_PLAN.md, controller-guide.md, PUBLISHERS.md, AIRGAP.md |
| **CI enforcements** | Not visible | Condition constant grep guards, forbidden pattern checks |

---

## 7. Known Bugs in AIF

### 7.1 High Severity

| #   | Bug                                                                                                                                                             | Location                                                     | Impact                                                                 |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ | ---------------------------------------------------------------------- |
| B1  | **InstallAIExtension status lost on error** — `Reconcile` returns error at line 86 before `Status().Update()` at line 90; failed phase never persisted          | `internal/controller/installaiextension_controller.go:84-86` | Users see stale status, no failure reason                              |
| B2  | **Helm Pull creates unauthenticated registry client** — `registry.NewClient()` called without credentials on every pull; also a race condition on shared config | `pkg/helm/runner.go:139-142`                                 | Authenticated OCI pulls fail                                           |
| B3  | **Source collection OCI annotation reader issues** — missing URL scheme normalization, missing `Accept` header for OCI manifests, no Bearer-token auth exchange | `pkg/source_collection/annotation_reader.go:24,46,71-94`     | AppCo annotation fetches may fail against standard registries          |
| B4  | **`parseAppCoChartRef` uses slug name instead of chart name** — `strings.TrimSuffix` won't match when slug != chart name, producing malformed ChartRef          | `pkg/apps/appco_source.go:173-177`                           | Incorrect chart resolution for apps where slug differs from chart name |

### 7.2 Medium Severity

| # | Bug | Location | Impact |
|---|---|---|---|
| B6 | Blueprint controller ignores Workload spec updates — `UpdateFunc` returns false for ALL updates | `internal/controller/blueprint_controller.go:210` | Stale deploymentCount when workloads switch blueprints |
| B7 | InstallAIExtension re-installs on every reconcile — no guard for already-installed state | `installaiextension_controller.go:98-166` | Unnecessary Helm upgrades on every reconcile |
| B8 | Multiple API handlers leak internal error details to clients | `settings.go:93`, `publish.go:136`, `workloads.go:91`, `nvidia.go:112`, `apps.go:69` | K8s API addresses, Go types, connection strings exposed |
| B9 | `statusRecorder.WriteHeader` double-call — forwards to underlying writer unconditionally | `internal/api/middleware.go:180-186` | `http: superfluous response.WriteHeader call` log warnings |
| B10 | Annotation cache keyed by chart name only, ignoring version and repo | `source_collection/annotation_reader.go:32,57` | Stale annotations when querying different versions |
| B11 | Ticker interval never updates after `Start()` — `UpdateSettings` changes field but goroutine never resets ticker | `pkg/apps/nvidia_source.go:120-140`, `appco_source.go:116-136` | Refresh interval changes require operator restart |
| B12 | Bundle conversion aliases mutable data without deep copy | `pkg/bundle/conversions.go:10-27` | Mutations to domain model corrupt original CR |
| B13 | Bundle conversion drops Conditions, TestDeploys, ObservedGeneration on round-trip | `pkg/bundle/conversions.go` | Status fields silently lost |
| B14 | BundleTest drift detection uses live generation instead of snapshotted generation | `pkg/workload/deployer.go:299-306` | Drift detection always sees them as equal |
| B15 | `BundleFromCR` has no nil guard (unlike Blueprint equivalent) | `pkg/bundle/conversions.go:10` | Panic on nil input |
| B16 | Retrying on deterministic JSON parse errors | `source_collection/api_client.go:119-121` | Wastes time retrying unfixable errors |
| B17 | `operatorFetch` returns null on non-JSON success responses | `ui/.../utils/operator-api.ts:49-70` | Downstream crashes on `null.filter()` etc. |
| B18 | `selected` computed can be undefined in BlueprintCard | `ui/.../components/blueprints/BlueprintCard.vue:139-141` | UI crash when versions list changes |
| B19 | Optimistic mutation without rollback in AddToBundleDialog | `ui/.../components/apps/AddToBundleDialog.vue:171-172` | Phantom data in UI if save fails |
| B20 | Race condition in `loadApps` — concurrent calls not debounced | `ui/.../pages/apps.vue:198-214` | Stale filter results |

### 7.3 Low Severity

| # | Bug | Location | Impact |
|---|---|---|---|
| B21 | Auth cache never evicts expired entries | `internal/api/auth.go:92-165` | Unbounded memory growth |
| B22 | Settings controller hardcodes namespace "aif" for Secret lookup | `settings_controller.go:184-189` | Wrong namespace if Settings CR is elsewhere |
| B23 | `ComposeReleaseName` can return empty string on pathological input | `pkg/workload/release_name.go:25-38` | Helm SDK error downstream |
| B24 | Helm history sorted by timestamp, not revision number | `pkg/helm/engine.go:215` | Unreliable ordering on clock skew |
| B25 | GPU count integer overflow — int64/float64 truncated to int32 | `pkg/workload/deployer.go:236-258` | Silent truncation on large values |
| B26 | Blueprint controller lists ALL Workloads on every reconcile (no field selector) | `blueprint_controller.go:118-126` | O(N*M) API server load at scale |
| B27 | `selectDefaultVersion` returns undefined on empty lineage | `ui/.../utils/blueprint.ts:122-128` | UI crash on empty lineage |
| B28 | Multiple blueprint components rely on implicit global `t()` instead of explicit injection | BlueprintCard, VersionPicker, VersionsPanel, PhasePill, 4 stub pages | Testing fragility |
| B29 | Settings PUT endpoint has no per-user auth check | `internal/api/settings.go:88-118` | Any caller can overwrite Settings if API is exposed directly |

## 8. Recommendations & Prioritization

> Note
> These recommendations were generated with the assistance of an LLM (Opus 4.6, high context variant, highest effort).

### 8.1 Immediate Bug Fixes (before next release)

| Priority | Bug | Effort |
|---|---|---|
| 1 | B1 — Blueprint CR name separator mismatch | Low (one-line fix in `conversions.go:137`, change `"-"` to `"."`) |
| 2 | B2 — InstallAIExtension status lost on error | Low (move status update before error return) |
| 3 | B3 — Helm Pull unauthenticated | Medium (wire credentials from EngineSettings) |
| 4 | B8 — API handlers leaking internal errors | Low (wrap errors with generic messages per handler) |
| 5 | B5 — parseAppCoChartRef slug mismatch | Low (use ChartRef field instead of slug) |

### 8.2 UI Feature Porting Priority

| Priority | Feature                                    | Gap # | Estimated Effort | Rationale                                                       |
| -------- | ------------------------------------------ | ----- | ---------------- | --------------------------------------------------------------- |
| 1        | AI Workloads page (full implementation)    | G4    | Medium           | Core operational page — users need to see deployed workloads    |
| 2        | Overview Dashboard                         | G8    | Medium           | Platform entry point — first thing users see                    |
| 3        | Installation wizard (apps)                 | G1    | High             | Core user workflow — deploying apps to clusters                 |
| 4        | Blueprint Install wizard                   | G7    | High             | Core user workflow — deploying blueprints                       |
| 5        | Manage/Edit existing workloads             | G6    | Medium           | Operational necessity — modifying running workloads             |
| 6        | Uninstall workflow (UI)                    | G11   | Low              | Basic lifecycle action                                          |
| 7        | Bundles page (full implementation)         | N/A   | Medium           | AIF-specific feature, currently stubbed                         |
| 8        | Pending Reviews page (full implementation) | N/A   | Medium           | AIF-specific governance feature, currently stubbed              |
| 9        | Blueprint Create wizard                    | G9    | Medium           | Direct blueprint authoring (alternative to publish-by-approval) |
| 10       | App Instances drilldown                    | G5    | Medium           | Per-app deployment view                                         |

### 8.3 Backend Feature Priority

| Priority | Feature                                      | Gap # | Estimated Effort | Rationale                                                   |
| -------- | -------------------------------------------- | ----- | ---------------- | ----------------------------------------------------------- |
| 1        | Fleet GitOps backend                         | G14   | High             | GitOps deploy strategy is defined in CRD but non-functional |
| 2        | Rollback API endpoint                        | G10   | Low              | Helm Engine already has Rollback(); just needs API route    |
| 3        | Workload delete API/action                   | G11   | Low              | Helm Engine has Uninstall(); needs REST endpoint            |
|
