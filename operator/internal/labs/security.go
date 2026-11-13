package labs

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// NamePrefix is required on a lab's chart name and UIPlugin name, so labs live in
// their own namespace of Helm release and UIPlugin names and never collide with the
// core AIF UI (aif-ui, release aif-ui-server) or any non-lab extension.
const NamePrefix = "aif-lab-"

// ValidateNames rejects a lab whose extension name or Helm chart name (which
// determines the server release name) is outside the aif-lab- namespace.
func ValidateNames(src v1alpha1.ExtensionSource, ext v1alpha1.ExtensionConfig) error {
	if !strings.HasPrefix(ext.Name, NamePrefix) {
		return fmt.Errorf("extension.name %q must start with %q", ext.Name, NamePrefix)
	}
	if src.Kind == v1alpha1.ExtensionSourceKindHelm && src.Helm != nil {
		chart := chartName(src.Helm.ChartURL)
		if !strings.HasPrefix(chart, NamePrefix) {
			return fmt.Errorf("chart name %q (from chartURL) must start with %q", chart, NamePrefix)
		}
	}
	return nil
}

// chartName mirrors the release-name derivation in the InstallAIExtension
// controller (last path segment, without a trailing -server).
func chartName(chartURL string) string {
	p := chartURL
	if u, err := url.Parse(chartURL); err == nil {
		p = u.Path
	}
	return strings.TrimSuffix(path.Base(p), "-server")
}

// sanitizeExternal removes fields an external catalog must not control.
// auth and tls reference Secrets in the operator namespace; letting a remote
// catalog set them would send the operator's registry credentials to whatever
// host the catalog's chartURL names.
func sanitizeExternal(e Entry) Entry {
	e.Source = *e.Source.DeepCopy()
	if e.Source.Helm != nil {
		e.Source.Helm.Auth = nil
		e.Source.Helm.TLS = nil
	}
	return e
}
