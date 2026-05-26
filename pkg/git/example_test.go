package git_test

import (
	"fmt"
	"log/slog"

	"github.com/SUSE/aif/pkg/git"
)

// Example_push demonstrates constructing the git Engine and configuring
// it via UpdateSettings. Doubles as `make verify-git-mock` — exists so
// the package can be probed without external services. The actual
// happy-path round-trip is covered by gogit_engine_test.go (in-process
// bare repo) and live_test.go (real remote, env-gated).
func Example_push() {
	e := git.NewEngine(slog.Default())
	e.UpdateSettings(git.EngineSettings{
		RepoURL: "file:///example/path/to/bare.git",
		Branch:  "main",
	})
	_ = e
	fmt.Println("git engine constructed")
	// Output: git engine constructed
}
