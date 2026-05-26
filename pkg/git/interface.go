package git

import "context"

// Engine pushes a manifest tree to a remote git repository on a branch.
// Idempotent: pushing the same tree to the same branch is a no-op
// (`git diff --quiet` → no commit).
//
// 2 methods (within ISP ≤4 target).
type Engine interface {
	// Push clones (or shallow-fetches), overwrites the owned subtrees,
	// commits if changed, and pushes. Returns the commit SHA (or
	// PushResult{NoOp:true} if nothing changed). Concurrent calls
	// serialize per-engine to avoid non-fast-forward races.
	Push(ctx context.Context, req PushRequest) (PushResult, error)

	// UpdateSettings receives credentials and the target repo/branch
	// from SettingsReconciler via engine_bus. The engine reads no
	// K8s Secrets directly.
	UpdateSettings(s EngineSettings)
}
