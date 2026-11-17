package labs

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/installaiextension"
)

// NamePrefix is required on a lab's chart name and UIPlugin name, so labs live in
// their own namespace of Helm release and UIPlugin names and never collide with the
// core AIF UI (aif-ui, release aif-ui-server) or any non-lab extension.
const NamePrefix = "aif-lab-"

// ErrNoAllowlist is returned for external catalogs when the operator runs without
// a registry host allowlist: a remote catalog may then only point at hosts the
// admin approved, so no allowlist means no external catalogs.
var ErrNoAllowlist = errors.New("external lab catalogs require a registry host allowlist " +
	"(manager.allowedRegistryHosts / --allowed-registry-hosts)")

// ValidateNames rejects a lab whose extension name or Helm chart name (which
// determines the server release name) is outside the aif-lab- namespace.
func ValidateNames(src v1alpha1.ExtensionSource, ext v1alpha1.ExtensionConfig) error {
	if !strings.HasPrefix(ext.Name, NamePrefix) {
		return fmt.Errorf("extension.name %q must start with %q", ext.Name, NamePrefix)
	}
	if src.Kind == v1alpha1.ExtensionSourceKindHelm && src.Helm != nil {
		chart := ChartName(src.Helm.ChartURL)
		if !strings.HasPrefix(chart, NamePrefix) {
			return fmt.Errorf("chart name %q (from chartURL) must start with %q", chart, NamePrefix)
		}
	}
	return nil
}

// ChartName is the server release name without its "-server" suffix.
func ChartName(chartURL string) string {
	return strings.TrimSuffix(installaiextension.ReleaseName(chartURL), "-server")
}

// ReleaseName is the Helm release the InstallAIExtension controller creates for src,
// or "" for non-Helm sources.
func ReleaseName(src v1alpha1.ExtensionSource) string {
	if src.Kind != v1alpha1.ExtensionSourceKindHelm || src.Helm == nil {
		return ""
	}
	return installaiextension.ReleaseName(src.Helm.ChartURL)
}

// HostAllowed reports whether chartURL's registry host is on the allowlist, using
// the same match as the InstallAIExtension controller ("host:port" or hostname,
// case-insensitive). An empty allowlist allows nothing here.
func HostAllowed(allowed []string, chartURL string) bool {
	u, err := url.Parse(chartURL)
	if err != nil || u.Host == "" {
		return false
	}
	for _, h := range allowed {
		if strings.EqualFold(h, u.Host) || strings.EqualFold(h, u.Hostname()) {
			return true
		}
	}
	return false
}

// chartURLPattern matches the HelmSource.chartURL CRD pattern.
var chartURLPattern = regexp.MustCompile(`^(oci://|https://).+`)

// validateExternal applies, in Go, the rules the API server enforces on
// LabCatalog status.entries[] (so one bad entry is skipped with a reason instead
// of failing the whole status write), plus the external-catalog trust rules:
// Helm sources only, and the chart host must be on the allowlist.
func validateExternal(e Entry, allowed []string) error {
	switch e.Maturity {
	case "", "experimental", "preview":
	default:
		return fmt.Errorf("maturity %q must be experimental or preview", e.Maturity)
	}
	if e.Source.Kind != v1alpha1.ExtensionSourceKindHelm {
		return fmt.Errorf("source.kind %q not allowed: external catalogs may only use Helm", e.Source.Kind)
	}
	h := e.Source.Helm
	switch {
	case h == nil:
		return errors.New("source.helm is required")
	case e.Source.Git != nil:
		return errors.New("source.git must not be set with kind Helm")
	case !chartURLPattern.MatchString(h.ChartURL):
		return fmt.Errorf("chartURL %q must start with oci:// or https://", h.ChartURL)
	case h.Version == "":
		return errors.New("source.helm.version is required")
	case e.Extension.Version == "":
		return errors.New("extension.version is required")
	case !HostAllowed(allowed, h.ChartURL):
		return fmt.Errorf("chart host of %q is not on the registry host allowlist", h.ChartURL)
	}
	return ValidateNames(e.Source, e.Extension)
}

// sanitizeExternal removes fields an external catalog must not control.
//   - auth and tls reference Secrets in the operator namespace; letting a remote
//     catalog set them would send the operator's registry credentials to whatever
//     host the catalog's chartURL names.
//   - values can repoint the server image at any registry, bypassing the
//     allowlist (which only checks the chart host).
func sanitizeExternal(e Entry) Entry {
	e.Source = *e.Source.DeepCopy()
	if e.Source.Helm != nil {
		e.Source.Helm.Auth = nil
		e.Source.Helm.TLS = nil
		e.Source.Helm.Values = nil
	}
	return e
}
