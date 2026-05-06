// Package blueprint owns the Blueprint domain model and its operations.
//
// The types in this file are pure-Go value objects with no kubebuilder
// markers, no metav1 fields, and no api/v1alpha1 imports — that's enforced
// by CLAUDE.md's layering rule. CR <-> domain translation lives in
// conversions.go (the only file in this package allowed to import aifv1).
package blueprint

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Phase is the lifecycle status of a published Blueprint version.
type Phase string

const (
	PhaseActive     Phase = "Active"
	PhaseDeprecated Phase = "Deprecated"
	PhaseWithdrawn  Phase = "Withdrawn"
)

// SourceType discriminates how the Blueprint came into existence.
type SourceType string

const (
	// SourceTypePublished — minted by the publish-by-approval workflow from
	// an author's Bundle.
	SourceTypePublished SourceType = "Published"
	// SourceTypeWrapsVendorChart — auto-wrapped by AIF around a vendor-published
	// Reference Blueprint Helm chart mirrored into SUSE Registry.
	SourceTypeWrapsVendorChart SourceType = "WrapsVendorChart"
)

// ComponentKind discriminates whether a component is an App chart or another
// Blueprint embedded into this one.
type ComponentKind string

const (
	ComponentKindApp       ComponentKind = "App"
	ComponentKindBlueprint ComponentKind = "Blueprint"
)

// Blueprint is the domain representation of an AIF Blueprint version. Identity
// is (Name, Version); Name corresponds to the K8s object name (typically
// "{lineage}.{version}").
type Blueprint struct {
	// Identity
	Name    string // K8s object name
	Lineage string // spec.blueprintName — shared across versions
	Version string // semver, no v prefix

	// Content
	UseCase           string
	Description       string
	ChangeDescription string
	Source            Source
	Components        []ComponentRef
	ValueOverrides    map[string]string

	// Provenance
	PublishedAt time.Time
	PublishedBy string

	// Observed
	Status Status
}

// Source captures Blueprint origin — exactly one of PublishedFrom or Vendor
// is set, matching Type.
type Source struct {
	Type          SourceType
	PublishedFrom *PublishedFromRef
	Vendor        *VendorChartRef
}

// PublishedFromRef points at the source Bundle that was approved to mint this
// Blueprint version.
type PublishedFromRef struct {
	BundleNamespace  string
	BundleName       string
	BundleGeneration int64
}

// VendorChartRef points at the vendor-published Helm chart that AIF wrapped.
type VendorChartRef struct {
	Provider string
	Repo     string
	Chart    string
	Version  string
}

// ComponentRef references one component pinned in a Blueprint.
// Exactly one of App or Blueprint is set, matching Kind.
type ComponentRef struct {
	Name      string // local handle, used as the key for ValueOverrides
	Kind      ComponentKind
	App       *AppRef
	Blueprint *BlueprintRef
}

// AppRef references a Helm chart in a repository.
type AppRef struct {
	Repo    string
	Chart   string
	Version string
}

// BlueprintRef references another Blueprint by lineage name and version.
type BlueprintRef struct {
	Name    string
	Version string
}

// Status captures observed Blueprint state.
//
// Conditions uses metav1.Condition (K8s upstream) rather than a hand-rolled
// shape because every consumer interops with K8s conditions and reinventing
// the type buys nothing. metav1 is K8s standard, NOT aifv1, so this does not
// violate the layering rule that pkg/<x>/types.go must be free of aifv1.
type Status struct {
	Phase              Phase
	DeploymentCount    int32
	Deprecation        *Deprecation
	Conditions         []metav1.Condition
	ObservedGeneration int64
}

// Deprecation captures who/when/why a Blueprint version was deprecated or
// withdrawn.
type Deprecation struct {
	Reason     string
	ActionedBy string
	ActionedAt time.Time
}
