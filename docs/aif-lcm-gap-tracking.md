# SUSE AI Lifecycle Manager vs SUSE AI Factory — Feature Comparison

> **Purpose:** Track which features from `suse-ai-lifecycle-manager` (LCM) have been ported to `suse-ai-factory` (AIF), which are new in AIF, and which gaps remain.
> **Last updated:** 2026-06-08

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

| CRD | LCM | AIF | Delta |
|---|---|---|---|
| **AIWorkload / Workload** | `AIWorkload` — displayName, source (App/Blueprint), targetNamespace, targetClusters, deployStrategy (Helm/FleetBundle/GitOps), componentValues, fleetBundleNames | `Workload` — name, source (App/Blueprint), targetClusters, deployStrategy (helm/gitops), valueOverrides, replicas, strategy (Rolling/BlueGreen/Canary/AutoRecovery), scaling (HPA/VPA), paused, deploymentHistory, recoveryFailureCount | AIF adds replicas, update strategies, scaling, deployment history, recovery state machine, paused flag; LCM has targetNamespace, fleetBundleNames, 3 deploy strategies (incl. FleetBundle) |
| **Blueprint** | Namespaced; displayName, version, description, deprecated (bool), components[] | Cluster-scoped; blueprintName (lineage), version, useCase, description, changeDescription, source (Published/WrapsVendorChart), components[], valueOverrides, publishedBy/At; status: phase (Active/Deprecated/Withdrawn), deploymentCount, deprecation, conditions | AIF adds governance (immutability webhook), lineage tracking, vendor-chart wrapping, deployment counting, use-case tagging, publisher attribution; LCM has no status subresource |
| **Bundle** | Not present | Namespaced; title, targetBlueprint, useCase, authors, components, valueOverrides, paused; status: phase (Draft/Submitted/ChangesRequested), submission, review, testDeploys, publishedVersions, conditions | **New in AIF** — publish-by-approval workflow |
| **Settings** | Fleet, AppCo creds, SUSE Registry creds, **Nvidia (NGC)**, registry endpoints, image rewrite, catalog discovery | ApplicationCollection, SUSERegistry, Fleet, registry endpoints, image rewrite, catalog discovery, **BlueprintClassification** | Nearly equivalent; LCM has Nvidia/NGC section; AIF replaces it with BlueprintClassification and routes NIM through SUSE Registry |
| **InstallAIExtension** | Helm-only source; extension name/version/metadata; status: phase (string), message | Discriminated union source (Helm/Git); extension name/version; status: phase (typed enum), conditions, helmReleaseName, helmReleaseRevision, activeExtensionName, activeSourceKind | AIF adds Git source, typed phase enum, standard conditions, release tracking, source-change detection |

### 1.2 CRD Field Detail — AIWorkload (LCM) vs Workload (AIF)

| Field | LCM AIWorkload | AIF Workload |
|---|---|---|
| `spec.displayName` / `spec.name` | Yes (`displayName`) | Yes (`name`) |
| `spec.source.sourceType` / `spec.source.kind` | App, Blueprint | App, Blueprint |
| `spec.targetNamespace` | **Yes** | No |
| `spec.targetClusters` | Yes (cluster IDs) | Yes (informational) |
| `spec.deployStrategy` | Helm, FleetBundle, GitOps | helm, gitops (no FleetBundle enum) |
| `spec.componentValues` / `spec.valueOverrides` | `[]ComponentValueOverride` | `map[string]string` (flat YAML strings) |
| `spec.fleetBundleNames` | **Yes** | No |
| `spec.replicas` | No | **Yes** (default 1) |
| `spec.strategy` | No | **Yes** (RollingUpdate, BlueGreen, Canary, AutomaticRecovery) |
| `spec.scaling` | No | **Yes** (HPA min/max/targetCPU, VPA updateMode) |
| `spec.paused` | No | **Yes** |
| `status.phase` | Pending, Running, Degraded, Failed, Unknown | Pending, Deploying, Running, Degraded, Failed, **RecoveryInProgress** |
| `status.clusterStatuses` / `status.perCluster` | Per-cluster phase + message | **Per-cluster deployment status** (from Fleet) |
| `status.componentReleases` | No | **Yes** (per-component Helm release status) |
| `status.replicas` / `status.readyReplicas` | No | **Yes** |
| `status.deploymentHistory` | No | **Yes** (ordered revision records) |
| `status.recoveryFailureCount` | No | **Yes** |

### 1.3 CRD Field Detail — Blueprint

| Field | LCM Blueprint | AIF Blueprint |
|---|---|---|
| **Scope** | Namespaced | **Cluster-scoped** |
| `spec.displayName` / `spec.blueprintName` | displayName | blueprintName (lineage identifier) |
| `spec.version` | Yes (semver) | Yes (semver, immutable, stricter pattern) |
| `spec.description` | Yes | Yes (max 1024 chars) |
| `spec.deprecated` | Yes (boolean) | No (replaced by status.phase) |
| `spec.components[]` | `BlueprintComponent` (chartRepo, chartName, chartVersion, values) | `ComponentRef` (discriminated union: App/Blueprint refs) |
| `spec.useCase` | No | **Yes** (rag/vision/fine-tuning/inference/other) |
| `spec.source` | No | **Yes** (Published/WrapsVendorChart discriminator) |
| `spec.changeDescription` | No | **Yes** (max 2048 chars) |
| `spec.valueOverrides` | Via components[].values | **Separate field** (per-component YAML strings) |
| `spec.publishedBy` / `spec.publishedAt` | No | **Yes** (publisher attribution) |
| **Status subresource** | **None** | **Yes** |
| `status.phase` | No | **Yes** (Active/Deprecated/Withdrawn) |
| `status.deprecation` | No | **Yes** (reason, actionedBy, actionedAt) |
| `status.deploymentCount` | No | **Yes** (active workload count) |
| `status.conditions` | No | **Yes** (standard K8s conditions) |
| **Immutability** | Not enforced | **Webhook-enforced** (spec changes rejected after creation) |

### 1.4 CRD Field Detail — Settings

| Field | LCM Settings | AIF Settings |
|---|---|---|
| `spec.fleet` | FleetSettings (repoURL, branch, authType, credSecretRef) | FleetConfig (repoURL, branch, authType as typed enum, credSecretRef as corev1.SecretKeySelector) |
| `spec.applicationCollection` | ApplicationCollectionSettings | ApplicationCollectionConfig |
| `spec.suseRegistry` | SUSERegistrySettings | SUSERegistryConfig |
| `spec.nvidia` | **NvidiaSettings** (NGC API key, org) | No (NIMs discovered via SUSE Registry) |
| `spec.registryEndpoints` | RegistryEndpointsSettings (incl. **nvidia** endpoint) | RegistryEndpointsSpec (no nvidia-specific endpoint) |
| `spec.catalogDiscovery` | CatalogDiscoverySettings | CatalogDiscoverySpec |
| `spec.imageRewrite` | ImageRewriteSettings | ImageRewriteSpec |
| `spec.blueprintClassification` | No | **BlueprintClassificationSpec** |
| `status.lastApplied` | `*metav1.Time` | `metav1.Time` |
| `status.conditions` | `[]metav1.Condition` | `[]metav1.Condition` |

---

## 2. Backend Comparison

### 2.1 Controllers

| Controller | LCM | AIF | Notes |
|---|---|---|---|
| **AIWorkload / Workload** | Reconciles via Helm/Fleet/GitOps strategy pattern, derives phase from cluster statuses, finalizer cleanup | Reconciles via Deployer port (Fleet Bundle/GitRepo engines), formal phase state machine with 6 phases, recovery counter, requeue cadence by phase, pull-secret validation (483 lines) | AIF more robust; formal state machine |
| **Blueprint** | Basic validation and deprecation | Validates spec via Manager, computes deploymentCount from Workloads, deletion protection while deploymentCount > 0, phase initialization (215 lines) | AIF adds governance |
| **Bundle** | Not present | Draft/Submitted/ChangesRequested reconciliation, self-healing (partial-approval recovery), finalizer cleanup (224 lines) | **New in AIF** |
| **Settings** | Watches Settings CR, creates/updates Fleet GitRepo, manages git credential secrets | Resolves Secret refs, translates to SettingsSnapshot, fans out to all engines via bus, stale-ref detection with Degraded condition (298 lines + 197 line applier) | AIF more decoupled |
| **InstallAIExtension** | Helm install + finalizer cleanup | Helm install + Git source support, UIPlugin CRD check, Deployment/Service/ClusterRepo/UIPlugin health monitoring, source-kind change detection, orphan release cleanup (732 lines + 120 line health module) | AIF significantly more capable |

### 2.2 REST API Endpoints

| Endpoint | LCM | AIF | Notes |
|---|---|---|---|
| **List/Get Apps** | Not in backend (frontend `app-collection.ts` calls Rancher APIs directly) | `GET /api/v1/apps`, `/apps/categories`, `/apps/{id}`, `/apps/{id}/values` — server-side catalog with chart defaults | AIF has dedicated catalog API with chart value inspection |
| **CRUD Workloads** | `GET/POST/PATCH/DELETE /api/v1/aiworkloads` (namespaced) | `GET /api/v1/workloads`, `POST`, `PUT`, `DELETE /api/v1/workloads/{ns}/{name}`, `POST .../upgrade` — full CRUD + upgrade with 5-rule validation + SAR auth checks | Both have full CRUD; AIF adds upgrade workflow and per-user auth |
| **CRUD Blueprints** | `GET/POST/GET/{name}/PUT/DELETE /api/v1/blueprints` — full CRUD with slug generation | `POST /api/v1/blueprints` (create), `PATCH` (deprecate/undeprecate), `DELETE` (guards active workloads) — read via Steve store | Both have create/delete; LCM has GET/PUT; AIF reads via Steve, adds deprecation PATCH |
| **Publish Workflow** | Not present | `POST /bundles/{ns}/{name}/submit\|withdraw` — working; `approve\|request-changes` — return 501 NotImplemented (P3-4, P3-5 pending) | **New in AIF** (partially implemented) |
| **NVIDIA NIM** | Not present | `GET /api/v1/nvidia/nims`, `/nims/{model}/sizing` | **New in AIF** |
| **Settings** | `GET/PUT /api/v1/settings`, `GET /settings/registry-credentials`, `POST /git/publish` | `GET/PUT /api/v1/settings` | LCM has registry-credentials decode and git publish endpoints; AIF handles both server-side |
| **Health/Metrics** | Basic healthz | healthz + readyz + Prometheus metrics | AIF more complete |

### 2.3 Business Logic Packages

| Package | LCM | AIF | Notes |
|---|---|---|---|
| **apps (catalog)** | `services/app-collection.ts` (frontend, 404 lines) | `pkg/apps/` (910 lines) — Aggregator, NVIDIASource, AppCoSource, stale-but-good cache, example+live tests | AIF has server-side catalog with multi-source aggregation |
| **bundle** | Not present | `pkg/bundle/` (312 lines) — Manager, Repository, Conversions | **New in AIF** |
| **blueprint** | `utils/blueprint-api.ts` (frontend, 123 lines) | `pkg/blueprint/` (940 lines) — Manager, Wrapper (vendor-chart auto-wrapping), Repository, Conversions, Validator | AIF much richer |
| **workload** | `services/app-lifecycle-service.ts` (493 lines) + `services/rancher-apps.ts` (1,399 lines) | `pkg/workload/` (1,987 lines) — Deployer, Upgrader, Phase state machine, Repository, release naming | AIF more structured; LCM has more Rancher-native integration |
| **publish** | Not present | `pkg/publish/` (225 lines) — Submit/Withdraw working; Approve/RequestChanges stubs (P3-4, P3-5) | **New in AIF** (partially implemented) |
| **helm** | `infra/helm/` (467 lines) — action, chart, client, values, index | `pkg/helm/` (1,519 lines) — Engine (install/uninstall/rollback/status/history), 6-layer values merge, image rewrite, chart inspector, example+envtest tests | AIF more complete |
| **fleet** | `services/fleet-bundle.ts` (frontend, 264 lines) — builds Fleet Bundle YAML | `pkg/fleet/` (1,111 lines) — FleetBundleEngine + FleetGitRepoEngine (SSA, per-cluster status aggregation, git auth injection), example+live tests | AIF fully implemented server-side; LCM was frontend-only |
| **git** | `internal/git/client.go` (backend) + `services/git-publish.ts` (frontend, 35 lines) | `pkg/git/` (530 lines) — go-git Engine (clone, readFile, manifest tree), SSH/token/basic auth, example+live tests | AIF fully implemented; both have backend git support |
| **nvidia** | Not present (NGC via Settings + registry) | `pkg/nvidia/` (1,117 lines) — Discovery, Deployer, Classifier, AnnotationReader, RegistryClient, example+live tests | **New in AIF** |
| **source_collection** | Not present (uses Rancher ClusterRepo) | `pkg/source_collection/` (894 lines) — API client, annotation reader, caching, example+live tests | **New in AIF** |
| **helm_oci** | Not present | `pkg/helm_oci/` (165 lines) — Chart.yaml parsing, OCI manifest, blob I/O | **New in AIF** |
| **conditions** | Not present | `pkg/conditions/` (135 lines) — Type/Reason constants, set helpers, action constants | **New in AIF** |
| **cluster-resources** | `services/cluster-resources.ts` (frontend, 506 lines) — Pod/Deployment/StatefulSet queries | Not present | **Gap in AIF** — no K8s resource inspection |
| **chart-service** | `services/chart-service.ts` (frontend, 284 lines) + `services/chart-values.ts` (440 lines) | Handled by `pkg/helm/` (chart inspector, values merge) | Architecture difference — AIF backend handles |
| **cluster-service** | `services/cluster-service.ts` (frontend, 127 lines) — cluster list, health, quotas | Not present | **Gap in AIF** — no cluster monitoring |

---

## 3. UI / Frontend Comparison

### 3.1 Pages

| Page | LCM Status | LCM Lines | AIF Status | AIF Lines | Gap |
|---|---|---|---|---|---|
| **Overview / Dashboard** | Complete (summary cards, recent workloads, blueprints, quick actions, 10s auto-refresh) | 484 | **Complete** (workload counts, running/issues, active blueprints, recent workloads table, quick action cards, 10s silent polling) | 366 | Near parity |
| **Apps Catalog** | Complete (search, repo filter, installed filter, tile/list views, external links, install actions) | 1,239 | **Complete** (search, registry selector SUSE/NVIDIA, tile/list toggle, category filter, result counts, error handling) | 394 | Near parity — LCM has installed filter; AIF has NVIDIA registry selector |
| **App Install Wizard** | Complete (delegates to AppWizard — 5 steps, multi-cluster, progress tracking) | 13 + wizard | **Complete** (4-step wizard: Basic Info → Target → Configuration → Review, chart defaults loading, DNS-1123 validation, progress modal) | 439 | Near parity — LCM wizard has 5 steps + 3 deploy strategies; AIF has 4 steps + localStorage persistence |
| **App Instances** | Complete (per-app deployments across clusters, search, filter by cluster/status, delete/manage actions) | 1,302 | Not present | — | MEDIUM |
| **Manage / Edit** | Complete (delegates to AppWizard in manage mode — edit values, upgrade version) | 13 + wizard | **Complete** (edit chart version and per-component values, app-workloads only, rejects blueprint workloads) | 169 | Partial — AIF supports app workloads only |
| **Blueprints** | Complete (search, deprecated toggle, tile grid, version selector, install/edit/copy/delete actions, active workload warnings) | 643 | **Complete** (search, lineage cards, version picker per card, deprecate/undeprecate modals, delete modal, admin role checks, phase filter) | 374 | Near parity — LCM has edit action; AIF has admin-gated lifecycle actions |
| **Blueprint Create** | Complete (4-step wizard: BasicInfo, SelectApps, Config, Review) | 78 + wizard | **Complete** (4-step wizard: Basic Info → Select Apps → Configuration → Review, copy/edit modes from existing, per-component values with Load Defaults, semver validation) | 370 | Near parity |
| **Blueprint Install** | Complete (deploy blueprint to clusters with per-component config) | 17 + wizard | **Complete** (3-step wizard: Basic Info → Target → Review, cluster selector with Fleet checkbox, DNS-1123 validation, progress modal) | 367 | Near parity |
| **AI Workloads** | Complete (search, phase/source filter, bulk delete, upgrade blueprint version, per-cluster drill-down, 10s auto-refresh) | 651 | **Complete** (status badges, source/version columns, manage/upgrade/delete actions, delete confirmation, upgrade modal with version picker, 10s polling) | 437 | Near parity — LCM has bulk delete, per-cluster drill-down; AIF has per-workload actions |
| **Bundles** | Not present | — | Not present | — | N/A (backend CRD exists; UI deferred) |
| **Pending Reviews** | Not present | — | Not present | — | N/A (backend workflow exists; UI deferred) |
| **Settings** | Complete (Fleet, AppCo, SUSE Registry, NGC, registry endpoints, catalog discovery, image rewrite — expandable sections) | 830 | **Complete** (ApplicationCollection, SUSE Registry, Fleet/GitOps, Advanced: image rewrite, catalog discovery, registry endpoints — accordion sections, SecretSelector, SSA-based PUT) | 638 | Near parity — LCM has NGC section; AIF has SSA-based saves |

### 3.2 Components

| Component | LCM | AIF | Gap |
|---|---|---|---|
| **AppWizard** (multi-step install/manage) | Complete — 1,834 lines, 5 steps, multi-cluster, 3 deploy strategies, progress tracking | Split into wizard pages (`app-install.vue`, `blueprint-install.vue`) + shared components (`WizardStepIndicator`, `InstallProgressModal`) | Near parity — different architecture (monolithic wizard vs page-based) |
| **BlueprintCreateWizard** (4-step) | Complete — 226 lines | `pages/wizards/blueprint-create.vue` — 370 lines, copy/edit modes, per-component values | Near parity |
| **BlueprintInstallWizard** (blueprint deployment) | Complete — 307 lines | `pages/wizards/blueprint-install.vue` — 367 lines, cluster selector + Fleet | Near parity |
| **Wizard Steps** (BasicInfo, Target, Values, Review + Blueprint variants) | 11 step files — 1,875 lines | Built into wizard pages directly | Architecture difference |
| **WizardStepIndicator** | Not present (wizard step UI embedded in AppWizard) | Complete — 101 lines, numbered circles, backward navigation | AIF ahead (reusable) |
| **InstallProgressModal** | `InstallProgressModal.vue` — 439 lines, per-cluster progress with retry | Complete — 95 lines, per-cluster rows with spinner/checkmark/warning | Near parity (AIF leaner) |
| **ClusterResourceTable** (K8s resources) | Complete — 792 lines | Not present | MEDIUM |
| **ClusterSelect** (multi-cluster picker) | Complete — 92 lines | Built into wizard pages | Architecture difference |
| **ValuesYaml** (YAML editor) | Complete — 81 lines | Built into wizard pages | Architecture difference |
| **AppCard** | Complete (logo, name, description, status, links) | Complete — 198 lines (logo with fallback, badges, version, install action) | Near parity |
| **BlueprintCard** | Not present | Complete — 222 lines (lineage card, version picker, phase pill, action buttons, admin gating) | AIF ahead |
| **BlueprintVersionPicker** | Not present | Complete — 65 lines | AIF ahead |
| **BlueprintPhasePill** | Not present | Complete — 55 lines (Active/Deprecated/Withdrawn) | AIF ahead |
| **AppStatusBadge** | Complete — 126 lines (color-coded status) | Not present (uses inline badges) | Minor |
| **AppHealthIndicator** | Complete — 185 lines (pod/resource health) | Not present | MEDIUM |
| **ClusterChips** | Complete — 173 lines (multi-cluster badges) | Not present | LOW |
| **InstallationProgress** | Complete — 119 lines (progress bar with ETA) | Not present (uses InstallProgressModal instead) | Architecture difference |
| **ResourceUsage** | Complete — 169 lines (CPU/memory display) | Not present | LOW |

### 3.3 Services & Utilities

| Service/Utility | LCM | AIF | Gap |
|---|---|---|---|
| **operator-api.ts** (REST client) | 103 lines — AIWorkload CRUD, Blueprint CRUD, Settings, git publish | 202 lines — 18+ functions: Apps (list, get, values), Blueprints (list, get, create, deprecate, delete), Workloads (list, get, create, put, delete, upgrade), Settings (get, put), mock API fallback | AIF more comprehensive |
| **blueprint-api.ts / blueprint.ts** | 123 lines — groupByFamily, latestVersion, semverCompare, slugify | 157 lines — groupByLineage, toBlueprintVersion, compareVersions, sortVersionsDesc, selectDefaultVersion, readUnreachable, readPublisherOverride | Near parity |
| **rancher-apps.ts** (Helm/cluster ops) | 1,399 lines — 15+ methods for cluster, namespace, chart, repo, secret management | Not present (backend handles these) | Architecture difference — AIF delegates to backend |
| **app-lifecycle-service.ts** | 493 lines — install, upgrade, delete, wait, rollback | Not present (backend `pkg/workload/` handles this) | Architecture difference |
| **app-collection.ts** | 404 lines — fetch catalog, repo lookup | Not present (backend `pkg/apps/` handles this) | Architecture difference |
| **fleet-bundle.ts** | 264 lines — build Fleet Bundle YAML for multi-cluster | Not present (backend `pkg/fleet/` handles this) | Architecture difference |
| **git-publish.ts** | 35 lines — commit and push to GitOps repo | Not present (backend `pkg/git/` handles this) | Architecture difference |
| **chart-service.ts** | 284 lines — chart metadata, versions | Not present (backend handles this) | Architecture difference |
| **chart-values.ts** | 440 lines — YAML parsing, merging | Not present (backend handles this) | Architecture difference |
| **cluster-resources.ts** | 506 lines — K8s resource CRUD | Not present | Gap — no resource inspection |
| **cluster-service.ts** | 127 lines — connectivity, health | Not present | Gap — no cluster monitoring |
| **ui-persist.ts** | 33 lines — localStorage with TTL | Not present (wizard pages use raw localStorage) | LOW gap |
| **repo-auth.ts** | 159 lines — credential management | Not present (backend handles this) | Architecture difference |
| **mock-api.ts** | Not present | 200+ lines — mock fallback with 10 sample apps for development | AIF ahead |
| **date.ts** | Not present | 14 lines — date formatting helpers | AIF ahead |
| **Store modules** (apps, clusters, installations, repositories) | 1,906 lines — Vuex state management | Not present (Vue component data + computed) | Architecture difference — AIF uses simpler state model |
| **Models** (app, chart, cluster, resource, installation) | 5,178 lines — rich data models | Not present (types in operator-api.ts returns) | Architecture difference — AIF uses server-side types |
| **Validators** (appInstallation, common, resourceManagement) | 3 files | Not present (validation in wizard pages) | LOW gap |
| **Feature flags** | `config/feature-flags.ts` + `utils/feature-flags.ts` — 10 flags with categories and dependency resolution | Not present | LOW gap |

---

## 4. Feature-by-Feature Gap Analysis

### 4.1 Features Successfully Ported to AIF

| # | Feature | LCM Implementation | AIF Implementation | Parity |
|---|---|---|---|---|
| 1 | App catalog browsing | `pages/Apps.vue` + `services/app-collection.ts` | `pages/apps.vue` + `pkg/apps/` server-side catalog | Full |
| 2 | App search (text) | Frontend filter on name/description | Frontend filter on name/displayName/description | Full |
| 3 | App category filtering | Via repository filter | `GET /api/v1/apps/categories` + dropdown | Full |
| 4 | App grid/list view toggle | Grid + table list modes | Tile + list toggle | Full |
| 5 | App install wizard | `AppWizard.vue` — 5-step multi-cluster | `pages/wizards/app-install.vue` — 4-step with progress modal | Full |
| 6 | SUSE Application Collection integration | `services/app-collection.ts` | `pkg/source_collection/` (HTTP API + OCI fallback) | Full (AIF richer) |
| 7 | SUSE Registry / NIM integration | Via catalog + NGC Settings | `pkg/nvidia/` — dedicated Discovery, Deployer, AnnotationReader | Full (AIF richer) |
| 8 | Helm-based workload deployment | `services/rancher-apps.ts` | `pkg/helm/engine.go` (direct Helm SDK) | Full |
| 9 | Helm values merging | `services/chart-values.ts` (2 layers) | `pkg/helm/values.go` (6 layers) | Full (AIF richer) |
| 10 | Settings page (credentials, Fleet, registry) | `pages/Settings.vue` | `pages/settings.vue` | Full |
| 11 | Settings CRD (singleton config) | Settings CRD | Settings CRD | Full |
| 12 | Air-gap support (image rewrite, endpoints, discovery mode) | Settings CRD fields | Settings CRD fields + image rewrite engine layer | Full |
| 13 | InstallAIExtension CRD | CRD + controller | CRD + controller (with Git source, health monitoring) | Full (AIF richer) |
| 14 | Blueprint listing & version management | `pages/Blueprints.vue` | `pages/blueprints.vue` | Full |
| 15 | Blueprint CRD | CRD (namespaced, no status) | CRD (cluster-scoped, immutable, full status) | Full (AIF richer) |
| 16 | Workload CRD & lifecycle | AIWorkload CRD | Workload CRD (richer state machine) | Full (AIF richer) |
| 17 | Internationalization (i18n) | `l10n/en-us.json` | `l10n/en-us.yaml` | Full |
| 18 | Overview dashboard | `pages/Overview.vue` — summary cards, recent workloads, quick actions | `pages/overview.vue` — workload counts, blueprints, recent workloads, quick actions, 10s polling | Full |
| 19 | Workload listing page | `pages/AIWorkloads.vue` — search, phase/source filter, 10s refresh | `pages/workloads.vue` — status badges, actions, upgrade modal, 10s polling | Full |
| 20 | Blueprint create wizard | `BlueprintCreateWizard.vue` — 4-step | `pages/wizards/blueprint-create.vue` — 4-step with copy/edit modes | Full |
| 21 | Blueprint install wizard | `BlueprintInstallWizard.vue` — cluster selection, config | `pages/wizards/blueprint-install.vue` — 3-step with Fleet checkbox | Full |
| 22 | Manage/edit workloads (apps) | `Manage.vue` — re-enter wizard to edit | `pages/manage.vue` — edit chart version and values | Partial (AIF: app workloads only) |
| 23 | Blueprint deprecation UI | Deprecated toggle + active workload warnings | Deprecate/undeprecate modals with admin role checks | Full |
| 24 | Workload delete from UI | Delete with confirmation + progress | Delete confirmation modal on workloads page | Full |
| 25 | Real-time install progress | `InstallProgressModal.vue` — per-cluster with retry | `InstallProgressModal.vue` — per-cluster with status icons | Full |
| 26 | Fleet GitOps backend | `services/fleet-bundle.ts` (frontend) + `internal/git/` | `pkg/fleet/` (1,111 lines) + `pkg/git/` (530 lines) — full server-side Fleet+Git engines | Full (AIF richer) |
| 27 | Multi-cluster target selection | `TargetStep.vue`, `ClusterSelect.vue` | Cluster selector in install wizard pages | Full |

### 4.2 Features New in AIF (Not in LCM)

| # | Feature | AIF Implementation | Status |
|---|---|---|---|
| 1 | **Bundle CRD & composition workflow** | `api/v1alpha1/bundle_types.go`, `pkg/bundle/` | Complete (backend) |
| 2 | **Publish-by-approval governance** | `pkg/publish/workflow.go`, `internal/api/publish.go` | Partial (submit/withdraw working; approve/request-changes return 501) |
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
| 13 | **NVIDIA NIM REST API** | `GET /api/v1/nvidia/nims`, `/nims/{model}/sizing` | Complete |
| 14 | **Server-side app catalog** | `pkg/apps/` aggregator with multi-source, stale-but-good cache | Complete |
| 15 | **OCI manifest parsing** | `pkg/helm_oci/` — Chart.yaml extraction, manifest parsing | Complete |
| 16 | **Hexagonal architecture** | Clean ports/adapters, ISP (<=4 methods), layering rule | Complete |
| 17 | **Condition constants library** | `pkg/conditions/` — 20+ typed reasons, CI-enforced | Complete |
| 18 | **Settings bus propagation** | `internal/manager/engine_bus.go` — fan-out to all engines | Complete |
| 19 | **Comprehensive test infrastructure** | envtest, Ginkgo, example tests, live tests, Makefile targets | Complete |
| 20 | **Blueprint lifecycle components** (UI) | BlueprintCard, VersionPicker, PhasePill | Complete |
| 21 | **Fleet Bundle Engine** (server-side) | `pkg/fleet/bundle_engine.go` — SSA, per-cluster status aggregation | Complete |
| 22 | **Fleet GitRepo Engine** (server-side) | `pkg/fleet/gitrepo_engine.go` — per-cluster GitRepo lifecycle, git auth | Complete |
| 23 | **Chart value inspection API** | `GET /api/v1/apps/{id}/values` — chart defaults for wizard | Complete |
| 24 | **InstallAIExtension Git source** | Discriminated union: Helm or Git source with health monitoring | Complete |
| 25 | **Mock API for development** | `utils/mock-api.ts` — 10 sample apps for offline dev | Complete |

### 4.3 Gaps: LCM Features Not Yet in AIF

#### Priority: HIGH — Core user workflows

| # | Feature | LCM Implementation | AIF Status | Impact |
|---|---|---|---|---|
| G1 | **App Instances page (per-app drilldown)** | `AppInstances.vue` — all deployments of specific app across clusters, filter by cluster/status, delete/manage actions (1,302 lines) | Not present | No per-app deployment view; users must use workloads page |
| G2 | **Bundles page (full)** | N/A | Backend CRD + workflow exist; no UI page | Users cannot compose bundles through the UI |
| G3 | **Pending Reviews page (full)** | N/A | Backend publish workflow exists (partially); no UI page | Reviewers cannot approve/reject through the UI |

#### Priority: MEDIUM — Operational features

| # | Feature | LCM Implementation | AIF Status | Impact |
|---|---|---|---|---|
| G4 | **Rollback support** | `app-lifecycle-service.ts` → `rollbackApp()` | Backend has `helm.Engine.Rollback()` but no API endpoint or UI action | Users cannot roll back deployments |
| G5 | **Cluster resource inspection** | `ClusterResourceTable.vue` (792 lines) — view Pods/Deployments/StatefulSets | Not present | Users cannot inspect K8s resources backing a workload |
| G6 | **Image pull secret management per workload** | `ensureRegistrySecretSimple()`, `ensureServiceAccountPullSecret()` | Credentials at Settings level; no per-workload secret UI | Limited credential management |
| G7 | **App health indicators** | `AppHealthIndicator.vue` (185 lines) — visual pod/resource health | Phase badges only, no health component | Less visibility into app health |
| G8 | **Manage/edit blueprint workloads** | `Manage.vue` can edit both app and blueprint workloads | `manage.vue` rejects blueprint workloads (app-only) | Blueprint workloads cannot be edited post-deploy |
| G9 | **Publish approve/request-changes** | N/A | `POST /approve` and `POST /request-changes` return 501 NotImplemented | Publish-by-approval workflow incomplete |

#### Priority: LOW — Polish & infrastructure

| # | Feature | LCM Implementation | AIF Status | Impact |
|---|---|---|---|---|
| G10 | **Bulk operations** | Feature flag for multi-app install/upgrade/uninstall; bulk delete on AIWorkloads page | Not present | Single-item operations only |
| G11 | **Per-cluster status drill-down** | AIWorkloads page — click to see per-cluster deployment status | Workloads page shows status but no cluster-level drill-down | Less multi-cluster visibility |
| G12 | **Resource usage visualization** | `ResourceUsage.vue` (169 lines) — CPU/memory display | Not present | No resource consumption visibility |
| G13 | **Per-cluster status chips** | `ClusterChips.vue` (173 lines) — badges showing deployed clusters | Not present | Less multi-cluster visibility |
| G14 | **Feature flags system** | `config/feature-flags.ts` — 10 flags with categories and dependency resolution | Not present | No runtime feature toggling |
| G15 | **Notification configuration** | Settings for install/upgrade/error notifications with duration | Not present | No configurable notifications |
| G16 | **Cluster health & capability monitoring** | `ClusterCapabilities`, health checks, node info (cluster-service.ts) | Not present | No downstream cluster modeling |
| G17 | **Custom chart repository management** | `store/modules/repositories.ts` — add/sync/manage repos | Not present (pre-configured sources only) | Cannot add custom repos |
| G18 | **Installed apps filter** | Checkbox to show only installed apps | Not present | Minor filtering gap |
| G19 | **UI state persistence (TTL-based)** | `ui-persist.ts` — localStorage with TTL | Not present (raw localStorage in wizards) | Minor; wizards have some persistence |
| G20 | **Vuex store modules** | 4 modules (apps, clusters, installations, repositories) — 1,906 lines | Not present (component-local state) | Architecture difference — AIF uses simpler pattern |
| G21 | **Rich data models** | 5+ model files — 5,178 lines | Not present (lightweight server-side types) | Architecture difference — AIF delegates to backend |

---

## 5. Helm Charts Comparison

| Chart | LCM | AIF | Notes |
|---|---|---|---|
| **Operator Chart** | `charts/suse-ai-operator/` — Deployment, RBAC, CRDs, Settings, metrics, webhooks | `charts/aif-operator/` — Deployment, RBAC, CRDs, Settings, PVC, PDB, webhook (cert-manager), metrics | AIF adds PVC, PDB, cert-manager TLS |
| **UI Extension Chart** | `charts/suse-ai-lifecycle-manager/` — UIPlugin deployment | `charts/aif-ui/` — UIPlugin manifest | Equivalent |
| **Reference Blueprint Charts** | Not present | `charts/nim-llm/`, `charts/nim-vlm/`, `charts/generic-container/` | **New in AIF** |
| **CRD Count in Chart** | 4 YAMLs (AIWorkload, Blueprint, Settings, InstallAIExtension) | 5 YAMLs (Workload, Blueprint, Bundle, Settings, InstallAIExtension) | AIF adds Bundle CRD |

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

| # | Bug | Location | Status | Impact |
|---|---|---|---|---|
| B1 | ~~InstallAIExtension status lost on error~~ | `installaiextension_controller.go` | **FIXED** — status flushed before error return | ~~Users see stale status~~ |
| B2 | ~~Helm Pull creates unauthenticated registry client~~ | `pkg/helm/runner.go` | **FIXED** — credentials now wired via registry client | ~~Authenticated OCI pulls fail~~ |
| B3 | **Source collection OCI annotation reader issues** — missing URL scheme normalization, missing `Accept` header for OCI manifests, Basic-Auth only (no Bearer-token auth exchange) | `pkg/source_collection/annotation_reader.go` | OPEN | AppCo annotation fetches may fail against standard registries |
| B4 | ~~`parseAppCoChartRef` uses slug name instead of chart name~~ | `pkg/apps/appco_source.go` | **FIXED** — uses correct ID field | ~~Incorrect chart resolution~~ |

### 7.2 Medium Severity

| # | Bug | Location | Status | Impact |
|---|---|---|---|---|
| B6 | **Blueprint controller ignores Workload spec updates** — `UpdateFunc` returns false for ALL updates | `blueprint_controller.go:210` | OPEN | Stale deploymentCount when workloads switch blueprints |
| B7 | **InstallAIExtension re-installs on every reconcile** — no guard for already-installed state | `installaiextension_controller.go` | OPEN | Unnecessary Helm upgrades on every reconcile |
| B8 | **Multiple API handlers may leak internal error details** — error infrastructure exists (`writeError` helper) but individual handlers need audit | `internal/api/*.go` | PARTIAL | Internal errors could reach clients |
| B9 | ~~`statusRecorder.WriteHeader` double-call~~ | `internal/api/middleware.go` | **FIXED** — `wroteHeader` guard present | ~~Log warnings~~ |
| B10 | **Annotation cache keyed by chart name only**, ignoring version and repo | `source_collection/annotation_reader.go` | OPEN | Stale annotations when querying different versions |
| B11 | **Ticker interval never updates after `Start()`** — `UpdateSettings` changes field but goroutine never resets ticker | `pkg/apps/nvidia_source.go`, `appco_source.go` | OPEN | Refresh interval changes require operator restart |
| B12 | **Bundle conversion aliases mutable data without deep copy** | `pkg/bundle/conversions.go` | OPEN | Mutations to domain model may corrupt original CR |
| B13 | **Bundle conversion drops Conditions, TestDeploys, ObservedGeneration on round-trip** | `pkg/bundle/conversions.go` | OPEN | Status fields silently lost |
| B15 | **`BundleFromCR` has no nil guard** (unlike Blueprint equivalent) | `pkg/bundle/conversions.go` | OPEN | Panic on nil input |
| B16 | **Retrying on deterministic JSON parse errors** | `source_collection/api_client.go` | OPEN | Wastes time retrying unfixable errors |
| B17 | **`operatorFetch` returns null on non-JSON success responses** | `ui/.../utils/operator-api.ts` | OPEN | Downstream crashes on `null.filter()` etc. |
| B18 | **`selected` computed can be undefined in BlueprintCard** | `BlueprintCard.vue` | OPEN | UI crash when versions list changes |
| B20 | **Race condition in `loadApps`** — concurrent calls not debounced | `ui/.../pages/apps.vue` | OPEN | Stale filter results |

### 7.3 Low Severity

| # | Bug | Location | Status | Impact |
|---|---|---|---|---|
| B21 | **Auth cache never evicts expired entries** — stale entries detected on lookup but never proactively removed | `internal/api/auth.go` | OPEN | Unbounded memory growth |
| B22 | **Settings controller hardcodes namespace "aif"** for Secret lookup | `internal/api/settings.go` | OPEN | Wrong namespace if Settings CR is elsewhere |
| B23 | ~~`ComposeReleaseName` can return empty string on pathological input~~ | `pkg/workload/release_name.go` | **FIXED** — sanitization handles normal inputs | ~~Helm SDK error downstream~~ |
| B24 | **Helm history sorted by timestamp, not revision number** | `pkg/helm/engine.go` | OPEN | Unreliable ordering on clock skew |
| B25 | **GPU count integer overflow** — int64/float64 truncated to int32 | `pkg/workload/deployer.go` | OPEN | Silent truncation on large values |
| B26 | **Blueprint controller lists ALL Workloads on every reconcile** (no field selector) | `blueprint_controller.go` | OPEN | O(N*M) API server load at scale |
| B27 | **`selectDefaultVersion` returns undefined on empty lineage** | `ui/.../utils/blueprint.ts` | OPEN | UI crash on empty lineage |
| B28 | ~~Multiple blueprint components rely on implicit global `t()`~~ | BlueprintCard, VersionPicker, PhasePill | **FIXED** — uses scoped `getCurrentInstance().proxy` | ~~Testing fragility~~ |
| B29 | **Settings PUT endpoint has no per-user auth check** | `internal/api/settings.go` | OPEN | Any caller can overwrite Settings if API is exposed directly |

### 7.4 Resolved / Not Applicable

| # | Bug | Resolution |
|---|---|---|
| B1 | InstallAIExtension status lost on error | Fixed — status flushed before error return |
| B2 | Helm Pull unauthenticated | Fixed — credentials wired |
| B4 | parseAppCoChartRef slug mismatch | Fixed — uses correct field |
| B9 | statusRecorder.WriteHeader double-call | Fixed — guard added |
| B14 | BundleTest drift detection uses live generation | N/A — BundleTest source kind was removed |
| B19 | Optimistic mutation in AddToBundleDialog | N/A — component removed (no Bundles UI) |
| B23 | ComposeReleaseName empty string | Fixed — sanitization handles normal inputs |
| B28 | Implicit global t() in blueprint components | Fixed — scoped instance pattern |

---

## 8. Recommendations & Prioritization

### 8.1 Remaining Bug Fixes (by priority)

| Priority | Bug | Effort |
|---|---|---|
| 1 | B3 — Source collection annotation reader (scheme, Accept header, Bearer auth) | Medium |
| 2 | B6 — Blueprint controller ignores Workload updates (stale deploymentCount) | Low (fix UpdateFunc predicate) |
| 3 | B7 — InstallAIExtension re-installs on every reconcile | Low (add installed-state guard) |
| 4 | B8 — API handlers leaking internal errors (audit individual handlers) | Low |
| 5 | B10 — Annotation cache key includes version and repo | Low (composite key) |
| 6 | B11 — Ticker interval reset after UpdateSettings | Medium (goroutine coordination) |
| 7 | B29 — Settings PUT auth check | Low (add SAR middleware) |

### 8.2 UI Feature Priority

| Priority | Feature | Gap # | Estimated Effort | Rationale |
|---|---|---|---|---|
| 1 | Bundles page (full implementation) | G2 | Medium | Backend exists; need UI for composition workflow |
| 2 | Pending Reviews page (full implementation) | G3 | Medium | Backend partially exists; governance feature |
| 3 | Publish approve/request-changes (backend + UI) | G9 | Medium | Completes publish-by-approval workflow |
| 4 | App Instances drilldown | G1 | Medium | Per-app deployment view |
| 5 | Cluster resource inspection | G5 | Medium | K8s resource visibility for workloads |
| 6 | Manage/edit blueprint workloads | G8 | Low | Extend manage page to support blueprint sources |
| 7 | App health indicators | G7 | Low | Richer health visibility |
| 8 | Rollback support (API + UI) | G4 | Low | Backend engine exists; needs endpoint + button |

### 8.3 Backend Feature Priority

| Priority | Feature | Gap # | Estimated Effort | Rationale |
|---|---|---|---|---|
| 1 | Publish approve/request-changes workflow | G9 | Medium | Core governance feature — endpoints return 501 |
| 2 | Rollback API endpoint | G4 | Low | Helm Engine already has Rollback(); just needs API route |
| 3 | Cluster resource inspection API | G5 | Medium | Need endpoint for K8s resource queries per workload |
