# Unreleased

## Registry and chart repository verification

AI Factory Settings now reports three separate results when you click **Test**:

- **Registry connection:** the configured registry responds. A successful login
  does not establish permission to read charts.
- **Chart access:** the operator tests a sample chart using the current form's
  endpoint, credentials and CA bundle. OCI sources are checked by reading a chart
  manifest; HTTPS Helm sources are checked by reading the index and checking a
  chart file's accessibility with a one-byte range request. The result names the
  chart and version tested.
- **Rancher repository:** the saved ClusterRepo has a downloaded index and current
  download status. Unsaved settings and reconciliation still in progress are
  identified separately.

For OCI sources, **Chart to test** can be changed to a chart present in your
repository. The default samples are `ollama` (Application Collection), `qdrant`
(SUSE Registry), and `aiq-aira` (a NVIDIA mirror). A missing sample is reported as
unavailable, not as proof of invalid credentials or a missing subscription.
Only the listed chart/version is checked; full chart downloads, other charts,
container images and cluster installation prerequisites are not verified.

An access denial from the public SUSE AI chart repository advises checking both
credentials and their SUSE AI subscription entitlement. Private mirror failures
advise checking mirror permissions and content. HTTP 401/403, missing content,
invalid indexes, TLS failures and timeouts have distinct explanations.

The Overview displays managed repository failures and links to Settings and the
native Rancher repository page. The Applications page keeps known catalog entries
for unavailable repositories visible, with installation disabled and a Settings
link. Healthy private mirrors continue to show only their discovered subset in
dynamic catalog mode. Direct wizard links also check repository readiness and
explain missing indexes instead of reporting `configmaps "" not found`.

### Repositories managed by AI Factory

AI Factory uses the stable ClusterRepo names `application-collection`,
`suse-ai-registry`, `nvidia`, and `nvidia-blueprints`. In connected mode, NVIDIA
team repositories from the bundled catalog are also created, including gated
repositories such as Run:ai. Public NVIDIA repositories are accessed anonymously;
classified gated repositories use the selected NGC credentials. An NGC key may
have access to some repositories and lack access to others.

An unavailable optional team repository does not establish that the NGC key is
invalid for every NVIDIA application. Review the individual source results.

With private endpoints configured, the same stable repository names point to
those mirrors. The test uses the configured path, credentials and private CA;
it does not fall back to the public source. NVIDIA mirror mode tests only the
mirror and preserves both NVIDIA aliases instead of testing public team sources.

**Test** is read-only. **Apply** saves form changes. **Refresh** explicitly asks
Rancher to retry a saved repository download; it cannot grant registry permissions.
After correcting credentials or mirror content, use Refresh and then Test again.
Deploy the matching operator and extension versions to enable direct chart checks.

## SUSE partner blueprint catalog

The chart now syncs the SUSE partner blueprint catalog by default. It renders a
Fleet `GitRepo`, `fleet-local/aif-partner-blueprints`, that tracks the `partners`
folder of `https://github.com/suse/partner-blueprints.git` on `main`. Partner
blueprints appear on the Blueprints page as Fleet syncs them.

- **Turning it off:** set `partnerBlueprints.enabled=false` (with
  `--reuse-values`). Fleet then removes the synced partner blueprints.
- **Air-gapped clusters:** the `GitRepo` reports clone errors until the catalog
  is disabled or pointed at an internal mirror with `partnerBlueprints.git.*`.
  Mirrors that need credentials or a private CA are added as an ordinary catalog
  instead. See the air-gap guide.
- **Existing catalogs:** if you already added a catalog on the Settings page
  that points at the partner repository, remove it after upgrading. Otherwise
  two `GitRepo`s sync the same blueprints and conflict.

The partner catalog is a chart resource, not a Settings page entry, so upgrading
the chart never touches catalogs added on the Settings page.

### Helm-declared catalogs are read-only

Catalogs declared in the chart's `blueprintCatalogs` value are now listed in the
`ai-factory.suse.com/helm-managed` annotation on the `Settings` resource. The
Settings page shows them with a **Managed by Helm** badge and no edit controls.
They are changed or removed only with `helm upgrade`.

- `PUT /api/v1/settings` returns HTTP 400 if a request changes a Helm-declared
  catalog. It returns HTTP 500 if the live `Settings` resource can't be read or
  its `helm-managed` annotation is malformed.
- A catalog added on the Settings page with the same name as a Helm-declared one
  but a different repository, branch or paths makes `helm upgrade` fail with a
  server-side apply conflict. Remove or rename it before upgrading.
- Keeping Settings-page catalogs next to Helm-declared ones needs server-side
  apply. Releases first installed with Helm 3, including ones upgraded through
  Rancher Apps, keep client-side apply until one `helm upgrade --server-side=true`.
  This only matters when `blueprintCatalogs` is not empty.
- After `helm uninstall` the annotation stays on the kept `Settings` resource.
  Remove it with
  `kubectl annotate settings.ai-factory.suse.com settings -n aif-operator ai-factory.suse.com/helm-managed-`
  to edit those catalogs on the Settings page again.

## Blueprint partner icons and Partner source

Blueprints accept two new optional `spec` values:

- **`icon`:** a partner logo, shown next to the source badge on the Blueprints
  page. It never replaces the badge. Only `https://` URLs and base64 raster
  `data:` URIs (png, gif, jpeg, webp) are accepted, up to 16 KB. Plain `http://`
  and SVG are rejected. The UI also skips icons that point at IP addresses or
  internal names (such as `*.svc`, `*.local` or `metadata.google.internal`), and
  hides the logo if it fails to load. Air-gapped installs should use `data:` URIs.
- **`source: Partner`:** for blueprints published through the partner catalog.
  Like every `source` value, it is declared by the blueprint author and is not
  verified by the operator.

Editing a blueprint keeps its icon and source. Copying a blueprint creates a
Custom blueprint without an icon.

Updating a blueprint with an invalid spec now returns HTTP 422 instead of 500.

**Compatibility:** both fields need the Blueprint CRD shipped with this release,
which the chart applies on upgrade. aif-operator 2.2.0 rejects `source: Partner`,
so catalog authors must not use it until the minimum supported operator version
includes this change.

## Community blueprint source

Blueprints accept **`source: Community`** for blueprints published through the
community catalog. It renders as a text badge on the Blueprints page and, like
every `source` value, is declared by the blueprint author and not verified by
the operator. `icon` works the same as for Partner blueprints.

**Compatibility:** needs the Blueprint CRD shipped with this release, which the
chart applies on upgrade. Older operators reject `source: Community`, so catalog
authors must not use it until the minimum supported operator version includes
this change.
