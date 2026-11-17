package installaiextension

import (
	urlpkg "net/url"
	"path"
	"strings"
)

// ReleaseName is the Helm release name the InstallAIExtension controller uses for
// the server chart at chartURL: the last path segment with a single "-server"
// suffix. The Lab name checks call it too, so both always agree.
func ReleaseName(chartURL string) string {
	p := chartURL
	if u, err := urlpkg.Parse(chartURL); err == nil {
		p = u.Path
	}
	return strings.TrimSuffix(path.Base(p), "-server") + "-server"
}
