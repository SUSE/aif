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

package autovalues

import (
	"encoding/json"
	"reflect"
	"testing"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFind(t *testing.T) {
	vals := mustJSON(t, `{"global":{"domain":"aif:auto-domain:runai","ingress":{"ingressClass":"aif:auto-ingress-class"}},"x":"y"}`)
	got := Find(vals)
	want := []Sentinel{
		{Path: "global.domain", Kind: KindDomain, Label: "runai"},
		{Path: "global.ingress.ingressClass", Kind: KindIngressClass, Label: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Find = %+v, want %+v", got, want)
	}
}

func TestFind_IgnoresNonStringsAndLists(t *testing.T) {
	vals := mustJSON(t, `{"n":1,"b":true,"list":["aif:auto-ingress-class",{"d":"aif:auto-domain:x"}],"nil":null}`)
	if got := Find(vals); len(got) != 0 {
		t.Fatalf("Find = %+v, want none", got)
	}
}

func TestFind_SkipsDottedKeys(t *testing.T) {
	vals := mustJSON(t, `{"annotations":{"external-dns.alpha.kubernetes.io/hostname":"aif:auto-domain:x"}}`)
	if got := Find(vals); len(got) != 0 {
		t.Fatalf("Find = %+v, want none", got)
	}
}

func TestContainsSentinel(t *testing.T) {
	for s, want := range map[string]bool{
		`{"a":{"b.c":"aif:auto-domain:x"}}`:      true,
		`{"a":[{"b":"aif:auto-ingress-class"}]}`: true,
		`{"a":["x",1,null],"b":{"c":"y"}}`:       false,
	} {
		if got := ContainsSentinel(mustJSON(t, s)); got != want {
			t.Errorf("ContainsSentinel(%s) = %v, want %v", s, got, want)
		}
	}
}

func TestIsSentinel(t *testing.T) {
	for v, want := range map[any]bool{
		"aif:auto-domain:runai":  true,
		"aif:auto-domain:":       true,
		"aif:auto-ingress-class": true,
		"aif:auto-ingress":       false,
		"runai.example.com":      false,
		42:                       false,
	} {
		if got := IsSentinel(v); got != want {
			t.Errorf("IsSentinel(%v) = %v, want %v", v, got, want)
		}
	}
}

func TestGetSet(t *testing.T) {
	vals := map[string]any{"a": map[string]any{"x": "keep"}}
	Set(vals, "a.b.c", "v")
	got, ok := Get(vals, "a.b.c")
	if !ok || got != "v" {
		t.Fatalf("Get(a.b.c) = %v, %v", got, ok)
	}
	if x, _ := Get(vals, "a.x"); x != "keep" {
		t.Fatalf("Set clobbered sibling: %v", vals)
	}
	if _, ok := Get(vals, "a.missing.c"); ok {
		t.Fatal("Get on missing path returned ok")
	}
	if _, ok := Get(vals, "a.x.deeper"); ok {
		t.Fatal("Get through a scalar returned ok")
	}
}

func TestMergedValues(t *testing.T) {
	c := aiplatformv1alpha1.BlueprintComponent{
		ChartName: "cp",
		Values:    &apixv1.JSON{Raw: []byte(`{"global":{"domain":"aif:auto-domain:runai","replicas":1},"list":[1,2]}`)},
	}
	overrides := []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "other", Values: &apixv1.JSON{Raw: []byte(`{"global":{"replicas":9}}`)}},
		{ComponentName: "cp", Values: &apixv1.JSON{Raw: []byte(`{"global":{"domain":"runai.example.com"},"list":[3]}`)}},
		{ComponentName: "cp", Values: &apixv1.JSON{Raw: []byte(`{"global":{"replicas":7}}`)}},
	}
	got, err := MergedValues(c, overrides)
	if err != nil {
		t.Fatal(err)
	}
	want := mustJSON(t, `{"global":{"domain":"runai.example.com","replicas":1},"list":[3]}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MergedValues = %v, want %v", got, want)
	}

	got, err = MergedValues(aiplatformv1alpha1.BlueprintComponent{ChartName: "none"}, nil)
	if err != nil || len(got) != 0 || got == nil {
		t.Fatalf("MergedValues(no values) = %v, %v; want empty non-nil map", got, err)
	}
}
