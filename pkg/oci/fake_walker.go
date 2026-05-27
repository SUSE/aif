package oci

import "context"

// FakeWalker is an in-memory Walker for unit tests.
type FakeWalker struct {
	// Catalog maps repository → tags.
	Catalog map[string][]string
	// Settings records the most recent UpdateSettings call.
	Settings EngineSettings
	// EnumerateErr / ListTagsErr force an error from the matching call.
	EnumerateErr error
	ListTagsErr  error
}

func (f *FakeWalker) EnumerateCharts(_ context.Context, prefix string, exclude []string) ([]ChartCoordinate, error) {
	if f.EnumerateErr != nil {
		return nil, f.EnumerateErr
	}
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, seg := range exclude {
		excludeSet[seg] = struct{}{}
	}
	out := []ChartCoordinate{}
	for repo, tags := range f.Catalog {
		if prefix != "" && !startsWith(repo, prefix) {
			continue
		}
		remainder := repo[len(prefix):]
		first := remainder
		for i := 0; i < len(remainder); i++ {
			if remainder[i] == '/' {
				first = remainder[:i]
				break
			}
		}
		if _, skip := excludeSet[first]; skip {
			continue
		}
		for _, t := range tags {
			out = append(out, ChartCoordinate{Repository: repo, Tag: t})
		}
	}
	return out, nil
}

func (f *FakeWalker) ListTags(_ context.Context, repo string) ([]string, error) {
	if f.ListTagsErr != nil {
		return nil, f.ListTagsErr
	}
	return f.Catalog[repo], nil
}

func (f *FakeWalker) UpdateSettings(s EngineSettings) { f.Settings = s }

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
