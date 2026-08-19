#!/usr/bin/env bash
# Verifies CRDs are delivered ONLY via the hook Job's ConfigMap, never as
# release-manifest objects (which would let Helm's native SSA fight the Job).
set -euo pipefail
CHART="$(cd "$(dirname "$0")/.." && pwd)"

# 1. The magic crds/ directory must be gone; files/crds/ must hold all 4 CRDs.
if [ -d "$CHART/crds" ]; then
  echo "FAIL: chart still has a top-level crds/ directory (Helm would natively manage it)"; exit 1
fi
count=$(ls "$CHART"/files/crds/ai-factory.suse.com_*.yaml 2>/dev/null | wc -l | tr -d ' ')
[ "$count" = "4" ] || { echo "FAIL: expected 4 CRDs in files/crds/, found $count"; exit 1; }

# 2. Rendered release manifest must contain NO top-level CustomResourceDefinition
#    (top-level kinds start at column 0; the ConfigMap's embedded copies are indented).
render=$(helm template rel "$CHART" --namespace aif-operator)
if grep -Eq '^kind: CustomResourceDefinition' <<< "$render"; then
  echo "FAIL: a CustomResourceDefinition appears as a release object"; exit 1
fi

# 3. The pre-install/pre-upgrade CRD ConfigMap must carry all 4 CRDs as data keys.
for crd in aiworkloads blueprints installaiextensions settings; do
  grep -Eq "ai-factory.suse.com_${crd}\.yaml: \|" <<< "$render" \
    || { echo "FAIL: CRD $crd missing from the crd-apply ConfigMap"; exit 1; }
done

# 4. The Job must use the explicit field manager and force-conflicts.
grep -q -- '--field-manager=aif-operator-crds' <<< "$render" \
  || { echo "FAIL: crd-apply Job is not using --field-manager=aif-operator-crds"; exit 1; }
grep -q -- '--force-conflicts' <<< "$render" \
  || { echo "FAIL: crd-apply Job is not using --force-conflicts"; exit 1; }

# 5. RBAC is least-privilege: the mutating verbs (get/update/patch) must be
#    resourceNames-scoped to exactly this chart's CRDs (create/list stay cluster-wide).
rbac=$(helm template rel "$CHART" --namespace aif-operator --show-only templates/crds/crd-apply-rbac.yaml)
grep -q 'resourceNames:' <<< "$rbac" \
  || { echo "FAIL: crd-apply ClusterRole is not resourceNames-scoped"; exit 1; }
for crd in aiworkloads blueprints installaiextensions settings; do
  grep -Eq -- "^ +- ${crd}\.ai-factory\.suse\.com$" <<< "$rbac" \
    || { echo "FAIL: CRD $crd missing from ClusterRole resourceNames"; exit 1; }
done

# 6. crds.rbac.create=false suppresses the chart-managed RBAC (the whole rbac
#    template renders empty, so --show-only errors) while the Job still applies
#    CRDs using a pre-provisioned ServiceAccount.
if helm template rel "$CHART" --namespace aif-operator --set crds.rbac.create=false \
     --show-only templates/crds/crd-apply-rbac.yaml >/dev/null 2>&1; then
  echo "FAIL: crds.rbac.create=false still renders CRD-apply RBAC"; exit 1
fi
job=$(helm template rel "$CHART" --namespace aif-operator \
        --set crds.rbac.create=false --set crds.serviceAccountName=preprovisioned-sa \
        --show-only templates/crds/crd-apply-job.yaml)
grep -q 'serviceAccountName: preprovisioned-sa' <<< "$job" \
  || { echo "FAIL: Job does not honor crds.serviceAccountName override"; exit 1; }

echo "PASS: CRDs delivered single-manager via hook Job only; RBAC scoped and overridable"
