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
// every chart-managed ServiceAccount's imagePullSecrets in the target
// namespace on the downstream cluster.
//
// Why a Job and not declarative SA manifests: the operator cannot list SAs
// on a downstream cluster (no remote client today), and a Bundle shipping
// fully-formed SA manifests would clobber any pre-existing imagePullSecrets
// the cluster operator added.
//
// IMPORTANT — atomic-list caveat: ServiceAccount.ImagePullSecrets is
// declared `+listType=atomic` in core/v1 (unlike PodSpec.ImagePullSecrets,
// which carries patchStrategy:"merge"/patchMergeKey:"name"). A
// strategic-merge patch on an atomic list performs a wholesale REPLACE, so
// the Job CANNOT just send its desired list — that would silently wipe any
// pre-existing entries (e.g. a private-registry pull secret added by the
// cluster admin). The script therefore does a read-modify-write per SA:
// read the current names, compute the union with the desired set, and only
// patch when the union differs (so unchanged SAs don't generate spurious
// update events).
//
// Owner scope: the Job filters SAs by label
// `app.kubernetes.io/managed-by=Helm` so it only touches chart-created
// ServiceAccounts. SAs the cluster admin pre-created for other purposes are
// left alone even if they share the namespace.
//
// The script uses ONLY POSIX shell builtins, `sort`, `tr`, and `kubectl`
// — no `jq`, `awk`, `sed`, or `grep` — so a minimal kubectl image (e.g.
// `registry.suse.com/suse/kubectl`) is sufficient.
//
// The deliberate compromise: this Job is one-shot — ServiceAccounts created
// AFTER the Job runs are NOT patched until the Bundle re-applies. For
// typical chart-managed workloads where SAs ship with the chart, this is
// acceptable; a future enhancement could deploy a small controller for
// continuous reconciliation.
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

	// The Job's shell script reads each SA's current imagePullSecrets and
	// unions them with this list (one name per line, sorted) before
	// patching. We render the desired names as a newline-joined literal so
	// the script can `sort -u` them with the existing set in one pipeline.
	desiredLines := strings.Join(sortedNames, "\n")

	data := struct {
		Namespace      string
		JobName        string
		ServiceAccount string
		Image          string
		DesiredNames   string // newline-separated, sorted, unique
	}{
		Namespace:      namespace,
		JobName:        jobName,
		ServiceAccount: saMergeServiceAccount,
		Image:          image,
		DesiredNames:   desiredLines,
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
// The Job script lists chart-managed SAs (label-scoped to
// app.kubernetes.io/managed-by=Helm) and, for each one:
//  1. reads the SA's current imagePullSecrets names,
//  2. computes the sorted union with the desired names (one per line),
//  3. patches the SA strategic-merge with that FULL union — necessary
//     because SA.ImagePullSecrets is +listType=atomic and strategic-merge
//     replaces the whole list (see buildSAMergeResources for the design
//     rationale and the atomic-list caveat),
//  4. skips the patch when the union equals the existing set, so unchanged
//     SAs don't generate spurious update events on re-apply.
//
// Job-level retries are bounded by backoffLimit=4.
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
              # Desired names, one per line, sorted unique (rendered by the operator).
              DESIRED='{{ .DesiredNames }}'
              # Only patch chart-managed SAs; cluster-admin-created SAs in the
              # namespace are left alone. See buildSAMergeResources comments.
              for sa in $(kubectl -n {{ .Namespace }} get sa -l 'app.kubernetes.io/managed-by=Helm' -o jsonpath='{.items[*].metadata.name}'); do
                # Read SA's current imagePullSecrets names, one per line.
                EXISTING=$(kubectl -n {{ .Namespace }} get sa "$sa" -o jsonpath='{range .imagePullSecrets[*]}{.name}{"\n"}{end}')
                # Union: existing + desired, deduped + sorted. tr+sort handles
                # empty EXISTING (no .imagePullSecrets field) cleanly.
                UNION=$(printf '%s\n%s\n' "$EXISTING" "$DESIRED" | tr ' ' '\n' | sort -u | tr -s '\n' ' ')
                EXISTING_SORTED=$(printf '%s\n' "$EXISTING" | tr ' ' '\n' | sort -u | tr -s '\n' ' ')
                if [ "$UNION" = "$EXISTING_SORTED" ]; then
                  echo "$sa: already has desired imagePullSecrets, skipping"
                  continue
                fi
                # Build JSON array from the unioned names (POSIX sh, no jq).
                JSON=''
                SEP=''
                for n in $UNION; do
                  JSON="${JSON}${SEP}{\"name\":\"$n\"}"
                  SEP=','
                done
                PATCH="{\"imagePullSecrets\":[$JSON]}"
                echo "$sa: patching with $PATCH"
                # Strategic-merge here is REPLACE-semantics on this atomic
                # list, but we send the full union so the end state is
                # correct. See buildSAMergeResources doc.
                kubectl -n {{ .Namespace }} patch sa "$sa" --type=strategic -p "$PATCH"
              done
      volumes:
        - name: tmp
          emptyDir: {}
`))
