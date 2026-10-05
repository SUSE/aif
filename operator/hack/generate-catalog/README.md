# generate-catalog

Regenerates `operator/internal/catalog/default-catalog.json` from the NGC catalog
search API. Generator-owned entries — `nvidia`-library entries marked
`"source": "ngc"` — are rebuilt from the current NGC data each run:

- NVAIE-supported charts carry the single `{code:"supported",name:"Supported"}` chip;
- an owned chart that loses `nvaie_supported` but is still published stays, without
  the chip, and gets it back if the designation returns;
- an owned chart NGC no longer publishes (or whose path is no longer deployable) is
  removed.

Hand-added entries (no `source`) and the entire `suse-ai` library are preserved
untouched. Run manually or via the weekly `refresh-catalog` CI workflow; commit the
result.

## Usage

    cd operator
    GOTOOLCHAIN=auto go run ./hack/generate-catalog

Flags:
- `-catalog` path to `default-catalog.json` (default `internal/catalog/default-catalog.json`).
- `-overrides` path to `catalog-overrides.json` (default `internal/catalog/catalog-overrides.json`).
- `-page-size` NGC search page size (default 100).
- `-report-dir` if set, write `report.json` and `summary.md` describing the run into
  this directory (the CI workflow uses them for the PR body and job summary).

## What it does

1. Fetches all HELM_CHART resources from the anonymous NGC search API (paginated),
   then checks the collected count against the `resultTotal` NGC reports. A
   truncated or empty/degraded response fails the run before anything is written,
   so a partial list can never remove entries.
2. Classifies each NVAIE-supported chart's NGC repo path via `catalog.ClassifyNGCPath`:
   - **excluded** paths (invalid Helm index) are skipped;
   - **unclassified** paths are skipped and reported — add them to
     `internal/catalog/ngc_repos.go` before they can be listed;
   - org/public/gated paths are kept and become owned entries with the Supported chip.
3. Derives NGC-only fields (name, slug, description, logo, last-updated, repo URL);
   the project/docs/source/changelog/reference URLs NGC does not provide are left empty.
4. Applies any pinned fields from `catalog-overrides.json` on top of the derived
   values (see Overrides below), then sets the chip from the NGC designation and the
   `source` marker.
5. Reconciles every other owned entry by its NGC resource key (repo path from
   `repository_url` plus `slug_name`): still published on a deployable path → kept,
   refreshed, without the chip; otherwise removed. A chart that moves to another
   repo counts as removed at the old location.
6. Preserves unowned entries, unless a supported chart takes the same slug
   (promotion). The `nvidia` array is sorted by `slug_name`, so re-runs with
   unchanged NGC data and overrides produce no diff.
7. Reports added, removed (with reason), delabeled and relabeled apps, and
   unclassified paths with their charts and versions — in the log, and in
   `report.json`/`summary.md` when `-report-dir` is set.

## Overrides

`internal/catalog/catalog-overrides.json` is the sole place manual curation of owned
entries lives. It maps a `slug_name` to a partial catalog entry — only the JSON keys
present are pinned:

    {
      "some-chart-slug": {
        "description": "A better, hand-written description.",
        "documentation_url": "https://docs.example.com/some-chart"
      }
    }

- Only the listed fields overwrite the NGC-derived values; every other field stays
  fresh from the search API. A slice-valued key (e.g. `labels`) is replaced
  wholesale, not merged — but the Supported chip always follows the NGC designation
  afterward, so an override can add other labels yet never add or strip the chip.
- `repository_url`, `slug_name` and `source` cannot be overridden: they tie an owned
  entry back to its NGC chart, so the tool rejects an overrides file that pins them.
- Pinning a field never affects removal: an override for a chart NGC no longer
  publishes does not resurrect it. Remove the stale override entry when convenient.
- The file is read only by this tool; the operator does not embed it. An absent or
  empty file (`{}`) means no overrides.

## Maintenance

- When a run reports an unclassified path, add it to the appropriate map in
  `ngc_repos.go` (org/public/gated/excluded) and re-run.
- The NGC response shape is pinned by `testdata/ngc_response.json`. If the live API
  changes, update that fixture and the `ngc*` structs in `transform.go` together.
- Curated blueprint entries use custom slugs that do not match NGC chart names; no
  blueprint chart is `nvaie_supported` today, so no duplicate is produced. If one
  becomes supported, add a skip guard so the curated entry wins.
- **Ownership is the `source` marker**, never the Supported chip. To hand-author an
  entry (supported or not), leave `source` unset: the generator never rebuilds or
  removes it, though a supported NGC chart with the same slug still promotes over
  it. Only set `"source": "ngc"` on an entry whose `repository_url` is an NGC Helm
  repo and whose `slug_name` is the NGC chart name — the run fails otherwise.
  `catalog.Normalize` clears `source`, so it is never served by the API.
