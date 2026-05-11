# SUSE AI Factory — Software Specification

> **Audience:** Product managers, designers, customer success, and customer-facing teams.
> **Scope:** What users see, feel, and can do. No implementation or infrastructure details.
> **Version:** v1.1 (conceptual model: App / Bundle / Blueprint / Workload)

---

## Table of Contents

1. [Product Vision](#1-product-vision)
2. [User Personas](#2-user-personas)
3. [Platform Navigation](#3-platform-navigation)
4. [Overview Dashboard](#4-overview-dashboard)
5. [Apps — AI Catalog](#5-apps--ai-catalog)
6. [Blueprints](#6-blueprints)
7. [Bundles](#7-bundles)
8. [Workloads](#8-workloads)
9. [Settings](#9-settings)
10. [Install & Deploy Flow](#10-install--deploy-flow)
11. [Notifications and Feedback](#11-notifications-and-feedback)
12. [Out of Scope for v1](#12-out-of-scope-for-v1)
13. [Glossary](#13-glossary)
14. [Cross-Reference Appendix](#14-cross-reference-appendix)

---

## 1. Product Vision

**SUSE AI Factory** is the AI platform management layer built into Rancher. It gives platform engineers, AI/ML practitioners, and operations teams a single place to discover AI applications, compose them into validated AI stacks, publish those stacks as reusable blueprints, and deploy and monitor AI workloads on any Rancher-managed Kubernetes cluster — all without leaving the familiar Rancher Dashboard.

### Conceptual Model

SUSE AI Factory uses four nouns, each with a distinct role:

| Noun | What it is | Mutability | Scope |
|------|------------|------------|-------|
| **App** | A building-block AI application (NIM model, vector database, LLM serving runtime, vendor-published Reference Blueprint chart, …) — anything packaged as a Helm chart in SUSE Registry or SUSE Application Collection | Immutable; sourced from the SUSE catalog | Catalog-wide (not a CRD; discovered from registries) |
| **Bundle** | A workshop where an author composes Apps and existing Blueprints into a candidate AI stack | Mutable; owned by an author or small team | Namespaced (CRD in author's namespace) |
| **Blueprint** *(AIF Blueprint)* | A published, immutable, versioned AI stack tied to a use case (e.g., RAG, vision pipeline) — an **AIF concept**, distinct from vendor-published Reference Blueprints (see note below) | Immutable per version; new versions are minted, never edited in place | Cluster-scoped (CRD visible org-wide) |
| **Workload** | A running instance of an App or a Blueprint on a target cluster | Status-only | Namespaced (CRD in workload's namespace) |

**Scope clarification:** "Namespaced" means the resource exists within a Kubernetes namespace and is isolated per team/project. "Cluster-scoped" means the resource is visible across the entire cluster (all namespaces). Apps are not Kubernetes resources — they're catalog entries discovered from OCI registries.

Apps, Blueprints, and Bundles are all deployable. Apps and Blueprints are deployed as production workloads; Bundles are test-deployed during authoring to validate the stack before submission. A Bundle becomes a Blueprint version through an approval workflow; the Bundle persists after publishing so the author can iterate toward the next version.

> **A note on terminology — "Blueprint" in this document means "AIF Blueprint."** NVIDIA and other vendors publish their own "Reference Blueprints" (e.g., NVIDIA RAG, NVIDIA AIQ). Those are **Helm charts** mirrored into SUSE Registry; AIF treats each one as an App in the catalog and additionally **wraps** it as a single-component AIF Blueprint so it appears on the Blueprints page with versioning and governance. A NVIDIA Blueprint is not an AIF Blueprint — it's a chart that AIF wraps. See [§6 Blueprints](#6-blueprints) and the Glossary for details.

### Goals

- **Discover** — Browse a curated catalog of NVIDIA NIM inference models and SUSE-certified AI applications in one place.
- **Compose** — Build AI stacks in a Bundle workshop by combining Apps and existing Blueprints, then iterate until ready.
- **Publish** — Submit a Bundle for review; once approved by a publisher, it becomes an immutable, versioned Blueprint that any team can reuse.
- **Deploy** — Install AI workloads on clusters by deploying an App directly or a Blueprint, using Helm or GitOps (Rancher Fleet). GPU-accelerated NVIDIA NIM models are first-class.
- **Operate** — Monitor the health of every deployed AI workload, scale replicas, trigger rollbacks, and clean up with confidence.
- **Configure** — Centrally manage all platform credentials, registries, and GitOps integrations.

### What Makes It Distinct

- Zero new UIs to learn — it lives inside Rancher Dashboard as a native extension.
- First-class NVIDIA NIM support: deploy LLM and VLM inference microservices directly from the catalog. NIM containers and Helm charts reach the platform through a SUSE-managed mirror process — they are pulled from SUSE Registry at deploy time, never directly from NVIDIA NGC.
- Designed for environments served by SUSE Registry and SUSE Application Collection, including air-gapped deployments where those registries are themselves mirrored downstream by the customer's existing OCI tooling (e.g., Harbor).
- **Publish-by-approval governance**: a Bundle becomes a Blueprint only after a designated publisher approves it. Blueprint versions are immutable; deprecation and withdrawal are first-class lifecycle states on a published Blueprint.
- **Air-gap as a first-class deployment scenario**: every AIF capability — discover, compose, publish, deploy, operate, configure — works in an air-gapped cluster. AIF reaches no external service the customer hasn't already mirrored. Settings exposes per-endpoint registry overrides; the Helm values merge layer automatically rewrites image references; an air-gap release bundle ships every required image and chart in a single `tar.gz`. Air-gap is configuration, not a separate mode — same operator, same UI, same workflows. See [§9 Settings (Advanced)](#9-settings) and `AIRGAP.md`.

---

## 2. User Personas

### Platform Engineer
**Responsibilities:** Manages Kubernetes clusters, configures platform integrations, governs what gets deployed, enforces standards. Typically also holds the Blueprint Publisher role. In air-gap deployments, additionally owns the customer-side mirror leg.

**How they use SUSE AI Factory:**
- Configures SUSE Registry and SUSE Application Collection credentials in Settings.
- Sets up the GitOps (Fleet) integration for production deployments.
- Acts as Blueprint Publisher: reviews Bundles submitted for publication and approves or requests changes.
- Monitors cluster-level workload health on the Overview dashboard.
- **Air-gap operations** (when applicable): mirrors the AIF release bundle into the customer's OCI registry; configures Settings → Advanced → Registry Endpoints to point at the local registry; sets up the customer's chosen webhook TLS mode (`cert-manager` / `manual` / `helm-hook`); maintains the re-mirror cadence for new NIM and Reference Blueprint releases. See `AIRGAP.md` for the end-to-end procedure.

### AI/ML Practitioner (Bundle Author)
**Responsibilities:** Experiments with AI models, composes AI stacks for their project, deploys Apps and Blueprints, authors Bundles for publication.

**How they use SUSE AI Factory:**
- Browses the Apps catalog to discover NVIDIA NIM models and SUSE AI applications.
- Installs individual Apps directly from the catalog using the Install & Deploy wizard.
- Deploys existing Blueprints to a target cluster.
- Creates and iterates on Bundles by combining Apps and existing Blueprints.
- Test-deploys a Bundle, then submits it for review when ready to publish.
- Triggers catalog refreshes to pick up newly mirrored entries in SUSE Application Collection and SUSE Registry (the SUSE-managed mirror process keeps NVIDIA NIMs flowing into SUSE Registry; AIF only consumes from there).

### Operations Engineer
**Responsibilities:** Keeps deployed AI workloads healthy, responds to incidents, manages scaling.

**How they use SUSE AI Factory:**
- Monitors the Workloads page for unhealthy deployments.
- Inspects each Workload's source (App, Blueprint version, or test deploy from a Bundle) to plan upgrades and rollbacks.
- Uninstalls failed workloads and re-triggers deployment from the corresponding source.
- Checks Settings to verify catalog sync schedules are running correctly.

### Blueprint Publisher *(role, typically held by Platform Engineer)*
**Responsibilities:** Reviews Bundles submitted by authors and decides whether to publish them as Blueprint versions.

**How they use SUSE AI Factory:**
- Sees a "Pending Reviews" queue on the Bundles page listing all Submitted Bundles.
- Inspects each pending Bundle's components, values, and test-deploy history.
- Approves a Bundle to mint a new Blueprint version, or requests changes with a comment.

#### How a user becomes a Blueprint Publisher

Designation of users as Blueprint Publishers happens **outside the AIF UI**, through standard Kubernetes RBAC. The cluster admin binds the `aif-blueprint-publisher` ClusterRole — provided by the AIF operator chart — to one or more users or groups using `kubectl create clusterrolebinding`, the Rancher Cluster RBAC UI, or by mapping an existing OIDC group to it. The role is cluster-scoped: any bound subject can approve or reject Bundle submissions from any namespace, and can deprecate, withdraw, or reactivate any Blueprint version. AIF reads the binding via `SubjectAccessReview` at request time; no separate AIF-side publisher configuration exists. See `PUBLISHERs.md` for binding examples (kubectl, Rancher RBAC UI, OIDC group mapping).

---

## 3. Platform Navigation

SUSE AI Factory appears as a top-level product in the Rancher Dashboard sidebar, labeled **"SUSE AI Factory"** with the SUSE AI Factory logo mark.

The product contains seven navigation pages, accessible from the left sidebar within the SUSE AI Factory product context:

| Page | Icon | Purpose |
|------|------|---------|
| Overview | Dashboard icon | Platform health snapshot and active workloads summary |
| Apps | Catalog/grid icon | Unified AI application catalog (NVIDIA + SUSE) |
| Blueprints | Blueprint/stack icon | Gallery of validated, reusable AI stacks |
| Bundles | Bundle/package icon | Compose AI stacks and submit them for publication |
| Workloads | Compute/deploy icon | Monitor and manage deployed AI workloads |
| Pending Reviews | Review/queue icon | Publisher review queue — submitted Bundles awaiting approval |
| Settings | Gear icon | Platform credentials, registries, and integrations |

SUSE AI Factory operates in a **hub-on-management-cluster** model: the AIF operator and all CRs live on the Rancher management cluster. There is no cluster switcher within the product — all pages are always in the context of the management cluster. Downstream cluster selection happens at deploy time inside the Install & Deploy wizard (see [§10 Install & Deploy Flow](#10-install--deploy-flow)).

SUSE AI Factory integrates fully with Rancher's native product and sidebar framework. It appears and behaves exactly like any other built-in Rancher product — same keyboard navigation, same breadcrumbs, same namespace filter. Users who know Rancher already know how to navigate SUSE AI Factory.

---

## 4. Overview Dashboard

The Overview page is the first page users see when entering SUSE AI Factory. It provides an at-a-glance picture of the platform's health and active deployments.

### Platform Snapshot (top section)

A row of status cards displays:

| Card | What it shows |
|------|---------------|
| Operator Status | Green "Running" or yellow "Unknown" pill indicating whether the AI Factory operator is reachable |
| Catalog Status | Green "Connected" or yellow "Stale" pill indicating last successful refresh from SUSE Application Collection and the SUSE Registry NIM index (within 24h = Connected) |
| Apps Available | Total number of AI applications in the catalog |
| Blueprints | Total number of distinct Blueprints (grouped by name); subtitle shows total published versions |
| Bundles | Total Bundle count, with breakdown: "X drafts · Y submitted" |
| Active Workloads | Count of currently deployed workloads |

### Active Workloads Table (middle section)

A table listing every currently deployed workload with columns:

| Column | Description |
|--------|-------------|
| Name | Workload display name |
| Type | Inference / Training / Generic |
| Status | Color-coded health status pill (Active / Deploying / Degraded / Failed) |
| Source | Where this Workload came from: `App: <name>@<version>`, `Blueprint: <name>@<version>`, or `Bundle (test): <name>` |
| Created | Relative time since deployment (e.g., "3 hours ago") |

The table is empty if no workloads are deployed, with a friendly hint message directing the user to deploy from the Apps catalog or the Blueprints page.

### Quick Actions (bottom section)

Three clickable action cards provide shortcuts:

| Action | Destination |
|--------|------------|
| Explore Blueprints | Opens the Blueprints page |
| Browse Apps Library | Opens the Apps page |
| Manage Bundles | Opens the Bundles page |

### Banner

When no Blueprint Publishers are bound to the `aif-blueprint-publisher` ClusterRole, the **no-publishers banner** appears at the top of the Overview page (and the Bundles page) — see [§11 Role-Based UI States](#role-based-ui-states) for the text and behaviour. The banner is non-blocking; it informs the platform admin that Bundle approvals will not proceed until a publisher is designated. It auto-disappears once at least one subject is bound.

### Behavior

- The page auto-fetches all data on load.
- A global loading state ("Loading platform data...") is shown until all API calls complete.
- If any API call fails, an error banner is shown at the top of the page with the error message.

---

## 5. Apps — AI Catalog

The Apps page is the discovery hub. It aggregates AI applications from two sources — the SUSE-mirrored NVIDIA NIM catalog in SUSE Registry and the SUSE Application Collection — into a single browsable catalog.

### Header

The header displays:
- Total number of available apps.
- Breakdown counts: "X NVIDIA" and "Y SUSE" apps (displayed as colored pills).

### Toolbar

| Control | Description |
|---------|-------------|
| Search box | Full-text search across app name and description (instant, client-side filtering) |
| Source filter | Dropdown: All / NVIDIA / SUSE |
| Category filter | Dropdown: All / LLM / VLM / Database / AI / Observability / etc. |
| View toggle | Switch between Tile (grid) and List (table) views |
| Include Reference Blueprints toggle | Off by default. When off, vendor-published Reference Blueprint charts (e.g., NVIDIA RAG, NVIDIA AIQ) are filtered out of the Apps catalog (they're surfaced on the Blueprints page instead). Flip on to reveal them in the catalog. |
| Refresh button | Re-fetches the app catalog from the platform |

### Tile View (default)

Each app card displays:
- **Logo** (top-left corner)
- **Publisher badge** — green pill for NVIDIA; blue pill for SUSE
- **Title** (bold)
- **Description** (2–3 lines, truncated with ellipsis)
- **Tags** — up to 2 framework/use-case tags (e.g., "LLM", "Inference", "PyTorch")
- **Metadata row** — version, last updated date
- **Primary action — Install** — opens the Install & Deploy wizard for this app
- **Secondary action — Add to Bundle** — adds this App as a component to a new or existing Draft Bundle in the user's namespace
- **External link icon** — navigates to the app's project homepage in a new tab (when available)

### List View

A compact table with columns: Logo, Name, Publisher, Category, Version, Updated, Actions (Install · Add to Bundle).

### Empty & Error States

- **No results:** "No applications match your filters. Try clearing the search or changing the source filter."
- **No catalog configured:** "No apps found. Configure NVIDIA and SUSE credentials in Settings, then sync the catalog."
- **Registry unreachable:** *"Cannot reach SUSE Registry at `<endpoint>`. The Apps catalog cannot refresh until the endpoint is reachable from the operator pod. Check Settings → Advanced → Registry Endpoints, or verify network connectivity to your local registry. Last successful refresh: `<timestamp>`."* The page falls back to the last cached catalog if available; otherwise displays empty. Includes a link to `AIRGAP.md` §8 (Day-2 ops troubleshooting).
- **Error:** Error banner at top with message text.

### Reference Blueprint Charts and the Apps Catalog

Vendor-published **Reference Blueprints** — for example NVIDIA RAG and NVIDIA AIQ — are technically Helm charts (typically umbrella charts that bundle several components internally). When the SUSE-managed mirror process places them into SUSE Registry, AIF treats them as Helm chart Apps **and** auto-wraps each one as a single-component AIF Blueprint (see [§6 Blueprints](#6-blueprints)).

**Default visibility rule: Reference Blueprint charts are *not* shown in the Apps catalog by default.** They surface exclusively on the Blueprints page, where users get the full AIF lifecycle (versioned Upgrade action, Active/Deprecated/Withdrawn status, governance metadata). The Apps catalog stays focused on building-block components (Milvus, vLLM, individual NIM models, Qdrant, LiteLLM, etc.).

To show them in the Apps catalog anyway — for example, to inspect the underlying chart, deploy without an AIF Blueprint wrapper, or browse the full registry contents — flip the **Include Reference Blueprints** toolbar toggle. When visible, a Reference Blueprint chart is distinguished by a **Reference Blueprint** category badge.

The toggle exists for completeness; the recommended path remains the Blueprints page.

#### Why hidden by default

A Reference Blueprint chart can technically be deployed via the Apps page Install wizard, but the resulting Workload's `Source` is `App: <name>@<version>` — a one-shot Helm release with no link back to the wrapping AIF Blueprint. That means no Upgrade action surfaces when a newer chart version is mirrored, and no deprecation warnings appear if the chart is later marked deprecated. Deploying via the Blueprints page produces a Workload with `Source: Blueprint: <name>@<version>` and full lifecycle. Hiding the chart from the default Apps view nudges users toward the surface that gives them the better operate experience.

#### Where Reference Blueprints always appear (regardless of the toggle)

| Surface | Why |
|---|---|
| **Blueprints page** (default tab) | Primary surface; full AIF lifecycle |
| **Bundle Wizard → Step 2 (Components)** under both **Apps** and **Blueprints** filter tabs | Composition surface; an author may legitimately include a Reference Blueprint chart as a single component in a larger custom Bundle |

#### What stays on the Apps page regardless

The fact that something is a Helm chart doesn't qualify it to be hidden — only charts the mirror process has classified as Reference Blueprints (via the `ai.suse.com/role: reference-blueprint` annotation) are filtered out. NIM model charts, building-block components, and SUSE Application Collection apps all continue to appear in the Apps catalog as normal.

### App Install and Bundle Composition

Apps are first-class deployable units — you can install one directly without composing a Bundle. Clicking **Install** on any app opens the [Install & Deploy wizard](#10-install--deploy-flow) pre-filled with that app's chart repository, chart name, and latest version.

When the goal is to compose a multi-component AI stack instead of deploying a single App, click **Add to Bundle**. This opens a dialog asking whether to add the App to an existing Draft Bundle in the user's namespace or to create a new Bundle starting from this App.

Container images are pulled at deploy time from SUSE Registry (`registry.suse.com`) or SUSE Application Collection (`dp.apps.rancher.io`), using image-pull secrets configured in Settings. The platform does not pull from NVIDIA NGC directly — required NIM containers and charts (including vendor Reference Blueprint charts) are placed into SUSE Registry by an external SUSE-managed mirror process before they appear in the catalog.

---

## 6. Blueprints

A Blueprint is a published, immutable, versioned AI stack tied to a specific use case (e.g., RAG with Llama, vision pipeline, fine-tuning sandbox). Blueprints are cluster-scoped — once published, any team in the cluster can discover and deploy them.

A Blueprint pins its components and their versions. Once a Blueprint version is published, it is never modified. To change anything, the author iterates on a Bundle and publishes a new version.

### What Is (and Isn't) a Blueprint

A Blueprint is **always an AIF concept** — a published, immutable, versioned, governed artifact owned by AIF's lifecycle. Two paths create one:

1. **Internal publication** — A Bundle author submits their work; a publisher approves it; AIF mints a new immutable Blueprint version. This is the primary path and the reason Blueprints exist.
2. **Vendor-chart wrapping** — When a vendor-published Reference Blueprint Helm chart (e.g., NVIDIA RAG, NVIDIA AIQ) is mirrored into SUSE Registry, AIF auto-creates an AIF Blueprint that **wraps that single chart** as its only component. The wrapping Blueprint version tracks the chart's version. This makes vendor Reference Blueprints discoverable and deployable on the Blueprints page alongside internally-published Blueprints, while preserving AIF's lifecycle semantics (Active / Deprecated / Withdrawn).

> **Vendor Reference Blueprints are not the same as AIF Blueprints.** A NVIDIA Blueprint is a Helm chart packaged by NVIDIA. An AIF Blueprint is AIF's governed, versioned artifact. The vendor's chart can be deployed *as an App* directly from the Apps page, *included as a component in a Bundle*, **or** *wrapped as an AIF Blueprint* — these are three different presentations of the same underlying chart for three different user intents. AIF never claims authorship of vendor content; it wraps it.

> **The Blueprints page is the canonical surface for Reference Blueprints.** Reference Blueprint charts are hidden from the default Apps catalog view (see [§5 Reference Blueprint Charts and the Apps Catalog](#reference-blueprint-charts-and-the-apps-catalog)). Deploying a wrapped Reference Blueprint from this page produces a Workload with `Source: Blueprint: <name>@<version>`, which enables the Upgrade action when newer chart versions are mirrored and surfaces deprecation warnings. The Apps page exposes the same chart only when a user opts in via the **Include Reference Blueprints** toggle — primarily for inspection or one-shot deploys without lifecycle tracking.

### Header

Displays:
- Total number of distinct Blueprints (grouped by name).
- Total count of published versions across all Blueprints.

### Blueprint Gallery

A responsive card grid. Cards are grouped by Blueprint name; each card represents a Blueprint lineage and exposes a version selector to inspect any published version.

Each Blueprint card shows:

| Element | Description |
|---------|-------------|
| Title | Blueprint display name |
| Version selector | Dropdown of published versions (semver-sorted, latest first) |
| Use case | The use case this Blueprint targets (e.g., RAG, vision pipeline, fine-tuning) |
| Origin | `Wraps vendor chart` (with the wrapped chart name shown in tooltip / detail panel) **or** `Published from Bundle` (internally authored) |
| Description | 2–3 lines |
| Components | Chip list of included component names (Apps and/or other Blueprints) |
| Status pill | Active (default, blue) / Deprecated (yellow) / Withdrawn (gray) |
| Actions | Three buttons: Deploy, Start Bundle from Blueprint, View Versions |

### Actions

**Deploy** — Opens the [Install & Deploy wizard](#10-install--deploy-flow) pre-filled with the selected Blueprint version's components and default values. The user confirms the target cluster and any value overrides, then deploys. The user is navigated to the Workloads page after deployment begins.

**Start Bundle from Blueprint** — Creates a new Bundle in the user's namespace, pre-populated with the selected Blueprint version's components and values as the starting point. Used to author the next version.

**View Versions** — Opens a side panel listing all published versions of this Blueprint with publish dates, publisher names, status (Active / Deprecated / Withdrawn), and a brief change description per version.

### Publisher-only Actions

Users with the Blueprint Publisher role see two additional per-version actions:

- **Deprecate** — Flips a version's status pill to `Deprecated`. Existing Workloads on that version are unaffected; new deploys show a warning. Requires confirmation.
- **Withdraw** — Flips a version's status pill to `Withdrawn` and hides it from the default version selector. Existing Workloads continue to run; the version is no longer offered for new deploys. Requires confirmation.

Withdrawing a version is reversible (the publisher can re-activate it); deprecation and withdrawal never modify the published manifest itself.

### Empty & Error States

- **No blueprints:** "No blueprints available yet. Sync the catalog from Settings to wrap vendor Reference Blueprint charts as AIF Blueprints, or compose and publish a Bundle to create your first internally-authored Blueprint."
- **Registry unreachable:** *"Cannot refresh wrapped Reference Blueprints from the configured registry. Vendor-chart auto-wrapping is paused. Existing Blueprints (both vendor-wrapped and internally published) remain visible and deployable from the cached state. Check Settings → Advanced → Registry Endpoints."*
- **Error:** Error banner at top.

---

## 7. Bundles

A Bundle is a mutable workshop where an author composes a candidate AI stack from Apps and existing Blueprints. When the Bundle is ready, the author submits it for review; a Blueprint Publisher approves it, and the Bundle becomes a new Blueprint version. The Bundle itself persists, so the same author can iterate toward the next version.

Bundles are owned by an author or a small team and live in the author's namespace. They are not visible on the cluster-wide Blueprints page until a Blueprint version has been published from them.

### Bundle Workflow

```
Draft  →  Submitted  →  Approved → mints Blueprint vX.Y.Z (Bundle returns to Draft)
            ↘  Changes Requested → back to Draft
```

Status pills:

| State | Color | Meaning |
|-------|-------|---------|
| Draft | Gray | Author is editing; not under review |
| Submitted | Yellow | Awaiting publisher review |
| Changes Requested | Orange | Publisher rejected; reviewer comment is attached |

There is no "Ready" or "Certified" state on a Bundle — those concepts live on Blueprints. A Bundle is either in active iteration (Draft) or under review (Submitted / Changes Requested).

### Header

Displays total Bundle count plus a breakdown by status (e.g., "3 Drafts · 1 Submitted · 0 Changes Requested").

### Bundle Table

Columns: Title, Target Blueprint, Status, Author, Last Modified, Actions.

Context-aware actions change based on the Bundle's current state:

| State | Available Actions |
|-------|------------------|
| Draft | Edit, Test Deploy, Submit for Review, Delete |
| Submitted | Withdraw Submission, Delete |
| Changes Requested | Edit, View Reviewer Comment, Submit for Review, Delete |

All destructive actions (Delete, Withdraw Submission) require a confirmation dialog.

### Creating a Bundle — 4-Step Wizard

Clicking **New Bundle** opens the Bundle Wizard as an inline panel above the Bundle table. The wizard uses Rancher's native form components — the same inputs, selects, checkboxes, and YAML editors used throughout Rancher Dashboard — so the experience is visually and behaviourally consistent with the rest of the product.

---

**Step 1 — Basic Info**

| Field | Required | Description |
|-------|----------|-------------|
| Title | Yes | Human-readable Bundle name (private to the author until publication) |
| Target Blueprint Name | Yes | The Blueprint name this Bundle will publish into (e.g., `rag-with-llama`). A new name creates a new Blueprint lineage; an existing name appends a new version on publish. |
| Use Case | Yes | RAG / vision pipeline / fine-tuning sandbox / inference / other |
| Description | No | Free-text description |
| Authors | No | List of author names; add with Enter or the + button; remove with × |

The Next button is disabled until Title and Target Blueprint Name are provided.

---

**Step 2 — Components**

The author selects which Apps and existing Blueprints to include in this Bundle:

- A search box filters the list in real time by name or description.
- Filter tabs: All / Apps / Blueprints.
- Each component is shown as a selectable card with a checkbox, title, description, publisher/type, and version.
- Blueprint cards are visually distinguished with a blue left border. Selecting a Blueprint includes it as a sub-component (the deployed stack will include everything that Blueprint deploys).
- A "Selected: N components" summary updates as selections change.
- If the Bundle was started from an existing Blueprint via "Start Bundle from Blueprint" on the Blueprints page, that Blueprint's components are pre-selected and can be modified.

> **Note on Reference Blueprints:** Vendor-published Reference Blueprint charts (e.g., NVIDIA RAG) appear in this picker under **both** the **Apps** filter tab (because they're technically Helm chart Apps that can be included as a single component) and the **Blueprints** filter tab (because AIF wraps each one as a single-component AIF Blueprint). They appear here regardless of the **Include Reference Blueprints** toggle on the Apps page — composition isn't a discovery surface, so visibility rules from the Apps page don't apply. Selecting the same Reference Blueprint from either tab produces an equivalent component reference; it's the same underlying chart.

---

**Step 3 — Configuration**

For each selected component, the author can customize Helm values. A YAML editor per component is pre-filled with the component's default values. Authors can edit these directly. No component-specific guidance is shown — the editor is intentionally generic to support any chart.

---

**Step 4 — Save**

A summary screen shows all selections:
- Bundle title, target Blueprint name, use case, description
- Selected components (names and versions)
- Configuration overrides per component

A **Save Draft** button persists the Bundle as a Draft in the author's namespace. The wizard closes and the Bundle appears in the table with status **Draft**. A success toast is shown.

The author can re-open the Bundle later to edit it, test deploy it, or submit it for review. Target cluster and deployment strategy are *not* part of the Bundle — those are chosen at deploy time (see Test Deploy below).

### Bundle Actions

**Edit** — Re-opens the Bundle Wizard with the current contents loaded.

**Test Deploy** — Deploys the Bundle to a target cluster as a test Workload. The user picks the target cluster and deployment strategy (Helm or GitOps) at deploy time. Test Workloads are clearly labeled with `Source: Bundle (test): <name>` and intended to be short-lived. Test deploys are not required before submission, but their history is visible to publishers during review.

**Pre-flight Check** *(any user)* — Verifies that every component referenced by this Bundle (charts and images) is currently resolvable in the configured registry. Returns one of three results:

- **All resolvable** — green checkmark, no further action needed.
- **Missing items found** — yellow warning banner listing the missing charts/images, with copy-to-clipboard `skopeo copy` commands for the platform admin to remediate.
- **Cannot reach registry** — red banner with link to Settings → Advanced → Registry Endpoints.

Pre-flight is **informational only** — it does not block Submit or Approve. A Bundle can still be submitted with missing items; the publisher sees the same warning during review and can decide whether to approve, request changes, or wait for the missing items to be mirrored. This is especially useful in air-gap deployments where missing items typically mean "the customer hasn't re-mirrored that chart yet."

**Submit for Review** — Opens a small dialog asking for an optional change description and a proposed semantic version (e.g., "1.2.0"). Setting the Bundle status to **Submitted** places it in the publisher's review queue.

**Withdraw Submission** — Available while Submitted. Returns the Bundle to Draft state without notifying anyone.

**View Reviewer Comment** — Available when status is Changes Requested. Shows the publisher's comment so the author understands what to revise.

**Delete** — Removes the Bundle permanently. Requires confirmation. Available in any state. Deleting a Bundle does not affect any Blueprints already published from it.

### Publisher Review

Users with the Blueprint Publisher role see a "Pending Reviews" section above the Bundles table that lists all Submitted Bundles across all namespaces. For each pending Bundle, the publisher can:

- **Inspect** — View the Bundle's components, values, change description, proposed version, and any test deploy history. Above the Bundle details, the most recent **Pre-flight Check result** is rendered prominently (green checkmark / yellow warning with missing items / red registry-unreachable banner). If no pre-flight has been run since the last Bundle edit, a "Run Pre-flight" button appears in its place. Pre-flight is informational only; the publisher can approve regardless.
- **Approve & Publish** — Mints a new immutable Blueprint version with the proposed name and version. The Bundle returns to Draft state, ready for the next iteration. The new Blueprint version appears on the Blueprints page.
- **Request Changes** — Returns the Bundle to the author with a comment explaining what needs to change. Bundle status becomes Changes Requested.

The Bundle's content is the input to publishing; the Bundle itself is never moved into the Blueprint catalog.

---

## 8. Workloads

The Workloads page gives operations teams visibility into every deployed AI workload and the ability to act on them.

### Header

Displays:
- Total deployed workloads count.
- Number of healthy workloads (status Active or Running).

### Deployed Workloads Table

Columns: Name, Type, Status, Source, Created, Actions.

| Column | Description |
|--------|-------------|
| Name | Workload display name |
| Type | Inference / Training / Generic |
| Status | Color-coded pill: Active (green) / Deploying (blue) / Degraded (yellow) / Failed (red) |
| Source | Where this Workload came from. One of: `App: <name>@<version>` (single-App install), `Blueprint: <name>@<version>` (deployed from a published Blueprint), or `Bundle (test): <name>` (test deploy from an in-progress Bundle). |
| Created | Relative time (e.g., "2 days ago") |
| Actions | Uninstall button; for `Blueprint:` sources, also an "Upgrade" action that opens the deploy wizard against a newer Blueprint version |

**Uninstall** removes the workload and its underlying Kubernetes resources. Requires a confirmation dialog: "Are you sure you want to uninstall [name]? This will remove all associated Kubernetes resources." Cannot be undone.

**Upgrade** *(Blueprint-sourced workloads only)* — Opens the deploy wizard pre-selected with a newer published version of the Blueprint, carrying over the existing value overrides. The user confirms and re-deploys; AIF does not auto-upgrade workloads when a new Blueprint version is published.

### Deployment Pipeline Visualization

Below the table, a horizontal pipeline diagram visualizes the deployment stages for each workload:

```
[ Source ]  →  [ Resolve ]  →  [ Deploy ]
  App / Blueprint   Component       Workload
  / Bundle ref      versions        status
```

Each stage node shows:
- Stage label (Source / Resolve / Deploy)
- Relevant identifier (App or Blueprint name + version, or Bundle name for test deploys)
- Status indicator: green checkmark (complete/healthy), spinner (in progress), red X (failed)

For workloads in a healthy state, all three stages show green checkmarks and the Deploy node shows "Active". For workloads still deploying, the Deploy node shows a spinner.

### Empty State

"No workloads deployed yet. Install an App from the Apps catalog, deploy a Blueprint from the Blueprints page, or test-deploy a Bundle in progress."

---

## 9. Settings

The Settings page centralizes all platform configuration. Changes take effect immediately upon saving and trigger background catalog syncs where applicable.

### Page Layout

A single-page form divided into four labeled sections (with an optional fifth, **Advanced**). Each section is visually grouped with a section heading. A persistent **Save Settings** and **Reset** button bar appears at the bottom of the page.

The page header includes:

- An **Advanced** toggle (off by default) that reveals Section 5 — Registry Endpoints when enabled.
- An indicator chip **"Custom registry endpoints active"** (info-coloured) that appears whenever any field in Section 5 differs from the upstream defaults, **regardless of the Advanced toggle state**. This makes it visible at a glance that the cluster is in a non-default configuration (typical of air-gap installs).

---

**Section 1 — Fleet / GitOps**

| Field | Description |
|-------|-------------|
| Repository URL | Git repository URL for Fleet-managed deployments |
| Branch | Git branch to commit manifests to (default: `main`) |
| Auth Type | Token / SSH / Basic |
| Credentials | Token string, SSH private key path, or `user:password` depending on Auth Type |

---

**Section 2 — SUSE Application Collection**

| Field | Description |
|-------|-------------|
| Username | SUSE Application Collection username |
| Access Token | SUSE Application Collection token |
| Categories | Multi-select chip toggle for catalog categories to sync |

Available category chips: `db` · `workflow-management` · `ai` · `observability` · `app-ui` · `data` · `security` · `networking` · `ci-cd`

Clicking a chip toggles its inclusion in the sync filter. Active chips are highlighted.

---

**Section 3 — SUSE Registry**

| Field | Description |
|-------|-------------|
| Username | SUSE Registry username (auths `registry.suse.com`, including the SUSE-mirrored NVIDIA NIM charts and images) |
| Access Token | SUSE Registry access token |
| NIM Index Refresh Interval | How often to refresh the NIM catalog from the SUSE Registry chart index (e.g., `10m`) |
| Refresh NIM Index Now button | Manually triggers an immediate refresh against `oci://registry.suse.com/ai/charts/nvidia/` |

The **Refresh NIM Index Now** button shows a loading spinner while the refresh is running. It displays a success toast when complete.

> The platform does not include a "NVIDIA NGC" or "NVIDIA AI Enterprise" credential section. The SUSE-managed mirror process holds the NGC API key out-of-band and is responsible for placing the required NIMs into SUSE Registry; AIF only ever authenticates against SUSE Registry.

---

**Section 4 — Image Pull Secrets** *(informational)*

Read-only panel describing the single Kubernetes pull-secret the operator reconciles into each workload namespace from the SUSE Registry and SUSE Application Collection credentials above:

| Pull-secret name | Authenticates against | Image registry |
|------------------|------------------------|-----------------|
| `suse-registry-creds` (docker-config; one Secret covers both hosts) | SUSE Registry (incl. mirrored NVIDIA NIM) and SUSE Application Collection | `registry.suse.com`, `dp.apps.rancher.io` |

A copy-to-clipboard helper produces the corresponding `kubectl create secret docker-registry` command. The operator never hosts an internal image registry; air-gapped deployments are expected to use the customer's existing OCI tooling (e.g., Harbor) to mirror these upstream SUSE-hosted registries privately.

---

**Section 5 — Registry Endpoints (Advanced)** *(visible only when the page-header **Advanced** toggle is enabled)*

Override the upstream defaults to point AIF discovery and image references at customer-internal registries. Typical use case: air-gapped deployments where `registry.suse.com` and `dp.apps.rancher.io` are unreachable and the customer mirrors these registries to e.g. `harbor.example.com`.

| Field | Description |
|-------|-------------|
| SUSE Registry endpoint | Default `registry.suse.com`. Override to e.g. `harbor.example.com`. Used for NIM discovery, vendor-chart wrapping, and image references in NIM-generated values. |
| SUSE Application Collection endpoint | Default `dp.apps.rancher.io`. Override to e.g. `harbor.example.com`. Used for SUSE App Collection chart pulls. |
| SUSE Application Collection API | Default `https://api.apps.rancher.io`. Override to your internal mirror's API endpoint, or set to empty to disable HTTP catalog discovery (in which case AIF lists the OCI catalog at the SUSE Application Collection endpoint above). |
| Catalog Discovery Mode | `API` (default — uses the HTTP API) / `Registry Fallback` (try API; on connection error or HTTP 5xx, fall back to listing the OCI catalog) / `Disabled` (skip API entirely; OCI catalog only). Air-gap default is `Registry Fallback` or `Disabled`. |
| Image Rewrite Rules | Add prefix substitution rules. Each rule has a "Match prefix" (e.g., `registry.suse.com/`) and "Replace with" (e.g., `harbor.example.com/suse/`). Rules apply in order; first match wins per field. Applied during Helm values merge to ensure deployed pods can pull images. Leave empty when the endpoint overrides above are sufficient (typically when your local registry uses the same paths, just a different hostname). |
| Test Connection button | Tests reachability of every endpoint configured above; shows per-endpoint status and latency in a small panel below the button. Doesn't mutate state — safe to click before clicking Save. |

Below the table:

> **Reference:** see `AIRGAP.md` for an end-to-end air-gap install walkthrough including which endpoints to set, what to put in Image Rewrite Rules for common Harbor configurations, and how to verify the result.

---

### Save / Reset

- **Save Settings** — Persists all changes. Shows a loading spinner on the button while saving. On success, shows a green "Settings saved successfully" banner that auto-dismisses after 3 seconds. On failure, shows an error banner.
- **Reset** — Reverts all unsaved changes back to the last saved state.

---

## 10. Install & Deploy Flow

The Install & Deploy wizard is the single, unified flow used both for installing a single App from the catalog and for deploying a Blueprint version. It also runs when a Bundle is test-deployed. The wizard opens as a full-screen modal and guides the user through 4 steps. The wizard follows the same step-by-step visual pattern as Rancher's cluster provisioning and import wizards — users who have configured a cluster in Rancher will find the install experience immediately familiar.

Entry points:
- **Apps page** → click **Install** on any App tile (single-component deploy).
- **Blueprints page** → click **Deploy** on any Blueprint version (multi-component deploy with Blueprint provenance recorded on the resulting Workload).
- **Bundles page** → click **Test Deploy** on a Draft Bundle (Workload is labeled `Source: Bundle (test): <name>`).

### Step Indicator

A horizontal progress bar at the top of the modal shows the four steps: **Basic Info → Target → Config → Review**. Completed steps are marked with a checkmark. The current step is highlighted.

---

**Step 1 — Basic Info**

| Field | Required | Description |
|-------|----------|-------------|
| Release Name | Yes | Kubernetes Helm release name (auto-populated from app ID, editable) |
| Namespace | Yes | Target namespace. Dropdown of existing namespaces, plus "Create new namespace" option. Entering a new name creates it on install. |
| Chart Repository | Yes | URL of the Helm chart repository. Pre-populated from the app's chart source. |
| Chart Name | Yes | Helm chart name. Pre-populated from the app. |
| Version | Yes | Dropdown of available chart versions, sorted newest-first. Pre-selected to latest. |

---

**Step 2 — Target**

**Cluster selection:** A list of downstream clusters managed by Rancher. The user selects one or more clusters to deploy this workload to. Each cluster entry shows its name, ID, and status. The workload is delivered to the selected clusters by Rancher Fleet — no direct access from the user's browser or from the AIF operator to the downstream cluster APIs is required. The cluster list is sourced from Rancher's management cluster registry (`management.cattle.io/cluster`).

**Delivery strategy:** A radio selection below the cluster list:
- **Helm** *(default)* — AIF creates a Fleet Bundle CR containing the OCI chart and merged values. No git repository required.
- **GitOps** — AIF generates manifests and pushes them to the Fleet git repository configured in Settings (`Settings.spec.fleet.repoURL` / `Settings.spec.fleet.branch`), then creates a Fleet GitRepo CR. Fleet watches the repo and generates the Bundle automatically. No git URL input is collected in this wizard step.

Both strategies use Fleet for cross-cluster delivery. The selection is written to `Workload.spec.deployStrategy`.

> **CRD note:** `spec.targetClusters` is a `[]string` field. The Fleet CR `spec.targets` array is populated with one entry per cluster in this slice. The rename from the original singular `targetCluster` field is tracked in story P6-1-refactor.

---

**Step 3 — Configuration**

The configuration form adapts based on the type of app being installed:

**For NVIDIA NIM apps:**
- Model information panel (model name, type, recommended GPU count)
- Helm values YAML editor pre-filled with NIM-specific defaults (image, resources, GPU requests, tolerations, persistence settings)

**For generic container apps:**
- Form fields: This comes from a questions.yaml if one is present. Container image, replica count, port, service type (ClusterIP / NodePort / LoadBalancer)
- GPU toggle: Enable/disable GPU resource requests; if enabled, GPU count field appears
- Helm values YAML editor for advanced customization

**For all apps:**
- A **Reset to defaults** button reverts the YAML editor to the chart's default values

---

**Step 4 — Review**

A read-only summary of all selections before installation:
- Release name and namespace
- Chart repository, name, and version
- Target clusters and delivery strategy (Helm / GitOps)
- Configuration highlights (image, replicas, GPU, key overrides)

A large **Install** button with a loading spinner triggers the deployment. The modal closes on success and a success toast appears. On failure, an error banner appears within the modal.

---

## 11. Notifications and Feedback

### Loading States

Every data fetch (page load, action buttons) shows a visual loading indicator:
- Full-page loads: a centered "Loading [resource]..." message with a spinner.
- Button-triggered actions: the button label is replaced by a spinner while the action is in progress; the button is disabled to prevent double-submission.

### Success Notifications

On successful create, update, submit, approve, deploy, save, or sync actions:
- A green success toast appears in the bottom-right corner.
- The message text confirms the action (e.g., "Bundle submitted for review", "Blueprint v1.2.0 published", "Workload deployed").
- The toast auto-dismisses after 3 seconds.
- The relevant data table or section auto-refreshes to show the updated state.

### Error Notifications

On any failed API call:
- An error banner appears at the top of the current page.
- The banner shows the error message text.
- The banner persists until the user dismisses it or successfully retries the action.
- Errors on modal wizards appear within the modal, not at the page level.

### Confirmation Dialogs

Destructive or irreversible actions require an explicit user confirmation before proceeding:

| Action | Confirmation message |
|--------|---------------------|
| Delete Bundle | "Are you sure you want to delete '[title]'? This cannot be undone. Blueprints already published from this Bundle are unaffected." |
| Withdraw Submission | "Withdraw '[title]' from review? It will return to Draft and the publisher will no longer see it in the queue." |
| Uninstall Workload | "Are you sure you want to uninstall '[name]'? This will remove all associated Kubernetes resources." |
| Deprecate Blueprint | "Mark '[name]@[version]' as Deprecated? Existing deployments are unaffected; new deploys will surface a warning." |

Confirmation dialogs have two buttons: **Cancel** (closes dialog, no action taken) and a red **Confirm** button that proceeds.

### Role-Based UI States

Two UI behaviours surface the publisher-role state to users:

- **No-publishers banner** — When AIF detects that no subjects are bound to the `aif-blueprint-publisher` ClusterRole, an informational banner appears at the top of the **Bundles** and **Overview** pages: *"No Blueprint Publishers are configured. Bundle approvals will not proceed until the `aif-blueprint-publisher` ClusterRole is bound to a user or group."* The banner includes a link to `PUBLISHERs.md`. It is non-blocking — authors can still create, edit, test-deploy, and submit Bundles; the queue accumulates until a publisher is bound. The banner disappears automatically once at least one subject is bound.
- **Non-publisher action gating** — When a user without the publisher role views a Submitted Bundle, the **Approve & Publish** and **Request Changes** buttons render in a disabled state with the tooltip *"Requires the Blueprint Publisher role."* The Inspect action remains enabled so any user can view what was submitted. The same gating applies to the **Deprecate**, **Withdraw**, and **Reactivate** actions on Blueprint version cards.

---

## 12. Out of Scope for v1

The following features are explicitly not included in the v1 release:

- **Embedded image registry / image mirroring inside AIF** — the platform does not host or proxy an OCI registry. Container images are pulled at deploy time from SUSE Registry and SUSE Application Collection (or, in air-gap deployments, from the customer-internal registry the customer has mirrored those into). **(Updated for v1.1: AIF now supports image-reference rewriting at the Helm values merge layer — see [§9 Settings (Section 5)](#9-settings) — so air-gapped deployments can transparently retarget every container image to a customer-internal registry. AIF still does not host or proxy a registry; customers continue to use their existing OCI tooling for the mirror leg.)**
- **Customer-side mirror tooling** — AIF does not provide a mirror tool for the customer's "SUSE Registry → local Harbor" leg. The Air-Gap Release Bundle ships an opinionated `mirror.sh` (skopeo-based) for convenience, but customers are free to use Harbor's built-in replication, `oras`, or any other OCI tool. The contract AIF requires is: preserve digests and chart annotations.
- **Direct NVIDIA NGC access** — AIF does not connect to `nvcr.io`, `helm.ngc.nvidia.com`, or `integrate.api.nvidia.com`. The NIMs available in the platform are exactly those that the SUSE-managed mirror process has placed into SUSE Registry. Adding a new NIM to the catalog is an operational task on that mirror process, performed outside AIF before the NIM appears in the UI.
- **Multi-cluster workload federation** — deploying a single workload across multiple clusters simultaneously and managing it as one unit.
- **Multi-step or multi-reviewer approval workflows** — v1 supports a single-publisher approval gate. Two-stage review, required reviewer counts, and review-board policies are deferred.
- **Self-approval policy enforcement** — in v1, a user with the Blueprint Publisher role can approve their own submissions. A configurable "no self-approval" policy may be added later.
- **Test-deploy gating on submission** — v1 does not require a successful test deploy before a Bundle can be submitted for review. Test-deploy history is shown to the publisher as a soft signal during review.
- **Bundle branching, forking, or merging** — v1 supports linear iteration of a Bundle. Concurrent variant authoring against the same Blueprint lineage is done by creating separate Bundles; there is no in-product merge.
- **Editing a published Blueprint version** — Blueprint versions are immutable. To change a Blueprint, the author iterates on a Bundle and publishes a new version.
- **In-place Blueprint upgrade of running Workloads** — when a new Blueprint version is published, existing Workloads are not auto-upgraded. The Operations Engineer triggers an upgrade by re-deploying against the new version from the Workloads page.
- **Custom RBAC within the platform UI** — access control to SUSE AI Factory features is managed at the Rancher project/namespace level, plus the Blueprint Publisher role. Finer-grained RBAC inside the product is deferred.
- **Mobile and small-screen layouts** — the UI is designed for desktop browser viewports (1280px+).
- **Workload scaling from the UI** — scaling replica counts is managed through Kubernetes tooling, not the SUSE AI Factory UI.
- **Rollback from the UI** — rollbacks are triggered through Kubernetes tooling, not the SUSE AI Factory UI.
- **Real-time log streaming** — pod logs are not surfaced in the SUSE AI Factory UI; users use Rancher's built-in log viewer.

---

## 13. Glossary

| Term | Definition |
|------|------------|
| **AI Factory / AIF** | The product described by this specification — a Rancher Dashboard extension and operator that deploys, governs, and operates AI/ML workloads. |
| **App** | A Helm-chart-packaged AI application in the catalog, sourced from SUSE Application Collection (`dp.apps.rancher.io`) or from SUSE Registry (including the mirrored-NVIDIA namespace `registry.suse.com/ai/charts/nvidia/`). Includes both fine-grained building blocks (Milvus, vLLM, individual NIMs) and vendor-published Reference Blueprint charts (e.g., NVIDIA RAG, NVIDIA AIQ). Apps are directly deployable on their own and are also the components that authors combine in a Bundle. |
| **Bundle** | A mutable workshop where an author composes Apps and existing Blueprints into a candidate AI stack. Owned by an author, lives in the author's namespace. Becomes a Blueprint version through the publish-approval workflow; persists after publishing so the author can iterate. |
| **Blueprint** *(AIF Blueprint)* | A published, immutable, versioned AI stack tied to a use case — an **AIF concept**, distinct from vendor-published Reference Blueprints (see "NVIDIA Blueprint" below). Cluster-scoped and discoverable by all teams. Created by (a) a Bundle author submitting work and a publisher approving it, or (b) AIF auto-wrapping a vendor-published Reference Blueprint Helm chart from SUSE Registry as a single-component AIF Blueprint. New versions are minted; existing versions are never edited in place. |
| **Workload** | A running instance of an App or a Blueprint on a target cluster. Owns Kubernetes Deployments and Services. Records its `Source` (App, Blueprint version, or Bundle test deploy) so the operate experience can offer context-aware actions like Upgrade. |
| **Blueprint Publisher** | A role (typically held by Platform Engineers) responsible for reviewing Submitted Bundles and either approving them — minting a new Blueprint version — or requesting changes. |
| **Publish** | The action of approving a Submitted Bundle, which mints a new immutable Blueprint version and returns the Bundle to Draft state. |
| **NIM** | NVIDIA Inference Microservices — containerized inference servers (e.g., Llama, Mistral) deployed as workloads. The platform consumes NIMs from SUSE Registry only; the originals at `nvcr.io` are reached only by the SUSE-managed mirror process, not by AIF. |
| **NVIDIA Blueprint** *(synonym: Reference Blueprint, Vendor Reference Blueprint)* | A vendor-published reference workflow (e.g., NVIDIA RAG, NVIDIA AIQ) packaged as a Helm chart and mirrored into SUSE Registry. From AIF's perspective, a NVIDIA Blueprint is a **Helm chart App**: it appears in the Apps catalog, is deployable via the Install & Deploy wizard, and can be included as a single component in a Bundle. AIF additionally wraps each NVIDIA Blueprint chart as a single-component **AIF Blueprint** so it is discoverable on the Blueprints page. **NVIDIA Blueprints and AIF Blueprints are not the same concept** — the wrapping is a presentation/governance convenience that lets a vendor chart benefit from AIF's lifecycle (Active/Deprecated/Withdrawn). AIF does not author or modify the chart; it wraps it. |
| **NGC** | NVIDIA GPU Cloud — the upstream NVIDIA registry/catalog. The platform does not connect to NGC; it is mentioned only as the source the mirror process pulls from. |
| **Mirror process** | A SUSE-managed offline process that copies the agreed set of NIM containers and Helm charts from NVIDIA NGC into SUSE Registry. AIF only ever sees the SUSE Registry side of this pipeline. |
| **SUSE Application Collection** | SUSE's catalog of certified applications (Milvus, Ollama, vLLM, etc.) at `apps.rancher.io`. |
| **SUSE Registry** | SUSE's OCI image registry at `registry.suse.com`. |
| **Fleet** | Rancher's multi-cluster delivery engine. AIF uses Fleet for all workload delivery to downstream clusters. Two modes: `helm` (AIF creates a Fleet Bundle CR containing the OCI chart + values directly) and `gitops` (AIF creates a Fleet GitRepo CR pointing at a user-owned git repo; Fleet reads the repo and generates a Bundle). In both cases fleet-agents on downstream clusters pull from the Fleet controller on the management cluster — no outbound connections from the operator to downstream cluster APIs. |
| **Fleet Bundle CR** | A `fleet.cattle.io/v1alpha1 Bundle` resource on the management cluster, created by the AIF operator for `deployStrategy: helm`. Contains the Helm chart reference and merged values. Fleet agents on downstream clusters pull and apply it. Not to be confused with an AIF Bundle CR. |
| **Fleet GitRepo CR** | A `fleet.cattle.io/v1alpha1 GitRepo` resource on the management cluster, created by the AIF operator for `deployStrategy: gitops`. Points to the user's git repository; Fleet reads it and auto-generates a Fleet Bundle. |
| **deployStrategy** | How a Workload's components are delivered to the target downstream cluster. `helm`: AIF operator creates a Fleet Bundle CR (OCI chart + merged values, no git). `gitops`: AIF operator creates a Fleet GitRepo CR (git repo is the source of truth; Fleet generates the Bundle). Both strategies use Fleet for cross-cluster delivery. |
| **Steve** | Rancher's typed API surface used by the dashboard and extensions. |
| **UIPlugin** | Rancher's extension registration resource (`catalog.cattle.io/v1`). |
| **Source** *(of a Workload)* | Provenance metadata recorded on every Workload identifying where it came from: `App: <name>@<version>`, `Blueprint: <name>@<version>`, or `Bundle (test): <name>`. |
| **Pipeline Stage** | A user-visible step in the deployment pipeline visualization on the Workloads page. |
| **Air-gap** | A deployment scenario where the AIF cluster has no internet access and reaches only customer-internal services. AIF supports air-gap as a first-class configuration variant — same operator binary, same UI, same workflows; only Settings → Advanced → Registry Endpoints values, the operator chart's image config, and the customer's mirror process differ. There is no separate "Air-Gap Edition" of AIF. See `AIRGAP.md`. |
| **Registry Endpoint Override** | A field under Settings → Advanced → Registry Endpoints that points AIF at a customer-internal registry instead of the upstream SUSE-hosted defaults. Setting any override surfaces the "Custom registry endpoints active" chip in the Settings page header. |
| **Image Rewrite** | A Helm values transformation that substitutes hardcoded image-repository prefixes per the customer's rules, so deployed pods can pull from a customer-internal registry. Configured under Settings → Advanced → Registry Endpoints → Image Rewrite Rules. |
| **Air-Gap Release Bundle** | A `tar.gz` artifact published with each AIF release containing every image, chart, mirror script, and example values needed for an air-gap install. See `AIRGAP.md` §3. |
| **Pre-flight Check** | A non-blocking informational action on a Bundle that verifies all referenced charts and images are resolvable in the configured registry. Surfaces missing items as a warning to authors and publishers; does not block Submit or Approve. Especially useful in air-gap deployments to catch un-mirrored items before publishing. |

---

## 14. Cross-Reference Appendix

For technical details corresponding to each customer-facing concept:

| Customer concept (this doc) | Architecture section | Project Plan stories |
|------------------------------|------------------------|------------------------|
| §1 Product Vision (four-noun model) | `ARCHITECTURE.md` §2 System Context, §2.3 Conceptual Model | P0-2 |
| §2 User Personas (incl. Blueprint Publisher) | `ARCHITECTURE.md` §8.5 Publisher Role, §10.1 Authorization | P3-4, P3-5, P7-5; **admin doc:** `PUBLISHERs.md` |
| §3 Platform Navigation | `ARCHITECTURE.md` §7 UI Extension Architecture | P6-0, P6-1 |
| §4 Overview Dashboard (incl. no-publishers banner) | `ARCHITECTURE.md` §5 REST API (Auth endpoint), §11 Observability | P5-6, P6-10 |
| §5 Apps — AI Catalog (incl. Reference Blueprint visibility toggle) | `ARCHITECTURE.md` §5 (Apps endpoints + `?includeReferenceBlueprints` query param), §13.1 Reference Blueprint detection contract | P2-1, P2-2, P2-3, P2-4, P6-7 |
| §6 Blueprints (immutable, versioned, AIF concept) | `ARCHITECTURE.md` §4.3 Blueprint CRD (incl. `BlueprintSource` and Version Mapping for Wrapped Blueprints), §8.3 Immutability Webhook, §13.1 Reference Blueprint detection | P1-2, P1-5, P2-5 (Vendor-Chart Wrapper), P3-5, P6-5 |
| §7 Bundles (Draft/Submitted/ChangesRequested + publish workflow) | `ARCHITECTURE.md` §4.2 Bundle CRD, §5 Bundles & Publish endpoints | P1-1, P3-1, P3-2, P3-3, P3-4, P3-5, P3-6, P6-2, P6-3, P6-4 |
| §8 Workloads (with `Source` provenance) | `ARCHITECTURE.md` §4.4 Workload CRD (state machine + NIM sizing), §6.6 Helm Values Merge | P0-2, P1-3, P4-2, P5-1, P5-2, P5-3, P6-6 |
| §9 Settings | `ARCHITECTURE.md` §4.5 Settings CRD, §13.4 Image Pull Secrets | P0-2, P1-4, P5-4, P5-5, P6-9, P7-2 |
| §10 Install & Deploy Flow (unified wizard) | `ARCHITECTURE.md` §5 (deploy/test-deploy endpoints), §6.6 Helm Values Merge Precedence | P4-1, P4-5, P6-8 |
| §11 Notifications and Feedback (incl. Role-Based UI States) | `ARCHITECTURE.md` §5 Auth endpoint, §8.5 Publisher Role, §11 Observability (Events, Logging) | P5-6, P6-4, P6-10, P8-2, P8-3 |
| §12 Out of Scope | `ARCHITECTURE.md` §13.4 Image Pull Secrets (rationale for no internal registry); Project Plan Scope-Critical Decisions | — |
| §13 Glossary (incl. NVIDIA Blueprint disambiguation) | `ARCHITECTURE.md` §15 Glossary (engineering subset, includes matching disambiguation) | — |
| **Air-gap as first-class** — §1 Vision (air-gap bullet); §2 Platform Engineer extension; §5/§6 registry-unreachable empty states; §7 Pre-flight Check; §9 Section 5 Registry Endpoints (Advanced) | `ARCHITECTURE.md` §4.5 Settings additions (registryEndpoints, imageRewrite, catalogDiscovery, blueprintClassification), §4.4 NIM image parameterization, §5 (test-connection + preflight endpoints), §6.6 layer 5 image-rewrite, §9.1 chart values (image.registry, webhook.tlsMode), §13.1 customer-side re-mirroring, §16 Air-Gap Release Bundle | P0-6 (operator chart air-gap values), P0-7 (helm-hook TLS), P3-8 (preflight endpoint), P4-6 (image-rewrite pass), P5-7 (Settings CRD fields), P5-8 (catalog discovery modes), P5-9 (test-connection endpoint), P9-6 (release bundle), P9-7 (admin doc); plus updates to P6-4 (preflight banner), P6-5/P6-6 (unreachable states), P6-9 (Advanced toggle); **admin doc:** `AIRGAP.md` |
