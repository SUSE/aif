package blueprint

// This file is the ONLY file in pkg/blueprint allowed to import api/v1alpha1.
// It owns the translation between the K8s CR shape and the domain shape.

import (
	aifv1 "github.com/SUSE/aif/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FromCR builds a domain Blueprint from the K8s CR.
func FromCR(cr *aifv1.Blueprint) Blueprint {
	if cr == nil {
		return Blueprint{}
	}
	return Blueprint{
		Name:              cr.Name,
		Lineage:           cr.Spec.BlueprintName,
		Version:           cr.Spec.Version,
		UseCase:           cr.Spec.UseCase,
		Description:       cr.Spec.Description,
		ChangeDescription: cr.Spec.ChangeDescription,
		Source:            sourceFromCR(cr.Spec.Source),
		Components:        componentsFromCR(cr.Spec.Components),
		ValueOverrides:    copyStringMap(cr.Spec.ValueOverrides),
		PublishedAt:       cr.Spec.PublishedAt.Time,
		PublishedBy:       cr.Spec.PublishedBy,
		Status:            statusFromCR(cr.Status),
	}
}

// ToCR builds a K8s CR (sans TypeMeta/ObjectMeta beyond Name) from a domain
// Blueprint. Caller is responsible for setting metav1 annotations/labels and
// for invoking the API client.
func ToCR(b Blueprint) *aifv1.Blueprint {
	cr := &aifv1.Blueprint{}
	cr.Name = b.Name
	cr.Spec = aifv1.BlueprintSpec{
		BlueprintName:     b.Lineage,
		Version:           b.Version,
		UseCase:           b.UseCase,
		Description:       b.Description,
		ChangeDescription: b.ChangeDescription,
		Source:            sourceToCR(b.Source),
		Components:        componentsToCR(b.Components),
		ValueOverrides:    copyStringMap(b.ValueOverrides),
		PublishedBy:       b.PublishedBy,
		PublishedAt:       metav1.NewTime(b.PublishedAt),
	}
	cr.Status = statusToCR(b.Status)
	return cr
}

// --- helpers ---

func sourceFromCR(s aifv1.BlueprintSource) Source {
	out := Source{Type: SourceType(s.Type)}
	if s.PublishedFrom != nil {
		out.PublishedFrom = &PublishedFromRef{
			BundleNamespace:  s.PublishedFrom.BundleNamespace,
			BundleName:       s.PublishedFrom.BundleName,
			BundleGeneration: s.PublishedFrom.BundleGeneration,
		}
	}
	if s.VendorChartRef != nil {
		out.Vendor = &VendorChartRef{
			Provider: s.VendorChartRef.Provider,
			Repo:     s.VendorChartRef.Repo,
			Chart:    s.VendorChartRef.Chart,
			Version:  s.VendorChartRef.Version,
		}
	}
	return out
}

func sourceToCR(s Source) aifv1.BlueprintSource {
	out := aifv1.BlueprintSource{Type: aifv1.BlueprintSourceType(s.Type)}
	if s.PublishedFrom != nil {
		out.PublishedFrom = &aifv1.PublishedFromRef{
			BundleNamespace:  s.PublishedFrom.BundleNamespace,
			BundleName:       s.PublishedFrom.BundleName,
			BundleGeneration: s.PublishedFrom.BundleGeneration,
		}
	}
	if s.Vendor != nil {
		out.VendorChartRef = &aifv1.VendorChartRef{
			Provider: s.Vendor.Provider,
			Repo:     s.Vendor.Repo,
			Chart:    s.Vendor.Chart,
			Version:  s.Vendor.Version,
		}
	}
	return out
}

func componentsFromCR(in []aifv1.ComponentRef) []ComponentRef {
	if in == nil {
		return nil
	}
	out := make([]ComponentRef, len(in))
	for i, c := range in {
		out[i] = ComponentRef{
			Name: c.Name,
			Kind: ComponentKind(c.Kind),
		}
		if c.App != nil {
			out[i].App = &AppRef{Repo: c.App.Repo, Chart: c.App.Chart, Version: c.App.Version}
		}
		if c.Blueprint != nil {
			out[i].Blueprint = &BlueprintRef{Name: c.Blueprint.Name, Version: c.Blueprint.Version}
		}
	}
	return out
}

func componentsToCR(in []ComponentRef) []aifv1.ComponentRef {
	if in == nil {
		return nil
	}
	out := make([]aifv1.ComponentRef, len(in))
	for i, c := range in {
		out[i] = aifv1.ComponentRef{
			Name: c.Name,
			Kind: aifv1.ComponentKind(c.Kind),
		}
		if c.App != nil {
			out[i].App = &aifv1.AppRef{Repo: c.App.Repo, Chart: c.App.Chart, Version: c.App.Version}
		}
		if c.Blueprint != nil {
			out[i].Blueprint = &aifv1.BlueprintRef{Name: c.Blueprint.Name, Version: c.Blueprint.Version}
		}
	}
	return out
}

func statusFromCR(s aifv1.BlueprintStatus) Status {
	out := Status{
		Phase:              Phase(s.Phase),
		DeploymentCount:    s.DeploymentCount,
		Conditions:         copyConditions(s.Conditions),
		ObservedGeneration: s.ObservedGeneration,
	}
	if s.Deprecation != nil {
		out.Deprecation = &Deprecation{
			Reason:     s.Deprecation.Reason,
			ActionedBy: s.Deprecation.ActionedBy,
			ActionedAt: s.Deprecation.ActionedAt.Time,
		}
	}
	return out
}

func statusToCR(s Status) aifv1.BlueprintStatus {
	out := aifv1.BlueprintStatus{
		Phase:              aifv1.BlueprintPhase(s.Phase),
		DeploymentCount:    s.DeploymentCount,
		Conditions:         copyConditions(s.Conditions),
		ObservedGeneration: s.ObservedGeneration,
	}
	if s.Deprecation != nil {
		out.Deprecation = &aifv1.DeprecationStatus{
			Reason:     s.Deprecation.Reason,
			ActionedBy: s.Deprecation.ActionedBy,
			ActionedAt: metav1.NewTime(s.Deprecation.ActionedAt),
		}
	}
	return out
}

func copyConditions(in []metav1.Condition) []metav1.Condition {
	if in == nil {
		return nil
	}
	out := make([]metav1.Condition, len(in))
	copy(out, in)
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
