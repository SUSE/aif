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
