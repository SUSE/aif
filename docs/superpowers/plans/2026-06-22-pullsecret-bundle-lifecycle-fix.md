# Pull-Secret Bundle Lifecycle Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent Fleet from garbage-collecting long-named pull-secret releases, prevent pull-secret teardown from deleting workload namespaces, and keep the one-shot merge Job visible to Fleet.

**Architecture:** Keep the existing Fleet Bundle object identity, but give its Helm release an explicit deterministic name capped at Helm's 53-character limit using Fleet's own MD5/5-character truncation algorithm. Matching Fleet is migration-critical: the existing downstream release must remain recognizable instead of being garbage-collected once more. Mark the Bundle-managed Namespace with Helm's keep policy, and retain the completed merge Job instead of deliberately creating drift through TTL garbage collection.

**Tech Stack:** Go 1.25, Kubernetes API types, Fleet Bundle CRs, Helm release conventions, controller-runtime fake client.

---

### Task 1: Harden pull-secret Bundle lifecycle metadata

**Files:**
- Modify: `aif-operator/internal/cluster/bundle_client.go`

- [x] **Step 1: Add a focused release-name helper**

Add a deterministic `pullSecretReleaseName` helper that returns names up to 53 bytes unchanged and caps longer ASCII DNS labels exactly like Fleet v0.14.1: 47 bytes of prefix, `-`, and five lowercase hexadecimal characters from the name's MD5 digest. Preserve Fleet's separator behavior when the prefix already ends in `-`.

- [x] **Step 2: Emit lifecycle-safe Bundle fields**

Set `metadata.annotations["helm.sh/resource-policy"] = "keep"` on the Namespace manifest and set `spec.helm.releaseName` to `pullSecretReleaseName(bundleName)` alongside `takeOwnership: true`.

- [x] **Step 3: Retain completed merge Jobs**

Remove `ttlSecondsAfterFinished: 600` from `aif-operator/internal/cluster/sa_merge.go` and update its comments so Fleet's desired state remains present after execution.

### Task 2: Add regression coverage after implementation

**Files:**
- Modify: `aif-operator/internal/cluster/bundle_client_test.go`

- [x] **Step 1: Verify Bundle lifecycle fields**

Assert the Namespace YAML contains `helm.sh/resource-policy: keep`, `spec.helm.releaseName` is explicit, and the merge manifest has no `ttlSecondsAfterFinished`.

- [x] **Step 2: Verify Helm boundary behavior**

Add table-driven cases for 53-, 54-, and 55-character Bundle names. Assert the release name is unchanged at 53, capped to at most 53 above the boundary, stable across calls, DNS-label safe, and collision-resistant for long shared prefixes. Add an exact regression assertion that `ai-pullsecrets-aiq-aira-c-58kz8-c-58kz8-aiq-aira-system` maps to Fleet's existing `ai-pullsecrets-aiq-aira-c-58kz8-c-58kz8-aiq-air-f3763` release name.

- [x] **Step 3: Run focused tests**

Run `go test ./internal/cluster` from `aif-operator/`. Expected: exit 0.

### Task 3: Document the diagnosed failure and corrected recovery guidance

**Files:**
- Modify: `docs/evaluate-pullsecret-fix.md`

- [x] **Step 1: Correct TTL expectations**

Update the verification ladder and known-gotchas section to expect a retained completed merge Job.

- [x] **Step 2: Add the long-release regression signature**

Document Fleet's `Deleting unknown bundle ID, helm uninstall` signature, the 53-character release-name requirement, and the need for Namespace keep protection.

### Task 4: Verify the complete change

**Files:**
- Verify: `aif-operator/internal/cluster/*.go`
- Verify: `docs/evaluate-pullsecret-fix.md`

- [x] **Step 1: Format and inspect**

Run `gofmt` on modified Go files and `git diff --check`. Expected: no output from `git diff --check`.

- [x] **Step 2: Run operator tests**

Run `go test ./...` from `aif-operator/`. Expected: exit 0 with no package failures.

- [x] **Step 3: Review scope**

Confirm `git diff` contains only the planned operator, test, documentation, and plan changes; preserve the pre-existing UI modification and unrelated untracked files.
