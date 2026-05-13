package nvidia_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	"github.com/SUSE/aif/pkg/nvidia"
)

// Example_discovery demonstrates the end-to-end Discovery flow against an
// in-process OCI Distribution v2 stub. It also doubles as the contract
// `make verify-nim-mock` runs to prove the package works without a live
// registry.
//
// Spec hooks: ARCHITECTURE.md §13.1 (mirror path convention + chart→type
// heuristic) and §6.2 (Discovery interface).
func Example_discovery() {
	// 1. Spin up a fake OCI Distribution v2 registry serving two NVIDIA
	//    charts (one LLM, one VLM) plus one unrelated chart that should be
	//    filtered out by the prefix.
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"repositories":[
			"ai/charts/nvidia/nim-llm",
			"ai/charts/nvidia/nim-vlm",
			"other/something"
		]}`)
	})
	tags := map[string]string{
		"ai/charts/nvidia/nim-llm": `{"name":"ai/charts/nvidia/nim-llm","tags":["1.0.0","1.1.0"]}`,
		"ai/charts/nvidia/nim-vlm": `{"name":"ai/charts/nvidia/nim-vlm","tags":["2.0.0"]}`,
	}
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/"), "/tags/list")
		body, ok := tags[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 2. Construct Discovery and configure it with the stub's address.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // suppress log output for clean Example output
	d, _ := nvidia.NewDiscovery(logger)
	d.UpdateSettings(nvidia.EngineSettings{
		RegistryEndpoint: ts.URL,
		Username:         "demo-user",
		Token:            "demo-token",
	})

	// 3. Refresh + Index.
	ctx := context.Background()
	if err := d.Refresh(ctx); err != nil {
		fmt.Println("Refresh error:", err)
		return
	}
	entries, _ := d.Index(ctx)

	// 4. Print result deterministically (Index returns sorted by ID).
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	for _, e := range entries {
		fmt.Printf("%-15s  type=%-3s  chart=%s\n", e.ID, e.Type, e.Chart)
	}

	// Output:
	// nim-llm:1.0.0    type=llm  chart=nim-llm
	// nim-llm:1.1.0    type=llm  chart=nim-llm
	// nim-vlm:2.0.0    type=vlm  chart=nim-vlm
}

// Example_chartAnnotations exercises the AnnotationReader against an
// in-process Helm OCI stub. Doubles as the contract `make verify-nim-mock`
// runs to prove digest-cached annotation fetching works without a live
// registry.
func Example_chartAnnotations() {
	chartYaml := "apiVersion: v2\nname: my-chart\nannotations:\n  ai.suse.com/role: reference-blueprint\n  ai.suse.com/use-case: rag\n"
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "my-chart/Chart.yaml", Mode: 0o644, Size: int64(len(chartYaml))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(chartYaml))
	_ = tw.Close()
	_ = gz.Close()
	layerBytes := tarBuf.Bytes()
	layerSum := sha256.Sum256(layerBytes)
	layerDigest := "sha256:" + hex.EncodeToString(layerSum[:])
	manifest := fmt.Sprintf(`{"schemaVersion":2,"layers":[{"mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip","digest":%q,"size":%d}]}`, layerDigest, len(layerBytes))
	manifestSum := sha256.Sum256([]byte(manifest))
	manifestDigest := "sha256:" + hex.EncodeToString(manifestSum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/ai/charts/nvidia/my-chart/manifests/1.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", manifestDigest)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.WriteString(w, manifest)
	})
	mux.HandleFunc("/v2/ai/charts/nvidia/my-chart/blobs/"+layerDigest, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(layerBytes)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, ar := nvidia.NewDiscovery(logger)
	d.UpdateSettings(nvidia.EngineSettings{RegistryEndpoint: ts.URL})

	ann, err := ar.ChartAnnotations(context.Background(), "my-chart", "1.0.0")
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println("role:", ann["ai.suse.com/role"])
	fmt.Println("use-case:", ann["ai.suse.com/use-case"])

	// Output:
	// role: reference-blueprint
	// use-case: rag
}

// Example_deployerGenerateValues demonstrates the Deployer.GenerateValues
// happy path against an in-process deployerImpl (no upstream dep). Doubles
// as the contract `make verify-nim-mock` runs to prove the package's
// public surface works without hitting registry.suse.com.
//
// Spec hooks: ARCHITECTURE.md §4.4 (NIM Resource Sizing Formulas) and
// §6.2 (nvidia.Deployer interface).
func Example_deployerGenerateValues() {
	d := nvidia.NewDeployer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	gpus := int32(1)
	out, _ := d.GenerateValues(context.Background(), nvidia.GenerateRequest{
		Entry: nvidia.NIMEntry{
			Chart:   "nim-llm",
			Version: "1.3.0",
			Type:    nvidia.TypeLLM,
		},
		Replicas: 1,
		GPUs:     &gpus,
	})
	img := out["image"].(map[string]any)
	res := out["resources"].(map[string]any)["requests"].(map[string]any)
	fmt.Printf("repository=%s\n", img["repository"])
	fmt.Printf("tag=%s\n", img["tag"])
	fmt.Printf("cpu=%s memory=%s gpus=%s\n", res["cpu"], res["memory"], res["nvidia.com/gpu"])
	// Output:
	// repository=registry.suse.com/ai/containers/nvidia/nim-llm
	// tag=1.3.0
	// cpu=8 memory=32Gi gpus=1
}
