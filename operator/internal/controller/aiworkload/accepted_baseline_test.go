package aiworkload

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktypes "k8s.io/apimachinery/pkg/types"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func uidType(s string) ktypes.UID { return ktypes.UID(s) }

func helmOpWithAccepted(uid string, gen int64, status, msg string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetGroupVersionKind(helmOpGVK)
	o.SetUID(uidType(uid))
	o.SetGeneration(gen)
	conds := []any{map[string]any{"type": "Accepted", "status": status, "reason": "r", "message": msg, "lastUpdateTime": "t1"}}
	_ = unstructured.SetNestedSlice(o.Object, conds, "status", "conditions")
	return o
}

func TestAcceptedFalseTerminal(t *testing.T) {
	ho := helmOpWithAccepted("uid1", 4, "False", "bad tag")
	baseFP := "sha256:stale"
	base := &aiplatformv1alpha1.RenderBaseline{HelmOpUID: "uid1", RenderDigest: "d", RetryEpoch: 2, HelmOpGeneration: 4, AcceptedFingerprint: baseFP}

	// Fingerprint changed since baseline + attempt matches → terminal.
	if !acceptedFalseTerminal(ho, base, "d", 2, 4) {
		t.Fatal("post-baseline Accepted=False for current attempt must be terminal")
	}
	// Same fingerprint as baseline → NOT terminal (stale condition).
	base.AcceptedFingerprint = acceptedConditionFingerprint(ho)
	if acceptedFalseTerminal(ho, base, "d", 2, 4) {
		t.Fatal("unchanged fingerprint must not be terminal")
	}
	// Attempt mismatch (epoch) → NOT terminal.
	base.AcceptedFingerprint = baseFP
	if acceptedFalseTerminal(ho, base, "d", 3, 4) {
		t.Fatal("epoch mismatch must not be terminal")
	}
	// No baseline → NOT terminal.
	if acceptedFalseTerminal(ho, nil, "d", 2, 4) {
		t.Fatal("missing baseline must not be terminal")
	}
}

// TestUpsertRenderBaseline_PreservesFingerprintOnUnchangedIdentity guards the fix for the bug
// where re-recording the baseline every reconcile overwrote the first-observed AcceptedFingerprint
// with the current (rejected) one, making acceptedFalseTerminal permanently false.
func TestUpsertRenderBaseline_PreservesFingerprintOnUnchangedIdentity(t *testing.T) {
	orig := aiplatformv1alpha1.RenderBaseline{HelmOpUID: "uid1", RenderDigest: "d", RetryEpoch: 2, HelmOpGeneration: 4, AcceptedFingerprint: "fp-at-first-observe"}
	bl := upsertRenderBaseline(nil, orig)

	// Same attempt identity, but a NEW (Fleet-driven Accepted=False) fingerprint → must be ignored.
	bl = upsertRenderBaseline(bl, aiplatformv1alpha1.RenderBaseline{HelmOpUID: "uid1", RenderDigest: "d", RetryEpoch: 2, HelmOpGeneration: 4, AcceptedFingerprint: "fp-rejected-now"})
	if len(bl) != 1 || bl[0].AcceptedFingerprint != "fp-at-first-observe" {
		t.Fatalf("unchanged attempt must preserve original fingerprint, got %+v", bl)
	}

	// A changed attempt identity (new generation) → baseline is replaced.
	bl = upsertRenderBaseline(bl, aiplatformv1alpha1.RenderBaseline{HelmOpUID: "uid1", RenderDigest: "d", RetryEpoch: 2, HelmOpGeneration: 5, AcceptedFingerprint: "fp-new-attempt"})
	if len(bl) != 1 || bl[0].HelmOpGeneration != 5 || bl[0].AcceptedFingerprint != "fp-new-attempt" {
		t.Fatalf("changed attempt identity must replace baseline, got %+v", bl)
	}

	// A different UID appends a second entry.
	bl = upsertRenderBaseline(bl, aiplatformv1alpha1.RenderBaseline{HelmOpUID: "uid2", RenderDigest: "d", RetryEpoch: 2, HelmOpGeneration: 1})
	if len(bl) != 2 {
		t.Fatalf("new UID must append, got %+v", bl)
	}
}
