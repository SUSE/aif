/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package kustomizeoverlay is the single home for the structural checks on a
// component's Kustomize overlay, shared by the API (rejecting a bad
// install-time overlay at submit time) and the AIWorkload controller (checking
// every overlay, inline or fetched from git, before it is embedded in a Bundle).
package kustomizeoverlay

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// FileNames are the root file names kustomize recognizes.
var FileNames = []string{"kustomization.yaml", "kustomization.yml", "Kustomization"}

// Validate checks an overlay without fetching anything: a valid dir, exactly
// one content source, inline files that stay under dir, and a gitRef naming a
// repo and a path inside it. A nil overlay is valid.
func Validate(k *aiplatformv1alpha1.ComponentKustomize) error {
	if k == nil {
		return nil
	}
	hasFiles, hasGitRef := len(k.Files) > 0, k.GitRef != nil
	switch {
	case hasFiles && hasGitRef:
		return errors.New("set exactly one of files or gitRef")
	case hasFiles:
		return ValidateFiles(k.Dir, k.Files)
	case !hasGitRef:
		return errors.New("set one of files or gitRef")
	}
	if err := ValidateDir(k.Dir); err != nil {
		return err
	}
	if k.GitRef.ChartRepo == "" {
		return errors.New("gitRef.chartRepo is required")
	}
	if k.GitRef.Path == "" {
		return errors.New("gitRef.path is required")
	}
	return checkInsideRoot(path.Clean(k.GitRef.Path))
}

// ValidateDir checks that dir, which becomes the Bundle's spec.kustomize.dir,
// is a clean relative path below the bundle root: no empty, "." or ".."
// segments and no leading or trailing slash. The CRD enforces the same rule.
func ValidateDir(dir string) error {
	if dir == "" {
		return errors.New("dir is required")
	}
	if dir == "." || path.Clean(dir) != dir {
		return fmt.Errorf("dir %q must be a clean relative path (no empty, \".\" or \"..\" segments, no trailing slash)", dir)
	}
	return checkInsideRoot(dir)
}

// ValidateFiles checks an overlay's files keyed by bundle path: dir is valid,
// every file sits under dir (so none can land next to, or shadow, the chart's
// own files), and dir holds a root kustomization file.
func ValidateFiles(dir string, files map[string]string) error {
	if err := ValidateDir(dir); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names) // report the same offending file every time
	prefix := dir + "/"
	for _, name := range names {
		clean := path.Clean(name)
		if err := checkInsideRoot(clean); err != nil {
			return err
		}
		if !strings.HasPrefix(clean, prefix) {
			return fmt.Errorf("file %q is outside overlay dir %q", name, dir)
		}
	}
	if !HasKustomization(files, dir) {
		return fmt.Errorf("no kustomization.yaml under %q", dir)
	}
	return nil
}

// HasKustomization reports whether files contains a kustomize root file
// directly under dir. Fleet's post-render runs `kustomize build <dir>`, which
// fails without one, so it is caught up front with a clear message instead.
func HasKustomization(files map[string]string, dir string) bool {
	for _, name := range FileNames {
		if _, ok := files[dir+"/"+name]; ok {
			return true
		}
	}
	return false
}

// checkInsideRoot rejects a cleaned path that escapes its root.
func checkInsideRoot(name string) error {
	if name == ".." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
		return fmt.Errorf("path %q escapes the bundle root", name)
	}
	return nil
}
