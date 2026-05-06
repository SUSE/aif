//go:build tools
// +build tools

package tools

// Import toolchain dependencies with compatible versions for Go 1.26
import (
	// golangci-lint adds 170+ transitive dependencies, but is required by the spec
	_ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	_ "github.com/onsi/ginkgo/v2/ginkgo"
	_ "go.uber.org/mock/mockgen"
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
