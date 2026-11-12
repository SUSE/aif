package labs

import (
	"fmt"
	"strings"
	"testing"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func helmSrc(url string) v1alpha1.ExtensionSource {
	return v1alpha1.ExtensionSource{Kind: v1alpha1.ExtensionSourceKindHelm, Helm: &v1alpha1.HelmSource{ChartURL: url, Version: "1"}}
}

func TestValidateNames(t *testing.T) {
	ok := v1alpha1.ExtensionConfig{Name: "aif-lab-demo", Version: "1"}
	cases := []struct {
		name string
		src  v1alpha1.ExtensionSource
		ext  v1alpha1.ExtensionConfig
		bad  bool
	}{
		{"valid", helmSrc("oci://r/chart/aif-lab-demo"), ok, false},
		{"valid -server suffix", helmSrc("oci://r/chart/aif-lab-demo-server"), ok, false},
		{"core ui chart", helmSrc("oci://r/chart/aif-ui"), ok, true},
		{"core ui server chart", helmSrc("https://r/aif-ui-server"), ok, true},
		{"core ui extension", helmSrc("oci://r/chart/aif-lab-demo"), v1alpha1.ExtensionConfig{Name: "aif-ui", Version: "1"}, true},
		{"other extension", helmSrc("oci://r/chart/aif-lab-demo"), v1alpha1.ExtensionConfig{Name: "kubewarden", Version: "1"}, true},
	}
	for _, c := range cases {
		if err := ValidateNames(c.src, c.ext); (err != nil) != c.bad {
			t.Errorf("%s: err=%v, want error=%v", c.name, err, c.bad)
		}
	}
}

func TestParseExternalSanitizes(t *testing.T) {
	doc := `[
	  {"name":"steal","source":{"kind":"Helm","helm":{"chartURL":"oci://evil/chart/aif-lab-steal","version":"1",
	    "auth":{"basic":{"userSecretRef":{"name":"suse-registry","key":"user"},"tokenSecretRef":{"name":"suse-registry","key":"token"}}},
	    "tls":{"insecureSkipVerify":true,"acknowledgeInsecure":true}}},
	   "extension":{"name":"aif-lab-steal","version":"1"}},
	  {"name":"hijack","source":{"kind":"Helm","helm":{"chartURL":"oci://evil/chart/aif-ui","version":"1"}},
	   "extension":{"name":"aif-ui","version":"1"}},
	  {"name":"steal","source":{"kind":"Helm","helm":{"chartURL":"oci://x/chart/aif-lab-steal","version":"2"}},
	   "extension":{"name":"aif-lab-steal","version":"2"}},
	  {"name":"Bad_Name","source":{"kind":"Helm","helm":{"chartURL":"oci://x/chart/aif-lab-b","version":"1"}},
	   "extension":{"name":"aif-lab-b","version":"1"}}
	]`
	entries, skipped, msg, err := ParseExternal([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "steal" || entries[0].Source.Helm.Version != "1" {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[0].Source.Helm.Auth != nil || entries[0].Source.Helm.TLS != nil {
		t.Errorf("auth/tls not stripped: %+v", entries[0].Source.Helm)
	}
	if skipped != 3 || !strings.Contains(msg, "hijack") {
		t.Errorf("skipped=%d msg=%q", skipped, msg)
	}
	if _, _, _, err := ParseExternal([]byte("nope")); err == nil {
		t.Error("want JSON error")
	}
}

func TestParseExternalCapsEntries(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < MaxEntries+5; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"name":"lab-%d","source":{"kind":"Helm","helm":{"chartURL":"oci://r/chart/aif-lab-%d","version":"1"}},"extension":{"name":"aif-lab-%d","version":"1"}}`, i, i, i)
	}
	b.WriteString("]")
	entries, skipped, msg, err := ParseExternal([]byte(b.String()))
	if err != nil || len(entries) != MaxEntries || skipped != 5 || !strings.Contains(msg, "more than") {
		t.Fatalf("entries=%d skipped=%d msg=%q err=%v", len(entries), skipped, msg, err)
	}
}
