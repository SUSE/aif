# Recommended air-gap Jira backlog

**Source spike:** SUSEAI-882

These issue drafts map one-to-one to the confirmed gaps in the air-gap
assessment. Keep the `AG-*` identifier in the Jira description even if the
final project key changes; it provides traceability from implementation and QA
evidence back to the spike.

Suggested common labels are `air-gap`, `aif`, and the affected component
(`operator`, `ui`, `helm`, `fleet`, `catalog`, `security`, or `docs`).

## Essential and release-blocking work

### AG-001 — Publish a verified AI Factory air-gap artifact manifest and mirroring workflow

**Suggested type/priority:** Story / P0

**Description**

Create a release-owned, versioned bill of materials and mirroring workflow for
AI Factory and selected applications. It must cover charts and dependencies,
container images (including hooks and init containers), supported
architectures, signatures/SBOMs, optional model artifacts, and required Rancher
system images. The workflow must preserve multi-architecture indexes, verify
destination digests, fail on missing artifacts, and emit matching RKE2/Rancher
registry configuration.

**Acceptance criteria**

- A manifest is published for every supported release/version tuple.
- Artifact resolution uses the actual customer values and records immutable
  chart versions plus image digests.
- Export and import fail closed; skipped or unresolved artifacts make the run
  fail.
- Imported charts and image indexes are verified against recorded source
  digests.
- The output includes checksums, tool/version metadata, RKE2 mirror entries,
  private-CA placement instructions, and default-endpoint-disablement settings.
- CI validates the manifest on amd64 and every other supported architecture.

**Dependencies:** Coordinate the NVIDIA entries with AG-004 and AG-006, and
consume workload offline contracts from AG-015 as those become available.

### AG-002 — Propagate private registry CA trust to Fleet Helm authentication secrets

**Suggested type/priority:** Story / P0

**Description**

Allow each configured private chart registry to reference a CA bundle Secret.
Resolve the selected key from the operator namespace and copy it as `cacerts`
alongside `username` and `password` in the operator-managed auth Secrets in
`cattle-system`, `fleet-local`, and `fleet-default`. Reconcile CA creation,
rotation, removal, and missing-key failures without requiring insecure TLS.

**Acceptance criteria**

- AppCo, SUSE Registry, and NVIDIA chart settings each accept an optional CA
  Secret/key reference.
- Generated Fleet auth Secrets contain the exact PEM bytes under `cacerts` and
  remain `kubernetes.io/basic-auth` Secrets.
- CA Secret changes enqueue Settings reconciliation and update every managed
  copy.
- Removing a CA reference removes stale `cacerts` data; unreadable explicit
  references produce a clear Settings failure and do not silently use another
  CA.
- Unit/integration tests cover HTTPS and OCI ClusterRepo plus Fleet HelmOp
  pulls against a private-CA registry, including CA rotation.
- Existing configurations without a CA reference remain unchanged.

**Dependencies:** None. This is the recommended first implementation issue.

### AG-003 — Support typed Git authentication and private trust for AIF and Fleet GitOps

**Suggested type/priority:** Story / P0

**Description**

Replace the ambiguous Fleet Git credential fields with a validated auth model
that AIF's Git client and generated Fleet `GitRepo` consume consistently.
Support HTTPS basic auth, HTTPS token auth with an explicit/default username,
and SSH key auth with known-hosts verification. Add private-CA trust for HTTPS
and write Fleet's supported `spec.caBundle` and client Secret fields.

**Acceptance criteria**

- Invalid or incomplete auth combinations are rejected with actionable field
  errors.
- AIF read/write operations and Fleet reconciliation both work with private-CA
  Gitea over HTTPS using basic and token auth.
- SSH uses a private key and verified known-hosts data; host-key checking is not
  disabled.
- Referenced token, CA, key, and known-hosts Secret rotation triggers
  reconciliation without an operator restart.
- The UI exposes only combinations implemented by both consumers.
- Existing token configurations have a documented, tested compatibility path.

**Dependencies:** Reuse this model in AG-012.

### AG-004 — Separate NVIDIA chart, image-registry, and API/model credentials

**Suggested type/priority:** Story / P0

**Description**

Redesign NVIDIA Settings so three independent trust domains can be configured:
the mirrored chart repository, the container image registry, and NGC/API/model
access. Do not reuse Harbor chart credentials as `ngc-api`, and do not assume an
NGC token authenticates a private chart mirror. Provide explicit endpoints,
credential Secret references, CA references where applicable, validation, UI
copy, and backward-compatible migration semantics.

**Acceptance criteria**

- Separate credential/endpoint groups exist for chart pull, image pull, and
  API/model access.
- Each generated Secret is derived only from its matching credential group.
- A private Harbor chart robot account can coexist with an NGC API token and a
  distinct image mirror account.
- Empty optional API/model credentials do not receive chart credentials as a
  fallback.
- Validation and status identify the exact incomplete trust domain.
- CRD, generated deepcopy code, API tests, UI forms, and migration docs are
  updated together.

**Dependencies:** Complete before AG-006 and the final NVIDIA portion of
AG-001.

### AG-005 — Preserve NVIDIA Blueprint ClusterRepo identity in mirrored mode

**Suggested type/priority:** Bug / P0

**Description**

Bundled NVIDIA Blueprints reference the logical ClusterRepo
`nvidia-blueprints`, but mirrored mode currently creates only `nvidia` and
deletes the Blueprint repo. Preserve stable logical repo identities when both
resolve to private mirrored content, or introduce a stable alias/rewrite that
is applied consistently to every bundled Blueprint.

**Acceptance criteria**

- Mirrored mode creates/resolves every ClusterRepo name referenced by bundled
  NVIDIA Blueprints.
- Switching connected to mirrored mode and back prunes only obsolete repos and
  leaves no public endpoint active in mirrored mode.
- Credentials and `cacerts` are attached to both logical repos when required.
- An automated contract test parses all bundled Blueprints and verifies their
  `chartRepo` values against Settings-produced repos in connected and mirrored
  modes.
- A bundled Blueprint reaches chart resolution in the no-GPU control-plane
  test; it may stop later at the documented GPU/runtime boundary.

**Dependencies:** Share the credential/trust model introduced by AG-002 and
AG-004.

### AG-006 — Normalize NVIDIA image endpoints before generating Docker credentials

**Suggested type/priority:** Bug / P0

**Description**

Use one shared resolver for NVIDIA image registry endpoints and Docker auth-map
keys. Chart OCI URLs must never be used as Docker registry keys. Normalize an
explicit image endpoint to a host (and optional port), reject paths/schemes that
cannot represent an image registry, and make NVIDIA-only and combined secret
generation follow the same contract.

**Acceptance criteria**

- Connected mode generates auth for `nvcr.io`.
- Mirrored mode generates auth for the configured image registry host, never
  for an `oci://host/path` chart URL.
- NVIDIA-only and combined pull-secret paths produce the same image host for
  the same Settings.
- Invalid image endpoints fail with a field-specific error and no malformed
  Secret is delivered.
- Contract tests cover default, hostname, hostname-with-port, HTTPS URL, and
  OCI chart URL inputs.

**Dependencies:** Requires the endpoint split from AG-004.

## Installation and deterministic behavior

### AG-007 — Bootstrap air-gap Settings during Helm installation without overwriting operators

**Suggested type/priority:** Story / P1

**Description**

Add Helm values that can initialize chart/image endpoints, Git configuration,
registry CA references, catalog behavior, and related Settings before the
operator performs its first reconciliation. Credentials must normally be
provided by existing Secret references. Define first-install and upgrade
ownership so Helm does not revert changes subsequently made through the UI/API.

**Acceptance criteria**

- A documented values file installs AIF with no initial public ClusterRepo
  reconciliation.
- Production credentials are referenced from Secrets and are not stored in
  Helm release values by default.
- Bootstrap is create-once or has an explicit opt-in ownership mode; normal
  Helm upgrades preserve UI/API edits.
- Missing referenced Secrets produce clear status without falling back to
  public endpoints.
- Combined and separate operator/UI installation tests cover fresh install and
  upgrade behavior.

**Dependencies:** Stabilize the Settings schema through AG-002–AG-004 first.

### AG-008 — Add a unified air-gap image override and reject public rendered images

**Suggested type/priority:** Story / P1

**Description**

Provide one documented image configuration that propagates to the operator,
nested or standalone UI, cleanup job, CRD job, and any chart hook/init
containers. Add render-time tests for an air-gap profile that enumerate all
workload images and fail if a public registry remains.

**Acceptance criteria**

- One values block can redirect every image shipped by both charts.
- Combined and separate installs render equivalent private image references.
- Cleanup, CRD, hook, and init-container images are covered.
- CI renders the air-gap profile and rejects any non-allow-listed public image.
- Existing per-component overrides remain compatible or have a documented
  migration.

**Dependencies:** Feed the final image list into AG-001.

### AG-009 — Make catalog metadata and assets disconnected-safe

**Suggested type/priority:** Story / P1

**Description**

Prevent the static catalog and browser UI from implying unavailable content or
performing automatic public asset requests. Bundle/proxy catalog imagery and
make mirrored discovery authoritative in air-gap mode. If internal remote
catalogs are supported, use an administrator-controlled destination allow-list
and private CA rather than disabling SSRF protections globally.

**Acceptance criteria**

- The Apps view makes no automatic public network request in air-gap mode.
- Logos required for supported entries are served from packaged/internal
  assets.
- Entries absent from the configured mirror are hidden or clearly marked
  unavailable.
- Internal catalog URLs require an explicit allow-list and optional CA Secret.
- SSRF tests continue to reject unapproved loopback, link-local, and private
  destinations.

**Dependencies:** Coordinate readiness reporting with AG-013.

### AG-010 — Implement or remove inactive offline settings and feature flags

**Suggested type/priority:** Task / P1

**Description**

Audit `applicationCollectionAPI`, `catalogDiscovery`, `imageRewrite`, and
`OFFLINE_MODE`. For each field or flag, either implement an end-to-end consumer
with visible behavior and tests or remove it from the public CRD/UI contract.
Avoid settings that imply air-gap protection without affecting runtime
behavior.

**Acceptance criteria**

- Every retained setting has a documented runtime consumer and tests from API
  persistence through observed behavior.
- Removed settings have upgrade/migration notes and no hidden UI controls.
- Image rewrite, if retained, uses a typed image-reference walker and is
  documented as complementary to node-level mirroring.
- No dead offline feature flag remains in production code.

**Dependencies:** Resolve before AG-013 declares readiness based on these
settings.

### AG-011 — Decide and enforce the Blueprint Helm deployment contract

**Suggested type/priority:** Spike followed by Story or API cleanup / P1

**Description**

Decide whether direct local Helm is a supported Blueprint strategy. If yes,
design and implement transactional multi-chart lifecycle, ordering, rollback,
status, and deletion. If no, remove Helm from Blueprint-specific API/UI choices
and the qualification requirement while retaining Helm for Apps where it is
supported.

**Acceptance criteria**

- A recorded architecture decision states the supported contract and rationale.
- CRD validation, UI choices, controller behavior, documentation, and QA matrix
  agree.
- If supported, automated lifecycle tests cover partial failure, rollback,
  upgrade, and uninstall.
- If unsupported, attempts are rejected early with an actionable message rather
  than silently ignored.

**Dependencies:** Decision can run independently; implementation size depends
on the chosen contract.

### AG-012 — Add private Git authentication and trust to Git-sourced extension installs

**Suggested type/priority:** Story / P1

**Description**

Extend Git-sourced `InstallAIExtension` to support authenticated private
repositories, HTTPS CA bundles, and verified SSH host keys. Reuse the same auth
and trust primitives as Fleet GitOps instead of introducing a second secret
format. If this is not supportable for the target release, explicitly validate
and document Helm-only disconnected extension installation.

**Acceptance criteria**

- Git extension sources can reference the supported typed auth/trust config.
- Private-CA HTTPS Gitea installation succeeds without insecure TLS.
- SSH installation verifies known hosts.
- Secret rotation requeues the extension and produces deterministic status.
- Unsupported combinations are rejected by API validation and explained in the
  UI/docs.

**Dependencies:** AG-003.

## Readiness, security, workload metadata, and documentation

### AG-013 — Add an AI Factory disconnected-readiness preflight API and UI

**Suggested type/priority:** Story / P1

**Description**

Provide a read-only preflight that explains whether the configured deployment
is ready for disconnected operation. Resolve chart, image, Git, catalog, and
runtime hosts; report CA/auth coverage; compare them with approved mirrors; and
run opt-in pull/connectivity probes where AIF has enough access. Do not report a
single green state from Settings alone when node mirror/fallback evidence is
unknown.

**Acceptance criteria**

- Results distinguish configured, verified, failed, and not-observable checks.
- Chart endpoints are clearly separated from image endpoints.
- Missing CA, credentials, artifacts, and default-endpoint-disablement each have
  actionable diagnostics.
- The API never returns credentials or sensitive Secret data.
- The UI shows the evidence timestamp and cannot label the system
  disconnected-ready while required checks are unknown/failed.
- Tests cover connected, partially mirrored, and deny-by-default profiles.

**Dependencies:** Consume the final semantics from AG-001–AG-004, AG-008, and
AG-010.

### AG-014 — Publish optional deny-by-default network policy and egress diagnostics

**Suggested type/priority:** Story / P1

**Description**

Ship supported, opt-in Kubernetes/Cilium policy examples that bound AIF, Fleet,
and workload namespaces to Kubernetes, Rancher, DNS, Harbor, Gitea, and declared
internal services. Treat the infrastructure firewall as the authoritative
air-gap boundary, while making policy denials distinguishable from missing
credentials/artifacts.

**Acceptance criteria**

- Policies are disabled by default and parameterize internal destinations.
- Enabling them preserves required control-plane and workload flows in the QA
  topology.
- Public egress probes fail from operator/Fleet/workload pods.
- Documentation explains DNS/IP/CNI limitations and the relationship to the
  perimeter firewall.
- Support diagnostics identify likely denied egress without exposing Secrets.

**Dependencies:** Align allow-list inputs with AG-013.

### AG-015 — Define machine-readable offline contracts for catalog workloads

**Suggested type/priority:** Story / P2

**Description**

Add per-application/Blueprint metadata describing all artifacts and runtime
network behavior needed offline: registries, charts, models/object artifacts,
licenses, initialization downloads, storage, architecture, and GPU
requirements. Use it to drive mirroring and to filter or warn in the catalog
when an air-gap profile is incomplete.

**Acceptance criteria**

- A versioned schema and validation exist for offline contract metadata.
- At least one SUSE, one NVIDIA, and the CPU smoke workload have complete
  contracts and tests.
- AG-001 tooling can consume the metadata without scraping arbitrary templates.
- The catalog reports missing artifacts/requirements before deployment.
- Runtime download requirements and license constraints are explicit.

**Dependencies:** AG-001 consumes this incrementally; catalog presentation
coordinates with AG-009.

### AG-016 — Document the supported end-to-end AI Factory air-gap journey

**Suggested type/priority:** Documentation task / P2 (release gate)

**Description**

Publish one tested journey covering prerequisites, artifact preparation and
transfer, Harbor/Gitea setup, RKE2/Rancher trust and mirror configuration,
combined and separate AIF installation, Settings, Helm/FleetBundle/GitOps
deployment, multi-cluster operation, verification, troubleshooting, upgrades,
and rollback. Generate command/value examples from maintained test fixtures
where possible.

**Acceptance criteria**

- The guide states the supported Rancher/RKE2/Fleet/AIF version matrix.
- Registry fallback, private CA trust domains, credential separation, browser
  isolation, and no-GPU test scope are explicit.
- Combined and separate installation paths are both exercised by QE.
- Every documented command is generated from or checked against maintained
  examples.
- Expected failures are removed only when linked implementation issues pass the
  air-gap test matrix.
- QE and documentation owners approve the evidence-backed procedure.

**Dependencies:** Finalize after the supported behavior above is implemented
and qualified; draft continuously from the existing documentation plan.

## Recommended delivery order

1. AG-002 (private registry CA propagation) and AG-005 (Blueprint repo
   identity) as small, independently reviewable fixes.
2. AG-004 followed by AG-006 (typed NVIDIA trust domains and image host
   normalization).
3. AG-003, then AG-012 (shared private Git auth/trust).
4. AG-001 and AG-008 in parallel once endpoint/image semantics stabilize.
5. AG-007, AG-009, AG-010, and AG-013 for a deterministic install and readiness
   journey.
6. AG-011 decision, AG-014 policy, AG-015 workload contracts, and AG-016 final
   qualification documentation.
