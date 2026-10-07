/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/util/validation"
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
//   - Job (install-time): runs when the Bundle is applied and repeats the
//     merge every saMergeJobIntervalSeconds for saMergeJobRuns passes, so SAs
//     and Pods the chart creates during the install are handled within seconds.
//     Its name carries a deterministic hash of (namespace + sorted secret names
//
//   - image + rendered script + repeat settings) so any change to the desired
//     state — including a new script from an operator upgrade — produces a new
//     Job (Job .spec is immutable after create, so reusing the name with a
//     changed pod template would fail with "field is immutable"). With
//     unchanged inputs the name is stable, so Fleet's re-apply is a no-op and a
//     completed Job stays completed (a TTL would create permanent Fleet drift).
//
//   - CronJob (recurring): closes the Job's gap — ServiceAccounts created
//     AFTER the Job finished (e.g. by a later chart upgrade) would otherwise
//     never be patched, because re-applying the immutable Job is a no-op. The
//     CronJob re-runs the same merge on a schedule, so late-created SAs (and
//     Pods that came up before their SA was patched) eventually converge. Its
//     name is stable; Fleet updates it in place when the secret set changes.
//     The Jobs the CronJob spawns are owned by the CronJob, not the Bundle, so
//     they don't register as Fleet drift.
func buildSAMergeResources(owner, namespace string, secretNames, releases []string, image string) (string, error) {
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
	// The workload's Helm releases in this namespace, also single-line: only
	// pods whose controller belongs to one of them are recreated. They are
	// rendered into a single-quoted shell string, so only valid Helm release
	// names (DNS-1123 labels of at most 53 characters) are kept.
	sortedReleases := make([]string, 0, len(releases))
	for _, release := range releases {
		if len(release) <= saMergeReleaseNameMax && len(validation.IsDNS1123Label(release)) == 0 {
			sortedReleases = append(sortedReleases, release)
		}
	}
	sort.Strings(sortedReleases)

	// Build the shell script once (the long, indentation-sensitive part) and
	// inject it into both the Job and the CronJob pod specs via the `indent`
	// template func, so the two runners share a single source of truth.
	var scriptBuf bytes.Buffer
	if err := saMergeScriptTemplate.Execute(&scriptBuf, struct {
		Namespace    string
		DesiredNames string
		Releases     string
	}{Namespace: namespace, DesiredNames: desiredLine, Releases: strings.Join(sortedReleases, " ")}); err != nil {
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
	jobName := saMergeJobName(owner, namespace, sortedNames, image, script, saMergeJobRuns, saMergeJobIntervalSeconds)
	// The runners belong to one workload: two workloads sharing a namespace
	// each get their own, so neither overwrites the other's releases.
	suffix := ownerSuffix(owner)
	// The CronJob name depends only on the owning workload, not on the script
	// or secrets: CronJob .spec is mutable, so Fleet updates it in place when
	// the script or secret set changes instead of orphaning a hash-named
	// predecessor that would keep running alongside the new one.
	cronJobName := fmt.Sprintf("%s-cron-%s", saMergeJobNamePrefix, suffix)

	data := struct {
		Namespace       string
		JobName         string
		CronJobName     string
		Schedule        string
		ServiceAccount  string
		Image           string
		Script          string
		JobRuns         int
		JobInterval     int
		JobDeadline     int
		CronJobDeadline int
	}{
		Namespace:       namespace,
		JobName:         jobName,
		CronJobName:     cronJobName,
		Schedule:        saMergeCronSchedule,
		ServiceAccount:  saMergeServiceAccount + "-" + suffix,
		Image:           image,
		Script:          script,
		JobRuns:         saMergeJobRuns,
		JobInterval:     saMergeJobIntervalSeconds,
		JobDeadline:     saMergeJobDeadlineSeconds,
		CronJobDeadline: saMergeCronJobDeadlineSeconds,
	}

	var buf bytes.Buffer
	if err := saMergeTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render SA-merge template: %w", err)
	}
	return buf.String(), nil
}

// saMergeJobName derives the install-time Job's name from every input that shapes
// its pod template, so any change to it yields a new Job (see
// buildSAMergeResources). sortedNames must already be sorted.
func saMergeJobName(owner, namespace string, sortedNames []string, image, script string, runs, interval int) string {
	h := sha1.New()
	h.Write([]byte(owner))
	h.Write([]byte{0})
	h.Write([]byte(namespace))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(sortedNames, ",")))
	h.Write([]byte{0})
	h.Write([]byte(image))
	h.Write([]byte{0})
	h.Write([]byte(script))
	h.Write([]byte{0})
	_, _ = fmt.Fprintf(h, "%d/%d", runs, interval) // a hash write never fails
	hashHex := hex.EncodeToString(h.Sum(nil))[:10]
	return fmt.Sprintf("%s-%s", saMergeJobNamePrefix, hashHex)
}

// ownerSuffix is a short, stable hash of the owning workload ("namespace/name")
// that keeps each workload's SA-merge runners apart within a namespace.
func ownerSuffix(owner string) string {
	sum := sha1.Sum([]byte(owner))
	return hex.EncodeToString(sum[:])[:8]
}

// saMergeReleaseNameMax is Helm's limit on release names.
const saMergeReleaseNameMax = 53

// saMergeCronSchedule is how often the recurring reconciliation runs. Five
// minutes balances "late-created SAs converge promptly" against the cost of a
// short-lived kubectl Pod per tick. The install-time Job covers the install-time
// common case, so this is a safety net, not the primary path.
const saMergeCronSchedule = "*/5 * * * *"

// saMergeJobRuns and saMergeJobIntervalSeconds make the install-time Job repeat
// the merge every 15 seconds for 5 minutes. Charts create their ServiceAccounts
// and Pods after the Bundle (and so the Job) is applied; repeating covers the
// install window so their Pods recover in seconds, well within an install
// wait, instead of on the next CronJob tick.
const (
	saMergeJobRuns            = 20
	saMergeJobIntervalSeconds = 15
)

// Deadlines end a merge run that hangs for any reason. The CronJob uses
// concurrencyPolicy Forbid, so one stuck run would otherwise skip every later
// one. The install-time Job's deadline is generous: a transient kubectl
// failure restarts its passes from the first one (restartPolicy OnFailure),
// and a healthy but slow Job must not end Failed. The CronJob's jobs end
// before the next five-minute tick.
//
// The deadline counts from the Job's start, so time spent scheduling the pod
// or pulling the kubectl image (e.g. while a private mirror is unreachable)
// uses it up too. A Job that passes its deadline is marked Failed
// (DeadlineExceeded) and, having no TTL, stays Failed even after later
// CronJob runs merge successfully. Nothing depends on it: workload status
// comes from the workload's own bundle, and the CronJob keeps merging every
// five minutes. To clear the failed state, delete the Job:
//
//	kubectl -n <namespace> delete job <ai-pullsecret-merge-...>
const (
	saMergeJobDeadlineSeconds     = 1800
	saMergeCronJobDeadlineSeconds = 240
)

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
//
//  1. lists chart-managed SAs (label app.kubernetes.io/managed-by=Helm),
//
//  2. for each, reads its current imagePullSecrets names,
//
//  3. computes the sorted union with the desired names,
//
//  4. patches the SA strategic-merge with that FULL union — necessary because
//     SA.ImagePullSecrets is +listType=atomic and strategic-merge replaces the
//     whole list (see buildSAMergeResources for the rationale),
//
//  5. skips the patch when the union equals the existing set, so unchanged SAs
//     don't generate spurious update events on re-apply, then
//
//  6. recreates Pods stuck in ImagePullBackOff/ErrImagePull, and Pending Pods
//     that have not started any container yet: a Pod's imagePullSecrets are
//     merged from its SA only at admission, so a Pod that started before its
//     SA was patched stays broken until recreated, and deleting it lets its
//     controller recreate it with the patched SA. A Pod qualifies when its SA
//     carries a desired secret that its spec lacks (whatever its labels —
//     many charts leave Pods without the Helm managed-by label), every pull
//     secret it lists is on its SA, and its ReplicaSet/StatefulSet/DaemonSet
//     pod template sets no imagePullSecrets; or when it is Helm-labelled,
//     failing to pull, and an SA was patched this pass. Pending Pods qualify
//     only through the first condition.
//
//     Both conditions bound the bounce. The first stops on its own once the
//     recreated Pod carries the secret. The second is guarded by "only if an SA
//     changed this pass": once SAs are stable the bounce never fires, so a genuinely
//     unpullable or slow-pulling Pod sits stably in ImagePullBackOff instead of
//     being deleted-and-recreated on every CronJob tick — the unconditional
//     bounce caused exactly that perpetual Pending<->Running redeploy churn.
var saMergeScriptTemplate = template.Must(template.New("sa-merge-script").Parse(`set -eu
# Desired names, space-separated (rendered by the operator). Kept single-line
# so the YAML block-scalar embedding this script doesn't break.
DESIRED='{{ .DesiredNames }}'
NS='{{ .Namespace }}'
# The workload's Helm releases, space-separated: only Pods whose controller
# belongs to one of them are recreated, so other apps sharing the namespace
# (and its "default" SA) are left alone.
RELEASES='{{ .Releases }}'
# pod_in_scope KIND NAME: the Pod's controller belongs to one of RELEASES. A
# ReplicaSet carries no Helm release annotation; its Deployment is read instead.
pod_in_scope() {
  case "$1" in
    "") return 1 ;;
    ReplicaSet)
      dep=$(kubectl -n "$NS" get "$1" "$2" -o jsonpath='{.metadata.ownerReferences[?(@.kind=="Deployment")].name}' 2>/dev/null || true)
      [ -n "$dep" ] || return 1
      rel=$(kubectl -n "$NS" get deployment "$dep" -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}' 2>/dev/null || true)
      ;;
    *)
      rel=$(kubectl -n "$NS" get "$1" "$2" -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}' 2>/dev/null || true)
      ;;
  esac
  [ -n "$rel" ] || return 1
  case " $RELEASES " in
    *" $rel "*) return 0 ;;
  esac
  return 1
}
# MERGE_RUNS passes MERGE_INTERVAL seconds apart (default: a single pass). The
# install-time Job repeats the merge over the install window, so ServiceAccounts
# and Pods the chart creates after the Job started are still handled promptly
# instead of waiting for the next CronJob tick.
RUNS="${MERGE_RUNS:-1}"
INTERVAL="${MERGE_INTERVAL:-15}"
merge_once() {
# Tracks whether we patched any SA this pass. The Pod-bounce below only fires
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
# Recreate Pods stuck pulling images so they re-read their SA's
# imagePullSecrets at admission (a Pod created before its SA was patched keeps
# its possibly-empty imagePullSecrets baked in until recreated). A Pod is
# recreated when its SA now carries a desired secret its spec lacks — this
# holds for charts that leave their Pods without the Helm managed-by label, and
# it stops on its own once the recreated Pod carries the secret. Helm-labelled
# Pods are also recreated, but ONLY when we actually changed an SA this run.
# Without that guard a genuinely unpullable or slow-pulling Pod would be deleted
# and recreated on every tick, which reads as the workload perpetually toggling
# Pending<->Running.
kubectl -n "$NS" get pods -o jsonpath='{range .items[*]}{.metadata.name}|{.spec.serviceAccountName}|{.metadata.labels.app\.kubernetes\.io/managed-by}|{.metadata.ownerReferences[?(@.controller==true)].kind}|{.metadata.ownerReferences[?(@.controller==true)].name}|{range .spec.imagePullSecrets[*]}{.name},{end}|{range .status.initContainerStatuses[*]}{.state.waiting.reason},{end}{range .status.containerStatuses[*]}{.state.waiting.reason},{end}|{.status.phase}|{range .status.initContainerStatuses[*]}{.state.running.startedAt}{.state.terminated.exitCode}{.lastState.terminated.exitCode}{end}{range .status.containerStatuses[*]}{.state.running.startedAt}{.state.terminated.exitCode}{.lastState.terminated.exitCode}{end}{"\n"}{end}' | while IFS='|' read -r pod psa managed okind oname podsecrets reasons phase started; do
  [ -n "$pod" ] || continue
  # A Pod that has not started any container yet is recreated right away
  # when it lacks a delivered secret (nothing is lost, and waiting for its
  # first failed pull could push recovery past the next tick).
  PULL_FAILING=0
  case ",$reasons" in
    *,ImagePullBackOff,*|*,ErrImagePull,*) PULL_FAILING=1 ;;
    *)
      if [ "$phase" != Pending ] || [ -n "$started" ]; then
        continue
      fi
      ;;
  esac
  [ -n "$psa" ] || psa=default
  SA_SECRETS=$(kubectl -n "$NS" get sa "$psa" -o jsonpath='{range .imagePullSecrets[*]}{.name}{"\n"}{end}' 2>/dev/null | tr '\n' ' ' || true)
  MISSING=0
  for n in $DESIRED; do
    case " $SA_SECRETS " in
      *" $n "*) ;;
      *) continue ;;
    esac
    case ",$podsecrets" in
      *",$n,"*) ;;
      *) MISSING=1 ;;
    esac
  done
  # A Pod listing a pull secret its SA lacks did not get its pull secrets from
  # the SA alone (e.g. a mutating webhook), so a recreated Pod may miss the
  # delivered secret again. Leave it alone: these recreations are not counted,
  # and this keeps them from repeating on every pass.
  if [ "$MISSING" = 1 ]; then
    for s in $(printf '%s' "$podsecrets" | tr ',' ' '); do
      case " $SA_SECRETS " in
        *" $s "*) ;;
        *) MISSING=0 ;;
      esac
    done
  fi
  # Only a ReplicaSet/StatefulSet/DaemonSet whose pod template sets no
  # imagePullSecrets recreates the Pod with its SA's secrets merged in. Bare
  # Pods are never recreated, deleted Job Pods count against the Job's backoff
  # limit, and a template listing its own secrets never gets the SA's — so
  # deleting any of those would not help (and could repeat on every run).
  if [ "$MISSING" = 1 ]; then
    case "$okind" in
      ReplicaSet|StatefulSet|DaemonSet)
        TEMPLATE_SECRETS=$(kubectl -n "$NS" get "$okind" "$oname" -o jsonpath='{.spec.template.spec.imagePullSecrets[*].name}' 2>/dev/null || echo unknown)
        [ -z "$TEMPLATE_SECRETS" ] || MISSING=0
        ;;
      *) MISSING=0 ;;
    esac
  fi
  REASON=''
  if [ "$MISSING" = 1 ]; then
    REASON="SA $psa has a pull secret the pod lacks, deleting so it is recreated with it"
  # The older Helm-label recreation is bounded by "an SA was patched this
  # pass" instead, so the SA-only pull secret check above does not apply to it.
  elif [ "$PULL_FAILING" = 1 ] && [ "$PATCHED" = 1 ] && [ "$managed" = Helm ]; then
    REASON="image pull failing after SA patch, deleting so it re-reads SA imagePullSecrets"
  fi
  if [ -n "$REASON" ] && pod_in_scope "$okind" "$oname"; then
    echo "$pod: $REASON"
    # --wait=false: kubectl would otherwise watch the Pod until it is gone,
    # which the Role does not allow (no watch on pods), and retry forever.
    # The Pod's controller recreates it; there is nothing to wait for.
    kubectl -n "$NS" delete pod "$pod" --ignore-not-found --wait=false
  fi
done
}
pass=1
while :; do
  merge_once
  [ "$pass" -lt "$RUNS" ] || break
  pass=$((pass + 1))
  sleep "$INTERVAL"
done
`))

// saMergeTemplate renders the manifests Fleet/Helm applies on the downstream
// cluster:
//
//   - ServiceAccount: the runners execute under a dedicated SA, NOT default,
//     so RBAC stays minimal.
//   - Role:           get/list/patch on serviceaccounts, get/list/delete on
//     pods (for the bounce step) and get on ReplicaSets/StatefulSets/DaemonSets/
//     Deployments (to read pod templates and Helm releases), scoped to the
//     workload's target namespace.
//   - RoleBinding:    binds the SA to the Role.
//   - Job:            the install-time merge (repeated over the install
//     window). Retained (no TTL) so
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
  - apiGroups: ["apps"]
    resources: ["replicasets", "statefulsets", "daemonsets", "deployments"]
    verbs: ["get"]
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
    ai-factory.suse.com/role: pullsecret-sa-merge
spec:
  backoffLimit: 4
  activeDeadlineSeconds: {{ .JobDeadline }}
  template:
    metadata:
      labels:
        ai-factory.suse.com/role: pullsecret-sa-merge
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
          env:
            - name: MERGE_RUNS
              value: "{{ .JobRuns }}"
            - name: MERGE_INTERVAL
              value: "{{ .JobInterval }}"
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
    ai-factory.suse.com/role: pullsecret-sa-merge
spec:
  schedule: "{{ .Schedule }}"
  concurrencyPolicy: Forbid
  startingDeadlineSeconds: 200
  successfulJobsHistoryLimit: 1
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      backoffLimit: 4
      activeDeadlineSeconds: {{ .CronJobDeadline }}
      template:
        metadata:
          labels:
            ai-factory.suse.com/role: pullsecret-sa-merge
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
