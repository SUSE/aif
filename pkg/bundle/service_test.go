package bundle

import (
	"strings"
	"testing"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
)

func TestValidate_InvalidUseCase(t *testing.T) {
	b := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: "test-blueprint",
		UseCase:         "invalid-usecase",
		Components:      []aifv1.ComponentRef{{Name: "test"}},
	}
	err := Validate(b)
	if err == nil {
		t.Fatal("expected error for invalid useCase, got nil")
	}
	if err.Error() != "invalid useCase: invalid-usecase" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_InvalidTargetBlueprint(t *testing.T) {
	cases := []string{
		"",                  // empty
		"Test-Blueprint",    // uppercase not allowed
		"-invalid",          // leading hyphen
		"invalid-",          // trailing hyphen
		"invalid_.name",     // underscore
		"invalid name",      // space
		"INVALID",           // all uppercase
	}
	for _, name := range cases {
		b := Bundle{
			Namespace:       "test-ns",
			Name:            "test-bundle",
			TargetBlueprint: name,
			UseCase:         "rag",
			Components:      []aifv1.ComponentRef{{Name: "test"}},
		}
		err := Validate(b)
		if err == nil {
			t.Errorf("expected error for invalid targetBlueprint %q, got nil", name)
			continue
		}
		if err.Error() != "targetBlueprint must be DNS-1123 format" {
			t.Errorf("unexpected error message for %q: %v", name, err)
		}
	}
}

func TestValidate_TargetBlueprintExceedsMaxLength(t *testing.T) {
	b := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: strings.Repeat("a", 254),
		UseCase:         "rag",
		Components:      []aifv1.ComponentRef{{Name: "test"}},
	}
	err := Validate(b)
	if err == nil {
		t.Fatal("expected error for targetBlueprint exceeding 253 characters, got nil")
	}
	if err.Error() != "targetBlueprint exceeds maximum length of 253 characters" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_EmptyComponents(t *testing.T) {
	b := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: "test-blueprint",
		UseCase:         "rag",
		Components:      []aifv1.ComponentRef{},
	}
	err := Validate(b)
	if err == nil {
		t.Fatal("expected error for empty components, got nil")
	}
	if err.Error() != "components must not be empty" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_ValidBundle(t *testing.T) {
	b := Bundle{
		Namespace:       "test-ns",
		Name:            "test-bundle",
		TargetBlueprint: "test-blueprint",
		UseCase:         "rag",
		Components:      []aifv1.ComponentRef{{Name: "test"}},
	}
	if err := Validate(b); err != nil {
		t.Fatalf("expected no error for valid bundle, got: %v", err)
	}
}
