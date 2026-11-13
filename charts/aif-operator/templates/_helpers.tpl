{{/*
Name of the chart.
Uses .Values.nameOverride if set, otherwise falls back to chart name.
*/}}
{{- define "aif-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Full name of the chart.
Always truncated to 63 chars for Kubernetes compatibility.
*/}}
{{- define "aif-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else }}
{{- include "aif-operator.name" . | trunc 63 | trimSuffix "-" -}}
{{- end }}
{{- end }}

{{/*
Namespace for generated references.
Always uses the Helm release namespace.
*/}}
{{- define "aif-operator.namespaceName" -}}
{{ .Release.Namespace }}
{{- end }}

{{/*
Service name with proper truncation for Kubernetes 63-character limit.
Takes a context with .suffix for the service type (e.g., "webhook-service").
If fullname + suffix exceeds 63 chars, truncates fullname to 45 chars.
*/}}
{{- define "aif-operator.serviceName" -}}
{{- $fullname := include "aif-operator.fullname" .context -}}
{{- if gt (len $fullname) 45 -}}
{{- printf "%s-%s" (trunc 45 $fullname | trimSuffix "-") .suffix | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" $fullname .suffix | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{/*
Whether the operator API is registered with Rancher's APIService aggregation,
as the string "true" or "false" (include returns a string). Used by both the
APIService and the manager's AGGREGATION_SECRET_* env vars, so the two cannot
disagree.
nil-safe: values stored by 2.4.0 (helm upgrade --reuse-values) have no
aggregation key. A missing key means enabled, because the operator requires it.
*/}}
{{- define "aif-operator.aggregationEnabled" -}}
{{- dig "enabled" true (.Values.aggregation | default dict) -}}
{{- end }}

{{/*
Name of the Secret Rancher writes the aggregation tunnel connection into.
*/}}
{{- define "aif-operator.aggregationSecretName" -}}
{{- include "aif-operator.serviceName" (dict "context" . "suffix" "aggregation") -}}
{{- end }}

{{/*
Namespace Rancher writes the aggregation Secret into (APIService
spec.secretNamespace), also passed to the manager as
AGGREGATION_SECRET_NAMESPACE. Defaults to cattle-system and must stay outside
the operator namespace: the Settings API's validate-credentials overrides
resolve Secret refs there and send their values to a caller-chosen URL, so the
tunnel token must not be resolvable from it.
nil-safe for values stored by 2.4.0 (helm upgrade --reuse-values).
*/}}
{{- define "aif-operator.aggregationSecretNamespace" -}}
{{- $ns := dig "secretNamespace" "cattle-system" (.Values.aggregation | default dict) | default "cattle-system" -}}
{{- if eq $ns .Release.Namespace -}}
{{- fail (printf "aggregation.secretNamespace (%s) must not be the release namespace: the Secret holds Rancher's tunnel token and must stay outside the operator namespace. Set aggregation.secretNamespace to a namespace other than %s." $ns $ns) -}}
{{- end -}}
{{- $ns -}}
{{- end }}

{{/*
Fails rendering when a pinned version is older (by major.minor) than the
chart's appVersion. `helm upgrade --reuse-values` reuses the previous
release's values, pinned image tag and extension versions included, so a new
chart would otherwise run an old operator with settings it does not know
(2.4.0 exits on unknown flags). Patch and pre-release differences pass
(2.4.0 vs 2.4.0-rc.1). Empty values, non-semver tags (latest, digests) and a
non-semver appVersion skip the check.
Takes a dict: context (root), name (value path for the message), value.
*/}}
{{- define "aif-operator.failIfOlderThanChart" -}}
{{- /*
Accepts only strings Helm's semver (Masterminds) parses, so semver never
aborts the render: each major.minor.patch segment is at most 19 significant
digits (always fits the uint64 semver parses into; leading zeros are allowed
and compared numerically), numeric pre-release identifiers have no leading
zero, and build metadata is unrestricted. Anything else is skipped.
*/ -}}
{{- $pre := `(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)` -}}
{{- $core := `0*[0-9]{1,19}` -}}
{{- $re := printf `^v?%s\.%s\.%s(-%s(\.%s)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$` $core $core $core $pre $pre -}}
{{- $value := toString (.value | default "") -}}
{{- $app := toString (.context.Chart.AppVersion | default "") -}}
{{- if and (regexMatch $re $value) (regexMatch $re $app) -}}
{{- $have := semver $value -}}
{{- $want := semver $app -}}
{{- if or (lt $have.Major $want.Major) (and (eq $have.Major $want.Major) (lt $have.Minor $want.Minor)) -}}
{{- fail (printf "%s %s is older than this chart (%s); a `helm upgrade --reuse-values` keeps the previous release's pinned versions. Upgrade with `--reset-then-reuse-values`, or set %s explicitly." .name $value $app .name) -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
Runs aif-operator.failIfOlderThanChart on every pinned version: the manager
image tag, and the UI extension versions when the extension is installed.
Invoked from manager.yaml, which always renders.
*/}}
{{- define "aif-operator.checkPinnedVersions" -}}
{{- include "aif-operator.failIfOlderThanChart" (dict "context" . "name" "manager.image.tag" "value" .Values.manager.image.tag) -}}
{{- $ext := .Values.aiExtension | default dict -}}
{{- if dig "enabled" false $ext -}}
{{- include "aif-operator.failIfOlderThanChart" (dict "context" . "name" "aiExtension.source.helm.version" "value" (dig "source" "helm" "version" "" $ext)) -}}
{{- include "aif-operator.failIfOlderThanChart" (dict "context" . "name" "aiExtension.extension.version" "value" (dig "extension" "version" "" $ext)) -}}
{{- end -}}
{{- end }}

{{/*
Common labels for Helm charts.
Includes app version, chart version, app name, instance, and managed-by labels.
*/}}
{{- define "aif-operator.labels" -}}
{{- if .Chart.AppVersion -}}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "aif-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
control-plane: controller-manager
{{- end }}

{{/*
Selector labels for matching pods and services.
Only includes name and instance for consistent selection.
*/}}
{{- define "aif-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aif-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Namespace where AI extensions (UI plugins, workloads, secrets) are deployed.
This is intentionally separate from the operator's release namespace.
*/}}
{{- define "aif-operator.extensionsNamespace" -}}
cattle-ui-plugin-system
{{- end -}}

{{/*
Return the proper Docker Image Registry Secret Names
*/}}
{{- define "aif-operator.imagePullSecrets" -}}
{{- $secrets := list -}}
{{- if .Values.global }}
{{- range .Values.global.imagePullSecrets }}
  {{- if kindIs "string" . }}
    {{- $secrets = append $secrets (dict "name" .) -}}
  {{- else }}
    {{- $secrets = append $secrets . -}}
  {{- end }}
{{- end }}
{{- end }}
{{- range .Values.manager.imagePullSecrets }}
  {{- if kindIs "string" . }}
    {{- $secrets = append $secrets (dict "name" .) -}}
  {{- else }}
    {{- $secrets = append $secrets . -}}
  {{- end }}
{{- end }}
{{- if $secrets }}
imagePullSecrets:
  {{- toYaml $secrets | nindent 2 }}
{{- end }}
{{- end -}}
