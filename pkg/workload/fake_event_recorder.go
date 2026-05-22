package workload

import (
	"context"
	"fmt"
)

// FakeUpgradeEventRecorder collects events in memory for test assertions.
// Mirrors pkg/publish/fake_event_recorder.go.
type FakeUpgradeEventRecorder struct {
	Events []string
}

func (f *FakeUpgradeEventRecorder) UpgradeStarted(_ context.Context, namespace, name, oldVersion, newVersion string) {
	f.Events = append(f.Events, fmt.Sprintf("UpgradeStarted:%s/%s:%s→%s", namespace, name, oldVersion, newVersion))
}
