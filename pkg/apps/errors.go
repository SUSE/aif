package apps

import "errors"

// Sentinel errors returned by Catalog and its adapters. Consumers MUST
// classify with errors.Is — never with strings.Contains (per CLAUDE.md
// Forbidden patterns).

// ErrAppNotFound is returned by Catalog.Get when the requested ID is
// absent from the dispatched Source's cache.
var ErrAppNotFound = errors.New("apps: app not found")

// ErrUnknownSource is returned by Catalog.Get when the ID's namespace
// prefix does not match any registered Source's Name().
var ErrUnknownSource = errors.New("apps: unknown source prefix in id")
