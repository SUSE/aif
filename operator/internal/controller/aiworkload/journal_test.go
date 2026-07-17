package aiworkload

import (
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func TestOperationJournalRoundTrip(t *testing.T) {
	op := aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeRetry, Nonce: "n1",
		RetryEpoch: 7, IntentDigest: "sha256:i", State: aiplatformv1alpha1.OperationStateInProgress,
	}
	s, err := encodeOperation(op)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeOperation(s)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Nonce != "n1" || got.RetryEpoch != 7 || got.State != aiplatformv1alpha1.OperationStateInProgress {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestHandledSetDedupAndCap(t *testing.T) {
	var ids []string
	for i := 0; i < 12; i++ {
		ids = addHandled(ids, handledID(aiplatformv1alpha1.OperationTypeUpgrade, string(rune('a'+i))))
	}
	if len(ids) != maxHandledOps {
		t.Fatalf("want cap %d, got %d", maxHandledOps, len(ids))
	}
	// Newest retained, oldest evicted.
	if !isHandled(ids, handledID(aiplatformv1alpha1.OperationTypeUpgrade, "l")) {
		t.Fatalf("newest id should be present")
	}
	if isHandled(ids, handledID(aiplatformv1alpha1.OperationTypeUpgrade, "a")) {
		t.Fatalf("oldest id should have been evicted")
	}
	// Dedup: re-adding an existing id does not grow the set.
	n := len(ids)
	ids = addHandled(ids, handledID(aiplatformv1alpha1.OperationTypeUpgrade, "l"))
	if len(ids) != n {
		t.Fatalf("dedup failed: %d -> %d", n, len(ids))
	}
}

func TestHandledIdentityDistinguishesType(t *testing.T) {
	ids := addHandled(nil, handledID(aiplatformv1alpha1.OperationTypeRetry, "x"))
	if isHandled(ids, handledID(aiplatformv1alpha1.OperationTypeRollback, "x")) {
		t.Fatalf("(type,nonce) identity must distinguish Retry from Rollback for same nonce")
	}
}

func TestHandledEncodeDecode(t *testing.T) {
	ids := []string{"Upgrade|a", "Retry|b"}
	s, err := encodeHandled(ids)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := decodeHandled(s)
	if len(got) != 2 || got[0] != "Upgrade|a" || got[1] != "Retry|b" {
		t.Fatalf("decode mismatch: %+v", got)
	}
	if len(decodeHandled("")) != 0 {
		t.Fatalf("empty string should decode to empty slice")
	}
}
