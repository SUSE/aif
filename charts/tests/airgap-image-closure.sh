#!/usr/bin/env bash
# Proves that one shared global values block redirects every workload image in
# combined and separate deployments, including Helm hook Jobs. It also checks
# compatibility with explicit nested aif-ui overrides and chart image metadata.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
CHARTS_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
OPERATOR_CHART="${CHARTS_DIR}/aif-operator"
UI_CHART="${CHARTS_DIR}/aif-ui"
AIRGAP_VALUES="${CHARTS_DIR}/values-airgap-images.example.yaml"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

for required_command in helm yq; do
  command -v "${required_command}" >/dev/null 2>&1 || fail "${required_command} is required"
done

PRIVATE_REGISTRY="$(yq -er '.global.imageRegistry // ""' "${AIRGAP_VALUES}")"
PULL_SECRET="$(yq -er '.global.imagePullSecrets[0].name // ""' "${AIRGAP_VALUES}")"
[[ -n "${PRIVATE_REGISTRY}" ]] || fail "global.imageRegistry must not be empty in ${AIRGAP_VALUES}"
[[ -n "${PULL_SECRET}" ]] || fail "global.imagePullSecrets[0].name must not be empty in ${AIRGAP_VALUES}"

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/aif-airgap-images.XXXXXX")"
cleanup() {
  if [[ -n "${TMP_ROOT:-}" && -d "${TMP_ROOT}" ]]; then
    rm -rf -- "${TMP_ROOT}"
  fi
}
trap cleanup EXIT

CHART_URL="oci://${PRIVATE_REGISTRY}/charts/aif-ui"

collect_workload_images() {
  local manifest="$1"
  yq ea -N -r '
    (select(.kind == "Pod") | .spec),
    (select(.kind == "CronJob") | .spec.jobTemplate.spec.template.spec),
    (select(
      .kind == "Deployment" or
      .kind == "StatefulSet" or
      .kind == "DaemonSet" or
      .kind == "ReplicaSet" or
      .kind == "ReplicationController" or
      .kind == "Job"
    ) | .spec.template.spec) |
    ((.containers // []) + (.initContainers // []) + (.ephemeralContainers // []))[] |
    select(tag == "!!map" and has("image")) |
    .image |
    select(tag == "!!str" and length > 0)
  ' "${manifest}"
}

assert_private_images() {
  local manifest="$1"
  local description="$2"
  local output
  local image
  local -a images=()

  output="$(collect_workload_images "${manifest}")" || fail "could not enumerate ${description} images"
  [[ -n "${output}" ]] || fail "${description} rendered no workload images"
  mapfile -t images <<< "${output}"

  for image in "${images[@]}"; do
    case "${image}" in
      "${PRIVATE_REGISTRY}"/*) ;;
      *) fail "${description} rendered non-allowlisted image: ${image}" ;;
    esac
  done
  printf 'PASS: %s rendered %d private workload image reference(s)\n' "${description}" "${#images[@]}"
}

assert_image_present() {
  local manifest="$1"
  local expected="$2"
  local description="$3"
  local output

  output="$(collect_workload_images "${manifest}")" || fail "could not enumerate ${description} images"
  while IFS= read -r image; do
    if [[ "${image}" == "${expected}" ]]; then
      return 0
    fi
  done <<< "${output}"
  fail "${description} did not render expected image ${expected}"
}

assert_pull_secret() {
  local manifest="$1"
  local description="$2"
  local results
  local result

  results="$(PULL_SECRET="${PULL_SECRET}" yq ea -N -r '
    (select(.kind == "Pod") | .spec),
    (select(.kind == "CronJob") | .spec.jobTemplate.spec.template.spec),
    (select(
      .kind == "Deployment" or
      .kind == "StatefulSet" or
      .kind == "DaemonSet" or
      .kind == "ReplicaSet" or
      .kind == "ReplicationController" or
      .kind == "Job"
    ) | .spec.template.spec) |
    [(.imagePullSecrets // [])[] | (.name // .)] |
    any_c(. == strenv(PULL_SECRET))
  ' "${manifest}")" || fail "could not inspect ${description} imagePullSecrets"
  [[ -n "${results}" ]] || fail "${description} rendered no Pod specifications"

  while IFS= read -r result; do
    [[ "${result}" == "true" ]] || fail "${description} has a Pod specification without pull Secret ${PULL_SECRET}"
  done <<< "${results}"
}

assert_equal() {
  local actual="$1"
  local expected="$2"
  local description="$3"
  [[ "${actual}" == "${expected}" ]] || fail "${description}: got '${actual}', expected '${expected}'"
}

annotation_has_image() {
  local chart="$1"
  local expected="$2"
  local annotation
  local line

  annotation="$(yq -er '.annotations."helm.sh/images"' "${chart}/Chart.yaml")"
  while IFS= read -r line; do
    if [[ "${line}" == "- image: ${expected}" ]]; then
      return 0
    fi
  done <<< "${annotation}"
  return 1
}

assert_annotation_covers_manifest() {
  local chart="$1"
  local manifest="$2"
  local description="$3"
  local output
  local image
  local -a images=()

  output="$(collect_workload_images "${manifest}" | LC_ALL=C sort -u)" || fail "could not enumerate ${description} metadata images"
  [[ -n "${output}" ]] || fail "${description} rendered no workload images for metadata validation"
  mapfile -t images <<< "${output}"
  for image in "${images[@]}"; do
    annotation_has_image "${chart}" "${image}" || fail "${description} helm.sh/images omits ${image}"
  done
}

printf '== Lint shared air-gap values ==\n'
helm lint "${OPERATOR_CHART}" \
  -f "${AIRGAP_VALUES}" \
  --set-string "aiExtension.source.helm.chartURL=${CHART_URL}"
helm lint "${UI_CHART}" -f "${AIRGAP_VALUES}" --set standalone=true

printf '== Render combined deployment ==\n'
helm template aif-operator "${OPERATOR_CHART}" \
  --namespace aif-operator \
  -f "${AIRGAP_VALUES}" \
  --set-string "aiExtension.source.helm.chartURL=${CHART_URL}" \
  > "${TMP_ROOT}/operator-combined.yaml"

yq ea -N 'select(.kind == "InstallAIExtension") | .spec.source.helm.values' \
  "${TMP_ROOT}/operator-combined.yaml" > "${TMP_ROOT}/managed-ui-values.yaml"

assert_equal \
  "$(yq -er '.global.imageRegistry' "${TMP_ROOT}/managed-ui-values.yaml")" \
  "${PRIVATE_REGISTRY}" \
  "combined install UI registry propagation"
assert_equal \
  "$(yq -er '.global.imagePullSecrets[0].name' "${TMP_ROOT}/managed-ui-values.yaml")" \
  "${PULL_SECRET}" \
  "combined install UI pull Secret propagation"

helm template aif-ui "${UI_CHART}" \
  --namespace cattle-ui-plugin-system \
  -f "${TMP_ROOT}/managed-ui-values.yaml" \
  > "${TMP_ROOT}/ui-managed.yaml"

printf '== Render separate deployment ==\n'
helm template aif-operator "${OPERATOR_CHART}" \
  --namespace aif-operator \
  -f "${AIRGAP_VALUES}" \
  --set aiExtension.enabled=false \
  > "${TMP_ROOT}/operator-separate.yaml"
helm template aif-ui "${UI_CHART}" \
  --namespace cattle-ui-plugin-system \
  -f "${AIRGAP_VALUES}" \
  --set standalone=true \
  > "${TMP_ROOT}/ui-standalone.yaml"

assert_private_images "${TMP_ROOT}/operator-combined.yaml" "combined operator chart"
assert_private_images "${TMP_ROOT}/ui-managed.yaml" "combined managed UI chart"
assert_private_images "${TMP_ROOT}/operator-separate.yaml" "separate operator chart"
assert_private_images "${TMP_ROOT}/ui-standalone.yaml" "standalone UI chart"
assert_pull_secret "${TMP_ROOT}/operator-combined.yaml" "combined operator chart"
assert_pull_secret "${TMP_ROOT}/ui-managed.yaml" "combined managed UI chart"
assert_pull_secret "${TMP_ROOT}/operator-separate.yaml" "separate operator chart"
assert_pull_secret "${TMP_ROOT}/ui-standalone.yaml" "standalone UI chart"

printf '== Prove the public-image guard fails closed ==\n'
PUBLIC_CANARY="docker.io/library/busybox:1.37"
cp "${TMP_ROOT}/operator-combined.yaml" "${TMP_ROOT}/operator-public-canary.yaml"
PUBLIC_CANARY="${PUBLIC_CANARY}" yq ea -i '
  (select(.kind == "Deployment" and .metadata.name == "aif-operator") |
    .spec.template.spec.containers[0].image) = strenv(PUBLIC_CANARY)
' "${TMP_ROOT}/operator-public-canary.yaml"
assert_image_present "${TMP_ROOT}/operator-public-canary.yaml" "${PUBLIC_CANARY}" "public-image rejection canary"
if GUARD_OUTPUT="$(assert_private_images "${TMP_ROOT}/operator-public-canary.yaml" "public-image rejection canary" 2>&1)"; then
  fail "image closure guard accepted public canary ${PUBLIC_CANARY}"
fi
case "${GUARD_OUTPUT}" in
  *"non-allowlisted image: ${PUBLIC_CANARY}"*) ;;
  *) fail "image closure guard failed for an unexpected reason: ${GUARD_OUTPUT}" ;;
esac
printf 'PASS: public image canary was rejected\n'

OPERATOR_REPOSITORY="$(yq -er '.manager.image.repository' "${OPERATOR_CHART}/values.yaml")"
OPERATOR_TAG="$(yq -r '.manager.image.tag // ""' "${OPERATOR_CHART}/values.yaml")"
if [[ -z "${OPERATOR_TAG}" ]]; then
  OPERATOR_TAG="$(yq -er '.appVersion' "${OPERATOR_CHART}/Chart.yaml")"
fi
UI_REPOSITORY="$(yq -er '.image.repository' "${UI_CHART}/values.yaml")"
UI_TAG="$(yq -r '.image.tag // ""' "${UI_CHART}/values.yaml")"
if [[ -z "${UI_TAG}" ]]; then
  UI_TAG="$(yq -er '.appVersion' "${UI_CHART}/Chart.yaml")"
fi
CLEANUP_REPOSITORY="$(yq -er '.aiExtension.cleanup.image.repository' "${OPERATOR_CHART}/values.yaml")"
CLEANUP_TAG="$(yq -er '.aiExtension.cleanup.image.tag' "${OPERATOR_CHART}/values.yaml")"
CRD_REPOSITORY="$(yq -er '.crds.image.repository' "${OPERATOR_CHART}/values.yaml")"
CRD_TAG="$(yq -er '.crds.image.tag' "${OPERATOR_CHART}/values.yaml")"

assert_image_present "${TMP_ROOT}/operator-combined.yaml" "${PRIVATE_REGISTRY}/${OPERATOR_REPOSITORY}:${OPERATOR_TAG}" "operator manager"
assert_image_present "${TMP_ROOT}/operator-combined.yaml" "${PRIVATE_REGISTRY}/${CLEANUP_REPOSITORY}:${CLEANUP_TAG}" "extension cleanup hook"
assert_image_present "${TMP_ROOT}/operator-combined.yaml" "${PRIVATE_REGISTRY}/${CRD_REPOSITORY}:${CRD_TAG}" "CRD apply hook"
assert_image_present "${TMP_ROOT}/ui-managed.yaml" "${PRIVATE_REGISTRY}/${UI_REPOSITORY}:${UI_TAG}" "managed UI"
assert_image_present "${TMP_ROOT}/ui-standalone.yaml" "${PRIVATE_REGISTRY}/${UI_REPOSITORY}:${UI_TAG}" "standalone UI"

MANAGED_UI_IMAGES="$(collect_workload_images "${TMP_ROOT}/ui-managed.yaml" | LC_ALL=C sort -u)"
STANDALONE_UI_IMAGES="$(collect_workload_images "${TMP_ROOT}/ui-standalone.yaml" | LC_ALL=C sort -u)"
assert_equal "${MANAGED_UI_IMAGES}" "${STANDALONE_UI_IMAGES}" "combined and standalone UI image equivalence"

printf '== Preserve explicit nested UI overrides ==\n'
helm template aif-operator "${OPERATOR_CHART}" \
  -f "${AIRGAP_VALUES}" \
  --set-string "aiExtension.source.helm.chartURL=${CHART_URL}" \
  --set-string aiExtension.source.helm.values.image.registry=ui.registry.example/team \
  > "${TMP_ROOT}/operator-legacy-override.yaml"
yq ea -N 'select(.kind == "InstallAIExtension") | .spec.source.helm.values' \
  "${TMP_ROOT}/operator-legacy-override.yaml" > "${TMP_ROOT}/legacy-ui-values.yaml"
assert_equal \
  "$(yq -er '.image.registry' "${TMP_ROOT}/legacy-ui-values.yaml")" \
  "ui.registry.example/team" \
  "legacy nested UI registry override"
assert_equal \
  "$(yq -r '.global | has("imageRegistry")' "${TMP_ROOT}/legacy-ui-values.yaml")" \
  "false" \
  "legacy nested UI registry must not be shadowed by a propagated global registry"
assert_equal \
  "$(yq -er '.global.imagePullSecrets[0].name' "${TMP_ROOT}/legacy-ui-values.yaml")" \
  "${PULL_SECRET}" \
  "pull Secret propagation alongside a legacy registry override"
helm template aif-ui "${UI_CHART}" \
  -f "${TMP_ROOT}/legacy-ui-values.yaml" \
  > "${TMP_ROOT}/ui-legacy-override.yaml"
assert_image_present \
  "${TMP_ROOT}/ui-legacy-override.yaml" \
  "ui.registry.example/team/${UI_REPOSITORY}:${UI_TAG}" \
  "legacy nested UI registry override"

yq -n '
  .aiExtension.source.helm.values.global.imageRegistry = "ui-global.example/team" |
  .aiExtension.source.helm.values.global.imagePullSecrets = []
' > "${TMP_ROOT}/explicit-ui-overrides.yaml"
helm template aif-operator "${OPERATOR_CHART}" \
  -f "${AIRGAP_VALUES}" \
  -f "${TMP_ROOT}/explicit-ui-overrides.yaml" \
  --set-string "aiExtension.source.helm.chartURL=${CHART_URL}" \
  > "${TMP_ROOT}/operator-global-override.yaml"
yq ea -N 'select(.kind == "InstallAIExtension") | .spec.source.helm.values' \
  "${TMP_ROOT}/operator-global-override.yaml" > "${TMP_ROOT}/global-ui-values.yaml"
assert_equal \
  "$(yq -er '.global.imageRegistry' "${TMP_ROOT}/global-ui-values.yaml")" \
  "ui-global.example/team" \
  "explicit nested global UI registry override"
assert_equal \
  "$(yq -er '.global.imagePullSecrets | length' "${TMP_ROOT}/global-ui-values.yaml")" \
  "0" \
  "explicit empty nested UI pull Secret list"
helm template aif-ui "${UI_CHART}" \
  -f "${TMP_ROOT}/global-ui-values.yaml" \
  > "${TMP_ROOT}/ui-global-override.yaml"
assert_image_present \
  "${TMP_ROOT}/ui-global-override.yaml" \
  "ui-global.example/team/${UI_REPOSITORY}:${UI_TAG}" \
  "explicit nested global UI registry override"

printf '== Validate chart image metadata ==\n'
helm template aif-operator "${OPERATOR_CHART}" > "${TMP_ROOT}/operator-default.yaml"
yq ea -N 'select(.kind == "InstallAIExtension") | .spec.source.helm.values' \
  "${TMP_ROOT}/operator-default.yaml" > "${TMP_ROOT}/default-ui-values.yaml"
helm template aif-ui "${UI_CHART}" \
  -f "${TMP_ROOT}/default-ui-values.yaml" \
  > "${TMP_ROOT}/ui-default.yaml"
assert_annotation_covers_manifest "${OPERATOR_CHART}" "${TMP_ROOT}/operator-default.yaml" "operator chart"
DEFAULT_UI_IMAGES="$(collect_workload_images "${TMP_ROOT}/ui-default.yaml" | LC_ALL=C sort -u)"
while IFS= read -r image; do
  annotation_has_image "${OPERATOR_CHART}" "${image}" || fail "operator chart helm.sh/images omits managed UI image ${image}"
done <<< "${DEFAULT_UI_IMAGES}"
assert_annotation_covers_manifest "${UI_CHART}" "${TMP_ROOT}/ui-default.yaml" "UI chart"

printf 'PASS: air-gap image closure, override compatibility, and metadata are valid\n'
