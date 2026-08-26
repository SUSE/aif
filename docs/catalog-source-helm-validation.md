# Validating the catalog source across Helm upgrades

The operator serves the application catalog from one of two sources, selected at
runtime by the `Settings` CR field `spec.appCatalog.remoteUrl` (set from the chart
value `appCatalog.remoteUrl`):

- **Bundled** (`remoteUrl == ""`): the operator serves the catalog embedded in its
  image (`operator/internal/catalog/default-catalog.json`).
- **Remote** (`remoteUrl` set): the operator fetches the URL (SSRF-filtered, http(s)
  only, 5 MiB cap) and **falls back to bundled** on any error or empty result.

This procedure validates switching between them across `helm upgrade`. Run against a
test cluster in the `aif-operator` namespace.

## 1. Install with the bundled catalog

    helm upgrade --install aif-operator ./charts/aif-operator \
      --namespace aif-operator --create-namespace \
      --set appCatalog.remoteUrl=""

Confirm the endpoint serves the embedded set (expect the bundled entries, including
the NVAIE "Supported" apps):

    kubectl -n aif-operator port-forward svc/aif-operator 8080:8080 &
    curl -s localhost:8080/api/v1/catalog | jq 'length'

## 2. Upgrade to a remote static catalog

    helm upgrade aif-operator ./charts/aif-operator \
      --namespace aif-operator \
      --set appCatalog.remoteUrl="https://<host>/default-catalog.json"

Confirm the `Settings` CR updated and the served catalog now reflects the remote
document:

    kubectl -n aif-operator get settings settings -o jsonpath='{.spec.appCatalog.remoteUrl}'; echo
    curl -s localhost:8080/api/v1/catalog | jq 'length'

Verify fallback: temporarily point `remoteUrl` at an unreachable URL and confirm the
endpoint still returns the bundled catalog (non-empty) rather than erroring.

## 3. Upgrade back to bundled

    helm upgrade aif-operator ./charts/aif-operator \
      --namespace aif-operator \
      --set appCatalog.remoteUrl=""

Confirm the served catalog is the embedded set again.

## Precedence gotcha (chart value vs UI Settings)

- The chart renders `spec.appCatalog.remoteUrl` into the `Settings` CR **only when
  the value is non-empty**. A `helm upgrade` **re-applies** a non-empty chart value
  every time (chart wins on upgrade).
- A `remoteUrl` set through the UI Settings page is **preserved** by Helm's
  three-way merge between upgrades (the chart only re-asserts
  `spec.appCatalog.remoteUrl` when its value is non-empty, so a UI-set value
  survives an upgrade that leaves the chart value empty; UI wins between upgrades).
- Net effect: if a value is set both in the chart and the UI, the chart value is
  reasserted on the next `helm upgrade`. This is the usual cause of "my catalog
  didn't change" confusion — check both places.

## Follow-up

An automated e2e test (install → upgrade → assert served catalog) is a planned
follow-up; this manual procedure is the current validation.
