package catalog

import (
	"strings"
	"testing"
)

func slugs(items []Item) map[string]Item {
	m := make(map[string]Item, len(items))
	for _, it := range items {
		m[it.SlugName] = it
	}
	return m
}

// The embedded bundled catalog is non-empty and every entry is libraried and valid.
func TestBundled(t *testing.T) {
	b := Bundled()
	if len(b) == 0 {
		t.Fatal("bundled catalog is empty")
	}
	if _, ok := slugs(b)["milvus"]; !ok {
		t.Fatalf("bundled catalog missing 'milvus'; got %d items", len(b))
	}
	for _, it := range b {
		if it.Name == "" || it.SlugName == "" || it.Library == "" {
			t.Fatalf("invalid bundled item: %+v", it)
		}
	}
}

func TestNormalize_LibraryKeyed(t *testing.T) {
	raw := []byte(`{"suse-ai":[{"name":"Zeta","slug_name":"zeta"},{"name":"Alpha","slug_name":"alpha"}],"custom":[{"name":"Cee","slug_name":"cee","library":"override"}]}`)
	got := Normalize(raw)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d: %+v", len(got), got)
	}
	m := slugs(got)
	if m["zeta"].Library != "suse-ai" || m["alpha"].Library != "suse-ai" {
		t.Fatalf("library not stamped from key: %+v", got)
	}
	// An entry's own library wins over the key.
	if m["cee"].Library != "override" {
		t.Fatalf("entry library should override key: %+v", m["cee"])
	}
	// Sorted by (library, name): custom, override... here libraries are custom-key
	// "override" (from entry) and "suse-ai". Order = override < suse-ai; within
	// suse-ai, alpha before zeta.
	if got[len(got)-1].SlugName != "zeta" {
		t.Fatalf("expected zeta last (name-sorted within suse-ai): %+v", got)
	}
}

func TestNormalize_FlatArray(t *testing.T) {
	raw := []byte(`[{"name":"Solo","slug_name":"solo","library":"nvidia"}]`)
	got := Normalize(raw)
	if len(got) != 1 || got[0].Library != "nvidia" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestNormalize_ItemsWrapper(t *testing.T) {
	raw := []byte(`{"items":[{"name":"A","slug_name":"a","library":"x"}]}`)
	got := Normalize(raw)
	if len(got) != 1 || got[0].SlugName != "a" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestNormalize_DropsInvalid(t *testing.T) {
	raw := []byte(`[
		{"name":"Good","slug_name":"good","library":"x"},
		{"name":"NoSlug","library":"x"},
		{"slug_name":"noname","library":"x"},
		{"name":"BadFmt","slug_name":"bad","packaging_format":"ZIP","library":"x"}
	]`)
	got := Normalize(raw)
	if len(got) != 1 || got[0].SlugName != "good" {
		t.Fatalf("want only 'good', got %+v", got)
	}
}

func TestNormalize_InvalidJSON(t *testing.T) {
	if got := Normalize([]byte("not json")); got != nil {
		t.Fatalf("want nil for invalid JSON, got %+v", got)
	}
	if got := Normalize([]byte(`[]`)); got != nil {
		t.Fatalf("want nil for empty array, got %+v", got)
	}
}

func TestNormalize_LabelsRoundTrip(t *testing.T) {
	raw := []byte(`[{"name":"NIM","slug_name":"nim","library":"nvidia","labels":[{"code":"nvaie_supported","name":"NVIDIA AI Enterprise Supported"},{"code":"nv-ai-enterprise","name":"NVIDIA AI Enterprise Essentials"}]}]`)
	got := Normalize(raw)
	if len(got) != 1 {
		t.Fatalf("want 1 item, got %d: %+v", len(got), got)
	}
	labels := got[0].Labels
	if len(labels) != 2 {
		t.Fatalf("want 2 labels, got %d: %+v", len(labels), labels)
	}
	if labels[0].Code != "nvaie_supported" || labels[0].Name != "NVIDIA AI Enterprise Supported" {
		t.Fatalf("unexpected first label: %+v", labels[0])
	}
}

func TestNormalize_DropsEmptyLabels(t *testing.T) {
	raw := []byte(`[{"name":"NIM","slug_name":"nim","library":"nvidia","labels":[{"code":"","name":""},{"code":"nvaie_supported","name":"NVIDIA AI Enterprise Supported"}]}]`)
	got := Normalize(raw)
	if len(got) != 1 {
		t.Fatalf("want 1 item, got %d", len(got))
	}
	if len(got[0].Labels) != 1 || got[0].Labels[0].Code != "nvaie_supported" {
		t.Fatalf("empty label not dropped: %+v", got[0].Labels)
	}
}

func TestNormalize_NoLabelsField(t *testing.T) {
	raw := []byte(`[{"name":"Milvus","slug_name":"milvus","library":"suse-ai"}]`)
	got := Normalize(raw)
	if len(got) != 1 || got[0].Labels != nil {
		t.Fatalf("want nil labels, got %+v", got[0].Labels)
	}
}

// RepresentativeChart returns a stable, supported sample chart actually served by
// a repository, and "" for a repository absent from the bundled catalog.
func TestRepresentativeChart(t *testing.T) {
	if got := RepresentativeChart(""); got != "" {
		t.Fatalf("empty URL: want \"\", got %q", got)
	}
	if got := RepresentativeChart("oci://mirror.internal/unknown"); got != "" {
		t.Fatalf("unknown repo: want \"\", got %q", got)
	}

	norm := func(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }
	supported := func(it Item) bool {
		for _, l := range it.Labels {
			if l.Code == supportedLabelCode {
				return true
			}
		}
		return false
	}
	byRepo := map[string][]Item{}
	for _, it := range Bundled() {
		if u := norm(it.RepositoryURL); u != "" {
			byRepo[u] = append(byRepo[u], it)
		}
	}

	for repo, items := range byRepo {
		got := RepresentativeChart(repo)
		if got == "" {
			t.Errorf("%s: representative chart is empty", repo)
			continue
		}
		if withSlash := RepresentativeChart(repo + "/"); withSlash != got {
			t.Errorf("%s: not slash-insensitive: %q vs %q", repo, got, withSlash)
		}
		var pick *Item
		for i := range items {
			if items[i].SlugName == got {
				pick = &items[i]
				break
			}
		}
		if pick == nil {
			t.Errorf("%s: picked %q which the repo does not serve", repo, got)
			continue
		}
		anySupported := false
		for _, it := range items {
			if supported(it) {
				anySupported = true
				break
			}
		}
		if anySupported && !supported(*pick) {
			t.Errorf("%s: picked unsupported %q while a supported chart exists", repo, got)
		}
	}
}
