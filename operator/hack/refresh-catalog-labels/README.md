# refresh-catalog-labels

Regenerates the `labels` arrays in `operator/internal/catalog/default-catalog.json`
from the NGC catalog search API. Run manually and commit the result.

## Usage

    cd operator
    GOTOOLCHAIN=auto go run ./hack/refresh-catalog-labels

Flags:
- `-catalog` path to `default-catalog.json` (default `internal/catalog/default-catalog.json`).
- `-page-size` NGC search page size (default 100).

## What it does

Fetches all public NVIDIA Helm charts from the NGC search API (a GET with a URL-encoded
`q` query, paginated), reads each resource's `labels[]`, selects the program/support labels,
and writes them onto matching catalog entries. A catalog entry matches an NGC resource when
`<path of repository_url>/<slug_name> == resourceId` — so a curator adding an NVIDIA entry
must set `slug_name` to the NGC chart name and `repository_url` to the chart's NGC repo.

Entries with no NGC program labels (e.g. the blueprint charts) are logged and left without
a `labels` field — this is expected, not an error.

## Selection & display names

Selection is automatic (`isProgramCode` in transform.go), so **new NVIDIA programs appear
without a code change**: a label is surfaced when it is in the NGC `productNames` label group
(curated products/subscriptions) or its code ends in `_supported`. Everything else
(`pltfm_*`, `soln_*`, `uscs_*`, `indus_*`, `NSPECT-*`) is treated as noise and skipped.

`programDisplayNames()` (allowlist.go) is only a display-name prettifier, not a gate. Each
label's text resolves in this order: the curated map → the API's resolved value (for
`_supported` codes NGC returns a nice name) → a humanized code (e.g. `nv-ai-enterprise` →
"Nv Ai Enterprise"). Any code that falls back to a humanized name is **logged** at the end of
the run, so you can add a nicer entry to `programDisplayNames()`.

`hiddenPrograms()` (allowlist.go) is an optional denylist for codes NGC exposes but you don't
want on the tiles (internal / developer-only / early-access entitlements).

## Maintenance

- New programs surface on their own. To improve a label's text, add its code + display name to
  `programDisplayNames()` (the run logs codes that need one). To hide a code, add it to
  `hiddenPrograms()`.
- The NGC response shape is pinned by `testdata/ngc_response.json` (captured 2026-07-16). If
  the live API changes, update that fixture and the `ngc*` structs in `transform.go` together.
- The script normalizes `default-catalog.json` formatting (2-space indent, struct field
  order, libraries sorted). The first run may produce a formatting-only diff; re-runs are
  stable.
- The script supports only the library-keyed catalog shape (`{"nvidia":[...], "suse-ai":[...]}`)
  used by `default-catalog.json`. A flat array or `{"items":[...]}` catalog is not handled by
  the write-back.
