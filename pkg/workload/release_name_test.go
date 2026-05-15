package workload

import "testing"

func TestComposeReleaseName_HappyPath(t *testing.T) {
	got := ComposeReleaseName("wid-abc", "vector-db")
	want := "wid-abc-vector-db"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComposeReleaseName_TruncatesTo53Chars(t *testing.T) {
	id := "very-long-workload-id-that-exceeds-the-helm-limit"
	comp := "and-here-is-the-component-name-too"
	got := ComposeReleaseName(id, comp)
	if len(got) > 53 {
		t.Errorf("len=%d, want ≤53; got=%q", len(got), got)
	}
}

func TestComposeReleaseName_DnsSanitizesUppercase(t *testing.T) {
	got := ComposeReleaseName("WID", "Comp")
	want := "wid-comp"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComposeReleaseName_DnsSanitizesDots(t *testing.T) {
	got := ComposeReleaseName("wid", "comp.v1")
	want := "wid-comp-v1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComposeReleaseName_DnsSanitizesUnderscores(t *testing.T) {
	got := ComposeReleaseName("wid", "comp_name")
	want := "wid-comp-name"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComposeReleaseName_StripsLeadingTrailingHyphens(t *testing.T) {
	got := ComposeReleaseName("wid", "-comp-")
	want := "wid-comp"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComposeReleaseName_TruncationDoesNotEndInHyphen(t *testing.T) {
	// Build a name that, when truncated at 53, would naturally end with '-'.
	// Verify we strip the trailing hyphen post-truncation.
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 46 chars
	comp := "-extra"
	got := ComposeReleaseName(id, comp)
	if len(got) > 53 {
		t.Errorf("len=%d, want ≤53", len(got))
	}
	if len(got) > 0 && got[len(got)-1] == '-' {
		t.Errorf("got %q ends in hyphen", got)
	}
}
