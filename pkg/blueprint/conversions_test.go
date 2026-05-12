package blueprint

import (
	"reflect"
	"testing"
	"time"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFromCR_NilCR(t *testing.T) {
	got := FromCR(nil)
	if !reflect.DeepEqual(got, Blueprint{}) {
		t.Errorf("FromCR(nil) = %#v, want zero Blueprint", got)
	}
}

func TestFromCR_PublishedSource(t *testing.T) {
	cr := &aifv1.Blueprint{}
	cr.Name = "rag-with-llama.1.0.0"
	cr.Spec = aifv1.BlueprintSpec{
		BlueprintName: "rag-with-llama",
		Version:       "1.0.0",
		UseCase:       "rag",
		Description:   "test blueprint",
		Source: aifv1.BlueprintSource{
			Type: aifv1.BlueprintSourcePublished,
			PublishedFrom: &aifv1.PublishedFromRef{
				BundleNamespace:  "alice",
				BundleName:       "rag-iter-3",
				BundleGeneration: 7,
			},
		},
		Components: []aifv1.ComponentRef{
			{Name: "llm", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "oci://r", Chart: "nim-llm", Version: "1.0.0"}},
		},
		PublishedBy: "carol",
		PublishedAt: metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
	}

	got := FromCR(cr)

	if got.Name != "rag-with-llama.1.0.0" || got.Lineage != "rag-with-llama" || got.Version != "1.0.0" {
		t.Errorf("identity wrong: %#v", got)
	}
	if got.Source.Type != SourceTypePublished {
		t.Errorf("source.type = %s, want Published", got.Source.Type)
	}
	if got.Source.PublishedFrom == nil || got.Source.PublishedFrom.BundleName != "rag-iter-3" {
		t.Errorf("source.publishedFrom not converted: %#v", got.Source)
	}
	if got.Source.Vendor != nil {
		t.Errorf("source.vendor unexpectedly set: %#v", got.Source.Vendor)
	}
	if len(got.Components) != 1 || got.Components[0].App == nil || got.Components[0].App.Chart != "nim-llm" {
		t.Errorf("components not converted: %#v", got.Components)
	}
	if !got.PublishedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("publishedAt = %v, want 2026-01-02T03:04:05Z", got.PublishedAt)
	}
}

func TestFromCR_VendorSource(t *testing.T) {
	cr := &aifv1.Blueprint{}
	cr.Spec = aifv1.BlueprintSpec{
		BlueprintName: "nvidia-rag",
		Version:       "2.1.0",
		Source: aifv1.BlueprintSource{
			Type: aifv1.BlueprintSourceWrapsVendorChart,
			VendorChartRef: &aifv1.VendorChartRef{
				Provider: "nvidia",
				Repo:     "oci://registry.suse.com/ai/charts/nvidia",
				Chart:    "rag",
				Version:  "2.1.0",
			},
		},
	}

	got := FromCR(cr)
	if got.Source.Type != SourceTypeWrapsVendorChart {
		t.Errorf("source.type = %s, want WrapsVendorChart", got.Source.Type)
	}
	if got.Source.Vendor == nil || got.Source.Vendor.Provider != "nvidia" {
		t.Errorf("vendor not converted: %#v", got.Source.Vendor)
	}
	if got.Source.PublishedFrom != nil {
		t.Errorf("publishedFrom unexpectedly set: %#v", got.Source.PublishedFrom)
	}
}

func TestRoundtrip_Published(t *testing.T) {
	original := &aifv1.Blueprint{}
	original.Name = "rag-with-llama.1.0.0"
	original.Spec = aifv1.BlueprintSpec{
		BlueprintName: "rag-with-llama",
		Version:       "1.0.0",
		UseCase:       "rag",
		Source: aifv1.BlueprintSource{
			Type:          aifv1.BlueprintSourcePublished,
			PublishedFrom: &aifv1.PublishedFromRef{BundleNamespace: "alice", BundleName: "b", BundleGeneration: 1},
		},
		Components: []aifv1.ComponentRef{
			{Name: "x", Kind: aifv1.ComponentKindApp, App: &aifv1.AppRef{Repo: "r", Chart: "c", Version: "v"}},
		},
		PublishedBy: "p",
		PublishedAt: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	}

	roundtrip := ToCR(FromCR(original))

	// Spec must roundtrip exactly (modulo TypeMeta/ObjectMeta beyond Name).
	if !reflect.DeepEqual(roundtrip.Spec, original.Spec) {
		t.Errorf("spec roundtrip drift\n got:  %#v\n want: %#v", roundtrip.Spec, original.Spec)
	}
	if roundtrip.Name != original.Name {
		t.Errorf("name roundtrip = %q, want %q", roundtrip.Name, original.Name)
	}
}

// TestRoundtrip_Status guards against the conversion silently dropping the
// Status.Conditions list (caught by the critical-review audit, finding M1).
func TestToWrappedCR_SetsLabelsAndStatus(t *testing.T) {
	b := Blueprint{
		Name:    "nvidia-nim-llm.1.0.0",
		Lineage: "nvidia-nim-llm",
		Version: "1.0.0",
		UseCase: "inference",
		Source: Source{
			Type:   SourceTypeWrapsVendorChart,
			Vendor: &VendorChartRef{Provider: "nvidia", Repo: "oci://registry.suse.com/ai/charts/nvidia", Chart: "nim-llm", Version: "1.0.0"},
		},
		Components: []ComponentRef{{
			Name: "nvidia-nim-llm",
			Kind: ComponentKindApp,
			App:  &AppRef{Repo: "oci://registry.suse.com/ai/charts/nvidia", Chart: "nim-llm", Version: "1.0.0"},
		}},
		PublishedBy: "aif-system",
		Status:      Status{Phase: PhaseActive},
	}

	cr := ToWrappedCR(b)

	if cr.Labels["ai.suse.com/blueprint-name"] != "nvidia-nim-llm" {
		t.Errorf("blueprint-name label = %q, want %q", cr.Labels["ai.suse.com/blueprint-name"], "nvidia-nim-llm")
	}
	if cr.Labels["ai.suse.com/blueprint-version"] != "1.0.0" {
		t.Errorf("blueprint-version label = %q, want %q", cr.Labels["ai.suse.com/blueprint-version"], "1.0.0")
	}
	if cr.Labels["ai.suse.com/blueprint-source"] != "wraps-vendor-chart" {
		t.Errorf("blueprint-source label = %q, want %q", cr.Labels["ai.suse.com/blueprint-source"], "wraps-vendor-chart")
	}
	if _, ok := cr.Labels["ai.suse.com/blueprint-prerelease"]; ok {
		t.Error("blueprint-prerelease label should not be set for stable version")
	}
	if cr.Status.Phase != "Active" {
		t.Errorf("status.phase = %q, want Active", cr.Status.Phase)
	}
	if cr.Name != "nvidia-nim-llm.1.0.0" {
		t.Errorf("name = %q, want %q", cr.Name, "nvidia-nim-llm.1.0.0")
	}
	if cr.Spec.BlueprintName != "nvidia-nim-llm" {
		t.Errorf("blueprintName = %q, want %q", cr.Spec.BlueprintName, "nvidia-nim-llm")
	}
}

func TestToWrappedCR_PrereleaseLabelSet(t *testing.T) {
	b := Blueprint{
		Name:    "nvidia-nim-llm.1.0.0-rc.1",
		Lineage: "nvidia-nim-llm",
		Version: "1.0.0-rc.1",
		Source:  Source{Type: SourceTypeWrapsVendorChart, Vendor: &VendorChartRef{Provider: "nvidia"}},
		Status:  Status{Phase: PhaseActive},
	}

	cr := ToWrappedCR(b)

	if cr.Labels["ai.suse.com/blueprint-prerelease"] != "true" {
		t.Errorf("blueprint-prerelease label = %q, want %q", cr.Labels["ai.suse.com/blueprint-prerelease"], "true")
	}
}

func TestRoundtrip_Status(t *testing.T) {
	cr := &aifv1.Blueprint{}
	cr.Status = aifv1.BlueprintStatus{
		Phase:              aifv1.BlueprintPhaseDeprecated,
		DeploymentCount:    3,
		ObservedGeneration: 7,
		Deprecation: &aifv1.DeprecationStatus{
			Reason:     "superseded",
			ActionedBy: "alice",
			ActionedAt: metav1.NewTime(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)),
		},
		Conditions: []metav1.Condition{{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "BlueprintValidated",
			Message:            "ok",
			ObservedGeneration: 7,
			LastTransitionTime: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		}},
	}

	roundtrip := ToCR(FromCR(cr))

	if !reflect.DeepEqual(roundtrip.Status, cr.Status) {
		t.Errorf("status roundtrip drift\n got:  %#v\n want: %#v", roundtrip.Status, cr.Status)
	}
}
