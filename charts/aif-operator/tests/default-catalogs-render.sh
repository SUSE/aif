#!/usr/bin/env bash
# Renders the chart with the toggle on/off and asserts the bundled Catalog CR
# is present (Helm-managed) only when enabled.
# yq is not assumed to be installed here, so this uses helm template + grep
# only, per the task-5 brief. Matches are anchored to the start of line
# (^kind: Catalog) so we assert on the top-level rendered resource, not the
# unrelated "kind: Catalog" text nested inside the CRD's spec.names.kind field
# (which always renders regardless of the toggle).
set -euo pipefail
CHART_DIR="$(cd "$(dirname "$0")/.." && pwd)"
fail=0

echo "== enabled=true =="
out_true="$(helm template "$CHART_DIR" --set defaultBlueprints.enabled=true)"
# Note: use here-strings (<<<), not `echo | grep -q`. With pipefail, grep -q's
# early exit on match SIGPIPEs the upstream echo on this large payload, which
# pipefail then reports as a pipeline failure even though grep matched.
grep -q "^kind: Catalog" <<< "$out_true" || { echo "FAIL: no bundled Catalog rendered"; fail=1; }
grep -q "  name: suse-default" <<< "$out_true" || { echo "FAIL: suse-default Catalog name missing"; fail=1; }
grep -q "app.kubernetes.io/managed-by: Helm" <<< "$out_true" || { echo "FAIL: managed-by=Helm label missing"; fail=1; }

echo "== enabled=false =="
out_false="$(helm template "$CHART_DIR" --set defaultBlueprints.enabled=false)"
grep -q "^kind: Catalog" <<< "$out_false" && { echo "FAIL: Catalog rendered while disabled"; fail=1; }

if [ "$fail" -eq 0 ]; then echo "PASS default-catalogs"; else echo "FAILED"; exit 1; fi
