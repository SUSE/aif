// Package helm_oci is the shared kernel for parsing Helm OCI artifacts.
// Pure parsing only — no HTTP, no auth, no engine-specific types. Both
// pkg/nvidia and pkg/source_collection depend on this package; this
// package depends on stdlib + sigs.k8s.io/yaml only.
//
// The CNCF Helm OCI artifact format is the source of truth: a manifest
// references a single tar+gzip layer with mediaType
// application/vnd.cncf.helm.chart.content.v1.tar+gzip; that tarball
// contains <chartname>/Chart.yaml with the annotations block.
package helm_oci

import "errors"

// ErrManifestMalformed is returned when the OCI manifest JSON cannot
// be parsed.
var ErrManifestMalformed = errors.New("helm_oci: manifest malformed")

// ErrChartLayerMissing is returned when a parsed manifest contains no
// layer with mediaType application/vnd.cncf.helm.chart.content.v1.tar+gzip.
var ErrChartLayerMissing = errors.New("helm_oci: chart-content layer not found in manifest")

// ErrChartYamlMissing is returned when the chart tarball has no entry
// matching */Chart.yaml. Callers may treat this as a legitimate "no
// annotations" case rather than an error.
var ErrChartYamlMissing = errors.New("helm_oci: Chart.yaml not found in chart tarball")
