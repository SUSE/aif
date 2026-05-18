package helm_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/SUSE/aif/pkg/helm"
)

// Example_fakeEngineLifecycle demonstrates the contract downstream
// consumers (controllers, REST handlers) use to exercise helm.Engine in
// tests: pass FakeEngine as the Engine port, drive the workflow, then
// assert against the recorded Calls slice. Doubles as the contract
// `make verify-helm-mock` runs to prove the package's public surface
// works without touching a real apiserver or OCI registry.
//
// Spec hooks: ARCHITECTURE.md §6.4 (Engine interface) and §8.2.1
// (UpdateSettings sole-writer pattern).
func Example_fakeEngineLifecycle() {
	fake := helm.NewFake()

	// Treat FakeEngine as the Engine port — exactly how a controller
	// under test would receive it via constructor injection.
	var eng helm.Engine = fake

	// Drive a minimal install -> status -> uninstall workflow.
	if _, err := eng.InstallChartFromRepo(context.Background(), helm.InstallRequest{
		Namespace:   "ns",
		ReleaseName: "ext",
		ChartRef:    "oci://example/ext:1.0",
		Overrides:   helm.Overrides{},
	}); err != nil {
		fmt.Println("install error:", err)
		return
	}
	// Default Status returns ErrReleaseNotFound; that's the documented
	// friendly default. Stub it via fake.StatusResult to override.
	if _, err := eng.Status(context.Background(), "ns", "ext"); !errors.Is(err, helm.ErrReleaseNotFound) {
		fmt.Println("unexpected status err:", err)
		return
	}
	if err := eng.Uninstall(context.Background(), "ns", "ext"); err != nil {
		fmt.Println("uninstall error:", err)
		return
	}

	// Calls are recorded in invocation order with the request/identity
	// fields populated per method.
	for _, c := range fake.Calls {
		switch c.Method {
		case "InstallChartFromRepo":
			fmt.Printf("%s ref=%s\n", c.Method, c.Request.ChartRef)
		default:
			fmt.Printf("%s ns=%s name=%s\n", c.Method, c.Namespace, c.Name)
		}
	}

	// Output:
	// InstallChartFromRepo ref=oci://example/ext:1.0
	// Status ns=ns name=ext
	// Uninstall ns=ns name=ext
}
