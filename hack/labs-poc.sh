#!/usr/bin/env bash
# SUSE AI Factory Labs PoC: build everything from this branch, publish it to
# ttl.sh, and upgrade an EXISTING AIF install in the current kubectl context.
#
#   hack/labs-poc.sh                       # build, publish, deploy
#   DEPLOY=0 hack/labs-poc.sh              # build + publish only (prints the values)
#   LAB=aif-lab-my-lab hack/labs-poc.sh    # publish another lab (see docs/labs.md §2)
#
# What gets built (for the cluster's CPU architecture):
#   - aif-operator image                     (Lab + LabCatalog CRDs and controllers)
#   - aif-ui image + chart                   (Settings -> Labs)
#   - lab image + chart                      (default: Token Factory mock)
# Every run uses a unique version (<base>-labs.<timestamp>) so Rancher never
# serves a cached copy of an older build.
#
# ttl.sh is anonymous, public and ephemeral: anyone with the path can pull AND
# push, and everything expires after 24h. Rerun the script to republish. Mock
# content only, throwaway clusters only. Images are deployed pinned by digest, so
# a later push to the same tag is not picked up. Charts cannot be pinned yet
# (InstallAIExtension takes a version, not a digest).
#
# Requires: docker (with buildx/BuildKit; cross-arch needs QEMU, which Rancher
# Desktop and Docker Desktop include), helm >= 3.14, kubectl, yarn, jq, yq, node.
set -euo pipefail
export DOCKER_BUILDKIT=1

ROOT=$(cd "$(dirname "$0")/.." && pwd)
# Long random path: ttl.sh has no auth, so anyone who knows a path can push to it.
TTL_NS=${TTL_NS:-aif-labs-$(uuidgen | tr '[:upper:]' '[:lower:]' | tr -d -)}
REG=ttl.sh/${TTL_NS}
LAB=${LAB:-aif-lab-token-factory}   # pkg dir under ui/pkg and chart dir under charts/
LAB_NAME=${LAB#aif-lab-}           # Lab CR / catalog entry name
DEPLOY=${DEPLOY:-1}
STAMP=$(date -u +%Y%m%d%H%M)

log() { printf '\n\033[1;32m>> %s\033[0m\n' "$*"; }
die() { printf '\033[1;31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }

for c in docker helm kubectl yarn jq yq node uuidgen; do
  command -v "$c" >/dev/null || die "missing command: $c"
done
[[ -d "$ROOT/ui/pkg/$LAB" && -d "$ROOT/charts/$LAB" && -f "$ROOT/build/Dockerfile.lab-$LAB_NAME" ]] \
  || die "lab $LAB needs ui/pkg/$LAB, charts/$LAB and build/Dockerfile.lab-$LAB_NAME"

# Version = base version without pre-release + "-labs.<stamp>".
ver() { local v; v=$(jq -r .version "$1"); echo "${v%%-*}-labs.${STAMP}"; }
UI_VER=$(ver "$ROOT/ui/pkg/aif-ui/package.json")
LAB_VER=$(ver "$ROOT/ui/pkg/$LAB/package.json")
OP_TAG=labs.${STAMP}

if [[ "$DEPLOY" == 1 ]]; then
  CTX=$(kubectl config current-context)
  ARCH=$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
  NS=$(helm list -A -o json | jq -r '.[] | select(.chart | startswith("aif-operator")) | .namespace' | head -1)
  REL=$(helm list -A -o json | jq -r '.[] | select(.chart | startswith("aif-operator")) | .name' | head -1)
  [[ -n "$NS" ]] || die "no aif-operator Helm release found in context $CTX (install AIF first)"
else
  ARCH=${ARCH:-amd64}
fi

cat <<EOF

  kube context : ${CTX:-<not deploying>}
  aif-operator : ${REL:-?} in ${NS:-?}
  platform     : linux/$ARCH
  registry     : $REG   (public, expires in 24h)
  aif-ui       : $UI_VER
  lab          : $LAB $LAB_VER
EOF
if [[ "$DEPLOY" == 1 && -z "${YES:-}" ]]; then
  read -r -p "Continue? [y/N] " a; [[ "$a" == [yY]* ]] || exit 1
fi

TMPD=$(mktemp -d)
# Files temporarily changed for the build; restored on exit.
BACKUP=(ui/package.json ui/pkg/aif-ui/package.json "ui/pkg/$LAB/package.json" operator/internal/labs/catalog.json)
for f in "${BACKUP[@]}"; do mkdir -p "$TMPD/bak/$(dirname "$f")"; cp "$ROOT/$f" "$TMPD/bak/$f"; done
restore() {
  for f in "${BACKUP[@]}"; do cp "$TMPD/bak/$f" "$ROOT/$f"; done
  rm -rf "$TMPD"
}
trap restore EXIT

setver() { jq --arg v "$2" '.version = $v' "$ROOT/$1" > "$TMPD/v.json" && cp "$TMPD/v.json" "$ROOT/$1"; }

# Digest of a pushed image (the RepoDigest for this repository).
digest_of() {
  docker image inspect "$1" | jq -r --arg r "${1%:*}" '.[0].RepoDigests[] | select(startswith($r + "@")) | sub(".*@"; "")' | head -1
}

# Build one UI extension catalog image: $1 = pkg, $2 = version, $3 = Dockerfile, $4 = target image.
build_ext() {
  local pkg=$1 v=$2 dockerfile=$3 img=$4
  log "Building extension $pkg $v"
  setver ui/package.json "$v"            # publish-pkgs tags the image with the root version
  setver "ui/pkg/$pkg/package.json" "$v"
  (
    cd "$ROOT/ui"
    rm -rf dist-pkg assets charts extensions tmp index.yaml   # publish-pkgs artifacts (gitignored/untracked)
    yarn publish-pkgs -c -i '' -r local -o aif-labs "$pkg"
  )
  local base
  base=local/aif-labs/$(jq -r .name "$ROOT/ui/package.json"):$v
  docker image inspect "$base" >/dev/null || die "publish-pkgs did not produce $base (see docker images)"
  # The repo Dockerfiles take the base image for the target platform; the plugin
  # assets are arch-independent, so take them from the local (build-platform) image.
  # shellcheck disable=SC2016  # literal Dockerfile text, not shell expansion
  sed 's|^FROM \${BASE} AS app|FROM --platform=$BUILDPLATFORM ${BASE} AS app|' \
    "$ROOT/$dockerfile" > "$TMPD/Dockerfile"
  docker build --platform "linux/$ARCH" -f "$TMPD/Dockerfile" --build-arg BASE="$base" -t "$img" "$ROOT"
  docker push "$img"
  LAST_DIGEST=$(digest_of "$img")
  [[ "$LAST_DIGEST" == sha256:* ]] || die "no digest for $img"
}

# Package a chart with image values pointing at ttl.sh:
# $1 = chart dir, $2 = chart version, $3 = repository, $4 = image tag (tag@digest).
push_chart() {
  local chart=$1 v=$2 repo=$3 tag=$4
  rm -rf "${TMPD:?}/chart" && mkdir -p "$TMPD/chart" && cp -R "$ROOT/charts/$chart" "$TMPD/chart/"
  yq -i ".version = \"$v\" | .appVersion = \"$v\"" "$TMPD/chart/$chart/Chart.yaml"
  yq -i ".image.registry = \"ttl.sh\" | .image.repository = \"$repo\" | .image.tag = \"$tag\"" "$TMPD/chart/$chart/values.yaml"
  helm package "$TMPD/chart/$chart" -d "$TMPD/chart" >/dev/null
  helm push "$TMPD/chart/$chart-$v.tgz" "oci://$REG/chart"   # charts under /chart: must not share a ref with images
}

# --- 1. Extensions ------------------------------------------------------------
build_ext aif-ui "$UI_VER" build/Dockerfile.ui "$REG/aif-ui:$UI_VER"
UI_TAG="$UI_VER@$LAST_DIGEST"
push_chart aif-ui "$UI_VER" "$TTL_NS/aif-ui" "$UI_TAG"

build_ext "$LAB" "$LAB_VER" "build/Dockerfile.lab-$LAB_NAME" "$REG/$LAB:$LAB_VER"
push_chart "$LAB" "$LAB_VER" "$TTL_NS/$LAB" "$LAB_VER@$LAST_DIGEST"

# --- 2. Operator (built-in labs catalog points at this run's lab chart) -------
log "Building aif-operator"
jq --arg name "$LAB_NAME" --arg url "oci://$REG/chart/$LAB" --arg v "$LAB_VER" \
  'map(if .name == $name then .source.helm.chartURL = $url | .source.helm.version = $v | .extension.version = $v else . end)' \
  "$ROOT/operator/internal/labs/catalog.json" > "$TMPD/catalog.json"
cp "$TMPD/catalog.json" "$ROOT/operator/internal/labs/catalog.json"
docker build --platform "linux/$ARCH" -f "$ROOT/build/Dockerfile.operator" \
  --build-arg VERSION="labs-poc-$STAMP" -t "$REG/aif-operator:$OP_TAG" "$ROOT/operator"
docker push "$REG/aif-operator:$OP_TAG"
OP_DIGEST=$(digest_of "$REG/aif-operator:$OP_TAG")
[[ "$OP_DIGEST" == sha256:* ]] || die "no digest for aif-operator"

HELM_SETS=(
  --set-string manager.image.registry=ttl.sh
  --set-string manager.image.repository="$TTL_NS/aif-operator"
  --set-string manager.image.tag="$OP_TAG@$OP_DIGEST"
  --set-string manager.image.pullPolicy=IfNotPresent
  --set aiExtension.enabled=true
  --set-string aiExtension.source.kind=Helm
  --set-string aiExtension.source.helm.chartURL="oci://$REG/chart/aif-ui"
  --set-string aiExtension.source.helm.version="$UI_VER"
  --set-string aiExtension.source.helm.values.image.registry=ttl.sh
  --set-string aiExtension.source.helm.values.image.repository="$TTL_NS/aif-ui"
  --set-string aiExtension.source.helm.values.image.tag="$UI_TAG"
  --set-string aiExtension.extension.version="$UI_VER"
)

if [[ "$DEPLOY" != 1 ]]; then
  log "Published. To deploy, from this branch:"
  echo "# operator CRDs are applied by the chart's hook Job (crds.manageWithJob, default true)"
  [[ -d "$ROOT/charts/$LAB-crds/crds" ]] && echo "kubectl apply -f charts/$LAB-crds/crds/"
  echo "helm upgrade <release> charts/aif-operator -n <namespace> --reset-then-reuse-values ${HELM_SETS[*]}"
  exit 0
fi

# --- 3. Deploy ----------------------------------------------------------------
log "Deploying to $CTX"
VALS=$(helm get values "$REL" -n "$NS" -a -o json)
[[ -z "$(jq -r '.global.imageRegistry // ""' <<<"$VALS")" ]] \
  || echo "WARN: global.imageRegistry is set and overrides the ttl.sh image registry; unset it for this test." >&2
ALLOWED=$(jq -r '.manager.allowedRegistryHosts // [] | join(",")' <<<"$VALS")
[[ -z "$ALLOWED" || ",$ALLOWED," == *",ttl.sh,"* ]] \
  || echo "WARN: manager.allowedRegistryHosts=$ALLOWED does not include ttl.sh; chart pulls will be refused." >&2

PREV_REV=$(helm list -n "$NS" -o json | jq -r --arg r "$REL" '.[] | select(.name == $r) | .revision')

# The chart's pre-upgrade hook Job applies every CRD in charts/aif-operator/crds/
# (incl. labs and labcatalogs). Only apply them here if that Job is turned off.
if [[ "$(jq -r '.crds.manageWithJob // true' <<<"$VALS")" == "false" ]]; then
  kubectl apply --server-side --force-conflicts -f "$ROOT/charts/aif-operator/crds/"
fi
# Lab-owned CRDs are installed by the admin, not by aif-operator (no CRD RBAC).
if [[ -d "$ROOT/charts/$LAB-crds/crds" ]]; then kubectl apply -f "$ROOT/charts/$LAB-crds/crds/"; fi

helm upgrade "$REL" "$ROOT/charts/aif-operator" -n "$NS" --reset-then-reuse-values "${HELM_SETS[@]}"
kubectl -n "$NS" rollout status deploy -l app.kubernetes.io/name=aif-operator --timeout=300s

# Labs with spec.catalogRef follow the catalog. A Lab from an older PoC run has an
# inline source pointing at an expired chart: drop it so the toggle recreates it.
if [[ -n "$(kubectl get lab.ai-factory.suse.com "$LAB_NAME" --ignore-not-found -o jsonpath='{.spec.source}')" ]]; then
  kubectl delete lab.ai-factory.suse.com "$LAB_NAME" --wait=true
fi

log "Waiting for the new aif-ui extension (up to 5 min)"
kubectl wait installaiextension.ai-factory.suse.com/aif-ui \
  --for=jsonpath='{.status.phase}'=Installed --timeout=300s \
  || echo "WARN: aif-ui not Installed yet; check: kubectl describe aifext aif-ui" >&2

cat <<EOF

>> Done ($CTX). Artifacts under $REG expire in 24h; rerun to republish.

   1. If you developer-loaded aif-ui before, remove it (Extensions -> Installed), then reload the browser.
   2. SUSE AI Factory -> Settings -> Labs -> tick "Token Factory".
   3. Watch: kubectl get aiflab -w   (Pending -> Installing -> Ready), then reload the browser.
   4. SUSE AI Factory -> Labs -> Token Factory.

   Revert:
     kubectl delete aiflab --all --wait=true
     kubectl delete aiflabcat --all
     helm rollback $REL $PREV_REV -n $NS
   (The added CRDs stay; they are harmless.)
EOF
