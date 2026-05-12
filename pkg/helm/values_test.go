package helm

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
	"sort"
	"testing"
)

func TestMergeValues_DeepMapMerge(t *testing.T) {
	in := MergeInput{
		ChartDefaults: map[string]any{
			"image": map[string]any{"repository": "registry.suse.com/ai/llm", "tag": "1.0"},
			"resources": map[string]any{
				"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
			},
		},
		BlueprintOverrides: map[string]any{
			"resources": map[string]any{
				"requests": map[string]any{"cpu": "500m"},
			},
		},
		WorkloadOverrides: map[string]any{
			"resources": map[string]any{
				"limits": map[string]any{"cpu": "1000m"},
			},
		},
	}

	got, err := MergeValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]any{
		"image": map[string]any{"repository": "registry.suse.com/ai/llm", "tag": "1.0"},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "500m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "1000m"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge result mismatch:\n  got:  %#v\n  want: %#v", got, want)
	}
}

func TestMergeValues_ListReplaceWholesale(t *testing.T) {
	in := MergeInput{
		ChartDefaults: map[string]any{
			"image": map[string]any{"repository": "r"},
			"env": []any{
				map[string]any{"name": "FOO", "value": "chart-foo"},
				map[string]any{"name": "BAR", "value": "chart-bar"},
			},
		},
		WorkloadOverrides: map[string]any{
			"env": []any{
				map[string]any{"name": "FOO", "value": "override-foo"},
			},
		},
	}

	got, err := MergeValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	envs, ok := got["env"].([]any)
	if !ok {
		t.Fatalf("env is not a list: %T", got["env"])
	}
	if len(envs) != 1 {
		t.Fatalf("expected env to be replaced wholesale (len=1), got len=%d: %#v", len(envs), envs)
	}
	first, _ := envs[0].(map[string]any)
	if first["value"] != "override-foo" {
		t.Errorf("expected env[0].value=override-foo, got %v", first["value"])
	}
}

func TestMergeValues_PureFunction_InputsUnchanged(t *testing.T) {
	chart := map[string]any{
		"image":     map[string]any{"repository": "r"},
		"resources": map[string]any{"requests": map[string]any{"cpu": "100m"}},
	}
	bp := map[string]any{
		"resources": map[string]any{"requests": map[string]any{"cpu": "500m"}},
	}

	chartCopy := deepCloneForTest(chart)
	bpCopy := deepCloneForTest(bp)

	if _, err := MergeValues(MergeInput{ChartDefaults: chart, BlueprintOverrides: bp}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(chart, chartCopy) {
		t.Errorf("ChartDefaults was mutated:\n  before: %#v\n  after:  %#v", chartCopy, chart)
	}
	if !reflect.DeepEqual(bp, bpCopy) {
		t.Errorf("BlueprintOverrides was mutated:\n  before: %#v\n  after:  %#v", bpCopy, bp)
	}
}

// deepCloneForTest is a tiny encoding/json-free deep clone used only by tests
// to snapshot inputs. Production code uses values.go's deepCopyMap.
func deepCloneForTest(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch tv := v.(type) {
		case map[string]any:
			out[k] = deepCloneForTest(tv)
		case []any:
			cp := make([]any, len(tv))
			for i, e := range tv {
				if m, ok := e.(map[string]any); ok {
					cp[i] = deepCloneForTest(m)
				} else {
					cp[i] = e
				}
			}
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// captureSlog returns a slog.Logger writing JSON to buf and a func to read it.
func captureSlog(t *testing.T) (*slog.Logger, func() string) {
	t.Helper()
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf.String
}

func TestMergeValues_DropsForbiddenKeys_OnLayer2And3(t *testing.T) {
	logger, read := captureSlog(t)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	in := MergeInput{
		ChartDefaults: map[string]any{
			"image":            map[string]any{"repository": "r"},
			"imagePullSecrets": []any{map[string]any{"name": "trusted"}}, // layer 1: trusted, kept
		},
		BlueprintOverrides: map[string]any{
			"imagePullSecrets": []any{map[string]any{"name": "evil-bp"}}, // dropped
			"nameOverride":     "evil-bp",                                 // dropped
			"fullnameOverride": "evil-bp-full",                            // dropped
			"serviceAccount":   map[string]any{"create": true, "name": "bp-sa"}, // create dropped, name kept
		},
		WorkloadOverrides: map[string]any{
			"imagePullSecrets": []any{map[string]any{"name": "evil-wl"}}, // dropped
			"serviceAccount":   map[string]any{"create": false},          // create dropped, map becomes empty
		},
		NIMGenerated: map[string]any{
			"imagePullSecrets": []any{map[string]any{"name": "operator"}}, // layer 4: trusted, wins
		},
	}

	got, err := MergeValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// imagePullSecrets in result must come from layer 4 (operator), not layer 2/3
	pulls, _ := got["imagePullSecrets"].([]any)
	if len(pulls) != 1 {
		t.Fatalf("expected one imagePullSecrets entry, got %d: %#v", len(pulls), pulls)
	}
	if name := pulls[0].(map[string]any)["name"]; name != "operator" {
		t.Errorf("imagePullSecrets must come from layer 4 (operator), got %v", name)
	}

	if _, present := got["nameOverride"]; present {
		t.Error("nameOverride must be dropped from layer 2")
	}
	if _, present := got["fullnameOverride"]; present {
		t.Error("fullnameOverride must be dropped from layer 2")
	}

	// serviceAccount.create dropped; serviceAccount.name from BlueprintOverrides survives
	sa, _ := got["serviceAccount"].(map[string]any)
	if _, present := sa["create"]; present {
		t.Error("serviceAccount.create must be dropped")
	}
	if sa["name"] != "bp-sa" {
		t.Errorf("serviceAccount.name should survive (got %v)", sa["name"])
	}

	// slog.Warn was emitted with the right keys
	logs := read()
	for _, k := range []string{"imagePullSecrets", "nameOverride", "fullnameOverride", "serviceAccount.create"} {
		if !bytes.Contains([]byte(logs), []byte(k)) {
			t.Errorf("expected slog.Warn to mention dropped key %q\nlogs: %s", k, logs)
		}
	}
	if !bytes.Contains([]byte(logs), []byte(`"layer":2`)) {
		t.Errorf("expected slog.Warn to record layer=2\nlogs: %s", logs)
	}
	if !bytes.Contains([]byte(logs), []byte(`"layer":3`)) {
		t.Errorf("expected slog.Warn to record layer=3\nlogs: %s", logs)
	}
}

func TestMergeValues_DropsForbiddenKeys_TrustedLayersUntouched(t *testing.T) {
	in := MergeInput{
		ChartDefaults: map[string]any{
			"image":            map[string]any{"repository": "r"},
			"imagePullSecrets": []any{map[string]any{"name": "chart-default"}},
			"nameOverride":     "chart-default",
		},
		NIMGenerated: map[string]any{
			"fullnameOverride": "operator-managed",
		},
	}

	got, err := MergeValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, present := got["nameOverride"]; !present {
		t.Error("layer 1 nameOverride must NOT be dropped (trusted)")
	}
	if _, present := got["fullnameOverride"]; !present {
		t.Error("layer 4 fullnameOverride must NOT be dropped (trusted)")
	}
}

func TestMergeValues_RequiresImageRepository_Absent(t *testing.T) {
	_, err := MergeValues(MergeInput{
		ChartDefaults: map[string]any{"replicas": 3}, // no image
	})
	if !errors.Is(err, ErrMissingImageRepository) {
		t.Fatalf("expected ErrMissingImageRepository, got %v", err)
	}
}

func TestMergeValues_RequiresImageRepository_Empty(t *testing.T) {
	_, err := MergeValues(MergeInput{
		ChartDefaults: map[string]any{"image": map[string]any{"repository": ""}},
	})
	if !errors.Is(err, ErrMissingImageRepository) {
		t.Fatalf("expected ErrMissingImageRepository, got %v", err)
	}
}

func TestMergeValues_RequiresImageRepository_NotAMap(t *testing.T) {
	_, err := MergeValues(MergeInput{
		ChartDefaults: map[string]any{"image": "not-a-map"},
	})
	if !errors.Is(err, ErrMissingImageRepository) {
		t.Fatalf("expected ErrMissingImageRepository when image is not a map, got %v", err)
	}
}

func TestMergeValues_RequiresImageRepository_Present(t *testing.T) {
	out, err := MergeValues(MergeInput{
		ChartDefaults: map[string]any{"image": map[string]any{"repository": "registry.suse.com/x"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out["image"].(map[string]any)["repository"]; got != "registry.suse.com/x" {
		t.Errorf("image.repository not preserved: %v", got)
	}
}

func TestApplyImageRefRules_FirstMatchWins(t *testing.T) {
	rules := []ImageRewriteRule{
		{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"},
		{Match: "registry.suse.com/ai/", Replace: "WRONG/"},
	}
	got := applyImageRefRules("registry.suse.com/ai/llm:1.0", rules)
	want := "harbor.example.com/suse/ai/llm:1.0"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyImageRefRules_NoMatch_Unchanged(t *testing.T) {
	rules := []ImageRewriteRule{{Match: "ghcr.io/", Replace: "harbor/"}}
	got := applyImageRefRules("registry.suse.com/foo:1", rules)
	if got != "registry.suse.com/foo:1" {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestApplyImageRefRules_EmptyMatchSkipped(t *testing.T) {
	rules := []ImageRewriteRule{
		{Match: "", Replace: "ANYTHING"},
		{Match: "x/", Replace: "y/"},
	}
	got := applyImageRefRules("x/foo", rules)
	if got != "y/foo" {
		t.Errorf("got %q, want y/foo", got)
	}
}

func TestApplyImageRefRules_EmptyRefStr(t *testing.T) {
	rules := []ImageRewriteRule{{Match: "x/", Replace: "y/"}}
	if got := applyImageRefRules("", rules); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestApplyImageRefRules_NilRules(t *testing.T) {
	if got := applyImageRefRules("foo", nil); got != "foo" {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestWalkImageRefs_StringImage(t *testing.T) {
	visited := []string{}
	v := map[string]any{
		"image": "registry.suse.com/foo:1",
	}
	walkImageRefs(v, func(s string) string {
		visited = append(visited, s)
		return s + "-rewritten"
	})
	if len(visited) != 1 || visited[0] != "registry.suse.com/foo:1" {
		t.Errorf("expected one visit on the image string, got %v", visited)
	}
	if v["image"] != "registry.suse.com/foo:1-rewritten" {
		t.Errorf("expected rewrite to be written back, got %v", v["image"])
	}
}

func TestWalkImageRefs_MapImage(t *testing.T) {
	visited := []string{}
	v := map[string]any{
		"image": map[string]any{
			"repository": "r/foo",
			"registry":   "g.example.com",
			"tag":        "1.0",
		},
	}
	walkImageRefs(v, func(s string) string {
		visited = append(visited, s)
		return s + "!"
	})
	sort.Strings(visited)
	want := []string{"g.example.com", "r/foo"}
	if !reflect.DeepEqual(visited, want) {
		t.Errorf("expected visits %v, got %v", want, visited)
	}
	img := v["image"].(map[string]any)
	if img["repository"] != "r/foo!" {
		t.Errorf("repository not rewritten: %v", img["repository"])
	}
	if img["registry"] != "g.example.com!" {
		t.Errorf("registry not rewritten: %v", img["registry"])
	}
	if img["tag"] != "1.0" {
		t.Errorf("tag must not be visited or modified, got %v", img["tag"])
	}
}

func TestWalkImageRefs_NestedInList(t *testing.T) {
	visited := []string{}
	v := map[string]any{
		"sidecars": []any{
			map[string]any{"name": "proxy", "image": "p/img:1"},
			map[string]any{"name": "redis", "image": "r/img:2"},
		},
	}
	walkImageRefs(v, func(s string) string {
		visited = append(visited, s)
		return "X"
	})
	sort.Strings(visited)
	if len(visited) != 2 || visited[0] != "p/img:1" || visited[1] != "r/img:2" {
		t.Errorf("expected p/img:1 and r/img:2 visited, got %v", visited)
	}
	sidecars := v["sidecars"].([]any)
	for _, s := range sidecars {
		if s.(map[string]any)["image"] != "X" {
			t.Errorf("sidecar image not written back: %v", s)
		}
	}
}

func TestWalkImageRefs_NonStringNonMapImage_Unchanged(t *testing.T) {
	visited := []string{}
	v := map[string]any{
		"image": 42,
	}
	walkImageRefs(v, func(s string) string {
		visited = append(visited, s)
		return "WRONG"
	})
	if len(visited) != 0 {
		t.Errorf("expected no visits for non-string/non-map image, got %v", visited)
	}
	if v["image"] != 42 {
		t.Errorf("expected unchanged, got %v", v["image"])
	}
}

func TestWalkImageRefs_DepthCap(t *testing.T) {
	// Build a 20-deep nested map with an image at depth 0 and another at depth 20+.
	deep := map[string]any{"image": "shallow:1"}
	cur := deep
	for i := 0; i < 20; i++ {
		next := map[string]any{}
		cur["nested"] = next
		cur = next
	}
	cur["image"] = "deepest:2"
	visited := []string{}
	walkImageRefs(deep, func(s string) string {
		visited = append(visited, s)
		return s
	})
	found := map[string]bool{}
	for _, s := range visited {
		found[s] = true
	}
	if !found["shallow:1"] {
		t.Errorf("expected shallow:1 to be visited, got %v", visited)
	}
	if found["deepest:2"] {
		t.Errorf("did not expect deepest:2 (beyond depth cap) to be visited, got %v", visited)
	}
}

// TestApplyImageRewrites_Spec66WorkedExample is the verbatim worked example
// from ARCHITECTURE.md §6.6 — a top-level string image, a sidecar list with
// one map-shaped image and one shorthand image. Locks the end-to-end
// orchestrator behavior against the spec.
func TestApplyImageRewrites_Spec66WorkedExample(t *testing.T) {
	in := map[string]any{
		"image": "registry.suse.com/ai/llm:1.0",
		"sidecars": []any{
			map[string]any{
				"name": "proxy",
				"image": map[string]any{
					"repository": "registry.suse.com/sidecars/proxy",
					"tag":        "2.1",
				},
			},
			map[string]any{
				"name":  "redis",
				"image": "redis:7",
			},
		},
	}
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"}}
	out := ApplyImageRewrites(in, rules)

	if out["image"] != "harbor.example.com/suse/ai/llm:1.0" {
		t.Errorf("top image: got %v", out["image"])
	}
	sidecars := out["sidecars"].([]any)
	proxyImg := sidecars[0].(map[string]any)["image"].(map[string]any)
	if proxyImg["repository"] != "harbor.example.com/suse/sidecars/proxy" {
		t.Errorf("proxy repository: got %v", proxyImg["repository"])
	}
	if proxyImg["tag"] != "2.1" {
		t.Errorf("proxy tag must be preserved: got %v", proxyImg["tag"])
	}
	redisImg := sidecars[1].(map[string]any)["image"]
	if redisImg != "redis:7" {
		t.Errorf("redis image must be unchanged (no host prefix): got %v", redisImg)
	}
}

// TestApplyImageRewrites_PureFunction_InputsUnchanged mirrors the existing
// TestMergeValues_PureFunction_InputsUnchanged. Snapshots the input via
// deepCloneForTest, calls ApplyImageRewrites, and asserts the input is
// deep-equal to the snapshot afterwards.
func TestApplyImageRewrites_PureFunction_InputsUnchanged(t *testing.T) {
	in := map[string]any{
		"image": "registry.suse.com/foo:1",
		"sidecars": []any{
			map[string]any{"image": "registry.suse.com/bar:2"},
		},
	}
	inSnap := deepCloneForTest(in)
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor/"}}
	_ = ApplyImageRewrites(in, rules)
	if !reflect.DeepEqual(in, inSnap) {
		t.Errorf("input was mutated:\n  before: %v\n  after:  %v", inSnap, in)
	}
}

// §6.6 required scenario 1: empty rules → input unchanged (deep-equal).
func TestApplyImageRewrites_EmptyRules_InputUnchanged(t *testing.T) {
	in := map[string]any{
		"image": "registry.suse.com/foo:1",
	}
	out := ApplyImageRewrites(in, nil)
	if !reflect.DeepEqual(out, in) {
		t.Errorf("expected output to equal input when rules is nil:\n  in:  %v\n  out: %v", in, out)
	}
}

// §6.6 required scenario 2: top-level string image is rewritten.
func TestApplyImageRewrites_StringImage_Rewritten(t *testing.T) {
	in := map[string]any{"image": "registry.suse.com/foo:1"}
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"}}
	out := ApplyImageRewrites(in, rules)
	if out["image"] != "harbor.example.com/suse/foo:1" {
		t.Errorf("expected rewritten, got %v", out["image"])
	}
}

// §6.6 required scenario 3: map image with repository sub-key is rewritten.
func TestApplyImageRewrites_MapImageRepository_Rewritten(t *testing.T) {
	in := map[string]any{
		"image": map[string]any{
			"repository": "registry.suse.com/sidecars/proxy",
			"tag":        "2.1",
		},
	}
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"}}
	out := ApplyImageRewrites(in, rules)
	img := out["image"].(map[string]any)
	if img["repository"] != "harbor.example.com/suse/sidecars/proxy" {
		t.Errorf("repository not rewritten: %v", img["repository"])
	}
	if img["tag"] != "2.1" {
		t.Errorf("tag must be preserved: %v", img["tag"])
	}
}

// §6.6 required scenario 4: nested-in-list walking.
func TestApplyImageRewrites_NestedInList_Walked(t *testing.T) {
	in := map[string]any{
		"sidecars": []any{
			map[string]any{
				"name":  "proxy",
				"image": map[string]any{"repository": "registry.suse.com/sidecars/proxy", "tag": "2.1"},
			},
			map[string]any{
				"name":  "redis",
				"image": "redis:7",
			},
		},
	}
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"}}
	out := ApplyImageRewrites(in, rules)
	sidecars := out["sidecars"].([]any)
	proxy := sidecars[0].(map[string]any)["image"].(map[string]any)
	if proxy["repository"] != "harbor.example.com/suse/sidecars/proxy" {
		t.Errorf("proxy image not rewritten: %v", proxy)
	}
	redis := sidecars[1].(map[string]any)
	if redis["image"] != "redis:7" {
		t.Errorf("redis image must be unchanged (no host prefix): %v", redis["image"])
	}
}

// §6.6 required scenario 5: digest preservation (foo@sha256:... pattern).
func TestApplyImageRewrites_DigestPreserved(t *testing.T) {
	in := map[string]any{"image": "registry.suse.com/foo@sha256:abc123"}
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"}}
	out := ApplyImageRewrites(in, rules)
	want := "harbor.example.com/suse/foo@sha256:abc123"
	if out["image"] != want {
		t.Errorf("got %v, want %v", out["image"], want)
	}
}

// §6.6 required scenario 6: first match wins, no chaining (rule 2 must NOT
// see rule 1's output).
func TestApplyImageRewrites_FirstMatchWins_NoChaining(t *testing.T) {
	in := map[string]any{"image": "a/foo:1"}
	rules := []ImageRewriteRule{
		{Match: "a/", Replace: "b/"},
		{Match: "b/", Replace: "WRONG/"},
	}
	out := ApplyImageRewrites(in, rules)
	if out["image"] != "b/foo:1" {
		t.Errorf("expected first-rule output 'b/foo:1' (no chaining), got %v", out["image"])
	}
}

// §6.6 required scenario 7: idempotency. Calling ApplyImageRewrites twice
// with the same rules produces the same output.
func TestApplyImageRewrites_Idempotency(t *testing.T) {
	in := map[string]any{"image": "registry.suse.com/foo:1"}
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor.example.com/suse/"}}
	once := ApplyImageRewrites(in, rules)
	twice := ApplyImageRewrites(once, rules)
	if !reflect.DeepEqual(once, twice) {
		t.Errorf("not idempotent:\n  once:  %v\n  twice: %v", once, twice)
	}
}

// §6.6 edge case: image: "" → unchanged.
func TestApplyImageRewrites_EmptyImage_Unchanged(t *testing.T) {
	in := map[string]any{"image": ""}
	rules := []ImageRewriteRule{{Match: "x/", Replace: "y/"}}
	out := ApplyImageRewrites(in, rules)
	if out["image"] != "" {
		t.Errorf("empty image must be unchanged, got %v", out["image"])
	}
}

// §6.6 edge case: image: nil → unchanged.
func TestApplyImageRewrites_NilImage_Unchanged(t *testing.T) {
	in := map[string]any{"image": nil}
	rules := []ImageRewriteRule{{Match: "x/", Replace: "y/"}}
	out := ApplyImageRewrites(in, rules)
	if out["image"] != nil {
		t.Errorf("nil image must be unchanged, got %v", out["image"])
	}
}

// §6.6 edge case: image as int / bool → unchanged (defensive against chart bugs).
func TestApplyImageRewrites_NonStringNonMapImage_Unchanged(t *testing.T) {
	cases := map[string]map[string]any{
		"int":  {"image": 42},
		"bool": {"image": true},
	}
	rules := []ImageRewriteRule{{Match: "x/", Replace: "y/"}}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out := ApplyImageRewrites(in, rules)
			if !reflect.DeepEqual(out["image"], in["image"]) {
				t.Errorf("expected unchanged for %s, got %v", name, out["image"])
			}
		})
	}
}

// §6.6 edge case: rule with empty Match is skipped (would match everything).
func TestApplyImageRewrites_EmptyMatchRule_Skipped(t *testing.T) {
	in := map[string]any{"image": "x/foo:1"}
	rules := []ImageRewriteRule{
		{Match: "", Replace: "ANYTHING"},
		{Match: "x/", Replace: "y/"},
	}
	out := ApplyImageRewrites(in, rules)
	if out["image"] != "y/foo:1" {
		t.Errorf("empty Match rule must be skipped, got %v", out["image"])
	}
}

// §6.6 edge case: refs with non-default port (host:5000/foo) are matched
// only when the rule includes the port.
func TestApplyImageRewrites_RefWithPort_RuleMustIncludePort(t *testing.T) {
	in := map[string]any{"image": "host:5000/foo:1"}
	// Rule without port: should NOT match (prefix differs at colon).
	out := ApplyImageRewrites(in, []ImageRewriteRule{{Match: "host/", Replace: "harbor/"}})
	if out["image"] != "host:5000/foo:1" {
		t.Errorf("rule without port must not match: %v", out["image"])
	}
	// Rule with port: should match.
	out = ApplyImageRewrites(in, []ImageRewriteRule{{Match: "host:5000/", Replace: "harbor/"}})
	if out["image"] != "harbor/foo:1" {
		t.Errorf("rule with port should rewrite, got %v", out["image"])
	}
}

// §6.6 edge case: shorthand refs without registry (just "redis:7") are not
// rewritten (no host prefix for any rule to match).
func TestApplyImageRewrites_ShorthandRef_NotRewritten(t *testing.T) {
	in := map[string]any{"image": "redis:7"}
	rules := []ImageRewriteRule{{Match: "registry.suse.com/", Replace: "harbor/"}}
	out := ApplyImageRewrites(in, rules)
	if out["image"] != "redis:7" {
		t.Errorf("shorthand ref must not be rewritten, got %v", out["image"])
	}
}
