// Package aicrimport converts an NVIDIA AI Cluster Runtime (AICR) bundle into
// an AI Factory blueprint catalog: Blueprint objects, a BlueprintCatalog, and
// the Settings.spec.customRepos entries their charts are pulled from.
//
// The input is the per-component folder layout `aicr bundle` writes
// (NNN-<release>/upstream.env + values.yaml [+ cluster-values.yaml]) plus the
// resolved recipe.yaml at the bundle root. AICR freezes that layout, so the
// importer depends on nothing else.
package aicrimport

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// folderRE matches AICR's NNN-<release> component folders.
var folderRE = regexp.MustCompile(`^(\d{3})-(.+)$`)

// Recipe is the subset of AICR's resolved recipe.yaml the importer reads.
type Recipe struct {
	Criteria      map[string]string `yaml:"criteria"`
	ComponentRefs []ComponentRef    `yaml:"componentRefs"`
	Metadata      struct {
		Version         string   `yaml:"version"`
		AppliedOverlays []string `yaml:"appliedOverlays"`
	} `yaml:"metadata"`
}

// ComponentRef is one recipe component.
type ComponentRef struct {
	Name           string   `yaml:"name"`
	Namespace      string   `yaml:"namespace"`
	DependencyRefs []string `yaml:"dependencyRefs"`
}

// Folder is one NNN-<release> component folder.
type Folder struct {
	Dir     string
	Release string
	// Local is true for folders carrying their own Chart.yaml (raw manifests
	// AICR wrapped into a chart). They have no chart repository.
	Local bool
	// Repo is the HTTP(S) chart repository, empty for OCI charts.
	Repo string
	// Chart is the chart name, or the full oci:// reference for OCI charts.
	Chart   string
	Version string
	Values  map[string]any
}

// Bundle is a parsed AICR bundle.
type Bundle struct {
	Recipe  Recipe
	Folders []Folder
}

// component returns the recipe component a folder belongs to.
func (b *Bundle) component(release string) (ComponentRef, bool) {
	for _, c := range b.Recipe.ComponentRefs {
		if c.Name == release {
			return c, true
		}
	}
	return ComponentRef{}, false
}

// ReadBundle parses the AICR bundle rooted at dir.
func ReadBundle(dir string) (*Bundle, error) {
	b := &Bundle{}
	data, err := os.ReadFile(filepath.Join(dir, "recipe.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read recipe.yaml (is %s an AICR bundle?): %w", dir, err)
	}
	if err := yaml.Unmarshal(data, &b.Recipe); err != nil {
		return nil, fmt.Errorf("parse recipe.yaml: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		m := folderRE.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		f, err := readFolder(filepath.Join(dir, e.Name()), m[2])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		f.Dir = e.Name()
		b.Folders = append(b.Folders, f)
	}
	sort.Slice(b.Folders, func(i, j int) bool { return b.Folders[i].Dir < b.Folders[j].Dir })
	if len(b.Folders) == 0 {
		return nil, fmt.Errorf("no NNN-<component> folders in %s", dir)
	}
	return b, nil
}

func readFolder(path, release string) (Folder, error) {
	f := Folder{Release: release}
	if _, err := os.Stat(filepath.Join(path, "Chart.yaml")); err == nil {
		f.Local = true
		return f, nil
	}
	env, err := readEnv(filepath.Join(path, "upstream.env"))
	if err != nil {
		return f, err
	}
	f.Repo, f.Chart, f.Version = env["REPO"], env["CHART"], env["VERSION"]
	if f.Chart == "" {
		return f, errors.New("upstream.env has no CHART")
	}
	for _, name := range []string{"values.yaml", "cluster-values.yaml"} {
		vals, err := readValues(filepath.Join(path, name))
		if err != nil {
			return f, err
		}
		f.Values = merge(f.Values, vals)
	}
	return f, nil
}

// readEnv parses AICR's upstream.env (KEY='value' lines).
func readEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	env := map[string]string{}
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		env[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `'"`)
	}
	return env, sc.Err()
}

// readValues returns the first YAML document of path, or nil if it is absent
// or empty.
func readValues(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
		if len(doc) > 0 {
			return doc, nil
		}
	}
}

// merge deep-merges src over dst (later files win, as with repeated helm -f).
func merge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				dst[k] = merge(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}
