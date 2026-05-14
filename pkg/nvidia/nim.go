package nvidia

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
)

// §4.4 constants. Unexported — these are implementation details of the
// formula, not a public knob.
const (
	cpuPerGPU           = 8  // cores per GPU (LLM and VLM)
	memGiPerGPULLM      = 32 // GiB per GPU for LLM (memoryPerGPU_LLM = "32Gi")
	memGiPerGPUVLM      = 64 // GiB per GPU for VLM (memoryPerGPU_VLM = "64Gi")
	suseRegistryDefault = "registry.suse.com"
	nimImagePathPrefix  = "ai/containers/nvidia"
)

// deployerImpl is the production Deployer. P4-4 implements GenerateValues
// per ARCHITECTURE.md §4.4 sizing formulas; UpdateSettings receives the
// per-cluster registry endpoint pushed by SettingsReconciler (P5-7).
//
// mu guards settings per §8.2.1 sole-writer pattern (mirrors helm.engine).
// UpdateSettings is the SOLE writer; GenerateValues calls snapshot() once
// at entry and uses the returned struct for the rest of the call.
type deployerImpl struct {
	logger *slog.Logger

	mu       sync.RWMutex
	settings EngineSettings
}

// NewDeployer returns a Deployer bound to the given logger. Initial settings
// are zero-valued; the deployer will use in-code defaults until UpdateSettings
// is called by SettingsReconciler.
func NewDeployer(logger *slog.Logger) Deployer {
	return &deployerImpl{logger: logger}
}

// UpdateSettings replaces the current settings snapshot. Sole writer.
// Logs the resolved registry endpoint at Info so ops can confirm the
// SettingsReconciler push landed (mirrors helm.engine.UpdateSettings).
func (d *deployerImpl) UpdateSettings(s EngineSettings) {
	d.mu.Lock()
	d.settings = s
	d.mu.Unlock()

	d.logger.Info("nvidia deployer settings updated",
		slog.String("component", "nvidia.deployer"),
		slog.String("registry_endpoint", s.RegistryEndpoint))
}

// snapshot returns the current settings under a read lock. Callers MUST
// invoke this once at method entry and use the returned struct for the
// remainder of the call; never hold the lock across logic.
func (d *deployerImpl) snapshot() EngineSettings {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.settings
}

// GenerateValues produces the layer-4 Helm values block for a single NIM
// deployment per ARCHITECTURE.md §4.4. Pure once validation passes; never
// reads K8s or upstream. Returns a sentinel error on validation failure
// (see pkg/nvidia/errors.go) so HTTP handlers can translate to 400.
func (d *deployerImpl) GenerateValues(_ context.Context, req GenerateRequest) (map[string]any, error) {
	if req.Entry.Chart == "" {
		return nil, fmt.Errorf("validate Entry.Chart: %w", ErrInvalidRequest)
	}
	if req.Entry.Version == "" {
		return nil, fmt.Errorf("validate Entry.Version: %w", ErrInvalidRequest)
	}
	if req.Replicas <= 0 {
		return nil, fmt.Errorf("validate Replicas: %w", ErrInvalidReplicas)
	}
	if req.Entry.Type != TypeLLM && req.Entry.Type != TypeVLM {
		// Defensive: an unknown Type would silently fall through to LLM
		// memory sizing, hiding misconfiguration (e.g., a JSON caller
		// passing "vlm-llama" instead of "vlm" → a 32GiB pod intended
		// for a 64GiB workload → OOM at load time).
		return nil, fmt.Errorf("validate Entry.Type: %w", ErrInvalidRequest)
	}

	gpuCount, err := resolveGPUCount(req.GPUs, req.Entry.DefaultGPUs)
	if err != nil {
		return nil, err
	}

	memGi := memGiPerGPULLM
	if req.Entry.Type == TypeVLM {
		memGi = memGiPerGPUVLM
	}

	s := d.snapshot()
	registry := s.RegistryEndpoint
	if registry == "" {
		registry = suseRegistryDefault
	}

	return map[string]any{
		"replicas": req.Replicas,
		"image": map[string]any{
			"repository": fmt.Sprintf("%s/%s/%s", registry, nimImagePathPrefix, req.Entry.Chart),
			"tag":        req.Entry.Version,
		},
		"resources": map[string]any{
			// Two distinct buildResourceMap calls so caller mutations
			// on requests don't alias limits (Guaranteed QoS pairs the
			// two maps deep-equal but they MUST be separate objects).
			"requests": buildResourceMap(gpuCount, memGi),
			"limits":   buildResourceMap(gpuCount, memGi),
		},
		"tolerations": []any{
			map[string]any{"key": "nvidia.com/gpu", "operator": "Exists", "effect": "NoSchedule"},
		},
		"nodeSelector": map[string]any{"nvidia.com/gpu.present": "true"},
	}, nil
}

// resolveGPUCount applies §4.4's GPU resolution table:
//   - GPUs nil + DefaultGPUs > 0  → DefaultGPUs
//   - GPUs nil + DefaultGPUs == 0 → ErrMissingGPUCount
//   - GPUs *v == 0                → ErrInvalidGPUCount
//   - GPUs *v < 0                 → ErrInvalidGPUCount
//   - GPUs *v > 0                 → *v (overrides default)
func resolveGPUCount(gpus *int32, defaultGPUs int32) (int32, error) {
	if gpus == nil {
		if defaultGPUs <= 0 {
			return 0, fmt.Errorf("resolve GPU count: %w", ErrMissingGPUCount)
		}
		return defaultGPUs, nil
	}
	if *gpus <= 0 {
		return 0, fmt.Errorf("resolve GPU count: %w", ErrInvalidGPUCount)
	}
	return *gpus, nil
}

// buildResourceMap constructs the cpu/memory/nvidia.com/gpu trio used in
// both requests and limits (Guaranteed QoS). Called twice per GenerateValues
// so requests and limits are distinct map objects (caller mutations don't
// alias).
func buildResourceMap(gpuCount int32, memGi int) map[string]any {
	return map[string]any{
		"cpu":            strconv.Itoa(int(gpuCount) * cpuPerGPU),
		"memory":         fmt.Sprintf("%dGi", int(gpuCount)*memGi),
		"nvidia.com/gpu": strconv.Itoa(int(gpuCount)),
	}
}
