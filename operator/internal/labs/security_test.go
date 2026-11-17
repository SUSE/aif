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

var allow = []string{"evil", "x", "r"}

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
	entries, skipped, msg, err := ParseExternal([]byte(doc), allow)
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
	if _, _, _, err := ParseExternal([]byte("nope"), allow); err == nil {
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
	entries, skipped, msg, err := ParseExternal([]byte(b.String()), allow)
	if err != nil || len(entries) != MaxEntries || skipped != 5 || !strings.Contains(msg, "more than") {
		t.Fatalf("entries=%d skipped=%d msg=%q err=%v", len(entries), skipped, msg, err)
	}
}

func TestParseExternalRequiresAllowlist(t *testing.T) {
	if _, _, _, err := ParseExternal([]byte("[]"), nil); err != ErrNoAllowlist {
		t.Fatalf("err=%v", err)
	}
}

// One malformed entry must be skipped with a reason, not reach the status write
// (where the CRD rules would reject the whole update).
func TestParseExternalValidatesEachEntry(t *testing.T) {
	ext := `"extension":{"name":"aif-lab-x","version":"1"}`
	bad := map[string]string{
		"no-helm":    `{"name":"no-helm","source":{"kind":"Helm"},` + ext + `}`,
		"http-url":   `{"name":"http-url","source":{"kind":"Helm","helm":{"chartURL":"http://x/aif-lab-x","version":"1"}},` + ext + `}`,
		"no-version": `{"name":"no-version","source":{"kind":"Helm","helm":{"chartURL":"oci://x/aif-lab-x","version":""}},` + ext + `}`,
		"no-ext-ver": `{"name":"no-ext-ver","source":{"kind":"Helm","helm":{"chartURL":"oci://x/aif-lab-x","version":"1"}},"extension":{"name":"aif-lab-x"}}`,
		"maturity":   `{"name":"maturity","maturity":"ga","source":{"kind":"Helm","helm":{"chartURL":"oci://x/aif-lab-x","version":"1"}},` + ext + `}`,
		"git":        `{"name":"git","source":{"kind":"Git","git":{"repo":"https://x/r","branch":"gh-pages"}},` + ext + `}`,
		"host":       `{"name":"host","source":{"kind":"Helm","helm":{"chartURL":"oci://other.io/aif-lab-x","version":"1"}},` + ext + `}`,
	}
	good := `{"name":"good","source":{"kind":"Helm","helm":{"chartURL":"oci://x/aif-lab-x","version":"1","values":{"image":{"repository":"evil.io/x"}}}},` + ext + `}`
	for name, e := range bad {
		entries, skipped, msg, err := ParseExternal([]byte("["+e+","+good+"]"), allow)
		if err != nil || skipped != 1 || len(entries) != 1 || entries[0].Name != "good" || !strings.Contains(msg, name) {
			t.Errorf("%s: entries=%v skipped=%d msg=%q err=%v", name, entries, skipped, msg, err)
			continue
		}
		if entries[0].Source.Helm.Values != nil {
			t.Errorf("values not stripped")
		}
	}
}

func TestHostAllowed(t *testing.T) {
	if !HostAllowed([]string{"GHCR.io"}, "oci://ghcr.io/suse/chart/x") || !HostAllowed([]string{"r:5000"}, "oci://r:5000/x") ||
		!HostAllowed([]string{"r"}, "oci://r:5000/x") || HostAllowed([]string{"ghcr.io"}, "oci://evil.io/x") || HostAllowed(nil, "oci://r/x") {
		t.Error("host match")
	}
}
