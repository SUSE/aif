package helm

import (
	"log/slog"
	"strings"
)

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

// forbiddenTopLevelKeys are the keys silently dropped from layer 2 (Blueprint
// overrides) and layer 3 (Workload overrides) per §6.6 "Forbidden top-level
// keys". serviceAccount.create is a sub-key — handled separately below.
var forbiddenTopLevelKeys = []string{
	"imagePullSecrets",
	"nameOverride",
	"fullnameOverride",
}

// MergeValues implements §6.6 layers 1-4. Pure function: deep-copies all
// inputs, returns a new map, never mutates inputs. Maps deep-merge; lists
// replace wholesale.
//
// Validation:
//   - Forbidden top-level keys silently dropped from layers 2 and 3 only,
//     each drop logged via slog.Warn.
//   - image.repository must be non-empty post-merge (see Task 4),
//     else returns ErrMissingImageRepository.
func MergeValues(in MergeInput) (map[string]any, error) {
	bp := dropForbiddenKeys(deepCopyMap(in.BlueprintOverrides), 2)
	wl := dropForbiddenKeys(deepCopyMap(in.WorkloadOverrides), 3)

	out := map[string]any{}
	out = mergeMap(out, deepCopyMap(in.ChartDefaults))
	out = mergeMap(out, bp)
	out = mergeMap(out, wl)
	out = mergeMap(out, deepCopyMap(in.NIMGenerated))

	if err := validateMerged(out); err != nil {
		return nil, err
	}
	return out, nil
}

// dropForbiddenKeys removes the §6.6 forbidden top-level keys from layer
// (mutates and returns it for chaining). Each drop emits a slog.Warn with
// layer + key for observability. Safe on nil input (returns nil).
func dropForbiddenKeys(layer map[string]any, layerIndex int) map[string]any {
	if layer == nil {
		return nil
	}
	for _, k := range forbiddenTopLevelKeys {
		if _, present := layer[k]; present {
			slog.Warn("dropped forbidden override",
				slog.Int("layer", layerIndex),
				slog.String("key", k))
			delete(layer, k)
		}
	}
	// serviceAccount.create is a sub-key; drop it without removing the rest
	// of the serviceAccount map so that legitimate fields (name, annotations)
	// survive. Spec §6.6 lists "serviceAccount.create" as forbidden.
	if sa, ok := layer["serviceAccount"].(map[string]any); ok {
		if _, present := sa["create"]; present {
			slog.Warn("dropped forbidden override",
				slog.Int("layer", layerIndex),
				slog.String("key", "serviceAccount.create"))
			delete(sa, "create")
		}
	}
	return layer
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

// validateMerged enforces §6.6 "Required after merge". Currently checks only
// image.repository non-empty; extend here when §6.6 grows new requirements.
func validateMerged(merged map[string]any) error {
	img, ok := merged["image"].(map[string]any)
	if !ok {
		return ErrMissingImageRepository
	}
	repo, _ := img["repository"].(string)
	if repo == "" {
		return ErrMissingImageRepository
	}
	return nil
}

// applyImageRefRules returns refStr with the first matching rule's Match
// prefix replaced by the rule's Replace prefix. Rules are scanned in order;
// first match wins; no chaining (the rewritten value is not re-scanned).
// Returns refStr unchanged if no rule matches, refStr is empty, or rules
// is empty. A rule with empty Match is skipped (would match everything).
func applyImageRefRules(refStr string, rules []ImageRewriteRule) string {
	if refStr == "" {
		return refStr
	}
	for _, rule := range rules {
		if rule.Match == "" {
			continue
		}
		if strings.HasPrefix(refStr, rule.Match) {
			return rule.Replace + refStr[len(rule.Match):]
		}
	}
	return refStr
}
