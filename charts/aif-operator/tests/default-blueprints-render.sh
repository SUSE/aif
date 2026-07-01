#!/usr/bin/env bash
# Renders the chart with the toggle on/off and asserts the bundled Blueprint
# CRs are present (with the source=bundled label) only when enabled.
set -euo pipefail
CHART_DIR="$(cd "$(dirname "$0")/.." && pwd)"
fail=0

echo "== enabled=true =="
out_true="$(helm template t "$CHART_DIR" --set defaultBlueprints.enabled=true)"
# Collect into an array and take length so the count is robust to the "---"
# document separators yq emits between multiple selected documents.
bp_count="$(echo "$out_true" | yq ea '[select(.kind == "Blueprint")] | length')"
echo "Blueprint CRs rendered: $bp_count"
[ "$bp_count" -ge 1 ] || { echo "FAIL: expected >=1 Blueprint, got $bp_count"; fail=1; }

# Every rendered Blueprint must carry source: bundled.
bad="$(echo "$out_true" | yq ea '[select(.kind == "Blueprint" and .metadata.labels."ai-factory.suse.com/source" != "bundled")] | length')"
[ "$bad" -eq 0 ] || { echo "FAIL: $bad Blueprint(s) missing source=bundled label"; fail=1; }

echo "== enabled=false =="
out_false="$(helm template t "$CHART_DIR" --set defaultBlueprints.enabled=false)"
bp_count_false="$(echo "$out_false" | yq ea '[select(.kind == "Blueprint")] | length')"
echo "Blueprint CRs rendered: $bp_count_false"
[ "$bp_count_false" -eq 0 ] || { echo "FAIL: expected 0 Blueprint, got $bp_count_false"; fail=1; }

if [ "$fail" -eq 0 ]; then echo "PASS"; else echo "FAILED"; exit 1; fi
