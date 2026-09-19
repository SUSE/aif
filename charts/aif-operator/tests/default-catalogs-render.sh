#!/usr/bin/env bash
# The chart must NEVER render a Catalog CR (kind: Catalog) as a release resource.
# A Catalog CR in the Helm release would force the Catalog CRD to exist at
# manifest-map time, which breaks `helm upgrade` from a version that predates the
# CRD (Helm maps release resources before its pre-upgrade CRD-apply hook runs).
# Instead:
#   - bundled mode  -> only Blueprint CRs; the UI synthesizes the "SUSE Blueprints"
#                      group from the source=bundled label (no Catalog CR).
#   - git-sourced   -> the chart renders a Fleet GitRepo; Fleet syncs the repo and
#                      creates the Catalog CR at runtime (never in the Helm release).
# Matches are anchored (^kind: Catalog) so the CRD's nested spec.names.kind text
# does not count. yq is not assumed present, so this uses helm template + grep.
set -euo pipefail
CHART_DIR="$(cd "$(dirname "$0")/.." && pwd)"
fail=0

assert_no_catalog_cr() {
  local label="$1" out="$2"
  if grep -q "^kind: Catalog$" <<< "$out"; then
    echo "FAIL ($label): chart rendered a Catalog CR as a release resource — it must not"; fail=1
  fi
}

echo "== bundled (enabled=true, gitSourced=false) =="
out_bundled="$(helm template "$CHART_DIR" --set defaultBlueprints.enabled=true --set defaultBlueprints.gitSourced=false)"
assert_no_catalog_cr "bundled" "$out_bundled"
grep -q "^kind: Blueprint$" <<< "$out_bundled" || { echo "FAIL (bundled): expected bundled Blueprint CRs"; fail=1; }

echo "== git-sourced (enabled=true, gitSourced=true) =="
out_git="$(helm template "$CHART_DIR" --set defaultBlueprints.enabled=true --set defaultBlueprints.gitSourced=true)"
assert_no_catalog_cr "git-sourced" "$out_git"
grep -q "name: blueprint-catalog-suse-default" <<< "$out_git" || { echo "FAIL (git-sourced): expected the default Fleet GitRepo"; fail=1; }

echo "== disabled (enabled=false) =="
out_off="$(helm template "$CHART_DIR" --set defaultBlueprints.enabled=false)"
assert_no_catalog_cr "disabled" "$out_off"

if [ "$fail" -eq 0 ]; then echo "PASS default-catalogs"; else echo "FAILED"; exit 1; fi
