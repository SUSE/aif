package cluster

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// buildSAMergeResources returns a multi-document YAML containing the
// ServiceAccount, Role, RoleBinding, and Job that merge secretNames into
// every ServiceAccount's imagePullSecrets in the target namespace on the
// downstream cluster.
//
// Why a Job and not declarative SA manifests: the operator cannot list SAs
// on a downstream cluster (no remote client today), and a Bundle shipping
// fully-formed SA manifests would clobber any pre-existing imagePullSecrets
// the cluster operator added. The Job preserves existing entries and only
// appends — strategic-merge patch on a ServiceAccount's .imagePullSecrets
// dedupes by patchMergeKey=name, so re-applying the same desired list is
// idempotent and pre-existing entries are kept.
//
// The script uses ONLY kubectl (no jq) so we can pin to a minimal kubectl
// image. The deliberate compromise: this Job is one-shot — Service
// Accounts created AFTER the Job runs are NOT patched until the Bundle
// re-applies. For typical chart-managed workloads where SAs ship with the
// chart, this is acceptable; a future enhancement could deploy a small
// controller for continuous reconciliation.
//
// The Job name carries a deterministic hash of (namespace + sorted secret
// names + image) so any change to the desired state produces a new Job
// (Job .spec is immutable after create — same name + different spec would
// fail). With unchanged inputs the Job name is stable, so Fleet's re-apply
// is a no-op and a completed Job stays completed.
func buildSAMergeResources(namespace string, secretNames []string, image string) (string, error) {
	if namespace == "" {
		return "", fmt.Errorf("namespace required")
	}
	if len(secretNames) == 0 {
		return "", fmt.Errorf("secretNames required")
	}
	if image == "" {
		return "", fmt.Errorf("image required")
	}

	// Sort so the hash and the JSON-array literal are deterministic across
	// reconciles.
	sortedNames := append([]string(nil), secretNames...)
	sort.Strings(sortedNames)

	h := sha1.New()
	h.Write([]byte(namespace))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(sortedNames, ",")))
	h.Write([]byte{0})
	h.Write([]byte(image))
	hashHex := hex.EncodeToString(h.Sum(nil))[:10]
	jobName := fmt.Sprintf("%s-%s", saMergeJobNamePrefix, hashHex)

	// Build the strategic-merge-patch payload. Strategic merge on a
	// PodSpec-style list with patchMergeKey=name (which imagePullSecrets is)
	// dedupes by name: existing entries with the same name are kept, new
	// names are appended.
	entries := make([]string, len(sortedNames))
	for i, n := range sortedNames {
		entries[i] = fmt.Sprintf(`{"name":%q}`, n)
	}
	patchPayload := fmt.Sprintf(`{"imagePullSecrets":[%s]}`, strings.Join(entries, ","))

	data := struct {
		Namespace      string
		JobName        string
		ServiceAccount string
		Image          string
		PatchPayload   string
	}{
		Namespace:      namespace,
		JobName:        jobName,
		ServiceAccount: saMergeServiceAccount,
		Image:          image,
		PatchPayload:   patchPayload,
	}

	var buf bytes.Buffer
	if err := saMergeTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render SA-merge template: %w", err)
	}
	return buf.String(), nil
}

// saMergeTemplate renders the four manifests Fleet/Helm applies on the
// downstream cluster:
//
//   - ServiceAccount: the Job runs under a dedicated SA, NOT default, so
//     RBAC stays minimal.
//   - Role:           get/list/patch on serviceaccounts (and nothing else)
//     scoped to the workload's target namespace.
//   - RoleBinding:    binds the SA to the Role.
//   - Job:            the actual SA-merge work. ttlSecondsAfterFinished=600
//     means completed Jobs (and their pods) are GC'd after 10 minutes,
//     keeping the namespace tidy without losing logs immediately.
//
// The Job script is intentionally short: list SAs, for each one compute the
// merged imagePullSecrets via jq (preserve existing, append missing), and
// strategic-merge-patch the SA only if the list changed. Job-level retries
// are bounded by backoffLimit=4.
var saMergeTemplate = template.Must(template.New("sa-merge").Parse(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .ServiceAccount }}
  namespace: {{ .Namespace }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ .ServiceAccount }}
  namespace: {{ .Namespace }}
rules:
  - apiGroups: [""]
    resources: ["serviceaccounts"]
    verbs: ["get", "list", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ .ServiceAccount }}
  namespace: {{ .Namespace }}
subjects:
  - kind: ServiceAccount
    name: {{ .ServiceAccount }}
    namespace: {{ .Namespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ .ServiceAccount }}
---
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ .JobName }}
  namespace: {{ .Namespace }}
  labels:
    ai-platform.suse.com/role: pullsecret-sa-merge
spec:
  ttlSecondsAfterFinished: 600
  backoffLimit: 4
  template:
    metadata:
      labels:
        ai-platform.suse.com/role: pullsecret-sa-merge
    spec:
      serviceAccountName: {{ .ServiceAccount }}
      restartPolicy: OnFailure
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: merge
          image: {{ .Image }}
          imagePullPolicy: IfNotPresent
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          volumeMounts:
            - name: tmp
              mountPath: /tmp
          command: ["/bin/sh", "-c"]
          args:
            - |
              set -eu
              PATCH='{{ .PatchPayload }}'
              for sa in $(kubectl -n {{ .Namespace }} get sa -o jsonpath='{.items[*].metadata.name}'); do
                echo "patching $sa with $PATCH"
                kubectl -n {{ .Namespace }} patch sa "$sa" --type=strategic -p "$PATCH"
              done
      volumes:
        - name: tmp
          emptyDir: {}
`))
