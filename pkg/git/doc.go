// Package git wraps go-git as a port + adapter so the Fleet engine can push
// rendered manifests to a remote git repository without depending on
// go-git directly.
//
// CLAUDE.md layering rule: this package MUST NOT import api/v1alpha1.
// All domain types live in types.go; CR↔domain translation belongs in
// the consuming package (pkg/workload/conversions.go in practice — but
// pkg/git itself never sees a Workload CR).
//
// Bounded-context exemption: this package owns rich value objects
// (EngineSettings, GitAuth as a tagged union, PushRequest, PushResult)
// and is a clear conceptual boundary (one external system: git remotes).
// See CLAUDE.md "Where ports live" / "bounded-context ports".
package git
