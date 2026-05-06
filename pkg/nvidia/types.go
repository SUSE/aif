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

// Type classifies a NIM by inference modality. Driven by the chart-name
// heuristic in classifier.go (regex per ARCHITECTURE.md §13.1).
type Type string

const (
	TypeLLM Type = "llm" // language model (default classification)
	TypeVLM Type = "vlm" // vision-language model
)

// NIMEntry describes one NIM model available in the SUSE-mirrored catalog.
// Spec source: ARCHITECTURE.md §6.2 (NIM discovery + deployer interfaces).
type NIMEntry struct {
	// ID is the canonical model identifier. For SUSE-Registry-discovered NIMs
	// the format is "<chart>:<version>" (e.g. "nim-llm:1.3.0").
	ID string

	// Chart is the chart name within registry.suse.com/ai/charts/nvidia/
	// (e.g. "nim-llm", "nim-vlm").
	Chart string

	// Version is the chart's OCI tag (semver, no "v" prefix).
	Version string

	// DisplayName is the human-readable name for the UI. Defaults to Chart.
	DisplayName string

	// Type classifies the NIM (LLM / VLM). Set by the chart-name heuristic.
	Type Type

	// DefaultGPUs is the recommended GPU count for a baseline deployment.
	// Populated by the deployer (P4-4); zero from discovery alone.
	DefaultGPUs int32

	// DefaultModel is the baseline model variant when an entry covers
	// multiple. Populated by the deployer (P4-4); empty from discovery alone.
	DefaultModel string

	// ChartRef is the full OCI reference to the chart, e.g.
	// "oci://registry.suse.com/ai/charts/nvidia/nim-llm:1.3.0".
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
