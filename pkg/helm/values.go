// pkg/helm/values.go
package helm

// MergeInput names the four input layers of §6.6 (layers 1-4). Layer 5
// (ApplyImageRewrites) is P4-6. Layer 6 (operator-managed imagePullSecrets)
// is added by the deployer.
//
// Layers 1 and 4 are trusted (chart authors / operator-generated). Layers 2
// and 3 are user-supplied and are scrubbed of forbidden top-level keys
// before merge (see §6.6 "Forbidden top-level keys").
type MergeInput struct {
	ChartDefaults      map[string]any // layer 1 (trusted)
	BlueprintOverrides map[string]any // layer 2 (scrubbed)
	WorkloadOverrides  map[string]any // layer 3 (scrubbed)
	NIMGenerated       map[string]any // layer 4 (trusted)
}

// MergeValues implements §6.6 layers 1-4. Pure function: deep-copies all
// inputs, returns a new map, never mutates inputs. Maps deep-merge; lists
// replace wholesale.
//
// Validation:
//   - Forbidden top-level keys silently dropped from layers 2 and 3 only,
//     each drop logged via slog.Warn (see Task 3).
//   - image.repository must be non-empty post-merge (see Task 4),
//     else returns ErrMissingImageRepository.
func MergeValues(in MergeInput) (map[string]any, error) {
	out := map[string]any{}
	out = mergeMap(out, deepCopyMap(in.ChartDefaults))
	out = mergeMap(out, deepCopyMap(in.BlueprintOverrides))
	out = mergeMap(out, deepCopyMap(in.WorkloadOverrides))
	out = mergeMap(out, deepCopyMap(in.NIMGenerated))
	return out, nil
}

// mergeMap merges src into dst. Maps deep-merge recursively; lists and
// scalars in src replace dst's value at the same key. Returns dst.
func mergeMap(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if existing, ok := dst[k]; ok {
			if existingMap, em := existing.(map[string]any); em {
				if vMap, vm := v.(map[string]any); vm {
					dst[k] = mergeMap(existingMap, vMap)
					continue
				}
			}
		}
		dst[k] = v
	}
	return dst
}

// deepCopyMap returns a deep copy of in. Maps are recursed; lists are
// shallow-copied at the slice level (with element maps deep-copied) because
// per §6.6 "MergeValues purity" lists are replaced wholesale and never
// merged into. Scalars are copied by value.
func deepCopyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		return deepCopyMap(tv)
	case []any:
		cp := make([]any, len(tv))
		for i, e := range tv {
			cp[i] = deepCopyValue(e)
		}
		return cp
	default:
		return v
	}
}
