#!/usr/bin/env bash
# Asserts each bundled blueprint file follows the operator's naming convention:
#   metadata.name        == slug(displayName) + "-" + version(dots->hyphens, +build stripped)
#   label blueprint-name == slug(displayName)
#   label blueprint-version == version
# and that metadata.name values are unique across files.
set -euo pipefail
DIR="$(cd "$(dirname "$0")/../files/blueprints" && pwd)"
fail=0
declare -A seen

slug() { echo "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'; }
namever() { echo "$1" | sed -E 's/\+.*$//; s/\./-/g'; }

shopt -s nullglob
for f in "$DIR"/*.yaml; do
  # The rendering template (fromYaml) reads only the first document in a file,
  # so a multi-document file would silently drop blueprints. Reject it here.
  docs="$(yq ea 'documentIndex' "$f" | tail -1)"
  [ "$docs" = "0" ] || { echo "FAIL $f: must contain exactly one YAML document (found $((docs + 1)))"; fail=1; continue; }

  dn="$(yq '.spec.displayName' "$f")"
  ver="$(yq '.spec.version' "$f")"
  name="$(yq '.metadata.name' "$f")"
  lname="$(yq '.metadata.labels."ai-factory.suse.com/blueprint-name"' "$f")"
  lver="$(yq '.metadata.labels."ai-factory.suse.com/blueprint-version"' "$f")"

  # Treat a missing field (yq prints "null") as a hard failure rather than
  # silently slugifying/comparing the literal string "null".
  miss=0
  for pair in "spec.displayName=$dn" "spec.version=$ver" "metadata.name=$name" \
              "blueprint-name label=$lname" "blueprint-version label=$lver"; do
    [ "${pair#*=}" != "null" ] || { echo "FAIL $f: missing ${pair%%=*}"; fail=1; miss=1; }
  done
  [ "$miss" -eq 0 ] || continue

  s="$(slug "$dn")"
  expname="${s}-$(namever "$ver")"

  [ "$name" = "$expname" ] || { echo "FAIL $f: metadata.name '$name' != expected '$expname'"; fail=1; }
  [ "$lname" = "$s" ]      || { echo "FAIL $f: blueprint-name label '$lname' != '$s'"; fail=1; }
  [ "$lver" = "$ver" ]     || { echo "FAIL $f: blueprint-version label '$lver' != '$ver'"; fail=1; }
  [ -z "${seen[$name]:-}" ] || { echo "FAIL $f: duplicate metadata.name '$name'"; fail=1; }
  seen[$name]=1
done

if [ "$fail" -eq 0 ]; then echo "PASS"; else echo "FAILED"; exit 1; fi
