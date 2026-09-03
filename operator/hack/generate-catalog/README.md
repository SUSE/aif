# generate-catalog

Regenerates `operator/internal/catalog/default-catalog.json` from the NGC catalog
search API. Every generator-owned entry — an `nvidia`-library entry carrying the
single `{code:"supported",name:"Supported"}` chip — is rebuilt from the current
NGC data each run, so changed fields refresh and charts NGC no longer lists are
removed. Hand-added entries (no Supported chip) and the entire `suse-ai` library
are preserved untouched. Run manually or via the weekly `refresh-catalog` CI
workflow; commit the result.

## Usage

    cd operator
    GOTOOLCHAIN=auto go run ./hack/generate-catalog

Flags:
- `-catalog` path to `default-catalog.json` (default `internal/catalog/default-catalog.json`).
- `-overrides` path to `catalog-overrides.json` (default `internal/catalog/catalog-overrides.json`).
- `-page-size` NGC search page size (default 100).

## What it does

1. Fetches all HELM_CHART resources from the anonymous NGC search API (paginated),
   then checks the collected count against the `resultTotal` NGC reports. A
   truncated or empty/degraded response fails the run rather than silently
   regenerating a stripped catalog (reconcile-on-run would otherwise drop every
   owned entry).
2. Selects charts carrying `nvaie_supported`.
3. Classifies each chart's NGC repo path via `catalog.ClassifyNGCPath`:
   - **excluded** paths (invalid Helm index) are skipped;
   - **unclassified** paths are skipped with a warning — add them to
     `internal/catalog/ngc_repos.go` before they can be listed;
   - org/public/gated paths are kept.
4. Derives NGC-only fields (name, slug, description, logo, last-updated, repo URL);
   the project/docs/source/changelog/reference URLs NGC does not provide are left empty.
5. Applies any pinned fields from `catalog-overrides.json` on top of the derived
   values (see Overrides below).
6. Syncs `default-catalog.json`:
   - generator-owned entries are discarded and rebuilt from the derived set, so
     every NGC-derivable field refreshes and an owned chart NGC no longer lists is
     removed;
   - a hand-added (unowned) entry is preserved, unless NGC now reports that chart as
     supported, in which case the derived entry takes over the slug (promotion);
   - the `nvidia` array is sorted by `slug_name`, so re-runs with unchanged NGC data
     and overrides produce no diff.

## Overrides

`internal/catalog/catalog-overrides.json` is the sole place manual curation lives,
so `default-catalog.json` can stay fully machine-owned. It maps a `slug_name` to a
partial catalog entry — only the JSON keys present are pinned:

    {
      "some-chart-slug": {
        "description": "A better, hand-written description.",
        "documentation_url": "https://docs.example.com/some-chart"
      }
    }

- Only the listed fields overwrite the NGC-derived values; every other field stays
  fresh from the search API. A slice-valued key (e.g. `labels`) is replaced
  wholesale, not merged — but the mandatory Supported chip is always re-asserted
  afterward, so an override can add labels yet never strip it.
- Pinning a field never affects removal: an override for a chart NGC no longer lists
  does not resurrect it. Remove the stale override entry when convenient.
- The file is read only by this tool; the operator does not embed it. An absent or
  empty file (`{}`) means no overrides.

## Maintenance

- When the run warns about an unclassified path, add it to the appropriate map in
  `ngc_repos.go` (org/public/gated/excluded) and re-run.
- The NGC response shape is pinned by `testdata/ngc_response.json`. If the live API
  changes, update that fixture and the `ngc*` structs in `transform.go` together.
- Curated blueprint entries use custom slugs that do not match NGC chart names; no
  blueprint chart is `nvaie_supported` today, so no duplicate is produced. If one
  becomes supported, add a skip guard so the curated entry wins.
- **Ownership is inferred from the Supported chip**: any `nvidia` entry carrying it
  is treated as machine-owned and will be rebuilt from — or dropped if absent from —
  the NGC-derived set. To hand-author a *supported* `nvidia` entry the anonymous
  search API does not surface, add its `slug_name` to `preservedNVIDIASlugs` in
  `merge.go`; a preserved slug keeps its chip and is never rebuilt or dropped (a
  derived NGC entry can still promote over it). Do **not** hand-add a Supported chip
  to an `nvidia` entry without also listing it there — it will be deleted on the next
  run.
