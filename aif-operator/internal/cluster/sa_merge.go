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
// ServiceAccount, Role, RoleBinding, Job, and CronJob that merge secretNames
// into every chart-managed ServiceAccount's imagePullSecrets in the target
// namespace on the downstream cluster.
//
// Why a Job/CronJob and not declarative SA manifests: the operator cannot list
// SAs on a downstream cluster (no remote client today), and a Bundle shipping
// fully-formed SA manifests would clobber any pre-existing imagePullSecrets
// the cluster operator added.
//
// IMPORTANT — atomic-list caveat: ServiceAccount.ImagePullSecrets is
// declared `+listType=atomic` in core/v1 (unlike PodSpec.ImagePullSecrets,
// which carries patchStrategy:"merge"/patchMergeKey:"name"). A
// strategic-merge patch on an atomic list performs a wholesale REPLACE, so
// the script CANNOT just send its desired list — that would silently wipe any
// pre-existing entries (e.g. a private-registry pull secret added by the
// cluster admin). The script therefore does a read-modify-write per SA:
// read the current names, compute the union with the desired set, and only
// patch when the union differs (so unchanged SAs don't generate spurious
// update events).
//
// Owner scope: the script patches chart-managed SAs (label
// `app.kubernetes.io/managed-by=Helm`) plus the namespace "default" SA — many
// charts and bundled subcharts run pods under "default" — and bounces only
// chart-managed Pods. Other cluster-admin-created SAs are left alone. Because
// the SA patch is a union (adds, never replaces), touching "default" is safe.
//
// The script uses ONLY POSIX shell builtins, `sort`, `tr`, and `kubectl`
// — no `jq`, `awk`, `sed`, or `grep` — so a minimal kubectl image (e.g.
// `registry.suse.com/suse/kubectl`) is sufficient.
//
// Two runners ship together:
//
//   - Job (one-shot): runs immediately when the Bundle is applied, so the
//     common case (SAs that ship with the chart) is patched within seconds of
//     install. Its name carries a deterministic hash of (namespace + sorted
//     secret names + image + rendered script) so any change to the desired
//     state — including a new script from an operator upgrade — produces a new
//     Job (Job .spec is immutable after create, so reusing the name with a
//     changed pod template would fail with "field is immutable"). With
//     unchanged inputs the name is stable, so Fleet's re-apply is a no-op and a
//     completed Job stays completed (a TTL would create permanent Fleet drift).
//
//   - CronJob (recurring): closes the one-shot's gap — ServiceAccounts created
//     AFTER the Job runs (e.g. by the workload chart itself) would otherwise
//     never be patched, because re-applying the immutable Job is a no-op. The
//     CronJob re-runs the same merge on a schedule, so late-created SAs (and
//     Pods that came up before their SA was patched) eventually converge. Its
//     name is stable; Fleet updates it in place when the secret set changes.
//     The Jobs the CronJob spawns are owned by the CronJob, not the Bundle, so
//     they don't register as Fleet drift.
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

	// The script reads each SA's current imagePullSecrets and unions them with
	// this list before patching. We render the desired names as a
	// SPACE-separated single-line literal so the entire DESIRED='…' assignment
	// fits on one YAML line — embedding newlines breaks out of the YAML
	// block-scalar's indent and causes Fleet's post-render to fail with
	// "could not find expected ':'". The script's pipeline normalises the
	// format to one-per-line via `tr ' ' '\n'` before sort -u, so a
	// space-separated source is fine.
	desiredLine := strings.Join(sortedNames, " ")

	// Build the shell script once (the long, indentation-sensitive part) and
	// inject it into both the Job and the CronJob pod specs via the `indent`
	// template func, so the two runners share a single source of truth.
	var scriptBuf bytes.Buffer
	if err := saMergeScriptTemplate.Execute(&scriptBuf, struct {
		Namespace    string
		DesiredNames string
	}{Namespace: namespace, DesiredNames: desiredLine}); err != nil {
		return "", fmt.Errorf("render SA-merge script: %w", err)
	}
	script := scriptBuf.String()

	// The Job name carries a hash of every input that affects the Job's pod
	// template — namespace, secret names, image, AND the rendered script. A
	// Job's spec.template is immutable, so if the operator ships a new script
	// (e.g. after an operator upgrade) under an unchanged name, Fleet's re-apply
	// fails with "field is immutable". Folding the script into the hash means a
	// script change yields a NEW Job name: Fleet prunes the old completed Job
	// and creates the new one cleanly. With unchanged inputs the name is stable,
	// so steady-state re-applies stay no-ops.
	h := sha1.New()
	h.Write([]byte(namespace))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(sortedNames, ",")))
	h.Write([]byte{0})
	h.Write([]byte(image))
	h.Write([]byte{0})
	h.Write([]byte(script))
	hashHex := hex.EncodeToString(h.Sum(nil))[:10]
	jobName := fmt.Sprintf("%s-%s", saMergeJobNamePrefix, hashHex)
	// The CronJob name is stable (no hash): CronJob .spec is mutable, so Fleet
	// updates it in place when the script or secret set changes instead of
	// orphaning a hash-named predecessor that would keep running alongside the
	// new one.
	cronJobName := fmt.Sprintf("%s-cron", saMergeJobNamePrefix)

	data := struct {
		Namespace      string
		JobName        string
		CronJobName    string
		Schedule       string
		ServiceAccount string
		Image          string
		Script         string
	}{
		Namespace:      namespace,
		JobName:        jobName,
		CronJobName:    cronJobName,
		Schedule:       saMergeCronSchedule,
		ServiceAccount: saMergeServiceAccount,
		Image:          image,
		Script:         script,
	}

	var buf bytes.Buffer
	if err := saMergeTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render SA-merge template: %w", err)
	}
	return buf.String(), nil
}

// saMergeCronSchedule is how often the recurring reconciliation runs. Five
// minutes balances "late-created SAs converge promptly" against the cost of a
// short-lived kubectl Pod per tick. The one-shot Job covers the install-time
// common case, so this is a safety net, not the primary path.
const saMergeCronSchedule = "*/5 * * * *"

// saMergeIndentFuncs lets the shared script be spliced into block scalars at
// different YAML depths (the Job and the CronJob's jobTemplate nest the pod
// spec at different indentation levels).
var saMergeIndentFuncs = template.FuncMap{
	"indent": func(spaces int, s string) string {
		pad := strings.Repeat(" ", spaces)
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if l != "" {
				lines[i] = pad + l
			}
		}
		return strings.Join(lines, "\n")
	},
}

// saMergeScriptTemplate is the POSIX-sh merge script, rendered standalone so it
// can be indented into both runners. It:
//  1. lists chart-managed SAs (label app.kubernetes.io/managed-by=Helm),
//  2. for each, reads its current imagePullSecrets names,
//  3. computes the sorted union with the desired names,
//  4. patches the SA strategic-merge with that FULL union — necessary because
//     SA.ImagePullSecrets is +listType=atomic and strategic-merge replaces the
//     whole list (see buildSAMergeResources for the rationale),
//  5. skips the patch when the union equals the existing set, so unchanged SAs
//     don't generate spurious update events on re-apply, then
//  6. ONLY IF at least one SA was actually patched this run, bounces
//     chart-managed Pods stuck in ImagePullBackOff/ErrImagePull: a Pod's
//     imagePullSecrets are merged from its SA only at admission, so a Pod that
//     started before its SA was patched stays broken until recreated, and
//     deleting it lets its controller recreate it with the patched SA.
//
//     The "only if an SA changed" guard is critical: it bounds the bounce to
//     the tick that actually fixed something. Once SAs are stable (the patch is
//     idempotent and skips), the bounce never fires, so a genuinely unpullable
//     or slow-pulling Pod sits stably in ImagePullBackOff instead of being
//     deleted-and-recreated on every CronJob tick — the unconditional bounce
//     caused exactly that perpetual Pending<->Running redeploy churn.
var saMergeScriptTemplate = template.Must(template.New("sa-merge-script").Parse(`set -eu
# Desired names, space-separated (rendered by the operator). Kept single-line
# so the YAML block-scalar embedding this script doesn't break.
DESIRED='{{ .DesiredNames }}'
NS='{{ .Namespace }}'
# Tracks whether we patched any SA this run. The Pod-bounce below only fires
# when this is 1, so a stable namespace (nothing to patch) never churns Pods.
PATCHED=0
# Patch chart-managed SAs (app.kubernetes.io/managed-by=Helm) PLUS the namespace
# "default" SA. Many charts — and bundled subcharts (e.g. the bitnami postgresql
# dependency of litellm) — run their pods under "default" rather than a chart SA,
# so image-pull creds must land there too or those pods can't pull (notably
# AppCollection images pulled by a SUSE-registry chart's subchart). Other
# cluster-admin SAs in the namespace are left alone. The merge is a union, so
# patching "default" only ADDS creds and never clobbers existing entries.
SAS=$(kubectl -n "$NS" get sa -l 'app.kubernetes.io/managed-by=Helm' -o jsonpath='{.items[*].metadata.name}')
for sa in $(printf 'default %s' "$SAS" | tr ' ' '\n' | sort -u); do
  # Read SA's current imagePullSecrets names, one per line.
  EXISTING=$(kubectl -n "$NS" get sa "$sa" -o jsonpath='{range .imagePullSecrets[*]}{.name}{"\n"}{end}')
  # Union: existing + desired, deduped + sorted. tr+sort handles empty EXISTING
  # (no .imagePullSecrets field) cleanly.
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
  # Strategic-merge here is REPLACE-semantics on this atomic list, but we send
  # the full union so the end state is correct. See buildSAMergeResources doc.
  kubectl -n "$NS" patch sa "$sa" --type=strategic -p "$PATCH"
  PATCHED=1
done
# Bounce chart-managed Pods stuck pulling images so they re-read their SA's
# imagePullSecrets at admission (a Pod created before its SA was patched keeps
# its possibly-empty imagePullSecrets baked in until recreated) — but ONLY when
# we actually changed an SA this run. Without this guard a genuinely unpullable
# or slow-pulling Pod would be deleted and recreated on every tick, which reads
# as the workload perpetually toggling Pending<->Running.
if [ "$PATCHED" = 1 ]; then
  kubectl -n "$NS" get pods -l 'app.kubernetes.io/managed-by=Helm' -o jsonpath='{range .items[*]}{.metadata.name}={range .status.initContainerStatuses[*]}{.state.waiting.reason},{end}{range .status.containerStatuses[*]}{.state.waiting.reason},{end}{"\n"}{end}' | while IFS='=' read -r pod reasons; do
    [ -n "$pod" ] || continue
    case ",$reasons" in
      *,ImagePullBackOff,*|*,ErrImagePull,*)
        echo "$pod: image pull failing after SA patch, deleting so it re-reads SA imagePullSecrets"
        kubectl -n "$NS" delete pod "$pod" --ignore-not-found
        ;;
    esac
  done
fi
`))

// saMergeTemplate renders the manifests Fleet/Helm applies on the downstream
// cluster:
//
//   - ServiceAccount: the runners execute under a dedicated SA, NOT default,
//     so RBAC stays minimal.
//   - Role:           get/list/patch on serviceaccounts and get/list/delete on
//     pods (for the bounce step), scoped to the workload's target namespace.
//   - RoleBinding:    binds the SA to the Role.
//   - Job:            the one-shot install-time merge. Retained (no TTL) so
//     Fleet doesn't see drift ten minutes after a successful install.
//   - CronJob:        the recurring merge, so SAs/Pods created after the Job ran
//     still converge. Its spawned Jobs are CronJob-owned, not Bundle-owned, so
//     they don't register as Fleet drift; history limits keep them bounded.
//
// Both runners execute the same script (saMergeScriptTemplate), spliced in at
// the correct YAML depth via the `indent` func.
var saMergeTemplate = template.Must(template.New("sa-merge").Funcs(saMergeIndentFuncs).Parse(`apiVersion: v1
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
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "delete"]
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
{{ indent 14 .Script }}
      volumes:
        - name: tmp
          emptyDir: {}
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: {{ .CronJobName }}
  namespace: {{ .Namespace }}
  labels:
    ai-platform.suse.com/role: pullsecret-sa-merge
spec:
  schedule: "{{ .Schedule }}"
  concurrencyPolicy: Forbid
  startingDeadlineSeconds: 200
  successfulJobsHistoryLimit: 1
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
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
{{ indent 18 .Script }}
          volumes:
            - name: tmp
              emptyDir: {}
`))
