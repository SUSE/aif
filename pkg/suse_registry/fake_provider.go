package suse_registry

import "context"

// FakeProvider is an in-memory Provider for unit tests.
type FakeProvider struct {
	Charts     map[string]SUSEChart // key: ID
	Settings   EngineSettings
	RefreshErr error
}

func (f *FakeProvider) List(_ context.Context) ([]SUSEChart, error) {
	out := make([]SUSEChart, 0, len(f.Charts))
	for _, c := range f.Charts {
		out = append(out, c)
	}
	return out, nil
}

func (f *FakeProvider) Get(_ context.Context, name, version string) (SUSEChart, error) {
	if c, ok := f.Charts[name+":"+version]; ok {
		return c, nil
	}
	return SUSEChart{}, ErrNotFound
}

func (f *FakeProvider) Refresh(_ context.Context) error { return f.RefreshErr }

func (f *FakeProvider) UpdateSettings(s EngineSettings) { f.Settings = s }
