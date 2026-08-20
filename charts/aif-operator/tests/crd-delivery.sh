#!/usr/bin/env bash
# Verifies CRD delivery: Helm's native crds/ path registers the CRDs on first
# install (so the chart's own Blueprint/Settings/InstallAIExtension resources
# resolve when Helm builds the release manifest), and the hook Job upgrades them
# thereafter under a dedicated field manager.
set -euo pipefail
CHART="$(cd "$(dirname "$0")/.." && pwd)"

# 1. The magic crds/ directory must exist and hold all 4 CRDs. Helm installs
#    these before it builds the release manifest; without them, a fresh
#    `helm install` fails with "no matches for kind Blueprint".
count=$(ls "$CHART"/crds/ai-factory.suse.com_*.yaml 2>/dev/null | wc -l | tr -d ' ')
[ "$count" = "4" ] || { echo "FAIL: expected 4 CRDs in crds/, found $count"; exit 1; }
if [ -d "$CHART/files/crds" ]; then
  echo "FAIL: stale files/crds/ directory — CRDs must live in crds/ only"; exit 1
fi

# Render the release manifest once; several checks below inspect it.
render=$(helm template rel "$CHART" --namespace aif-operator)

# 1b. The chart's own CRs must still render as release objects (they depend on
#     the native crds/ path having registered their kinds).
for kind in Blueprint Settings InstallAIExtension; do
  grep -Eq "^kind: ${kind}$" <<< "$render" \
    || { echo "FAIL: expected $kind CRs in the release manifest"; exit 1; }
done

# 2. CRDs must NOT be release-manifest objects. Files under crds/ are not
#    templated by Helm, so they never appear in `helm template` output; this
#    guards against someone re-adding them under templates/ (which would make
#    `helm uninstall` cascade-delete every custom resource).
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

echo "PASS: crds/ present for install-time registration; Job upgrades under a scoped field manager"
