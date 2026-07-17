package aiworkload

import (
	"encoding/json"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const (
	operationAnnotation    = "ai-factory.suse.com/operation"
	handledOpsAnnotation   = "ai-factory.suse.com/handled-ops"
	retryEpochAnnotation   = "ai-factory.suse.com/retry-epoch"
	supersededOpAnnotation = "ai-factory.suse.com/superseded-op"

	// Request-trigger annotations (set by the API endpoints, consumed by the controller).
	upgradeRequestAnnotation  = "ai-factory.suse.com/upgrade-request"
	rollbackRequestAnnotation = "ai-factory.suse.com/rollback-request"
	retryRequestAnnotation    = "ai-factory.suse.com/retry-request"

	// renderDigestLabel and workloadUIDLabel are stamped on HelmOps (Plan 3/4).
	renderDigestLabel = "ai-factory.suse.com/render-digest"
	workloadUIDLabel  = "ai-factory.suse.com/workload-uid"

	maxHandledOps = 8
)

func encodeOperation(op aiplatformv1alpha1.AIWorkloadOperation) (string, error) {
	b, err := json.Marshal(op)
	return string(b), err
}

func decodeOperation(s string) (aiplatformv1alpha1.AIWorkloadOperation, error) {
	var op aiplatformv1alpha1.AIWorkloadOperation
	err := json.Unmarshal([]byte(s), &op)
	return op, err
}

// handledID is the stable identity of a handled operation trigger.
func handledID(opType, nonce string) string { return opType + "|" + nonce }

func isHandled(existing []string, id string) bool {
	for _, e := range existing {
		if e == id {
			return true
		}
	}
	return false
}

// addHandled appends id (if new), keeping only the most recent maxHandledOps identities.
func addHandled(existing []string, id string) []string {
	if isHandled(existing, id) {
		return existing
	}
	out := append(append([]string(nil), existing...), id)
	if len(out) > maxHandledOps {
		out = out[len(out)-maxHandledOps:]
	}
	return out
}

func encodeHandled(ids []string) (string, error) {
	b, err := json.Marshal(ids)
	return string(b), err
}

func decodeHandled(s string) []string {
	if s == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return nil
	}
	return ids
}
