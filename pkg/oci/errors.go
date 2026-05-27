// Package oci provides a thin OCI Distribution v2 client (catalog walk,
// tag listing, manifest fetch with Bearer-token auth) for use by chart
// discovery packages. The package is unaware of higher-level concepts
// (NIMs, Helm charts beyond manifest layers, App.IDs); domain packages
// (pkg/nvidia, pkg/suse_registry) compose it.
//
// Per CLAUDE.md layering: this package MUST NOT import api/v1alpha1.
package oci

import "errors"

var (
	// ErrUnreachable: DNS/TLS/connection failures, network timeouts.
	ErrUnreachable = errors.New("oci: registry unreachable")

	// ErrUnauthorized: HTTP 401/403 from the registry or token realm.
	ErrUnauthorized = errors.New("oci: registry unauthorized")

	// ErrNotFound: HTTP 404 for manifests/blobs/tag lists.
	ErrNotFound = errors.New("oci: not found")

	// ErrUnexpectedResponse: non-2xx, non-401/403/404 or malformed body.
	ErrUnexpectedResponse = errors.New("oci: unexpected registry response")

	// ErrNotConfigured: Walker invoked before UpdateSettings supplied
	// a non-empty endpoint.
	ErrNotConfigured = errors.New("oci: walker not configured (call UpdateSettings first)")
)
