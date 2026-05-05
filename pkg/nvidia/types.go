// Package nvidia owns NIM (NVIDIA Inference Microservices) discovery and
// Helm-values generation. It speaks ONLY to SUSE Registry — the upstream
// NVIDIA NGC is reached out-of-band by the SUSE-managed mirror process and
// is invisible to this package.
//
// Per CLAUDE.md's layering rule, this package MUST NOT import api/v1alpha1.
// Domain types defined here are independent of the K8s CRD shape; controllers
// that need to bridge are responsible for the translation.
package nvidia

import "time"

// NIMEntry describes one NIM model available in the SUSE-mirrored catalog.
// Spec source: ARCHITECTURE.md §6.2 (NIM discovery + deployer interfaces).
type NIMEntry struct {
	// ID is the canonical model identifier (e.g. "meta/llama-3.1-8b-instruct").
	ID string

	// DisplayName is the human-readable name for the UI.
	DisplayName string

	// Type categorises the NIM: "llm" | "vlm" | "embed" | other.
	Type string

	// DefaultGPUs is the recommended GPU count for a baseline deployment.
	DefaultGPUs int32

	// DefaultModel is the baseline model variant when an entry covers multiple.
	DefaultModel string

	// ChartRef is the OCI reference to the nim-llm / nim-vlm chart that
	// deploys this entry. Includes registry, repo, chart, version.
	ChartRef string
}

// GenerateRequest is the input to Deployer.GenerateValues. Sizing formulas
// per ARCHITECTURE.md §4.4 NIM Sizing land in plan task P4-4; for now this
// shape is the minimum needed for the port to be defined.
type GenerateRequest struct {
	// Entry identifies which NIM to deploy.
	Entry NIMEntry

	// Replicas is the desired pod count.
	Replicas int32

	// GPUs is the per-pod GPU count; 0 means "use Entry.DefaultGPUs".
	GPUs int32
}

// EngineSettings is the slice of cluster-wide Settings that this engine
// needs. Pushed by SettingsReconciler whenever Settings or its referenced
// Secrets change (lands with P5-4); the engine SHOULD NOT read Secrets or
// Settings CRs directly.
type EngineSettings struct {
	// RegistryEndpoint is the SUSE Registry hostname (default: registry.suse.com,
	// override via Settings.spec.registryEndpoints.suseRegistry for air-gap).
	RegistryEndpoint string

	// Username + Token authenticate against RegistryEndpoint.
	Username string
	Token    string

	// RefreshInterval is the cadence for Discovery.Refresh background runs
	// (default: 10m, override via Settings.spec.refreshInterval).
	RefreshInterval time.Duration
}
