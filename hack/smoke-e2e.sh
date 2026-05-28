#!/usr/bin/env bash
# smoke-e2e.sh — always-fresh local end-to-end smoke for the AIF operator.
#
# Lifecycle:
#   1. Tear down any existing k3d cluster named $CLUSTER_NAME
#   2. Create a fresh k3d cluster
#   3. Install CRDs from charts/aif-operator/crds/
#   4. Generate self-signed webhook certs (the in-cluster webhook is NOT
#      registered when running out-of-cluster — see CLAUDE.md "Gotchas")
#   5. Build and start ./bin/aif-operator in the background
#   6. Poll http://localhost:8080/healthz until ready (or timeout)
#   7. Apply examples/{settings,blueprint,workload}-smoke.yaml
#   8. Wait for Blueprint phase=Active, then surface Workload phase
#   9. trap-on-EXIT cleanup: kill operator, delete k3d cluster
#
# Env vars (all optional):
#   SMOKE_CLUSTER_NAME  override the k3d cluster name (default: aif-dev-smoke)
#   SMOKE_KEEP=1        skip the cleanup (keep cluster + operator running)
#   SMOKE_TIMEOUT       seconds to wait for Blueprint phase=Active (default: 60)

set -euo pipefail

CLUSTER_NAME="${SMOKE_CLUSTER_NAME:-aif-dev-smoke}"
KEEP="${SMOKE_KEEP:-0}"
TIMEOUT="${SMOKE_TIMEOUT:-60}"

OPERATOR_PID_FILE="/tmp/aif-operator-smoke.pid"
OPERATOR_LOG="/tmp/aif-operator-smoke.log"
WEBHOOK_CERT_DIR="/tmp/k8s-webhook-server/serving-certs"

color() { printf '\033[1;36m%s\033[0m\n' "$*"; }
err()   { printf '\033[1;31m%s\033[0m\n' "$*" >&2; }

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    err "smoke-e2e: required tool '$1' not found in PATH"
    exit 127
  fi
}

require k3d
require kubectl
require go
require openssl
require curl

cleanup() {
  if [ "$KEEP" = "1" ]; then
    color ">>> SMOKE_KEEP=1 set — leaving cluster '$CLUSTER_NAME' and operator running"
    color "    operator log: $OPERATOR_LOG  (pid: $(cat "$OPERATOR_PID_FILE" 2>/dev/null || echo '?'))"
    color "    teardown:     k3d cluster delete $CLUSTER_NAME && kill \$(cat $OPERATOR_PID_FILE)"
    return
  fi
  color ">>> [smoke-e2e] cleanup"
  if [ -f "$OPERATOR_PID_FILE" ]; then
    pid=$(cat "$OPERATOR_PID_FILE")
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
    rm -f "$OPERATOR_PID_FILE"
  fi
  k3d cluster delete "$CLUSTER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

color ">>> [smoke-e2e] (1/9) tearing down any existing cluster '$CLUSTER_NAME'"
k3d cluster delete "$CLUSTER_NAME" >/dev/null 2>&1 || true

color ">>> [smoke-e2e] (2/9) creating fresh k3d cluster"
k3d cluster create "$CLUSTER_NAME" --k3s-arg "--disable=traefik@server:0"

export KUBECONFIG
KUBECONFIG="$(k3d kubeconfig write "$CLUSTER_NAME")"
color "    KUBECONFIG=$KUBECONFIG"

color ">>> [smoke-e2e] (3/9) installing CRDs"
kubectl apply -f charts/aif-operator/crds/
kubectl create namespace aif --dry-run=client -o yaml | kubectl apply -f -

color ">>> [smoke-e2e] (4/9) generating self-signed webhook certs"
mkdir -p "$WEBHOOK_CERT_DIR"
if [ ! -f "$WEBHOOK_CERT_DIR/tls.crt" ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -subj "/CN=aif-operator-webhook" \
    -keyout "$WEBHOOK_CERT_DIR/tls.key" \
    -out   "$WEBHOOK_CERT_DIR/tls.crt" 2>/dev/null
fi

color ">>> [smoke-e2e] (5/9) building operator"
GOTOOLCHAIN=auto go build -o bin/aif-operator ./cmd/operator

color ">>> [smoke-e2e] (6/9) starting operator in background"
: >"$OPERATOR_LOG"
./bin/aif-operator >"$OPERATOR_LOG" 2>&1 &
echo $! >"$OPERATOR_PID_FILE"

# Poll the REST /healthz on :8080. Controller-runtime exposes its own
# /healthz on :8081 but the REST router's one is the load-bearing signal
# that the route chain is wired up.
ready=0
for i in $(seq 1 30); do
  if curl -fsS http://localhost:8080/healthz >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" != "1" ]; then
  err "operator did not become ready in 30s"
  err "last 30 lines of operator log:"
  tail -30 "$OPERATOR_LOG" >&2
  exit 1
fi
color "    operator ready on :8080"

color ">>> [smoke-e2e] (7/9) applying example CRs"
kubectl apply -f examples/settings-smoke.yaml
kubectl apply -f examples/blueprint-smoke.yaml
kubectl apply -f examples/workload-smoke.yaml

color ">>> [smoke-e2e] (8/9) waiting up to ${TIMEOUT}s for Blueprint phase=Active"
if ! kubectl wait \
       --for=jsonpath='{.status.phase}'=Active \
       blueprint/smoke-blueprint.0.1.0 \
       --timeout="${TIMEOUT}s"; then
  err "Blueprint did not reach phase=Active in ${TIMEOUT}s"
  kubectl describe blueprint smoke-blueprint.0.1.0 >&2 || true
  err "operator log tail:"
  tail -40 "$OPERATOR_LOG" >&2
  exit 1
fi

# Workload must reach phase=Pending — with targetClusters=[] the
# WorkloadReconciler validates the spec but doesn't create a Fleet Bundle,
# so Pending is the steady state. If the controller is dead (e.g. cache
# sync timed out on a missing CRD), .status.phase stays unset and this
# wait fails loudly — that's exactly the signal a controller-down regression
# needs. Override via SMOKE_TIMEOUT (shared with the Blueprint wait above).
if ! kubectl wait \
       --for=jsonpath='{.status.phase}'=Pending \
       workload/smoke -n default \
       --timeout="${TIMEOUT}s"; then
  err "Workload did not reach phase=Pending in ${TIMEOUT}s"
  kubectl describe workload smoke -n default >&2 || true
  err "operator log tail:"
  tail -40 "$OPERATOR_LOG" >&2
  exit 1
fi
workload_phase="$(kubectl get workload -n default smoke -o jsonpath='{.status.phase}')"

color ">>> [smoke-e2e] (9/9) summary"
echo
kubectl get blueprints,workloads,settings -A
echo
echo "Workload smoke phase: $workload_phase"
echo
echo "Operator log tail:"
tail -20 "$OPERATOR_LOG"
echo
color ">>> [smoke-e2e] PASS"
