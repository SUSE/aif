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

// ValidTypes returns the set of known NIM types for filter validation.
func ValidTypes() []Type {
	return []Type{TypeLLM, TypeVLM}
}

// NIMEntry describes one NIM model available in the SUSE-mirrored catalog.
// Spec source: ARCHITECTURE.md §6.2 (NIM discovery + deployer interfaces).
type NIMEntry struct {
	// ID is the canonical model identifier. For SUSE-Registry-discovered NIMs
	// the format is "<chart>:<version>" (e.g. "nim-llm:1.3.0").
	ID string `json:"id"`

	// Chart is the chart-name-equals-image-name identifier under the SUSE
	// Registry mirror layout. The same identifier appears as the path
	// component in BOTH the OCI Helm chart ref
	// (oci://{registry}/ai/charts/nvidia/{Chart}) AND the container image
	// repository (template `{registry}/ai/containers/nvidia/{Chart}` per
	// ARCHITECTURE.md §4.4 — what §4.4 calls `{model}` is this same
	// `Chart` string in this package). Examples: "nim-llm", "nim-vlm",
	// "nvidia/llama-3-70b" (slashes are sub-paths under both prefixes).
	Chart string `json:"chart"`

	// Version is the chart's OCI tag (semver, no "v" prefix).
	Version string `json:"version"`

	// DisplayName is the human-readable name for the UI. Defaults to Chart.
	DisplayName string `json:"displayName"`

	// Type classifies the NIM (LLM / VLM). Set by the chart-name heuristic.
	Type Type `json:"type"`

	// DefaultGPUs is the recommended GPU count for a baseline deployment.
	// Populated by the deployer (P4-4); zero from discovery alone.
	DefaultGPUs int32 `json:"defaultGpus"`

	// DefaultModel is the baseline model variant when an entry covers
	// multiple. Populated by the deployer (P4-4); empty from discovery alone.
	DefaultModel string `json:"defaultModel"`

	// ChartRef is the full OCI reference to the chart, e.g.
	// "oci://registry.suse.com/ai/charts/nvidia/nim-llm:1.3.0".
	ChartRef string `json:"chartRef"`
}

// GenerateRequest is the input to Deployer.GenerateValues. Sizing formulas
// live in ARCHITECTURE.md §4.4; the deployer translates this request into
// the layer-4 Helm values block (per §6.6).
type GenerateRequest struct {
	// Entry identifies which NIM to deploy.
	Entry NIMEntry

	// Replicas is the desired pod count. Must be > 0; 0 or negative is
	// rejected with ErrInvalidReplicas.
	Replicas int32

	// GPUs is the per-pod GPU count.
	//   nil   = unset; fall back to Entry.DefaultGPUs (rejects with
	//           ErrMissingGPUCount if Entry.DefaultGPUs is 0)
	//   >0    = use as-is (overrides Entry.DefaultGPUs)
	//   <=0   = reject with ErrInvalidGPUCount (NIMs are GPU-bound;
	//           zero is misconfiguration, not CPU fallback)
	GPUs *int32
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
