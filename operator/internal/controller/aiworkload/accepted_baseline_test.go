package aiworkload

import (
	"testing"

	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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
