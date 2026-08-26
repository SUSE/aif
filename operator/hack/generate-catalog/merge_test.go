package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SUSE/aif-operator/internal/catalog"
)

const (
	multimodalSafetyNIMSlug = "multimodal-safety-nim"
	deepstreamITSSlug       = "deepstream-its"
	nvidiaHelmRepo          = "https://helm.ngc.nvidia.com/nvidia"
	nimHelmRepo             = "https://helm.ngc.nvidia.com/nim/nvidia"
)

const baseCatalog = `{
  "suse-ai": [
    {
      "name":"Milvus",
      "slug_name":"milvus",
      "packaging_format":"HELM_CHART",
      "repository_url":"oci://dp.apps.rancher.io/charts",
      "labels":[{"code":"supported","name":"Supported"}]
    }
  ],
  "nvidia": [
    {
      "name":"GPU Operator",
      "slug_name":"gpu-operator",
      "packaging_format":"HELM_CHART",
      "repository_url":"https://helm.ngc.nvidia.com/nvidia",
      "labels":[{"code":"supported","name":"Supported"}]
    },
    {
      "name":"DeepStream ITS",
      "slug_name":"deepstream-its",
      "packaging_format":"HELM_CHART",
      "repository_url":"https://helm.ngc.nvidia.com/nvidia"
    }
  ]
}`

func TestMergeNVAIE_AppendsNewEntry(t *testing.T) {
	derived := []catalog.Item{{
		Name: "Multimodal Safety NIM", SlugName: multimodalSafetyNIMSlug,
		PackagingFormat: helmChart, RepositoryURL: nimHelmRepo,
	}}
	out, added, err := mergeNVAIE([]byte(baseCatalog), derived)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != multimodalSafetyNIMSlug {
		t.Fatalf("added = %v", added)
	}
	var doc catalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	var found *catalog.Item
	for i := range doc.NVIDIA {
		if doc.NVIDIA[i].SlugName == multimodalSafetyNIMSlug {
			found = &doc.NVIDIA[i]
		}
	}
	if found == nil {
		t.Fatal("new entry not appended")
	}
	if len(found.Labels) != 1 || found.Labels[0] != (catalog.Label{Code: supportedCode, Name: "Supported"}) {
		t.Fatalf("new entry label = %+v", found.Labels)
	}
	// suse-ai untouched and still first in output.
	if len(doc.SuseAI) != 1 || doc.SuseAI[0].SlugName != "milvus" {
		t.Fatalf("suse-ai changed: %+v", doc.SuseAI)
	}
	if strings.Index(string(out), `"suse-ai"`) > strings.Index(string(out), `"nvidia"`) {
		t.Fatal("top-level key order must stay suse-ai then nvidia")
	}
}

func TestMergeNVAIE_PreservesExistingFields(t *testing.T) {
	// A derived entry matching an existing demo entry (deepstream-its) must set its
	// label to Supported but leave name/description/repo untouched.
	derived := []catalog.Item{{
		Name: "IGNORED", SlugName: deepstreamITSSlug, Description: "IGNORED",
		PackagingFormat: helmChart, RepositoryURL: nvidiaHelmRepo,
	}}
	out, added, err := mergeNVAIE([]byte(baseCatalog), derived)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatalf("matched entry must not be counted as added: %v", added)
	}
	var doc catalogDoc
	_ = json.Unmarshal(out, &doc)
	for _, e := range doc.NVIDIA {
		if e.SlugName == deepstreamITSSlug {
			if e.Name != "DeepStream ITS" {
				t.Fatalf("existing Name overwritten: %q", e.Name)
			}
			if len(e.Labels) != 1 || e.Labels[0].Code != supportedCode {
				t.Fatalf("Supported label not set: %+v", e.Labels)
			}
		}
	}
}

func TestMergeNVAIE_DeduplicatesSlugPreferNim(t *testing.T) {
	// The same chart name is published under two NGC repos (nim/nvidia and
	// nvidia/nemo-microservices). matchKey keeps them distinct, but they collide
	// on slug_name, which the UI keys tiles/routing on. Dedupe must keep exactly
	// one entry per slug, preferring the nim/nvidia repo.
	const slug = "nvidia-nim-llama-32-nv-embedqa-1b-v2"
	derived := []catalog.Item{
		{
			Name: "NVIDIA NIM for Text Embedding", SlugName: slug,
			PackagingFormat: helmChart,
			RepositoryURL:   "https://helm.ngc.nvidia.com/nvidia/nemo-microservices",
		},
		{
			Name: "NVIDIA NIM for Text Embedding", SlugName: slug,
			PackagingFormat: helmChart, RepositoryURL: nimHelmRepo,
		},
	}
	out, added, err := mergeNVAIE([]byte(baseCatalog), derived)
	if err != nil {
		t.Fatal(err)
	}
	var doc catalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	var matches []catalog.Item
	for _, e := range doc.NVIDIA {
		if e.SlugName == slug {
			matches = append(matches, e)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly 1 entry for slug %q, got %d", slug, len(matches))
	}
	if matches[0].RepositoryURL != nimHelmRepo {
		t.Fatalf("dedupe kept the wrong repo: %q (want nim/nvidia)", matches[0].RepositoryURL)
	}
	// The slug must appear at most once in the added list.
	n := 0
	for _, s := range added {
		if s == slug {
			n++
		}
	}
	if n > 1 {
		t.Fatalf("slug counted %d times in added; want <=1", n)
	}
}

func TestMergeNVAIE_DedupesPreexistingCatalogDuplicate(t *testing.T) {
	// A catalog that already contains a slug collision (e.g. committed before
	// dedupe existed) must be cleaned even when no derived entry touches it.
	const dupCatalog = `{
  "suse-ai": [],
  "nvidia": [
    {
      "name":"Dup","slug_name":"dup","packaging_format":"HELM_CHART",
      "repository_url":"https://helm.ngc.nvidia.com/nvidia/nemo-microservices",
      "labels":[{"code":"supported","name":"Supported"}]
    },
    {
      "name":"Dup","slug_name":"dup","packaging_format":"HELM_CHART",
      "repository_url":"https://helm.ngc.nvidia.com/nim/nvidia",
      "labels":[{"code":"supported","name":"Supported"}]
    }
  ]
}`
	out, _, err := mergeNVAIE([]byte(dupCatalog), nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc catalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.NVIDIA) != 1 {
		t.Fatalf("want 1 deduped entry, got %d", len(doc.NVIDIA))
	}
	if doc.NVIDIA[0].RepositoryURL != nimHelmRepo {
		t.Fatalf("dedupe kept wrong repo: %q", doc.NVIDIA[0].RepositoryURL)
	}
}

func TestMergeNVAIE_ExistingSlugUnderNewRepoNotReportedAdded(t *testing.T) {
	// The catalog already has a chart (slug "embed") under nim/nvidia. NGC also
	// publishes the same chart under nvidia/nemo-microservices, so the derived set
	// carries a same-slug/different-repo copy. That copy has no repo+slug match, is
	// appended, then dropped by dedupe (nim/nvidia wins). Since the slug already
	// existed, the merge must NOT report it as newly added — "added" tracks slug
	// novelty (what the UI keys on), not repo+slug novelty.
	const slug = "embed"
	catalogWithSlug := `{
  "suse-ai": [],
  "nvidia": [
    {
      "name":"Embed","slug_name":"embed","packaging_format":"HELM_CHART",
      "repository_url":"https://helm.ngc.nvidia.com/nim/nvidia",
      "labels":[{"code":"supported","name":"Supported"}]
    }
  ]
}`
	derived := []catalog.Item{{
		Name: "Embed", SlugName: slug, PackagingFormat: helmChart,
		RepositoryURL: "https://helm.ngc.nvidia.com/nvidia/nemo-microservices",
	}}
	out, added, err := mergeNVAIE([]byte(catalogWithSlug), derived)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatalf("pre-existing slug must not be reported as added, got %v", added)
	}
	var doc catalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.NVIDIA) != 1 || doc.NVIDIA[0].RepositoryURL != nimHelmRepo {
		t.Fatalf("want 1 entry under nim/nvidia, got %+v", doc.NVIDIA)
	}
}

func TestMergeNVAIE_Idempotent(t *testing.T) {
	derived := []catalog.Item{{
		Name: "Multimodal Safety NIM", SlugName: multimodalSafetyNIMSlug,
		PackagingFormat: helmChart, RepositoryURL: nimHelmRepo,
	}}
	out1, _, err := mergeNVAIE([]byte(baseCatalog), derived)
	if err != nil {
		t.Fatal(err)
	}
	out2, added, err := mergeNVAIE(out1, derived)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Fatalf("second run must add nothing, added %v", added)
	}
	if string(out1) != string(out2) {
		t.Fatal("second run produced a different document (not idempotent)")
	}
}
