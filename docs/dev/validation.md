# SUSE AI Factory — Validation Guide

This guide is the map for **proving the AIF code on this branch actually works**.
Run the rungs of the ladder below before a PR, before a release, or whenever
you want a quick "is the operator still wired up correctly?" answer.

It assumes:

- A working shell with `make`, `go ≥ 1.26` (or `GOTOOLCHAIN=auto`), `kubectl`.
- A `KUBECONFIG` pointing at a cluster (for the smoke and apply phases). A
  fresh local k3d cluster is fine — `make smoke-e2e` builds one for you.
- Docker daemon up (for the local cluster path).
- Optional: `.env` populated with the credentials listed in §5 for the
  live-upstream targets.

This doc lives in `docs/dev/` because it is a **developer reference**.
`docs/spec/*` is manager-owned and edits there need explicit sign-off
(see `CLAUDE.md` — Manager-owned docs).

---

## 1. Audience and scope

| Persona | When to use this doc | Which rungs to run |
|---|---|---|
| Contributor about to open a PR | Before pushing | §4.1–§4.3, plus the live verifies for any package you touched |
| Reviewer pulling a branch locally | Before approving | §4.1–§4.4 |
| Release engineer | Before tagging | All of §4 + §5 + §6 |
| Demo / smoke-test session | One-shot proof the stack works | `make smoke-e2e` alone is sufficient |

**Out of scope:** running an actual NIM model, mounting in a live Rancher
Dashboard, integration with downstream Fleet clusters under load. Those need
hardware and orchestration this doc does not assume you have.

---

## 2. Prerequisites

| Tool | Why | Install |
|---|---|---|
| `go` ≥ 1.26 | Build operator + run mocks | `GOTOOLCHAIN=auto` if your installed Go is older |
| `kubectl` | Apply CRs, observe status | OS package manager |
| `k3d` ≥ 5.x | Local cluster for §4.4 and §4.7 | `curl -s … \| bash` (see [k3d docs](https://k3d.io)) |
| Docker | k3d runtime | OS package manager |
| `helm` ≥ 3.13 | Install operator chart (only needed for §4.8 webhook check) | OS package manager |
| `openssl` | Generate self-signed webhook cert for `make run` | Usually preinstalled |
| `curl`, `jq` | Hit REST endpoints, parse JSON | OS package manager |
| `bun` (or `yarn`) + Node ≥ 24 | UI section §6 | [bun.sh](https://bun.sh) |

**Credentials.** Copy `.env.example` to `.env` and fill in any pair you
intend to exercise. The Makefile auto-loads `.env`. Live targets that have no
creds either fail loudly with an instructive message (`nim`, `appco`, `apps`,
`wrapper`) or skip silently (`fleet`, `git`) — see §5 for the matrix.

---

## 3. The validation ladder

Each rung is fast → slow, narrow → broad. Stop at the rung your change
warrants.

| Rung | Command | What it proves | Needs |
|---|---|---|---|
| 0 | `make lint && make manifests generate && git diff --exit-code` | Code passes linter; CRDs + deepcopy are committed up-to-date | Go toolchain |
| 1 | `make test` | Unit tests pass for every Go package | Go toolchain |
| 2 | `make test-controllers` | Controllers reconcile correctly against envtest (real apiserver + etcd, no real cluster) | envtest binaries (`make envtest` first time) |
| 3a | `make verify-<pkg>-mock` (one per pkg) | Each external-integration adapter speaks the right protocol against an in-process stub. The deterministic `Example_*` is the contract. | Nothing |
| 3b | `make test-api-apps` | REST handler routing and error envelope match | Nothing |
| 4 | `make smoke-e2e` | Operator builds, starts, reconciles Settings + Blueprint + Workload against a fresh k3d cluster, tears down cleanly | k3d + Docker |
| 5 | `make verify-<pkg>-live` (or `make verify-all-live`) | Each adapter works against the real upstream (SUSE Registry, Application Collection, Fleet, git host) | `.env` populated (see §5) |
| 6 | UI: `bun run test:unit && bun run build && bun run build-pkg ai-factory` | UI extension type-checks, unit-tests, and builds the Rancher-loadable bundle | Node ≥ 24 + bun (or yarn) |

---

## 4. Walkthrough — the happy path

Run these top-to-bottom on a clean checkout.

### 4.1 Code health

```bash
make lint
make manifests generate
git diff --exit-code -- charts/aif-operator/crds api/v1alpha1/zz_generated.deepcopy.go
```

**Expected:** `golangci-lint` exits 0; the in-repo CRD YAMLs and
`zz_generated.deepcopy.go` are byte-for-byte identical to what
`controller-gen` would produce now. A diff here means someone edited a CRD
type without regenerating — block the PR.

> **Caveat — `make manifests` does NOT regenerate RBAC.** Only the `crd`
> generator runs. `+kubebuilder:rbac:*` markers in source are documentation
> only; the live RBAC lives in `charts/aif-operator/templates/rbac.yaml` and
> is hand-maintained. If you added a new resource permission, edit the YAML.

### 4.2 Unit + envtest

```bash
make test            # pure unit tests, ~10–20s
make test-controllers # envtest (real kube-apiserver + etcd in-process), ~60s
```

**Expected:** Both exit 0. `test-controllers` also prints a coverage
percentage and writes `cover.out`. If envtest binaries are missing, run
`make envtest` once to download them.

### 4.3 External-integration mocks

Every package that talks to an external system ships a deterministic
`Example_*` test that doubles as the public-contract proof. Run them
individually or in a loop:

```bash
make verify-nim-mock      # pkg/nvidia: SUSE Registry-backed NIM discovery
make verify-appco-mock    # pkg/source_collection: SUSE Application Collection
make verify-apps-mock     # pkg/apps: unified catalog over the two above
make verify-helm-mock     # pkg/helm: Engine + FakeEngine contract
make verify-wrapper-mock  # pkg/blueprint: vendor-chart wrapper
make verify-fleet-mock    # pkg/fleet: FleetBundleEngine
make verify-git-mock      # pkg/git: go-git memfs round-trip
```

Or, in one go (there is no `verify-all-mock` aggregator target — by design,
because `smoke-e2e` covers the integration story at the cluster level):

```bash
for t in nim appco apps helm wrapper fleet git; do make "verify-$t-mock" || break; done
```

**Expected:** Each prints `PASS` for its `Example_*` block. Output is
deterministic; a diff in the `// Output:` comment is a real behaviour change
and must be reviewed.

### 4.4 Cluster smoke (local, fresh)

```bash
make smoke-e2e
```

**What it does** (see `hack/smoke-e2e.sh`):

1. Deletes any existing `aif-dev-smoke` k3d cluster, then creates a fresh one.
2. Installs CRDs from `charts/aif-operator/crds/`.
3. Generates self-signed webhook certs at `/tmp/k8s-webhook-server/serving-certs/`.
4. Builds `bin/aif-operator` and starts it in the background.
5. Polls `http://localhost:8080/healthz` until ready.
6. Applies `examples/{settings,blueprint,workload}-smoke.yaml`.
7. Waits for `Blueprint/smoke-blueprint.0.1.0` to reach `phase=Active`
   (`kubectl wait --for=jsonpath`).
8. Surfaces the Workload phase, prints a `kubectl get` summary, tails the
   operator log.
9. `trap`-on-EXIT cleanup: kills the operator and deletes the cluster.

**Expected:** Final line is `>>> [smoke-e2e] PASS`. The Workload will stay
at `phase=Pending` with `Ready=False, reason=PullSecretNotReady` because the
smoke harness deliberately does NOT seed `suse-registry-creds` in the
operator namespace — the workload reconciler returns its
`errPullSecretNotReady` sentinel before invoking the deployer, which is the
"operator observed and accepted the spec" assertion this rung is asserting.
The `targetClusters: []` choice in the example is independent: it would
keep the Workload at Pending even if the pull-secret were present, because
the deployer has nothing to roll out to. Both gates exist on purpose; the
pull-secret gate is just the one that fires first.

**Useful env overrides:**

```bash
SMOKE_KEEP=1 make smoke-e2e          # leave cluster + operator running after PASS
SMOKE_TIMEOUT=120 make smoke-e2e     # bigger Blueprint-reconcile timeout
SMOKE_CLUSTER_NAME=aif-foo make smoke-e2e
```

### 4.5 Settings reconciles

If you ran `SMOKE_KEEP=1 make smoke-e2e` (so the cluster is still up) — or
if you brought the cluster up yourself via the manual path
(`make dev-cluster && make dev-install && make run &`) — verify Settings is
propagated to the engine bus:

```bash
kubectl get settings -n aif settings -o jsonpath='{.status.conditions}' | jq .
# Look for: type=Ready, status=True, reason=SettingsApplied (or similar)

# The smoke-e2e harness writes its operator log to /tmp/aif-operator-smoke.log;
# `make run` writes to your terminal, so tail the appropriate source for your path.
tail -n 50 /tmp/aif-operator-smoke.log | grep -i "settings\|engineBus"
# Should show SettingsReconciler picked up the change and projected it onto each engine.
```

**Expected:** the Settings CR carries a Ready condition; operator log has
one or more `SettingsApplied` / `engineBus.Apply` / `projectFleet` entries
in JSON form.

### 4.6 Apps catalog REST

Hit the operator's REST surface directly. Endpoints (see Appendix B for the
full table):

```bash
# Liveness
curl -fsS http://localhost:8080/healthz | jq .
curl -fsS http://localhost:8080/api/v1/version | jq .

# Apps catalog — fans out to NVIDIASource + AppCoSource adapters.
# Response is a JSON array at the root (not wrapped in `{apps: [...]}`).
curl -fsS http://localhost:8080/api/v1/apps | jq 'length'
curl -fsS http://localhost:8080/api/v1/apps/categories | jq .

# Single app (pick an ID from the list above)
APP_ID="$(curl -fsS http://localhost:8080/api/v1/apps | jq -r '.[0].id')"
curl -fsS "http://localhost:8080/api/v1/apps/$APP_ID" | jq .
```

**Expected:** Without credentials, both adapters return empty / fall back to
stubs and you'll see an empty catalog — that's still a positive signal that
routing, CORS, and the error envelope all work. With creds set in `Settings`
(or via the live targets in §5), `length > 0` and at least the NIM
entries are populated.

### 4.7 Blueprint + Workload deploy (manual probe)

Apply the same examples by hand and walk the state transitions:

```bash
kubectl apply -f examples/blueprint-smoke.yaml
kubectl apply -f examples/workload-smoke.yaml

# Blueprint
kubectl get blueprint smoke-blueprint.0.1.0 \
  -o jsonpath='{.status.phase} {.status.conditions[?(@.type=="Ready")].reason}'
# → "Active BlueprintValidated"

# Workload
kubectl get workload -n default smoke -o yaml \
  | yq '.status'   # or jq via -o json
```

**Expected:**

- Blueprint reaches `phase=Active`, condition `Ready=True, reason=BlueprintValidated`.
- Workload picks up the Blueprint reference and stays at `Pending`. The first
  gate that fires is `Ready=False, reason=PullSecretNotReady` — the workload
  reconciler bails out before invoking the deployer until
  `suse-registry-creds` exists in the operator namespace (the pull-secret
  reconciler from P5-5 materializes it from `Settings.spec.suseRegistry`).
  Seed Settings with real creds (see §4.5 and §5) to clear this gate; once
  cleared, `targetClusters: []` becomes the next gate (no Fleet `Bundle`
  target). Populate the list with a real downstream cluster ID
  (`management.cattle.io.cluster` name) to drive `Deploying → Running` (or
  `Failed` for a placeholder chart that won't actually install).

### 4.8 Webhook end-to-end (chart install path)

> The Blueprint immutability webhook is **registered inside the operator
> process** when you `make run` or `smoke-e2e`, but the matching
> `ValidatingWebhookConfiguration` in the cluster is only created by the
> Helm chart. So in the local out-of-cluster path, you can patch a
> Blueprint's spec successfully and it will look like the webhook is
> broken — it's not, it's just not registered.

To exercise immutability end-to-end you need cert-manager available in the
cluster, because the chart's `values.schema.json` currently only enumerates
`webhook.tlsMode=cert-manager`. (The `helm-hook` mode is the deferred
follow-up wired to P0-7 / P0-3 in `PROJECT_PLAN.md` — until it ships, a
cert-manager prerequisite is the only supported path.)

```bash
# Prereq: cert-manager already installed in the cluster.
#   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
#   kubectl -n cert-manager rollout status deploy/cert-manager-webhook

# Install the chart instead of running out-of-cluster.
helm install aif-operator charts/aif-operator \
  --namespace aif --create-namespace

kubectl apply -f examples/blueprint-smoke.yaml

# This MUST be denied by the webhook:
kubectl patch blueprint smoke-blueprint.0.1.0 --type=merge \
  -p '{"spec":{"description":"forbidden mutation"}}'
# → Error from server: admission webhook ... denied the request: spec is immutable

# This MUST be allowed (status mutation, not spec):
kubectl patch blueprint smoke-blueprint.0.1.0 --subresource=status --type=merge \
  -p '{"status":{"phase":"Deprecated"}}'
# → blueprint.ai.suse.com/smoke-blueprint.0.1.0 patched
```

**Expected:** First patch rejected with the immutability message; second
patch succeeds.

---

## 5. Remote / live integration tests

These exercise the real upstream APIs and require credentials. Run them when
you change any package under `pkg/{nvidia,source_collection,apps,blueprint,fleet,git}`,
or as part of a release-readiness sweep.

| Target | Env vars (required to run) | What it proves | Behaviour without creds |
|---|---|---|---|
| `make verify-nim-live`     | `SUSE_REG_USER`, `SUSE_REG_TOKEN`         | SUSE Registry NIM discovery + index | Hard-fail with message |
| `make verify-appco-live`   | `SUSE_APPCO_USER`, `SUSE_APPCO_TOKEN`     | SUSE Application Collection list / search | Hard-fail with message |
| `make verify-apps-live`    | All four above                            | Unified Apps catalog fan-out + dedupe | Hard-fail with message |
| `make verify-wrapper-live` | All four above                            | Wrapper dry-run against real upstreams | Hard-fail with message |
| `make verify-fleet-live`   | `AIF_FLEET_LIVE_KUBECONFIG`, `AIF_FLEET_LIVE_NAMESPACE`, `AIF_FLEET_LIVE_TARGET_CLUSTER`, optionally `AIF_FLEET_LIVE_GIT_REPO` + `AIF_FLEET_LIVE_GIT_TOKEN` | Fleet `Bundle` + `GitRepo` round-trip against a real Fleet manager | Skips cleanly (`t.Skip`) |
| `make verify-git-live`     | `AIF_GIT_LIVE_REPO`, `AIF_GIT_LIVE_BRANCH`, plus one of: `AIF_GIT_LIVE_TOKEN` / `AIF_GIT_LIVE_USERNAME`+`AIF_GIT_LIVE_PASSWORD` / `AIF_GIT_LIVE_SSH_KEY_PATH` | Push to a real remote, fetch, diff | Skips cleanly (`t.Skip`) |

The SUSE Registry and SUSE Application Collection credentials are
**intentionally separate** even though many SCC accounts can authenticate
to both (see `ARCHITECTURE.md §13.2`). Don't reuse one as the other in
code — keep the env vars distinct.

**Run all of them in one shot:**

```bash
make verify-all-live
```

This invokes each `verify-*-live` target in sequence, continues past failures,
and prints a per-target pass/fail/skip summary at the end. Exit code is
non-zero iff at least one target failed (cleanly-skipping targets count as
PASS in the summary). If you only want the "skips don't bother me" ones for
CI:

```bash
make verify-fleet-live verify-git-live   # both skip without env vars
```

---

## 6. UI verification

The UI lives under `ui/ai-factory/` and ships as a Rancher Dashboard
extension built against `@rancher/shell`.

### 6.1 Install, type-check, test, build

```bash
cd ui/ai-factory

# bun is preferred (faster install, drop-in scripts).
bun install
bun run test:unit       # Vitest unit tests under tests/unit/
bun run test            # node --test scaffold smoke tests under pkg/ai-factory/test/
bun run build           # production webpack build (vue-cli-service)
```

`package.json` declares `engines.node: ">=24"`. Bun's Node compat works
fine for this repo, but the underlying build tool is still
`vue-cli-service` (webpack 5) — if you hit a plugin compatibility issue
with bun, fall back to `yarn`:

```bash
yarn install
yarn test:unit
yarn build
```

### 6.2 Build the extension package and serve it for a local Rancher

Rancher Dashboard loads extensions from a separately built `dist-pkg/`
bundle. To exercise the UI inside a real Rancher Dashboard:

```bash
cd ui/ai-factory

# Build the loadable package
bun run build-pkg ai-factory     # produces dist-pkg/ai-factory/...

# Serve dist-pkg over HTTP so Rancher can fetch it
bun run serve-pkgs               # defaults to http://127.0.0.1:4500

# In another shell, start a local Rancher Dashboard pointed at your cluster
# (out of scope here) and install the extension via:
#   Extensions → Install Local Extension → http://127.0.0.1:4500
```

The Steve store URL the UI talks to is the Rancher Dashboard's; AIF-specific
REST calls go through `pkg/ai-factory/utils/operator-api.ts` to the
operator's `/api/v1/*` endpoints. If you're running the operator via
`make smoke-e2e` (or `make run`) you can verify those endpoints exist with
the curl recipes in Appendix B before debugging the UI side.

---

## Appendix A — Makefile target reference

Grouped by purpose. Every target listed here is real (no "Not implemented
yet" stubs).

### Build, test, lint

| Target | Description |
|---|---|
| `make build` | Compile `bin/aif-operator` |
| `make test` | `go test ./...` |
| `make test-controllers` | Controller tests under envtest |
| `make lint` | `golangci-lint` + raw-HTTP-error grep guard |
| `make manifests` | Regenerate CRD YAMLs into `charts/aif-operator/crds/` |
| `make generate` | Regenerate `zz_generated.deepcopy.go` |
| `make install-tools` | Pin-install dev tools (controller-gen, golangci-lint, mockgen, ginkgo, setup-envtest) |
| `make envtest` | Download envtest binaries (etcd + kube-apiserver) |

### Local dev loop

| Target | Description |
|---|---|
| `make dev-cluster` | Create k3d cluster `aif-dev` |
| `make dev-cluster-down` | Delete k3d cluster `aif-dev` |
| `make dev-install` | Apply CRDs + create `aif` namespace |
| `make dev-certs` | Generate self-signed webhook cert (only needed for `make run`) |
| `make run` | Run operator out-of-cluster against your kubeconfig |
| `make examples` | Apply all CRs in `examples/` |
| `make smoke-e2e` | Full fresh local e2e (see §4.4) |

### Per-package verification trios

| Package | `test-*` | `verify-*-mock` | `verify-*-live` |
|---|---|---|---|
| `pkg/nvidia`            | `test-nim`     | `verify-nim-mock`     | `verify-nim-live`     |
| `pkg/source_collection` | `test-appco`   | `verify-appco-mock`   | `verify-appco-live`   |
| `pkg/apps`              | `test-apps`    | `verify-apps-mock`    | `verify-apps-live`    |
| `pkg/helm`              | `test-helm`    | `verify-helm-mock`    | — (deferred to P5-7) |
| `pkg/blueprint` wrapper | `test-wrapper` | `verify-wrapper-mock` | `verify-wrapper-live` |
| `pkg/fleet`             | `test-fleet`   | `verify-fleet-mock`   | `verify-fleet-live`   |
| `pkg/git`               | `test-git`     | `verify-git-mock`     | `verify-git-live`     |
| `internal/api` (Apps)   | `test-api-apps` | — | — |
| `pkg/helm` envtest      | `test-helm-envtest` | — | — |

### Aggregators

| Target | Description |
|---|---|
| `make verify-all-live` | Run every `verify-*-live` in sequence (§5) |

### Container / chart (stubs — not yet implemented)

| Target | Status |
|---|---|
| `make docker-build` | Works (real) |
| `make docker-push`  | Works (real) |
| `make helm-install` | **Stub** — prints "Not implemented yet" |
| `make helm-uninstall` | **Stub** |
| `make charts-package` | **Stub** |

---

## Appendix B — REST endpoint reference

Wired up in `internal/manager/routes.go`. Operator binds to `:8080` by
default; override with `--addr=:NNNN`.

| Method + path | Handler | What it does |
|---|---|---|
| `GET  /healthz` | `internal/manager/routes.go` | Liveness — returns `{"status":"ok"}` |
| `GET  /api/v1/version` | `internal/manager/routes.go` | Operator version |
| `GET  /api/v1/apps` | `internal/api/apps.go` | Unified Apps catalog (NIM + AppCo fan-out) |
| `GET  /api/v1/apps/categories` | `internal/api/apps.go` | Distinct category list |
| `GET  /api/v1/apps/{id}` | `internal/api/apps.go` | Single App by ID |
| `GET  /api/v1/apps/{id}/values` | `internal/api/apps.go` | Chart default `values.yaml` + `questions.yaml` (requires `?version=`) |
| `POST /api/v1/blueprints` | `internal/api/blueprints.go` | Create a Blueprint version (SAR-gated, `aif-blueprint-publisher`) |
| `PATCH /api/v1/blueprints/{name}/{version}` | `internal/api/blueprints.go` | Status patch (Active ↔ Deprecated; Withdrawn is terminal) |
| `DELETE /api/v1/blueprints/{name}/{version}` | `internal/api/blueprints.go` | Delete a Blueprint version (409 if Workloads still reference it) |
| `GET  /api/v1/workloads` | `internal/api/workloads.go` | List Workloads (SAR-gated; `?namespace=` to scope) |
| `GET  /api/v1/workloads/{namespace}/{name}` | `internal/api/workloads.go` | Single Workload |
| `POST /api/v1/workloads` | `internal/api/workloads.go` | Create a Workload (SAR-gated on body namespace) |
| `PUT  /api/v1/workloads/{namespace}/{name}` | `internal/api/workloads.go` | Full-replace a Workload spec (SAR-gated) |
| `DELETE /api/v1/workloads/{namespace}/{name}` | `internal/api/workloads.go` | Delete a Workload (SAR-gated) |
| `POST /api/v1/workloads/{namespace}/{name}/upgrade` | `internal/api/workloads.go` | Trigger Workload upgrade |
| `GET  /api/v1/nvidia/nims` | `internal/api/nvidia.go` | NVIDIA NIM index |
| `GET  /api/v1/nvidia/nims/{id}` | `internal/api/nvidia.go` | Single NIM |
| `GET  /api/v1/nvidia/nims/{id}/profiles` | `internal/api/nvidia.go` | NIM profile list |
| `POST /api/v1/nvidia/refresh` | `internal/api/nvidia.go` | Force NIM cache refresh |
| `GET  /api/v1/settings` | `internal/api/settings.go` | Read the Settings singleton |
| `PUT  /api/v1/settings` | `internal/api/settings.go` | Update Settings |
| `POST /api/v1/bundles/{namespace}/{name}/submit` | `internal/api/publish.go` | Bundle: Draft → Submitted |
| `POST /api/v1/bundles/{namespace}/{name}/approve` | `internal/api/publish.go` | Bundle: Submitted → Approved (mints Blueprint) |
| `POST /api/v1/bundles/{namespace}/{name}/request-changes` | `internal/api/publish.go` | Bundle: Submitted → ChangesRequested |
| `POST /api/v1/bundles/{namespace}/{name}/withdraw` | `internal/api/publish.go` | Bundle: any → Draft |

> The `/api/v1/bundles/*` endpoints are part of the publish-by-approval
> workflow. The Bundle concept is on a deprecation path — these endpoints
> still exist and still work, but new validation flows should center on
> `App → Blueprint → Workload` instead.

---

## Appendix C — Troubleshooting

### k3d loadbalancer container exits

The `k3d-aif-dev-serverlb` container occasionally exits with code 137 on its
own. `kubectl` then refuses with `connection refused`. Recover with:

```bash
k3d cluster start aif-dev
```

No need to recreate the cluster — the k3s server stays up across these
exits.

### `make run` says webhook works but `kubectl patch` succeeds

The Blueprint immutability webhook is **registered in-process** but the
`ValidatingWebhookConfiguration` resource ships in the Helm chart, not in
`charts/aif-operator/crds/`. Out-of-cluster operation cannot install it. To
exercise immutability end-to-end you must `helm install` the chart — see
§4.8.

### `go: ... requires Go 1.26` but my Go is 1.24

```bash
export GOTOOLCHAIN=auto
# or per-command:
GOTOOLCHAIN=auto go test ./...
```

The Makefile already prefixes targets that hit this with `GOTOOLCHAIN=auto`.

### `smoke-e2e` operator never reaches /healthz

Check the operator log: `tail -100 /tmp/aif-operator-smoke.log`. Common
causes:

- Port `:8080` already in use (kill the conflicting process or set
  `--addr=:9090` and update the curl recipes).
- The k3d cluster isn't actually ready when the operator starts —
  `kubectl --kubeconfig $(k3d kubeconfig write aif-dev-smoke) get nodes`
  should show one Ready node.

### `verify-all-live` reports one or more FAILs

The target now keeps going past failures and prints a summary at the end.
Re-run the individual `verify-<pkg>-live` to see its full output;
hard-failing targets print an `ERROR: set X and Y` message naming the env
vars they need.

### `bun install` fails on @rancher/shell

Fall back to yarn:

```bash
cd ui/ai-factory
rm -rf node_modules
yarn install
```

Webpack 5 + vue-cli-service have occasional rough edges under bun's
package layout. The Rancher shell build scripts themselves don't care
which package manager you used.
