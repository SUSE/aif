package publish

import "context"

// FakeEventRecorder collects events in memory for test assertions.
type FakeEventRecorder struct {
	Events []string
}

func (f *FakeEventRecorder) BundleSubmitted(_ context.Context, namespace, name, user, version string) {
	f.Events = append(f.Events, "BundleSubmitted:"+namespace+"/"+name+":"+user+":"+version)
}

func (f *FakeEventRecorder) BundleWithdrawn(_ context.Context, namespace, name, user string) {
	f.Events = append(f.Events, "BundleWithdrawn:"+namespace+"/"+name+":"+user)
}
