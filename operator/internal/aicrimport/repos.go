package aicrimport

import (
	"sort"
	"strings"

	aiv1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

const repoPrefix = "aicr-"

// repoSet collects the chart sources a bundle needs, one custom repo per HTTP
// repository URL and one per OCI namespace.
type repoSet struct {
	reuse map[string]string // source URL -> existing repo name
	repos map[string]aiv1.CustomRepoSpec
}

func newRepoSet(reuse map[string]string) *repoSet {
	norm := map[string]string{}
	for u, n := range reuse {
		norm[strings.TrimSuffix(u, "/")] = n
	}
	return &repoSet{reuse: norm, repos: map[string]aiv1.CustomRepoSpec{}}
}

// add registers f's source and returns the repo name and chart name a
// Blueprint component should use. For OCI charts the repo URL is the
// namespace and the chart is the last path segment: AI Factory appends the
// chart name when it builds the HelmOp reference.
func (s *repoSet) add(f Folder) (repoName, chart string) {
	url, typ := strings.TrimSuffix(f.Repo, "/"), "helm"
	chart = f.Chart
	if strings.HasPrefix(f.Chart, "oci://") {
		ref := strings.TrimSuffix(f.Chart, "/")
		i := strings.LastIndex(ref, "/")
		url, chart, typ = ref[:i], ref[i+1:], "oci"
	}
	if name, ok := s.reuse[url]; ok {
		return name, chart
	}
	if spec, ok := s.repos[url]; ok {
		return spec.Name, chart
	}
	name := repoNameFor(url)
	s.repos[url] = aiv1.CustomRepoSpec{Name: name, DisplayName: url, Type: typ, URL: url}
	return name, chart
}

func (s *repoSet) specs() []aiv1.CustomRepoSpec {
	out := make([]aiv1.CustomRepoSpec, 0, len(s.repos))
	for _, spec := range s.repos {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// repoNameFor derives a deterministic DNS-1123 label from a repository URL.
func repoNameFor(url string) string {
	for _, p := range []string{"oci://", "https://", "http://"} {
		url = strings.TrimPrefix(url, p)
	}
	return naming.TruncateDNS1123Label(repoPrefix+naming.Slugify(url), 63)
}
