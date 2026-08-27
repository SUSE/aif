package main

import (
	"testing"

	"github.com/SUSE/aif-operator/internal/catalog"
)

func TestLoadOverrides_EmptyInputs(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t ", "{}"} {
		ov, err := loadOverrides([]byte(in))
		if err != nil {
			t.Fatalf("loadOverrides(%q): %v", in, err)
		}
		if len(ov) != 0 {
			t.Fatalf("loadOverrides(%q) = %v, want empty", in, ov)
		}
	}
}

func TestLoadOverrides_Invalid(t *testing.T) {
	if _, err := loadOverrides([]byte(`{not json`)); err == nil {
		t.Fatal("want error for malformed overrides")
	}
}

func TestApply_OverwritesOnlyPinnedFields(t *testing.T) {
	ov, err := loadOverrides([]byte(`{"gpu-operator":{"description":"pinned","project_url":"https://example.test"}}`))
	if err != nil {
		t.Fatal(err)
	}
	item := catalog.Item{
		Name: gpuOperatorName, SlugName: gpuOperatorSlug,
		Description: "derived", RepositoryURL: "https://helm.ngc.nvidia.com/nvidia",
	}
	if err := ov.apply(&item); err != nil {
		t.Fatal(err)
	}
	if item.Description != "pinned" {
		t.Fatalf("Description = %q, want pinned", item.Description)
	}
	if item.ProjectURL != "https://example.test" {
		t.Fatalf("ProjectURL = %q", item.ProjectURL)
	}
	// Fields absent from the override object stay at their derived values.
	if item.Name != gpuOperatorName {
		t.Fatalf("Name overwritten: %q", item.Name)
	}
	if item.RepositoryURL != "https://helm.ngc.nvidia.com/nvidia" {
		t.Fatalf("RepositoryURL overwritten: %q", item.RepositoryURL)
	}
}

func TestApply_NoOverrideForSlugIsNoOp(t *testing.T) {
	ov, err := loadOverrides([]byte(`{"other":{"description":"pinned"}}`))
	if err != nil {
		t.Fatal(err)
	}
	item := catalog.Item{SlugName: "gpu-operator", Description: "derived"}
	if err := ov.apply(&item); err != nil {
		t.Fatal(err)
	}
	if item.Description != "derived" {
		t.Fatalf("unrelated override applied: %q", item.Description)
	}
}
