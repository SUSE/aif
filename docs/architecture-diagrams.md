# SUSE AI Factory (AIF) - C4 Architecture Diagrams

## Table of Contents
1. [C4 Level 1 - System Context](#c4-level-1---system-context)
2. [C4 Level 2 - Container](#c4-level-2---container)
3. [C4 Level 3 - Component](#c4-level-3---component)
4. [C4 Level 4 - Code](#c4-level-4---code)
5. [Deployment Architecture](#deployment-architecture)
6. [Key Data Flows](#key-data-flows)

---

## C4 Level 1 - System Context

### High-Level System Context

```mermaid
graph TB
    subgraph Users
        PE[Platform Engineer]
        AI[AI/ML Practitioner]
        OPS[Operations Team]
    end
    
    AIF[SUSE AI Factory<br/>AI Platform Management]
    
    subgraph "Rancher Ecosystem"
        RANCH[Rancher Dashboard<br/>v2.10+]
        FLEET[Fleet<br/>GitOps Engine]
    end
    
    subgraph "External Systems"
        SUSE_REG[SUSE Registry<br/>registry.suse.com]
        APP_COL[SUSE App Collection<br/>api.apps.rancher.io]
        GIT[Git Repository]
    end
    
    subgraph "Target Infrastructure"
        K8S[Downstream K8s Clusters]
    end
    
    PE --> RANCH
    AI --> RANCH
    OPS --> RANCH
    
    RANCH --> AIF
    AIF --> SUSE_REG
    AIF --> APP_COL
    AIF --> FLEET
    AIF --> GIT
    
    FLEET --> K8S
    K8S --> SUSE_REG
    
    style AIF fill:#326CE5,color:#fff,stroke:#0caadc,stroke-width:3px
    style RANCH fill:#00c853,color:#fff
    style SUSE_REG fill:#ff6b6b,color:#fff
    style APP_COL fill:#ff6b6b,color:#fff
    style K8S fill:#4ecdc4,color:#fff
```

### Key Interactions

| Actor/System | Interacts With | Purpose | Protocol |
|-------------|----------------|---------|----------|
| Platform Engineer | Rancher Dashboard | Create Bundles, publish Blueprints | HTTPS |
| AI Practitioner | Rancher Dashboard | Deploy Workloads | HTTPS |
| AIF | SUSE Registry | Pull Helm charts, discover NIMs | OCI/HTTPS |
| AIF | SUSE App Collection | Discover curated AI apps | REST API |
| AIF | Fleet | Create deployment manifests | K8s API |
| Fleet | Downstream Clusters | Deploy AI workloads | Fleet tunnel |

---

## C4 Level 2 - Container

### Container Architecture Overview

```mermaid
graph TB
    subgraph "Rancher Dashboard (Browser)"
        UI[AI Factory UI Extension<br/>Vue 3 / TypeScript]
    end
    
    subgraph "AIF Operator Pod (namespace: aif)"
        API[REST API Server<br/>:8080]
        CTRL[Kubernetes Controllers<br/>5 reconcilers]
        WEBHOOK[Admission Webhook<br/>:9443]
        BIZ[Business Logic<br/>pkg/*]
    end
    
    subgraph "Data Layer"
        CRDS[(Custom Resources<br/>Bundle, Blueprint, Workload)]
        SECRETS[(Secrets<br/>Credentials)]
        FLEET_CRS[(Fleet Resources<br/>Bundle, GitRepo)]
    end
    
    subgraph "External"
        EXT[External Systems<br/>Registry, App Collection, Git]
    end
    
    UI -->|HTTPS| API
    API --> BIZ
    CTRL --> BIZ
    WEBHOOK --> BIZ
    
    BIZ --> CRDS
    BIZ --> SECRETS
    BIZ --> FLEET_CRS
    BIZ --> EXT
    
    style API fill:#667eea,color:#fff
    style CTRL fill:#667eea,color:#fff
    style WEBHOOK fill:#667eea,color:#fff
    style BIZ fill:#326CE5,color:#fff,stroke-width:3px
    style CRDS fill:#ffd700,color:#000
```

### Port Allocation

| Component | Port | Purpose |
|-----------|------|---------|
| REST API Server | 8080 | HTTP endpoints for UI and external clients |
| Health Probes | 8081 | /healthz, /readyz |
| Metrics Server | 8082 | Prometheus metrics |
| Admission Webhook | 9443 | Blueprint immutability validation |

---

## C4 Level 3 - Component

### 3.1 API Layer Architecture

```mermaid
graph LR
    CLIENT[HTTP Client]
    
    subgraph "Middleware Chain"
        CORS[CORS Handler]
        REQ_ID[Request ID]
        AUTH[Auth Extractor]
    end
    
    subgraph "Route Handlers"
        APPS[Apps Handler<br/>GET /api/v1/apps]
        PUBLISH[Publish Handler<br/>POST /api/v1/bundles/.../submit]
        ERRORS[Error Translator]
    end
    
    subgraph "Business Logic"
        BIZ[pkg/* interfaces]
    end
    
    CLIENT --> CORS
    CORS --> REQ_ID
    REQ_ID --> AUTH
    AUTH --> APPS
    AUTH --> PUBLISH
    
    APPS --> BIZ
    PUBLISH --> BIZ
    APPS --> ERRORS
    PUBLISH --> ERRORS
    
    style APPS fill:#4ecdc4,color:#fff
    style PUBLISH fill:#4ecdc4,color:#fff
    style BIZ fill:#326CE5,color:#fff
```

### 3.2 Controller Layer Architecture

```mermaid
graph TB
    subgraph "Controllers"
        direction TB
        BC[BundleReconciler]
        BPC[BlueprintReconciler]
        WC[WorkloadReconciler]
        SC[SettingsReconciler]
        IC[InstallAIExtensionReconciler]
    end
    
    subgraph "Business Logic Packages"
        BUNDLE[pkg/bundle<br/>Repository]
        BLUEPRINT[pkg/blueprint<br/>Repository]
        WORKLOAD[pkg/workload<br/>Manager]
        ENGINES[pkg/helm, pkg/git<br/>Engines]
    end
    
    subgraph "Kubernetes"
        CRDS[Custom Resources]
        EVENTS[Events]
    end
    
    BC --> BUNDLE
    BPC --> BLUEPRINT
    WC --> WORKLOAD
    SC --> ENGINES
    
    BUNDLE --> CRDS
    BLUEPRINT --> CRDS
    WORKLOAD --> CRDS
    
    BC --> EVENTS
    BPC --> EVENTS
    WC --> EVENTS
    
    style BC fill:#95e1d3,color:#000
    style BPC fill:#95e1d3,color:#000
    style WC fill:#95e1d3,color:#000
    style SC fill:#95e1d3,color:#000
    style IC fill:#95e1d3,color:#000
```

### 3.3 Business Logic Layer (Hexagonal Architecture)

```mermaid
graph LR
    subgraph "Driving Adapters"
        API_H[REST Handlers]
        CONTROLLERS[Controllers]
    end
    
    subgraph "Core Business Logic (Ports)"
        direction TB
        CATALOG[apps.Catalog]
        REPO[bundle/blueprint<br/>Repository]
        WORKFLOW[publish.Workflow]
        WL_MGR[workload.Manager]
    end
    
    subgraph "Driven Adapters"
        K8S[K8s Client]
        HELM[Helm Engine]
        GIT[Git Engine]
        NVIDIA[NVIDIA Discovery]
        APPCO[App Collection Client]
    end
    
    API_H --> CATALOG
    API_H --> WORKFLOW
    CONTROLLERS --> REPO
    CONTROLLERS --> WL_MGR
    
    REPO --> K8S
    WL_MGR --> HELM
    WL_MGR --> GIT
    CATALOG --> NVIDIA
    CATALOG --> APPCO
    
    style CATALOG fill:#326CE5,color:#fff
    style REPO fill:#326CE5,color:#fff
    style WORKFLOW fill:#326CE5,color:#fff
    style WL_MGR fill:#326CE5,color:#fff
```

---

## C4 Level 4 - Code

### 4.1 Apps Package Structure

```mermaid
graph TB
    subgraph "pkg/apps"
        direction TB
        CAT_IF["Catalog (interface)<br/>List(), Get(), Start()"]
        AGG["Aggregator (concrete)<br/>Combines multiple sources"]
        SRC_IF["Source (interface)<br/>List(), Start()"]
    end
    
    subgraph "Sources"
        NVIDIA_SRC["NVIDIASource<br/>Discovers NIMs"]
        APPCO_SRC["AppCoSource<br/>Discovers curated apps"]
    end
    
    subgraph "External Adapters"
        NVIDIA_DISC["nvidia.Discovery"]
        APPCO_CLIENT["source_collection.Client"]
    end
    
    CAT_IF -.implements.- AGG
    AGG --> SRC_IF
    SRC_IF -.implements.- NVIDIA_SRC
    SRC_IF -.implements.- APPCO_SRC
    
    NVIDIA_SRC --> NVIDIA_DISC
    APPCO_SRC --> APPCO_CLIENT
    
    style CAT_IF fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style SRC_IF fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style AGG fill:#4ecdc4,color:#fff
```

### 4.2 Bundle & Blueprint Package Structure

```mermaid
graph TB
    subgraph "pkg/bundle"
        direction TB
        BUNDLE_REPO["Repository (interface)<br/>Get(), List(), Create(), Update()"]
        BUNDLE_K8S["K8sRepository (concrete)<br/>Kubernetes adapter"]
        BUNDLE_MGR["Manager<br/>Validation logic"]
    end
    
    subgraph "pkg/blueprint"
        direction TB
        BP_REPO["Repository (interface)<br/>Get(), List(), Create()"]
        BP_K8S["K8sRepository (concrete)<br/>Kubernetes adapter"]
        BP_MGR["Manager<br/>Validation + vendor detection"]
    end
    
    subgraph "Kubernetes"
        K8S_CLIENT["client.Client"]
    end
    
    BUNDLE_REPO -.implements.- BUNDLE_K8S
    BP_REPO -.implements.- BP_K8S
    
    BUNDLE_K8S --> K8S_CLIENT
    BP_K8S --> K8S_CLIENT
    
    style BUNDLE_REPO fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style BP_REPO fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style BUNDLE_K8S fill:#4ecdc4,color:#fff
    style BP_K8S fill:#4ecdc4,color:#fff
```

### 4.3 Publish Workflow Package

```mermaid
graph TB
    subgraph "pkg/publish"
        direction TB
        WF_IF["Workflow (interface)<br/>Submit(), Approve(), RequestChanges()"]
        WF_IMPL["PublishWorkflow (concrete)<br/>Business logic"]
        AUTHZ["Authorizer (interface)<br/>CanSubmit(), CanApprove()"]
        REC["EventRecorder (interface)<br/>Record()"]
    end
    
    subgraph "Dependencies"
        BUNDLE_R["bundle.Repository"]
        BP_R["blueprint.Repository"]
    end
    
    WF_IF -.implements.- WF_IMPL
    WF_IMPL --> AUTHZ
    WF_IMPL --> REC
    WF_IMPL --> BUNDLE_R
    WF_IMPL --> BP_R
    
    style WF_IF fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style AUTHZ fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style REC fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style WF_IMPL fill:#4ecdc4,color:#fff
```

### 4.4 Workload & Deployment Engines

```mermaid
graph TB
    subgraph "pkg/workload"
        WL_MGR["Manager<br/>Deploy(), GetStatus()"]
    end
    
    subgraph "Deployment Engines"
        direction LR
        HELM_IF["helm.Engine (interface)<br/>Install(), Upgrade()"]
        HELM_IMPL["DefaultEngine<br/>Helm SDK wrapper"]
        
        GIT_ENG["git.FleetEngine<br/>CreateBundle(), CreateGitRepo()"]
    end
    
    subgraph "External SDKs"
        HELM_SDK["Helm SDK<br/>helm.sh/helm/v3"]
        GO_GIT["go-git<br/>git operations"]
        FLEET_API["Fleet CRDs<br/>fleet.cattle.io"]
    end
    
    WL_MGR --> HELM_IF
    WL_MGR --> GIT_ENG
    
    HELM_IF -.implements.- HELM_IMPL
    HELM_IMPL --> HELM_SDK
    GIT_ENG --> GO_GIT
    GIT_ENG --> FLEET_API
    
    style HELM_IF fill:#e7f3ff,color:#000,stroke:#326CE5,stroke-width:2px
    style WL_MGR fill:#4ecdc4,color:#fff
    style HELM_IMPL fill:#4ecdc4,color:#fff
    style GIT_ENG fill:#4ecdc4,color:#fff
```

---

## Deployment Architecture

### Hub-on-Management-Cluster Pattern

```mermaid
graph TB
    subgraph "Rancher Management Cluster"
        direction TB
        
        subgraph "Namespace: aif"
            OP[AIF Operator Pod]
            SVC[Services]
            PVC[PVC: aif-data]
            SEC[Secrets]
        end
        
        subgraph "Namespace: cattle-ui-plugin-system"
            UI_PLUGIN[UIPlugin: ai-factory]
        end
        
        subgraph "Cluster-scoped"
            CRDS[CRDs]
            BLUEPRINTS[Blueprint CRs]
            WEBHOOK[Webhook Config]
            RBAC[RBAC Resources]
        end
        
        subgraph "Namespace: fleet-local"
            FLEET_BUNDLES[Fleet Bundle CRs]
            FLEET_GITREPOS[Fleet GitRepo CRs]
        end
        
        subgraph "Namespace: &lt;author-ns&gt;"
            BUNDLES[Bundle CRs]
            WORKLOADS[Workload CRs]
        end
    end
    
    subgraph "Downstream Cluster 1"
        FA1[fleet-agent]
        DEPLOY1[AI Workload Pods]
    end
    
    subgraph "Downstream Cluster N"
        FAN[fleet-agent]
        DEPLOYN[AI Workload Pods]
    end
    
    OP --> FLEET_BUNDLES
    OP --> FLEET_GITREPOS
    
    FLEET_BUNDLES -.Fleet tunnel.-> FA1
    FLEET_BUNDLES -.Fleet tunnel.-> FAN
    
    FA1 --> DEPLOY1
    FAN --> DEPLOYN
    
    style OP fill:#326CE5,color:#fff,stroke-width:3px
    style DEPLOY1 fill:#4ecdc4,color:#fff
    style DEPLOYN fill:#4ecdc4,color:#fff
    style FLEET_BUNDLES fill:#95e1d3,color:#000
```

### Deployment Facts

**Management Cluster:**
- ✅ AIF Operator runs here (single pod, scalable to ≥2 with PDB)
- ✅ All CRDs stored here (Bundle, Blueprint, Workload)
- ✅ UI Plugin registered here
- ✅ Fleet CRs created here

**Downstream Clusters:**
- ❌ No AIF Operator
- ❌ No AIF CRDs
- ✅ Only fleet-agent (pre-existing Rancher component)
- ✅ AI workload pods deployed by fleet-agent

---

## Key Data Flows

### Flow 1: Bundle Publish Workflow

```mermaid
sequenceDiagram
    participant User
    participant UI
    participant API
    participant Workflow
    participant K8s
    
    Note over User,K8s: Phase 1: Create Bundle
    User->>UI: Create Bundle
    UI->>API: POST /api/v1/bundles
    API->>K8s: Create Bundle CR
    K8s-->>UI: status.phase = Draft
    
    Note over User,K8s: Phase 2: Submit for Approval
    User->>UI: Click "Submit"
    UI->>API: POST /bundles/{ns}/{name}/submit
    API->>Workflow: Submit(req)
    Workflow->>K8s: Update Bundle status
    K8s-->>UI: status.phase = Submitted
    
    Note over User,K8s: Phase 3: Approve & Publish
    User->>UI: Click "Approve"
    UI->>API: POST /bundles/{ns}/{name}/approve
    API->>Workflow: Approve(req)
    Workflow->>K8s: Create Blueprint CR
    Workflow->>K8s: Update Bundle status
    K8s-->>UI: Blueprint v1.0.0 published
```

### Flow 2: Workload Deployment

```mermaid
sequenceDiagram
    participant User
    participant UI
    participant WorkloadCtrl
    participant Fleet
    participant FleetAgent
    participant DownstreamK8s
    
    User->>UI: Deploy Blueprint
    UI->>WorkloadCtrl: Create Workload CR
    
    Note over WorkloadCtrl: Resolve Blueprint components<br/>Merge Helm values
    
    WorkloadCtrl->>Fleet: Create Fleet Bundle CR
    Note over Fleet: Fleet Bundle contains:<br/>- Helm chart OCI ref<br/>- Values YAML<br/>- Pull secret<br/>- Target clusters
    
    FleetAgent->>Fleet: Poll for changes
    Fleet-->>FleetAgent: New Bundle manifest
    
    FleetAgent->>DownstreamK8s: Apply resources<br/>(Deployment, Service, Secret)
    DownstreamK8s-->>FleetAgent: Resources created
    
    FleetAgent->>Fleet: Report status
    Fleet->>WorkloadCtrl: Update Fleet Bundle status
    WorkloadCtrl->>UI: Workload phase = Running
```

### Flow 3: Apps Catalog Discovery

```mermaid
sequenceDiagram
    participant Timer
    participant Aggregator
    participant NVIDIASource
    participant AppCoSource
    participant Cache
    participant User
    
    Note over Timer,Cache: Background: Every 5 minutes
    
    Timer->>NVIDIASource: Refresh
    NVIDIASource->>NVIDIASource: Fetch from SUSE Registry OCI
    NVIDIASource->>Cache: Update NVIDIA Apps
    
    Timer->>AppCoSource: Refresh
    AppCoSource->>AppCoSource: Fetch from api.apps.rancher.io
    AppCoSource->>Cache: Update App Collection Apps
    
    Note over Timer,User: User Request
    
    User->>Aggregator: GET /api/v1/apps
    Aggregator->>Cache: Read all sources
    Cache-->>Aggregator: Merged app list
    Aggregator-->>User: JSON response
```

---

## Architecture Principles

### Hexagonal Architecture (Ports & Adapters)

```
┌─────────────────────────────────────────┐
│  Driving Adapters (Inputs)              │
│  • REST API handlers                    │
│  • Kubernetes controllers               │
│  • Admission webhooks                   │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│  Business Logic Core (Ports)            │
│  • Interfaces in pkg/*/interface.go     │
│  • apps.Catalog                         │
│  • bundle/blueprint.Repository          │
│  • publish.Workflow                     │
│  • workload.Manager                     │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│  Driven Adapters (Outputs)              │
│  • K8s repositories                     │
│  • Helm engine                          │
│  • Git engine                           │
│  • NVIDIA discovery                     │
│  • App Collection client                │
└─────────────────────────────────────────┘
```

### Dependency Flow

```
cmd/operator/main.go (≤250 lines, wiring only)
    ↓
internal/{api,controller,webhook}/
    ↓
pkg/<domain>/{interface.go, types.go, ...}
    ↓
api/v1alpha1 (CRDs) — ONLY in conversions.go + repository.go
    ↓
stdlib, third-party (controller-runtime, Helm SDK, etc.)
```

### Four-Noun Conceptual Model

```
App (Building Block)
  ↓ compose into
Bundle (Mutable Workshop)
  ↓ publish via approval workflow
Blueprint (Immutable Stack)
  ↓ deploy as
Workload (Running Instance)
```

---

## Technology Stack

| Layer | Technologies |
|-------|-------------|
| **Language** | Go 1.21+ |
| **Frameworks** | controller-runtime v0.17, Helm SDK v3.13, go-git v5 |
| **UI** | Vue 3, @rancher/shell ^3.0.10, TypeScript |
| **Storage** | Kubernetes CRDs (etcd-backed) |
| **Observability** | log/slog (JSON/text), Prometheus, K8s Events |
| **Security** | RBAC, Admission webhooks, Secrets |
| **Deployment** | Helm charts, Fleet (GitOps) |
| **Testing** | envtest, Ginkgo v2, go test |

---

## Critical Constraints

1. **No internal OCI registry** — AIF does not host/proxy/mirror images
2. **No direct NVIDIA NGC access** — All assets via SUSE Registry mirror
3. **Blueprint immutability** — Spec fields immutable per version (webhook-enforced)
4. **Workload provenance** — Every Workload records `spec.source`
5. **Air-gap first-class** — Registry endpoints configurable via Settings CR
6. **Publish-by-approval governance** — Bundle → Submitted → Approved → Blueprint
7. **Single pull-secret pattern** — One docker-config Secret per workload namespace

---

**Generated:** 2026-05-12  
**Version:** 2.0 (Simplified & Decluttered)  
**Based on:** AIF codebase analysis (api/v1alpha1, internal, pkg, ui, charts)
