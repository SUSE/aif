package bundle

import (
	"fmt"
	"regexp"
)

// dns1123Regex enforces the K8s DNS-1123 subdomain format on TargetBlueprint.
// Compiled once at package load.
var dns1123Regex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// validUseCases is the closed set Bundle.UseCase may take. Mirrors the
// kubebuilder enum on Bundle CRD; defence-in-depth (open question per
// memory feedback_oop_directives.md).
var validUseCases = map[string]bool{
	"rag":         true,
	"vision":      true,
	"fine-tuning": true,
	"inference":   true,
	"other":       true,
}

// Validate is the canonical Bundle spec validator. It is a free function over
// the pure-Go domain Bundle — no controller state, no K8s calls, deterministic.
//
// This replaces the cache-bearing Manager.Upsert from the P1-1 scaffold (the
// in-memory cache is gone per user directive — see memory feedback_oop_directives.md).
// Reconcilers and HTTP handlers should call Validate then forward the Bundle
// to bundle.Repository for persistence.
func Validate(b Bundle) error {
	if !validUseCases[b.UseCase] {
		return fmt.Errorf("invalid useCase: %s", b.UseCase)
	}
	if !dns1123Regex.MatchString(b.TargetBlueprint) {
		return fmt.Errorf("targetBlueprint must be DNS-1123 format")
	}
	if len(b.TargetBlueprint) > 253 {
		return fmt.Errorf("targetBlueprint exceeds maximum length of 253 characters")
	}
	if len(b.Components) == 0 {
		return fmt.Errorf("components must not be empty")
	}
	return nil
}
