package nvidia

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// newTestDeployer builds a deployerImpl with a discard logger. Settings
// default to zero (RegistryEndpoint=""), so image.repository falls back to
// the in-code suseRegistryDefault. Tests that need an override call
// d.UpdateSettings(...) directly.
func newTestDeployer(t *testing.T) *deployerImpl {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &deployerImpl{logger: logger}
}

func ptrInt32(v int32) *int32 { return &v }

// §4.4 worked example — Llama 8B (LLM, 1 GPU baseline).
func TestGenerateValues_LLM_8B_1GPU(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry: NIMEntry{
			Chart:   "nim-llm",
			Version: "1.3.0",
			Type:    TypeLLM,
		},
		Replicas: 1,
		GPUs:     ptrInt32(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantImage := map[string]any{
		"repository": "registry.suse.com/ai/containers/nvidia/nim-llm",
		"tag":        "1.3.0",
	}
	if !reflect.DeepEqual(out["image"], wantImage) {
		t.Errorf("image: got %v, want %v", out["image"], wantImage)
	}

	wantResources := map[string]any{
		"requests": map[string]any{"cpu": "8", "memory": "32Gi", "nvidia.com/gpu": "1"},
		"limits":   map[string]any{"cpu": "8", "memory": "32Gi", "nvidia.com/gpu": "1"},
	}
	if !reflect.DeepEqual(out["resources"], wantResources) {
		t.Errorf("resources: got %v, want %v", out["resources"], wantResources)
	}
}

// §4.4 worked example — Llama 70B (LLM, 8 GPU baseline).
func TestGenerateValues_LLM_70B_8GPU(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry: NIMEntry{
			Chart:   "nim-llm",
			Version: "1.3.0",
			Type:    TypeLLM,
		},
		Replicas: 1,
		GPUs:     ptrInt32(8),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantResources := map[string]any{
		"requests": map[string]any{"cpu": "64", "memory": "256Gi", "nvidia.com/gpu": "8"},
		"limits":   map[string]any{"cpu": "64", "memory": "256Gi", "nvidia.com/gpu": "8"},
	}
	if !reflect.DeepEqual(out["resources"], wantResources) {
		t.Errorf("resources: got %v, want %v", out["resources"], wantResources)
	}
}

// §4.4 — VLM-typed entry uses memoryPerGPU_VLM (64Gi).
func TestGenerateValues_VLM_2GPU(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry: NIMEntry{
			Chart:   "nim-vlm",
			Version: "1.0.0",
			Type:    TypeVLM,
		},
		Replicas: 1,
		GPUs:     ptrInt32(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantResources := map[string]any{
		"requests": map[string]any{"cpu": "16", "memory": "128Gi", "nvidia.com/gpu": "2"},
		"limits":   map[string]any{"cpu": "16", "memory": "128Gi", "nvidia.com/gpu": "2"},
	}
	if !reflect.DeepEqual(out["resources"], wantResources) {
		t.Errorf("resources: got %v, want %v", out["resources"], wantResources)
	}
}

func TestGenerateValues_GPUExplicitPositive_Used(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM, DefaultGPUs: 2},
		Replicas: 1,
		GPUs:     ptrInt32(4),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out["resources"].(map[string]any)["requests"].(map[string]any)
	if res["nvidia.com/gpu"] != "4" {
		t.Errorf("expected explicit GPUs=4 to win over DefaultGPUs=2, got %v", res["nvidia.com/gpu"])
	}
}

func TestGenerateValues_GPUExplicitZero_Rejected(t *testing.T) {
	d := newTestDeployer(t)
	_, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(0),
	})
	if !errors.Is(err, ErrInvalidGPUCount) {
		t.Fatalf("expected ErrInvalidGPUCount, got %v", err)
	}
}

func TestGenerateValues_GPUExplicitNegative_Rejected(t *testing.T) {
	d := newTestDeployer(t)
	_, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(-1),
	})
	if !errors.Is(err, ErrInvalidGPUCount) {
		t.Fatalf("expected ErrInvalidGPUCount, got %v", err)
	}
}

func TestGenerateValues_GPUNilWithDefault_UsesDefault(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM, DefaultGPUs: 2},
		Replicas: 1,
		GPUs:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out["resources"].(map[string]any)["requests"].(map[string]any)
	if res["nvidia.com/gpu"] != "2" {
		t.Errorf("expected fallback to DefaultGPUs=2, got %v", res["nvidia.com/gpu"])
	}
}

func TestGenerateValues_GPUNilNoDefault_Rejected(t *testing.T) {
	d := newTestDeployer(t)
	_, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM, DefaultGPUs: 0},
		Replicas: 1,
		GPUs:     nil,
	})
	if !errors.Is(err, ErrMissingGPUCount) {
		t.Fatalf("expected ErrMissingGPUCount, got %v", err)
	}
}

// §4.4: gpuCount > maxGPUs-on-largest-node is the SCHEDULER's call, not ours.
// We generate values without complaining; Kubernetes surfaces Unschedulable.
func TestGenerateValues_GPUExceedsMaxNode_GeneratesAnyway(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(99),
	})
	if err != nil {
		t.Fatalf("expected no error for large gpuCount (scheduler decides), got %v", err)
	}
	res := out["resources"].(map[string]any)["requests"].(map[string]any)
	if res["nvidia.com/gpu"] != "99" {
		t.Errorf("expected nvidia.com/gpu=99, got %v", res["nvidia.com/gpu"])
	}
}

func TestGenerateValues_EmptyChart_Rejected(t *testing.T) {
	d := newTestDeployer(t)
	_, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(1),
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateValues_EmptyVersion_Rejected(t *testing.T) {
	d := newTestDeployer(t)
	_, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(1),
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateValues_ZeroReplicas_Rejected(t *testing.T) {
	d := newTestDeployer(t)
	_, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 0,
		GPUs:     ptrInt32(1),
	})
	if !errors.Is(err, ErrInvalidReplicas) {
		t.Fatalf("expected ErrInvalidReplicas, got %v", err)
	}
}

func TestGenerateValues_NegativeReplicas_Rejected(t *testing.T) {
	d := newTestDeployer(t)
	_, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: -1,
		GPUs:     ptrInt32(1),
	})
	if !errors.Is(err, ErrInvalidReplicas) {
		t.Fatalf("expected ErrInvalidReplicas, got %v", err)
	}
}

// Entry.Type must be one of the documented constants. Empty or unknown
// values are rejected to prevent silent fall-through to LLM memory sizing
// (e.g., a JSON caller passing "vlm-llama" intending VLM → 32GiB pod →
// OOM under load).
func TestGenerateValues_UnknownType_Rejected(t *testing.T) {
	cases := map[string]Type{
		"empty":   "",
		"unknown": "vlm-llama",
	}
	for name, typ := range cases {
		t.Run(name, func(t *testing.T) {
			d := newTestDeployer(t)
			_, err := d.GenerateValues(context.Background(), GenerateRequest{
				Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: typ},
				Replicas: 1,
				GPUs:     ptrInt32(1),
			})
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest for Type=%q, got %v", typ, err)
			}
		})
	}
}

// Default registry path: never called UpdateSettings → image.repository
// starts with "registry.suse.com/" (the in-code default per §4.5).
func TestGenerateValues_DefaultRegistry_WhenSettingsEmpty(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo := out["image"].(map[string]any)["repository"].(string)
	if !strings.HasPrefix(repo, "registry.suse.com/") {
		t.Errorf("expected default registry, got %q", repo)
	}
}

// Override path: UpdateSettings(EngineSettings{RegistryEndpoint: ...}) is
// reflected on the next GenerateValues call.
func TestGenerateValues_OverridesRegistry_WhenSettingsSet(t *testing.T) {
	d := newTestDeployer(t)
	d.UpdateSettings(EngineSettings{RegistryEndpoint: "harbor.example.com"})
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo := out["image"].(map[string]any)["repository"].(string)
	if !strings.HasPrefix(repo, "harbor.example.com/") {
		t.Errorf("expected overridden registry, got %q", repo)
	}
}

// §4.4: model identifier is passed through as-is; slashes treated as
// sub-paths under containers/nvidia/.
func TestGenerateValues_ModelWithSlash_TreatedAsSubpath(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nvidia/llama-3-70b", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(8),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo := out["image"].(map[string]any)["repository"].(string)
	want := "registry.suse.com/ai/containers/nvidia/nvidia/llama-3-70b"
	if repo != want {
		t.Errorf("got %q, want %q", repo, want)
	}
}

// §4.4: requests == limits exactly (Guaranteed QoS).
func TestGenerateValues_GuaranteedQoS_RequestsEqualLimits(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(4),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := out["resources"].(map[string]any)
	if !reflect.DeepEqual(res["requests"], res["limits"]) {
		t.Errorf("requests must deep-equal limits (Guaranteed QoS):\n  requests: %v\n  limits:   %v", res["requests"], res["limits"])
	}
}

// §4.4: the toleration block + nodeSelector are emitted unchanged for every
// valid request.
func TestGenerateValues_AlwaysEmitsTolerationsAndNodeSelector(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTolerations := []any{
		map[string]any{"key": "nvidia.com/gpu", "operator": "Exists", "effect": "NoSchedule"},
	}
	if !reflect.DeepEqual(out["tolerations"], wantTolerations) {
		t.Errorf("tolerations: got %v, want %v", out["tolerations"], wantTolerations)
	}
	wantNodeSelector := map[string]any{"nvidia.com/gpu.present": "true"}
	if !reflect.DeepEqual(out["nodeSelector"], wantNodeSelector) {
		t.Errorf("nodeSelector: got %v, want %v", out["nodeSelector"], wantNodeSelector)
	}
}

// §4.4: imagePullSecrets is layer-6 (operator-managed). The deployer (layer 4)
// does NOT emit it. Locks the rule against the contradicting yaml example.
func TestGenerateValues_DoesNotEmitImagePullSecrets(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 1,
		GPUs:     ptrInt32(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := out["imagePullSecrets"]; present {
		t.Errorf("imagePullSecrets must NOT be present (layer-6 owns it); got %v", out["imagePullSecrets"])
	}
}

// Top-level replicas key must equal req.Replicas.
func TestGenerateValues_EmitsReplicas(t *testing.T) {
	d := newTestDeployer(t)
	out, err := d.GenerateValues(context.Background(), GenerateRequest{
		Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
		Replicas: 3,
		GPUs:     ptrInt32(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["replicas"] != int32(3) {
		t.Errorf("expected replicas=3, got %v (%T)", out["replicas"], out["replicas"])
	}
}

// §4.4: VLM uses 64Gi/GPU; LLM uses 32Gi/GPU. Subsumed by the worked-example
// tests but worth a parameterised confirmation that Type drives the choice.
func TestGenerateValues_VLMTypeUses64Gi(t *testing.T) {
	d := newTestDeployer(t)
	cases := map[string]struct {
		typ      Type
		gpuCount int32
		wantMem  string
	}{
		"LLM 1 GPU": {TypeLLM, 1, "32Gi"},
		"VLM 1 GPU": {TypeVLM, 1, "64Gi"},
		"LLM 4 GPU": {TypeLLM, 4, "128Gi"},
		"VLM 4 GPU": {TypeVLM, 4, "256Gi"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gpus := tc.gpuCount
			out, err := d.GenerateValues(context.Background(), GenerateRequest{
				Entry:    NIMEntry{Chart: "nim-x", Version: "1.0", Type: tc.typ},
				Replicas: 1,
				GPUs:     &gpus,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			res := out["resources"].(map[string]any)["requests"].(map[string]any)
			if res["memory"] != tc.wantMem {
				t.Errorf("memory: got %v, want %v", res["memory"], tc.wantMem)
			}
		})
	}
}

// TestDeployer_UpdateSettings_Race hammers UpdateSettings concurrently with
// GenerateValues. Must be run under -race; matches the race test on
// helm.engine for the same sole-writer pattern.
func TestDeployer_UpdateSettings_Race(t *testing.T) {
	d := newTestDeployer(t)
	const writers = 8
	const readers = 8
	const iters = 1000

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				d.UpdateSettings(EngineSettings{RegistryEndpoint: "r"})
			}
		}()
	}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_, _ = d.GenerateValues(context.Background(), GenerateRequest{
					Entry:    NIMEntry{Chart: "nim-llm", Version: "1.0", Type: TypeLLM},
					Replicas: 1,
					GPUs:     ptrInt32(1),
				})
			}
		}()
	}
	wg.Wait()
}
