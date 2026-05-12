// pkg/helm/values_test.go
package helm

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
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
