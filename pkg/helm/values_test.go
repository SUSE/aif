// pkg/helm/values_test.go
package helm

import (
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
