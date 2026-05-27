// Package suse_registry enumerates SUSE-published Helm charts on
// registry.suse.com at the ai/charts/ prefix, excluding the nvidia/
// subtree (owned by pkg/nvidia). It is the second feeder of the
// SUSE AI Library — alongside pkg/source_collection — flowing into
// pkg/apps via the apps.SUSERegistrySource adapter.
//
// Per CLAUDE.md layering: this package MUST NOT import api/v1alpha1.
package suse_registry

import "errors"

var (
	// ErrUnreachable: surfaces from the underlying OCI walker.
	ErrUnreachable = errors.New("suse_registry: registry unreachable")

	// ErrUnauthorized: 401/403 on either the catalog walk or a per-chart fetch.
	ErrUnauthorized = errors.New("suse_registry: registry unauthorized")

	// ErrNotFound: returned by Get for unknown name/version pairs;
	// also surfaces from walker 404s.
	ErrNotFound = errors.New("suse_registry: chart not found")

	// ErrNotConfigured: Refresh invoked before UpdateSettings supplied
	// a non-empty RegistryEndpoint.
	ErrNotConfigured = errors.New("suse_registry: provider not configured (call UpdateSettings first)")

	// ErrUnexpectedResponse: catch-all for non-2xx, non-401/403/404 status
	// or malformed bodies coming from the OCI walker.
	ErrUnexpectedResponse = errors.New("suse_registry: unexpected registry response")
)
