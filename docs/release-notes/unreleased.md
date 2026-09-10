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
