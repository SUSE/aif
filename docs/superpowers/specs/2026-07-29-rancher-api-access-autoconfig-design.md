# Rancher API Access auto-configuration

**Date:** 2026-07-29
**Status:** Design approved
**Related:** PR #156 (git-backed ClusterRepo support), `Settings.spec.rancherCatalog`

## Problem

Installing a chart from a git-backed `ClusterRepo` requires the operator to call
Rancher's Steve catalog API, because such repos expose no HTTP or OCI URL that
Fleet could pull from. That call needs a Rancher API token, a base URL and,
in practice, a CA bundle. Today all of it is entered by hand.

Two things are wrong with that.

**The CA field is a trap.** Rancher's serving certificate on the in-cluster
endpoint is signed by the CA in `cattle-system/tls-rancher-internal-ca`. The
certificate an administrator is most likely to reach for — the `cacerts`
Setting — is a *different* CA. On a live 2.13.1 cluster the two fingerprints are
`D4:D7:AE:61:BD:8B:25:7D:...` (correct) and `4C:E3:D6:25:DD:DB:CB:E4:...`
(`cacerts`). Following the obvious path produces an x509 failure whose nearest
apparent remedy is ticking `insecureSkipVerify`, which disables TLS verification
in production.

**The UI cannot complete the configuration.** Both credential fields render
`@shell/components/form/SecretSelector`, a Rancher core component that lists
existing Secrets in a namespace and cannot create one. An administrator must
therefore run `kubectl create secret` for the token — and again for the CA —
before the form is usable. The Settings page is a reference form, not a setup
form.

## Goals

- Remove the wrong-CA trap.
- Make the UI able to configure Rancher API access unaided.
- Make token expiry legible rather than a mysterious future failure.

Three constraints govern the solution:

1. It must work in air-gapped installations.
2. It must work when deploying AI workloads to downstream clusters.
3. It must work with **and** without the UI. Feature parity is not required,
   but each path must be usable on its own.

## Non-goals

- Making git-backed charts browsable under **Apps**. They remain deployable
  only via a Blueprint.
- Changing the Fleet Bundle size limit, which is orthogonal to how a chart is
  obtained.
- Eliminating the Rancher token. See *Alternatives considered*.

## Approach

Three independent changes.

| Piece | Where | Buys |
|---|---|---|
| CA auto-discovery | operator | kills the wrong-CA trap; headless drops from two fields to one |
| Authorize button | UI | UI stops requiring `kubectl` |
| Expiry surfacing | operator + UI | the day-90 failure is named, not mysterious |

The `Settings` CRD, the Helm values and the Go identifiers are unchanged. Every
existing field keeps working and keeps winning over discovery, so there is no
migration and no new operator RBAC — the operator's `ClusterRole` already grants
cluster-wide `secrets` get/list/watch.

The UI path spends the *logged-in user's* permissions, not the operator's: it
needs to mint a token and create a Secret in the operator namespace. Cluster
administrators have both. A user who has neither falls back to the manual path,
which is unchanged.

### Field-by-field rationale

**URL — no discovery.** The existing default
`https://rancher.cattle-system.svc` already works; dynamiclistener includes the
service name in the certificate's SANs, and this was exercised end to end on the
live cluster. The `internal-server-url` Setting resolves to a Service IP
(`https://10.43.110.71`), which is less stable than the DNS name across
reinstalls. Reading it would also depend on `management.cattle.io/settings`
access that the operator appears to have on the test cluster but that is granted
by no binding traceable to this chart — so it is not something to build on. The
field simply stops being displayed by default.

**CA — discovered.** See below.

**`insecureSkipVerify` — retained, hidden.** Once the CA resolves itself this
becomes vestigial, but it stays as an override for unusual topologies.

**Token — cannot be discovered.** Rancher mints a token only for the identity
making the request. A probe on the live cluster sent
`userPrincipal: local://user-xxxxx` and Rancher overwrote it with
`local://user-c4f4g` (login `admin`). A ServiceAccount therefore cannot mint one
for itself, and only a logged-in user can produce this credential.

## Component design

### A. CA resolver — operator

New file `operator/internal/infra/rancher/ca.go`:

```go
// DiscoverInternalCA returns the PEM that signed Rancher's in-cluster serving
// certificate, read from cattle-system/tls-rancher-internal-ca.
// Returns ErrCANotFound when the Secret or its tls.crt key is absent.
func DiscoverInternalCA(ctx context.Context, r client.Reader) ([]byte, error)
```

It takes a `client.Reader` rather than the reconciler, so it is testable against
a fake client with no Rancher present. It reads `tls.crt` **by key**. The same
Secret holds `tls.key`, the internal CA private key, which is sufficient to mint
certificates the cluster agents trust; it must never enter the operator's
memory. The operator can already read it — this is about not handling what we
do not need, not about a permission boundary.

`reconcileRancherCatalogClient` gains one resolution step:

| Condition | CA used | `caSource` |
|---|---|---|
| `caBundleSecretRef` set and readable | that Secret | `settings` |
| `caBundleSecretRef` unset | `DiscoverInternalCA` | `discovered` |
| unset and `ErrCANotFound` | none, system roots | `system` |

The resolved PEM is **not** written back into `Settings`. It is resolved at
client-build time, so a Rancher certificate rotation is picked up on the next
reconcile rather than baking a stale value into the CR. The existing
"Rancher catalog client configured" log line gains `caSource=` alongside the
current `customCA=`, so support can tell which path ran.

### B. Token minting — UI

New service `ui/pkg/aif-ui/services/rancher-token.ts`:

```
mintOperatorToken(store)
  → POST tokens.ext.cattle.io/v1 { ttl: 0, description: "AI Factory operator" }
    fallback: POST /v3/tokens        (Rancher older than 2.13)
  → { value, expiresAt, tokenName }

ensureTokenSecret(store, ns, name, value, expiresAt, tokenName)
  → create or update the Secret in the operator namespace

patchSettings(tokenSecretRef)
```

The request carries the current user's principal and lets Rancher overwrite it —
proven behaviour, so no impersonation logic is needed.

`ttl: 0` means "as long as Rancher permits", not "never". On the live cluster it
was clamped to `7776000000` ms, exactly the 90-day `auth-token-max-ttl-minutes`
default. The returned `status.expiresAt` is recorded rather than assumed.

Two annotations on the Secret:

- `ai-factory.suse.com/token-expires-at` — drives the UI warning with no extra
  API call.
- `ai-factory.suse.com/token-name` — lets a re-authorization delete the token it
  replaces, so re-minting every 90 days does not accumulate dead tokens.

Setting `auth-token-max-ttl-minutes` to lift the cap is explicitly **not**
recommended: asking an administrator to weaken a global security control to suit
this operator is the wrong trade.

### C. Expiry surfacing — operator and UI

`rancher.ErrUnauthorized` already exists and is already returned for 401/403.
The operator maps it to a distinct condition reason `RancherTokenRejected`, with
a message pointing at **Settings → Rancher API Access**, instead of folding it
into the generic fetch error.

The UI reads the expiry annotation and shows a banner when the token is past, or
within fourteen days of, `expiresAt`.

Expiry is deliberately **not** mirrored into `Settings.status`: the UI already
holds the annotation, so a second source of truth would buy nothing.

### UI layout

The section collapses to an **Authorize** button plus the current state — for
example "Authorized as `admin`, expires 27 Oct 2026". `url`,
`caBundleSecretRef` and `insecureSkipVerify` move behind an Advanced
disclosure and continue to win when set.

The same button re-authorizes. Pressing it mints a fresh token, overwrites the
Secret, and deletes the token named by the previous
`ai-factory.suse.com/token-name` annotation. It is therefore the remedy for both
initial setup and expiry, and repeated presses leave exactly one live token.

## Before and after

Headless, `values.yaml`:

```yaml
# before
rancherCatalog:
  url: "https://rancher.cattle-system.svc"
  token: "token-xxxxx:yyyyy"
  caBundle: |
    -----BEGIN CERTIFICATE-----
  insecureSkipVerify: false

# after
rancherCatalog:
  token: "token-xxxxx:yyyyy"   # or tokenSecret.name
```

| | Before | After |
|---|---|---|
| Headless fields to supply | token **and** CA | token |
| Wrong-CA trap | live | gone |
| UI can configure unaided | no — `kubectl` first | yes |
| UI controls shown | 4 | 1 button, plus Advanced |
| New RBAC | — | none |
| CRD and Helm keys | — | unchanged |

What does not improve: a Rancher token is still required and still expires after
90 days. Re-authorizing is one click and the failure is now named, but it
remains a recurring action.

## Error handling

| Case | Behaviour |
|---|---|
| `tls-rancher-internal-ca` absent | system roots, `caSource=system` — identical to today |
| `caBundleSecretRef` set but unreadable | log the error, use system roots; **do not** fall through to discovery |
| `tokens.ext.cattle.io` absent | UI falls back to `/v3/tokens` |
| user lacks permission to mint | surface Rancher's error and keep the manual instructions as an escape path |
| 401/403 at fetch time | `RancherTokenRejected` condition |
| past or near `expiresAt` | UI banner |

The second row is the one genuinely ambiguous case, so it is settled explicitly:
an administrator who pinned a CA gets a loud failure rather than a silent
substitution with a different certificate.

## Testing

**Go units.** CA resolution as a table: explicit ref wins; discovery used when
unset; `ErrCANotFound` yields a nil bundle; `tls.key` is never read. Separately,
`ErrUnauthorized` maps to the `RancherTokenRejected` reason.

**TypeScript units.** The mint service: the `ext.cattle.io` path, the `/v3`
fallback, and that a principal returned different from the one sent is accepted
rather than treated as an error.

**Live.** Re-run checks 1, 9 and 10 of the existing PR #156 validation matrix
(catalog client construction, token rotation without pod restart, Settings UI
round trip), plus one new check: clear `caBundleSecretRef` entirely and confirm
the client still builds with `customCA=true caSource=discovered`.

## Alternatives considered

### Token-free: clone the git repository directly

The operator would read `ClusterRepo.status.indexConfigMapName`, reassemble and
gunzip the chunked index, take the chart's relative path from its `urls` entry,
and clone the repository itself using `spec.clientSecret`. This removes the
Rancher credential entirely, which is the strongest possible answer to
constraint 3.

It was rejected on cost, not on principle:

- **It does not fit the operator we have.** The deployment runs one replica with
  a 128 Mi memory limit and `emptyDir` volumes only. Unpacking a 79 MB packfile
  does not happen inside that limit, so the memory floor would rise several-fold
  for every installation, including the majority that never use a git-backed
  repo.
- **`emptyDir` makes the clone recur.** It would be repeated on every upgrade,
  OOM, drain or reschedule — three repositories at 79 MB each on the test
  cluster. Making it genuinely one-time needs a PVC, which makes the operator
  stateful: RWO, no scale-out, and a StorageClass becomes an installation
  requirement where today there is none.
- **The size cannot be bounded.** `git.rancher.io` rejects partial clone
  (`filtering not recognized by server, ignoring`), so `--filter=blob:none`
  silently degrades to a full fetch. A `--depth 1 --no-checkout` clone measured
  79 MB in about 7 seconds; with a working tree, 830 MB. The equivalent Steve
  fetch for a single chart is roughly 12 KB.
- **It widens surface area and privilege.** We would own git over both
  `basic-auth` and `ssh-auth` including host-key verification, plus `caBundle`,
  `insecureSkipTLSVerify` and proxy parity — reimplementing what Rancher already
  does, with room to drift. Supporting a private chart repository means reading
  the git credentials Rancher holds, which is arguably *more* privilege than one
  scoped token, undercutting the main thing the approach was meant to buy.

One argument against it does **not** hold: `ClusterRepo.status.commit` exists
(`716fb0906d25151d8dfefc10a02dcf44a6a5335d` on the test cluster), so the clone
could be pinned to the same commit the index was built from. Time-of-check /
time-of-use drift is solvable. The approach remains viable if the operator ever
becomes stateful for other reasons.

### Discover the URL from `internal-server-url`

Rejected. The existing service-DNS default already works, the Setting resolves
to a less stable Service IP, and reading it depends on an access grant that
could not be traced to this chart.

## Evidence

All measurements are from a live Rancher v2.13.1 on RKE2 with Fleet.

| Claim | Evidence |
|---|---|
| `cacerts` is the wrong CA | fingerprints `4C:E3:D6:25:...` vs `D4:D7:AE:61:...` for `tls-rancher-internal-ca` |
| operator can already read the CA Secret | `kubectl auth can-i get secrets -n cattle-system --as=…:aif-operator` → yes, via the existing cluster-wide rule |
| tokens are minted for the requester | sent `local://user-xxxxx`, created as `local://user-c4f4g` / `admin` |
| `ttl: 0` is clamped, not unlimited | requested 0, stored `7776000000` ms; `auth-token-max-ttl-minutes` default `129600` |
| both token APIs present | `tokens.ext.cattle.io/v1` and `tokens.management.cattle.io/v3` |
| index gives chart paths | chunked ConfigMaps reassemble to 2,077,658 bytes / 50 charts, `urls: ['assets/…/….tgz']` |
| partial clone refused | `warning: filtering not recognized by server, ignoring` |
| clone sizes | 79 MB `--depth 1 --no-checkout` (~7 s); 830 MB with a checkout |
| operator resource envelope | 1 replica, `emptyDir` only, memory limit 128 Mi |

The probe token created for the third row was deleted immediately after
inspection.
