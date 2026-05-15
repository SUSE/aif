package apps

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestApp_JSONTags_MatchesArchitectureSchema pins the wire-format keys
// of the App type to the camelCase schema documented in
// ARCHITECTURE.md §5 Apps. Without the JSON tags retrofitted by P2-4,
// default Go marshaling would emit ID, Name, DisplayName … and break
// the API contract.
func TestApp_JSONTags_MatchesArchitectureSchema(t *testing.T) {
	a := App{
		ID:                 "nvidia.nim-llm:1.0.0",
		Name:               "nim-llm",
		DisplayName:        "NIM LLM",
		Description:        "an LLM",
		Publisher:          "NVIDIA",
		Version:            "1.0.0",
		LogoURL:            "https://example.com/logo.png",
		Source:             "nvidia",
		AssetType:          "chart",
		Categories:         []string{"llm"},
		Tags:               []string{"gpu"},
		ChartRef:           ChartRef{Repo: "oci://r", Chart: "nim-llm", Version: "1.0.0"},
		ProjectURL:         "https://nvidia.com",
		ReferenceBlueprint: false,
		LastUpdatedAt:      func() *time.Time { t := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC); return &t }(),
	}

	out, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)

	// Each documented camelCase key must be present in the output.
	wantKeys := []string{
		`"id"`,
		`"name"`,
		`"displayName"`,
		`"description"`,
		`"publisher"`,
		`"version"`,
		`"logoURL"`,
		`"source"`,
		`"assetType"`,
		`"categories"`,
		`"tags"`,
		`"chartRef"`,
		`"projectURL"`,
		`"referenceBlueprint"`,
		`"lastUpdatedAt"`,
	}
	for _, k := range wantKeys {
		if !strings.Contains(got, k) {
			t.Errorf("missing key %s in marshaled App: %s", k, got)
		}
	}

	// Default Go field names MUST NOT appear (they would mean the JSON
	// tag was forgotten on that field).
	forbidden := []string{
		`"ID":`,
		`"Name":`,
		`"DisplayName":`,
		`"LogoURL":`,
		`"AssetType":`,
		`"ChartRef":`,
		`"ProjectURL":`,
		`"ReferenceBlueprint":`,
		`"LastUpdatedAt":`,
	}
	for _, k := range forbidden {
		if strings.Contains(got, k) {
			t.Errorf("found default Go-field name %s in marshaled App — JSON tag missing: %s", k, got)
		}
	}
}

func TestChartRef_JSONTags(t *testing.T) {
	r := ChartRef{Repo: "oci://r", Chart: "c", Version: "1.0"}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)
	for _, k := range []string{`"repo"`, `"chart"`, `"version"`} {
		if !strings.Contains(got, k) {
			t.Errorf("missing key %s in ChartRef: %s", k, got)
		}
	}
	for _, k := range []string{`"Repo":`, `"Chart":`, `"Version":`} {
		if strings.Contains(got, k) {
			t.Errorf("found default Go-field name %s — JSON tag missing: %s", k, got)
		}
	}
}

func TestApp_JSONRoundTrip_PreservesAllFields(t *testing.T) {
	want := App{
		ID:                 "suse.ollama:0.4.1",
		Name:               "ollama",
		DisplayName:        "Ollama",
		Description:        "Local LLM runtime",
		Publisher:          "Ollama Inc",
		Version:            "0.4.1",
		LogoURL:            "https://example.com/ollama.png",
		Source:             "suse",
		AssetType:          "chart",
		Categories:         []string{"AI", "Inference"},
		Tags:               []string{"local"},
		ChartRef:           ChartRef{Repo: "oci://dp", Chart: "ollama", Version: "0.4.1"},
		ProjectURL:         "https://ollama.com",
		ReferenceBlueprint: true,
		LastUpdatedAt:      func() *time.Time { t := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC); return &t }(),
	}
	bytes, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got App
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name || got.DisplayName != want.DisplayName ||
		got.LogoURL != want.LogoURL || got.AssetType != want.AssetType ||
		got.ProjectURL != want.ProjectURL || got.ReferenceBlueprint != want.ReferenceBlueprint {
		t.Errorf("round-trip mismatch:\nwant=%+v\n got=%+v", want, got)
	}
	if got.ChartRef.Repo != want.ChartRef.Repo {
		t.Errorf("round-trip ChartRef mismatch: want=%+v got=%+v", want.ChartRef, got.ChartRef)
	}
	if (got.LastUpdatedAt == nil) != (want.LastUpdatedAt == nil) ||
		(got.LastUpdatedAt != nil && !got.LastUpdatedAt.Equal(*want.LastUpdatedAt)) {
		t.Errorf("round-trip LastUpdatedAt mismatch: want=%v got=%v", want.LastUpdatedAt, got.LastUpdatedAt)
	}
}

func TestParseTimePtr_ValidRFC3339Nano(t *testing.T) {
	got := parseTimePtr(nil, "2026-04-30T23:56:07.607227Z")
	if got == nil {
		t.Fatal("expected non-nil *time.Time")
	}
	if got.Year() != 2026 || got.Month() != time.April || got.Day() != 30 {
		t.Errorf("unexpected date: %v", got)
	}
}

func TestParseTimePtr_EmptyString(t *testing.T) {
	if got := parseTimePtr(nil, ""); got != nil {
		t.Errorf("expected nil for empty string, got %v", got)
	}
}

func TestParseTimePtr_Malformed(t *testing.T) {
	if got := parseTimePtr(nil, "not-a-date"); got != nil {
		t.Errorf("expected nil for malformed input, got %v", got)
	}
}

func TestParseTimePtr_RFC3339NoFraction(t *testing.T) {
	got := parseTimePtr(nil, "2026-03-04T10:05:02Z")
	if got == nil {
		t.Fatal("expected non-nil *time.Time")
	}
	if got.Second() != 2 {
		t.Errorf("unexpected second: %d", got.Second())
	}
}
