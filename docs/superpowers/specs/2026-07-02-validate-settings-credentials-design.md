# Design: Validate Settings Credentials

**Date:** 2026-07-02
**Status:** Approved (design)
**Scope:** On-demand credential validation for the AI Factory Settings page.

## Problem

Registry and Git credentials are configured once on the Settings page as references
to Kubernetes Secrets (`userSecretRef` / `tokenSecretRef` / `credSecretRef`). Today
there is no way to confirm those credentials actually authenticate — the first signal
that a credential is wrong is a failed deploy on a downstream cluster, which is slow
and hard to trace back to "the NGC token is stale."

This feature adds an on-demand **Test** action per credential section that verifies the
credential authenticates against its target, giving the user a fast, direct pass/fail.

## Scope

In scope (everything configured on the Settings page):

- **SUSE Registry** — Docker Registry v2 auth probe.
- **Application Collection (AppCo)** — Docker Registry v2 auth probe.
- **NVIDIA / NGC** — Docker Registry v2 auth probe (bearer-token flow).
- **Fleet GitOps git repo** — `ls-remote`-style auth check.

Explicitly out of scope:

- Model/provider API keys embedded inside blueprint Helm values — "valid" is
  chart-specific and cannot be checked generically.
- Pulling a specific image / resolving a specific tag — this is an **auth probe only**.
- Scheduled or background revalidation — validation is on-demand only.

## Key decisions

1. **Validation runs in the operator, not the browser.** Credentials live server-side
   as Secret references; the browser cannot resolve them and cannot reach registries
   directly (CORS, air-gap). A new operator endpoint performs the checks.
2. **Auth probe depth.** For registries we confirm the credentials *authenticate*
   (HTTP `200` from `/v2/`, following the bearer-token handshake). We do not assume any
   specific image exists.
3. **Test currently-selected refs (test-before-save).** The endpoint accepts the Secret
   refs currently chosen in the form; when a target's refs are omitted it falls back to
   the persisted Settings CR. This lets a user fix a bad selection before saving.
4. **One Test button per section, inline result.** The Settings page is an accordion;
   users open one section at a time. A per-section button matches that interaction,
   gives instant feedback on the credential being edited, and avoids a global button
   having to surface results for collapsed sections. The endpoint still supports a
   `targets` subset, so a "Test all" button can be added later at near-zero cost — it is
   deliberately left out of v1.
5. **Hand-rolled Registry v2 handshake.** No new Go dependency; ~150 lines of
   `net/http` covering the `401` → `Www-Authenticate: Bearer` → token → retry flow.
   Small and fully testable. (`go-containerregistry` was considered and rejected as a
   heavy dependency for a single feature.)

## Architecture

Four pieces:

1. **New operator endpoint** `POST /api/v1/settings/validate-credentials`, registered on
   the existing `SettingsHandler` (`operator/internal/api/settings.go`).
2. **New package `operator/internal/credcheck`** — the Docker Registry v2 auth probe.
3. **New method `CheckAuth` on `operator/internal/git.Client`** — an `ls-remote`-style
   auth check (no clone, no write).
4. **UI** — a `validateCredentials()` client in `ui/pkg/aif-ui/utils/operator-api.ts`
   plus a per-section **Test** button and inline result badge in
   `ui/pkg/aif-ui/pages/Settings.vue`.

### Endpoint contract

`POST /api/v1/settings/validate-credentials`

Request (all fields optional — this is what enables test-before-save):

```jsonc
{
  "targets": ["suseRegistry", "applicationCollection", "nvidia", "gitops"], // omit => all configured
  "overrides": {           // currently-selected form refs; omit a target => use saved Settings
    "suseRegistry":          { "userSecretRef": {"name": "...", "key": "..."}, "tokenSecretRef": {"name": "...", "key": "..."} },
    "applicationCollection": { "userSecretRef": {"name": "...", "key": "..."}, "tokenSecretRef": {"name": "...", "key": "..."} },
    "nvidia":                { "userSecretRef": {"name": "...", "key": "..."}, "tokenSecretRef": {"name": "...", "key": "..."} },
    "gitops":                { "credSecretRef": {"name": "...", "key": "..."}, "repoURL": "...", "branch": "..." }
  }
}
```

Response — always `200` unless the request itself is malformed; results are per-target:

```jsonc
{
  "results": [
    { "target": "suseRegistry",          "status": "ok",      "host": "registry.suse.com", "message": "authenticated", "latencyMs": 142 },
    { "target": "nvidia",                "status": "failed",  "host": "nvcr.io",           "message": "401 unauthorized" },
    { "target": "applicationCollection", "status": "skipped",                              "message": "not configured" },
    { "target": "gitops",                "status": "ok",                                   "message": "repository reachable" }
  ]
}
```

**Status enum:**

| status    | meaning                                                            |
|-----------|-------------------------------------------------------------------|
| `ok`      | credentials authenticated                                         |
| `failed`  | endpoint reached, credentials rejected (`401`/`403`)             |
| `error`   | could not reach the endpoint (DNS / dial / timeout / other code) |
| `skipped` | target not configured (no refs)                                   |

Splitting `failed` from `error` is deliberate: a network failure in an air-gapped
environment must not be reported as "your password is wrong."

### Registry probe (`internal/credcheck`)

`ProbeRegistry(ctx, host, user, pass) Result`:

1. `GET https://<host>/v2/` with HTTP Basic auth. `200` → `ok`.
2. On `401` with `Www-Authenticate: Bearer realm=…,service=…[,scope=…]`: GET the realm
   with those query params + Basic auth, parse `{"token"|"access_token": …}`, retry
   `/v2/` with `Authorization: Bearer <token>`. `200` → `ok`, else `failed`.
3. dial / DNS / timeout → `error`; other unexpected status codes → `error` with the code.

- ~10s context timeout.
- **Never echoes the password** into any result message.
- Host resolution **reuses the exact logic already in `getRegistryCredentials`**:
  AppCo and SUSE honor `RegistryEndpoints` overrides; NGC is always `nvcr.io`.

### Git auth check (`internal/git`)

`CheckAuth(ctx) error`: construct an in-memory remote for `repoURL` and call
`remote.ListContext(ctx, &gogit.ListOptions{Auth: c.auth})` — the go-git equivalent of
`git ls-remote`. `ErrEmptyRemoteRepository` is treated as `ok` (repo exists, is empty).
Auth errors vs network errors are classified into `failed` vs `error`. Built via the
existing `git.NewFromSettings` (or from override `repoURL` / `branch` / `credSecretRef`).

### UI

- `utils/operator-api.ts`: `validateCredentials(body): Promise<ValidateResult>` following
  the existing `operatorFetch` pattern.
- `pages/Settings.vue`: each credential section gets a **Test** button
  (`AsyncButton` is already imported) that validates that single target with the section's
  currently-selected refs and renders an inline result — ✓ with host + latency on `ok`,
  ✗ with message on `failed`/`error`, neutral on `skipped`. No new UI dependency.

## Error handling & edge cases

- Not configured (no refs) → `skipped`, not an error.
- Secret ref unresolvable (secret or key missing) → `failed` with a clear message
  (e.g. `secret "x" key "y" not found`).
- Timeout / unreachable → `error`.
- Partial results: one failing target does not fail the request; the endpoint returns
  `200` with mixed per-target results. Non-`200` only for a malformed request body.

## Security / RBAC

- No new RBAC permissions: the handler already reads operator-namespace Secrets via
  `readSecretKey`.
- The response returns only pass/fail + messages — **never** secret values, and error
  messages must never include the password.

## Known limitation

The probe tests **egress from the operator pod**. In air-gapped installs, NGC image
pulls go through a node-level registry proxy, so a control-plane probe to `nvcr.io` can
fail (`error`) even when real pulls succeed. The `error` (reachability) vs `failed`
(auth) split keeps this from being misread. This limitation is documented for users.

## Testing

- **Go — `credcheck`:** table tests against an `httptest.Server` simulating: `200` with
  basic auth; `401` → bearer → `200`; `401` bad credentials; `500`; timeout.
- **Go — `git.CheckAuth`:** mirror the existing `internal/git/client_test.go` remote
  setup; assert `ok` on a reachable authed repo, `ok` on empty repo, `failed` on bad
  auth, `error` on unreachable.
- **Go — handler:** fake client with seeded Secrets; assert per-target results, `skipped`
  when unconfigured, and override-vs-saved resolution.
- **UI:** unit test for the `validateCredentials` service function; button-wiring test in
  `Settings.vue` following existing patterns.
