package catalog

import "testing"

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
