package nvidia

import (
	"context"
	"io"
	"log/slog"
	"reflect"
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
