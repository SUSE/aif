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

// Package autovalues finds and rewrites the automatic-value sentinels a
// Blueprint may place in component Helm values:
//
//	aif:auto-domain:<label>   -> <label>.<ingress-ip>.sslip.io
//	aif:auto-ingress-class    -> the target cluster's default IngressClass
//
// Only string values reached through nested maps are recognised. Sentinels
// inside lists, or under a map key containing ".", are skipped because they
// have no unambiguous dot path; ContainsSentinel lets callers reject them.
package autovalues

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"dario.cat/mergo"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const (
	DomainPrefix      = "aif:auto-domain:"
	IngressClassToken = "aif:auto-ingress-class"

	// ResolvedValue.Source values.
	SourceAuto = "auto"
	SourceUser = "user"
)

type Kind string

const (
	KindDomain       Kind = "domain"
	KindIngressClass Kind = "ingressClass"
)

type Sentinel struct {
	Path  string
	Kind  Kind
	Label string
}

func parse(v any) (Sentinel, bool) {
	s, ok := v.(string)
	if !ok {
		return Sentinel{}, false
	}
	if s == IngressClassToken {
		return Sentinel{Kind: KindIngressClass}, true
	}
	if label, ok := strings.CutPrefix(s, DomainPrefix); ok {
		return Sentinel{Kind: KindDomain, Label: label}, true
	}
	return Sentinel{}, false
}

func IsSentinel(v any) bool {
	_, ok := parse(v)
	return ok
}

// Find returns every sentinel in values, sorted by Path.
func Find(values map[string]any) []Sentinel {
	var out []Sentinel
	var walk func(m map[string]any, prefix string)
	walk = func(m map[string]any, prefix string) {
		for k, v := range m {
			if strings.Contains(k, ".") {
				continue
			}
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			if sub, ok := v.(map[string]any); ok {
				walk(sub, p)
				continue
			}
			if s, ok := parse(v); ok {
				s.Path = p
				out = append(out, s)
			}
		}
	}
	walk(values, "")
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ContainsSentinel reports whether any sentinel string remains anywhere in v,
// including inside lists and under dotted keys.
func ContainsSentinel(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for _, e := range t {
			if ContainsSentinel(e) {
				return true
			}
		}
	case []any:
		for _, e := range t {
			if ContainsSentinel(e) {
				return true
			}
		}
	default:
		return IsSentinel(v)
	}
	return false
}

func Get(values map[string]any, path string) (any, bool) {
	var cur any = values
	for _, k := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[k]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// Set writes v at path, creating (or replacing non-map) intermediate values.
func Set(values map[string]any, path string, v any) {
	keys := strings.Split(path, ".")
	m := values
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = v
}

// MergedValues deep-merges the first override matching c.ChartName onto the
// component's Blueprint values (mergo.WithOverride: scalars replaced, maps
// merged key-by-key, slices replaced wholesale), as Helm layers -f files.
func MergedValues(c aiplatformv1alpha1.BlueprintComponent, overrides []aiplatformv1alpha1.ComponentValueOverride) (map[string]any, error) {
	vals := map[string]any{}
	if c.Values != nil {
		if err := json.Unmarshal(c.Values.Raw, &vals); err != nil {
			return nil, fmt.Errorf("unmarshal blueprint values for %s: %w", c.ChartName, err)
		}
	}
	for _, ov := range overrides {
		if ov.ComponentName != c.ChartName || ov.Values == nil {
			continue
		}
		override := map[string]any{}
		if err := json.Unmarshal(ov.Values.Raw, &override); err != nil {
			return nil, fmt.Errorf("unmarshal component override for %s: %w", c.ChartName, err)
		}
		if err := mergo.Merge(&vals, override, mergo.WithOverride); err != nil {
			return nil, fmt.Errorf("merge component override for %s: %w", c.ChartName, err)
		}
		break
	}
	return vals, nil
}
