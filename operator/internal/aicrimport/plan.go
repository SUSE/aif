package aicrimport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	aiv1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/naming"
)

// Options controls how a bundle maps onto blueprints.
type Options struct {
	// Name is the blueprint family slug (DNS-1123 label), e.g. "nvidia-gpu-platform".
	Name string
	// DisplayName is the human-readable blueprint title.
	DisplayName string
	// Version is the blueprint SemVer.
	Version string
	// Description is prepended to the generated provenance text.
	Description string
	// Catalog is the catalog name the blueprints are grouped under.
	Catalog string
	// CatalogDisplayName is the catalog card title.
	CatalogDisplayName string
	// Single puts every component in one blueprint. Allowed only when no
	// selected component depends on another selected one, because AI Factory
	// installs a blueprint's components concurrently.
	Single bool
	// Only restricts the export to these component (release) names.
	Only []string
	// RepoNames maps a chart source URL to an existing repo name to reuse
	// instead of declaring a custom repo for it.
	RepoNames map[string]string
}

// Result is everything the importer writes.
type Result struct {
	Blueprints  []aiv1.Blueprint
	Catalog     aiv1.BlueprintCatalog
	CustomRepos []aiv1.CustomRepoSpec
	// Warnings are adjustments the user must know about.
	Warnings []string
}

// Plan maps a parsed bundle onto blueprints.
func Plan(b *Bundle, opts Options) (*Result, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	folders, err := selectFolders(b, opts.Only)
	if err != nil {
		return nil, err
	}
	res := &Result{}

	res.Warnings = append(res.Warnings, applyCompanionRules(folders)...)

	repos := newRepoSet(opts.RepoNames)
	components := map[string]aiv1.BlueprintComponent{}
	for _, f := range folders {
		repoName, chart := repos.add(f)
		c := aiv1.BlueprintComponent{
			ChartRepo:       repoName,
			ChartName:       chart,
			ChartVersion:    f.Version,
			TargetNamespace: namespaceFor(b, f.Release),
			ReleaseName:     f.Release,
		}
		if len(f.Values) > 0 {
			raw, err := json.Marshal(f.Values)
			if err != nil {
				return nil, fmt.Errorf("%s: encode values: %w", f.Release, err)
			}
			c.Values = &apixv1.JSON{Raw: raw}
		}
		components[f.Release] = c
	}
	res.CustomRepos = repos.specs()

	tiers, err := tierize(b, folders)
	if err != nil {
		return nil, err
	}
	if opts.Single && len(tiers) > 1 {
		return nil, fmt.Errorf("--single: the selected components span %d dependency levels %v; "+
			"AI Factory installs a blueprint's components concurrently, so they need tiered blueprints",
			len(tiers), tiers)
	}
	if opts.Single {
		tiers = [][]string{flatten(tiers)}
	}

	for i, tier := range tiers {
		name, display := opts.Name, opts.DisplayName
		if len(tiers) > 1 {
			name = fmt.Sprintf("%s-tier%d", opts.Name, i+1)
			display = fmt.Sprintf("%s (tier %d of %d)", opts.DisplayName, i+1, len(tiers))
		}
		bp := aiv1.Blueprint{
			TypeMeta: metav1.TypeMeta{APIVersion: aiv1.GroupVersion.String(), Kind: "Blueprint"},
			ObjectMeta: metav1.ObjectMeta{
				Name: blueprintObjectName(name, opts.Version),
				Labels: map[string]string{
					aiv1.BlueprintNameLabel:    name,
					aiv1.BlueprintVersionLabel: opts.Version,
					aiv1.BlueprintCatalogLabel: opts.Catalog,
				},
			},
			Spec: aiv1.BlueprintSpec{
				DisplayName: display,
				Version:     opts.Version,
				Description: describe(b, opts, i, tiers),
				Source:      aiv1.BlueprintOriginNvidia,
			},
		}
		for _, release := range tier {
			bp.Spec.Components = append(bp.Spec.Components, components[release])
		}
		res.Blueprints = append(res.Blueprints, bp)
	}

	res.Catalog = aiv1.BlueprintCatalog{
		TypeMeta:   metav1.TypeMeta{APIVersion: aiv1.GroupVersion.String(), Kind: "BlueprintCatalog"},
		ObjectMeta: metav1.ObjectMeta{Name: opts.Catalog},
		Spec: aiv1.BlueprintCatalogSpec{
			DisplayName: opts.CatalogDisplayName,
			Description: "NVIDIA-validated GPU stacks generated from NVIDIA AI Cluster Runtime (AICR) recipes.",
		},
	}
	for _, bp := range res.Blueprints {
		res.Catalog.Spec.Blueprints = append(res.Catalog.Spec.Blueprints,
			aiv1.BlueprintCatalogMember{Name: bp.Labels[aiv1.BlueprintNameLabel]})
	}
	return res, nil
}

func validateOptions(o Options) error {
	for flag, v := range map[string]string{"name": o.Name, "catalog": o.Catalog} {
		if errs := validation.IsDNS1123Label(v); len(errs) > 0 {
			return fmt.Errorf("--%s %q: %s", flag, v, strings.Join(errs, "; "))
		}
	}
	if o.DisplayName == "" || o.Version == "" {
		return fmt.Errorf("--display-name and --version are required")
	}
	if o.Catalog == aiv1.BlueprintCatalogDefault {
		return fmt.Errorf("catalog name %q is reserved for the bundled catalog", o.Catalog)
	}
	return nil
}

// selectFolders returns the upstream-chart folders to export, rejecting local
// charts, which have no chart repository for a HelmOp to pull from.
func selectFolders(b *Bundle, only []string) ([]Folder, error) {
	want, missing := map[string]bool{}, map[string]bool{}
	for _, n := range only {
		want[n], missing[n] = true, true
	}
	out := make([]Folder, 0, len(b.Folders))
	var local []string
	for _, f := range b.Folders {
		if len(want) > 0 && !want[f.Release] {
			continue
		}
		delete(missing, f.Release)
		if f.Local {
			local = append(local, f.Dir)
			continue
		}
		out = append(out, f)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("--only names components not in the bundle: %s", strings.Join(sortedKeys(missing), ", "))
	}
	if len(local) > 0 {
		return nil, fmt.Errorf("local charts (raw manifests) have no chart repository and cannot be a Blueprint "+
			"component: %s; exclude them with --only, or exclude the owning component when bundling",
			strings.Join(local, ", "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no upstream-chart components selected")
	}
	return out, nil
}

// applyCompanionRules adjusts values that assume a component outside the
// selection. gpu-operator under AICR delegates NFD to the standalone nfd
// component (nfd.enabled: false); exporting it without nfd would install a GPU
// Operator that relies on labels nothing maintains.
func applyCompanionRules(folders []Folder) []string {
	have := map[string]bool{}
	for _, f := range folders {
		have[f.Release] = true
	}
	var warnings []string
	for i := range folders {
		f := &folders[i]
		if f.Release != "gpu-operator" || have["nfd"] {
			continue
		}
		nfd, _ := f.Values["nfd"].(map[string]any)
		if enabled, ok := nfd["enabled"].(bool); ok && !enabled {
			nfd["enabled"] = true
			warnings = append(warnings, "gpu-operator: set nfd.enabled=true because the standalone nfd component is not exported")
		}
	}
	return warnings
}

func namespaceFor(b *Bundle, release string) string {
	if c, ok := b.component(release); ok && c.Namespace != "" {
		return c.Namespace
	}
	return release
}

// tierize groups the selected releases by dependency level using the recipe's
// dependencyRefs, keeping bundle (install) order within a level. Dependencies
// on components outside the selection are ignored: they are assumed to be
// installed already.
func tierize(b *Bundle, folders []Folder) ([][]string, error) {
	selected := map[string]bool{}
	order := make([]string, 0, len(folders))
	for _, f := range folders {
		selected[f.Release] = true
		order = append(order, f.Release)
	}
	level := map[string]int{}
	var visit func(name string, path []string) (int, error)
	visit = func(name string, path []string) (int, error) {
		if l, ok := level[name]; ok {
			return l, nil
		}
		for _, p := range path {
			if p == name {
				return 0, fmt.Errorf("dependency cycle: %s", strings.Join(append(path, name), " -> "))
			}
		}
		l := 0
		c, _ := b.component(name)
		for _, dep := range c.DependencyRefs {
			if !selected[dep] {
				continue
			}
			dl, err := visit(dep, append(path, name))
			if err != nil {
				return 0, err
			}
			if dl+1 > l {
				l = dl + 1
			}
		}
		level[name] = l
		return l, nil
	}
	max := 0
	for _, n := range order {
		l, err := visit(n, nil)
		if err != nil {
			return nil, err
		}
		if l > max {
			max = l
		}
	}
	tiers := make([][]string, max+1)
	for _, n := range order {
		tiers[level[n]] = append(tiers[level[n]], n)
	}
	return tiers, nil
}

func describe(b *Bundle, o Options, tier int, tiers [][]string) string {
	var sb strings.Builder
	if o.Description != "" {
		sb.WriteString(o.Description + " ")
	}
	c := b.Recipe.Criteria
	fmt.Fprintf(&sb, "Generated from NVIDIA AICR %s recipe (service=%s, accelerator=%s, os=%s, intent=%s).",
		b.Recipe.Metadata.Version, c["service"], c["accelerator"], c["os"], c["intent"])
	if len(tiers) > 1 {
		fmt.Fprintf(&sb, " Tier %d of %d: %s.", tier+1, len(tiers), strings.Join(tiers[tier], ", "))
		if tier > 0 {
			fmt.Fprintf(&sb, " Install after tier %d is Running.", tier)
		}
	}
	return sb.String()
}

// blueprintObjectName follows AI Factory's <slug>-<version with dashes>.
func blueprintObjectName(slug, version string) string {
	return naming.TruncateDNS1123Label(slug+"-"+naming.Slugify(version), validation.DNS1123SubdomainMaxLength)
}

func flatten(tiers [][]string) []string {
	var out []string
	for _, t := range tiers {
		out = append(out, t...)
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
