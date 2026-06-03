package rancher

import "fmt"

type DependencyNotReadyError struct {
	Dependency string
}

func (e *DependencyNotReadyError) Error() string {
	return fmt.Sprintf("dependency %q is not ready", e.Dependency)
}
