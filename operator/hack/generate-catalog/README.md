# generate-catalog

Regenerates `operator/internal/catalog/default-catalog.json` from the NGC catalog
search API: adds every `nvaie_supported` Helm chart (under a deployable NGC repo
path) as a catalog entry with a single "Supported" chip, preserving all existing
entries. Run manually or via the weekly `refresh-catalog` CI workflow; commit the result.

## Usage

    cd operator
    GOTOOLCHAIN=auto go run ./hack/generate-catalog

Flags:
- `-catalog` path to `default-catalog.json` (default `internal/catalog/default-catalog.json`).
- `-page-size` NGC search page size (default 100).

## What it does

1. Fetches all HELM_CHART resources from the anonymous NGC search API (paginated).
2. Selects charts carrying `nvaie_supported`.
3. Classifies each chart's NGC repo path via `catalog.ClassifyNGCPath`:
   - **excluded** paths (invalid Helm index) are skipped;
   - **unclassified** paths are skipped with a warning — add them to
     `internal/catalog/ngc_repos.go` before they can be listed;
   - org/public/gated paths are kept.
4. Derives NGC-only fields (name, slug, description, logo, last-updated, repo URL);
   the project/docs/source/changelog/reference URLs NGC does not provide are left empty.
5. Merges into `default-catalog.json`: appends missing entries and stamps the single
   `{code:"supported",name:"Supported"}` chip on matched entries. Existing entries'
   other fields and the entire `suse-ai` library are never modified. The `nvidia`
   array is sorted by `slug_name`, so re-runs with unchanged NGC data produce no diff.

## Maintenance

- When the run warns about an unclassified path, add it to the appropriate map in
  `ngc_repos.go` (org/public/gated/excluded) and re-run.
- The NGC response shape is pinned by `testdata/ngc_response.json`. If the live API
  changes, update that fixture and the `ngc*` structs in `transform.go` together.
- Curated blueprint entries use custom slugs that do not match NGC chart names; no
  blueprint chart is `nvaie_supported` today, so no duplicate is produced. If one
  becomes supported, add a skip guard so the curated entry wins.
