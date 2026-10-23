package aiworkload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// etcdMaxRequestBytes is etcd's default request limit (1.5 MiB). A Bundle bigger than this is
// rejected by the API server with "etcdserver: request is too large".
const etcdMaxRequestBytes = 3 << 19

// decodeBase64Gz does what Fleet does with a Bundle resource whose encoding is "base64+gz".
func decodeBase64Gz(t *testing.T, content string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return out
}

func storedPayload(res []any) int {
	n := 0
	for _, r := range res {
		m := r.(map[string]any)
		n += len(m["content"].(string)) + len(m["name"].(string))
	}
	return n
}

func TestChartTgzToBundleResources_StoresEveryFileAsBase64Gz(t *testing.T) {
	files := map[string]string{
		"c/Chart.yaml":          "apiVersion: v2\nname: c\nversion: 1.0.0\n",
		"c/values.yaml":         "name: café ✓\n",                             // non-ASCII text
		"c/files/blob.bin":      string([]byte{0xff, 0xfe, 0x00, 0x01, 0xff}), // not valid UTF-8
		"c/templates/empty.txt": "",
	}
	res, dir, err := chartTgzToBundleResources(makeChartTgz(t, files))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "c" || len(res) != len(files) {
		t.Fatalf("dir=%q resources=%d, want c and %d", dir, len(res), len(files))
	}
	for name, want := range files {
		r, ok := resourceByName(res, name)
		if !ok {
			t.Fatalf("resource %s missing", name)
		}
		if r["encoding"] != "base64+gz" {
			t.Fatalf("%s encoding = %v, want base64+gz", name, r["encoding"])
		}
		if got := decodeBase64Gz(t, r["content"].(string)); string(got) != want {
			t.Fatalf("%s did not round-trip: got %q, want %q", name, got, want)
		}
	}
}

// A chart bigger than the budget when plain, but small once compressed, now fits. This is the
// alchemistudio-platform case (2.0 MB unpacked, 0.43 MB stored).
func TestChartTgzToBundleResources_CompressibleChartLargerThanBudgetFits(t *testing.T) {
	files := map[string]string{"c/Chart.yaml": "apiVersion: v2\nname: c\nversion: 1.0.0\n"}
	rawTotal := 0
	for i := 0; i < 40; i++ {
		body := strings.Repeat(fmt.Sprintf("key%02d: some repetitive helm template text\n", i), 2500) // ~110 KB each
		files[fmt.Sprintf("c/templates/f%02d.yaml", i)] = body
		rawTotal += len(body)
	}
	if rawTotal <= maxBundleResourcesBytes {
		t.Fatalf("fixture is only %d bytes raw; it must exceed the budget", rawTotal)
	}
	res, _, err := chartTgzToBundleResources(makeChartTgz(t, files))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := storedPayload(res); got > maxBundleResourcesBytes {
		t.Fatalf("stored payload %d exceeds the budget %d", got, maxBundleResourcesBytes)
	}
	for name, want := range files {
		r, _ := resourceByName(res, name)
		if got := decodeBase64Gz(t, r["content"].(string)); string(got) != want {
			t.Fatalf("%s did not round-trip", name)
		}
	}
}

// The Bundle object as the API server receives it must stay under etcd's request limit.
func TestBuildGitChartBundle_ObjectStaysUnderEtcdLimit(t *testing.T) {
	files := map[string]string{"c/Chart.yaml": "apiVersion: v2\nname: c\nversion: 1.0.0\n"}
	for i := 0; i < 165; i++ {
		files[fmt.Sprintf("c/templates/f%03d.yaml", i)] = strings.Repeat(fmt.Sprintf("line %03d of a repetitive template\n", i), 400)
	}
	b, err := buildGitChartBundle("wl-c", "ns", "fp", makeChartTgz(t, files),
		aiplatformv1alpha1.BlueprintComponent{ChartName: "c"}, nil, []any{map[string]any{"clusterName": "local"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, err := json.Marshal(b.Object)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) >= etcdMaxRequestBytes {
		t.Fatalf("Bundle object is %d bytes, over etcd's %d request limit", len(body), etcdMaxRequestBytes)
	}
	t.Logf("Bundle object: %d bytes (etcd limit %d)", len(body), etcdMaxRequestBytes)
}

// Optional: run the real chart without a cluster.
//
//	ALCHEMI_CHART_TGZ=/path/to/alchemistudio-platform-1.0.25.tgz \
//	  go test ./internal/controller/aiworkload/ -run RealChart -v
func TestChartTgzToBundleResources_RealChart(t *testing.T) {
	path := os.Getenv("ALCHEMI_CHART_TGZ")
	if path == "" {
		t.Skip("ALCHEMI_CHART_TGZ not set")
	}
	tgz, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Expected content of every file, read straight from the archive.
	want := map[string]string{}
	gz, _ := gzip.NewReader(bytes.NewReader(tgz))
	tr := tar.NewReader(gz)
	raw := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		data, _ := io.ReadAll(tr)
		want[h.Name] = string(data)
		raw += len(data) + len(h.Name)
	}

	b, err := buildGitChartBundle("wl-real", "alchemi", "fp", tgz,
		aiplatformv1alpha1.BlueprintComponent{ChartName: "alchemistudio-platform"}, nil, []any{map[string]any{"clusterName": "local"}})
	if err != nil {
		if errors.Is(err, errChartTooLarge) {
			t.Fatalf("chart rejected as too large: %v", err)
		}
		t.Fatal(err)
	}
	res := b.Object["spec"].(map[string]any)["resources"].([]any)
	body, _ := json.Marshal(b.Object)
	t.Logf("files=%d raw=%d stored=%d object=%d bytes (budget %d, etcd %d)", len(res), raw, storedPayload(res), len(body), maxBundleResourcesBytes, etcdMaxRequestBytes)
	if len(res) != len(want) {
		t.Fatalf("resources = %d, want %d", len(res), len(want))
	}
	for name, w := range want {
		r, ok := resourceByName(res, name)
		if !ok {
			t.Fatalf("resource %s missing", name)
		}
		if got := decodeBase64Gz(t, r["content"].(string)); string(got) != w {
			t.Fatalf("%s did not round-trip", name)
		}
	}
}
