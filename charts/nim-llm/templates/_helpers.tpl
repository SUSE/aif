{{/*
Expand the name of the chart.
*/}}
{{- define "nim-llm.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "nim-llm.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "nim-llm.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nim-llm.labels" -}}
helm.sh/chart: {{ include "nim-llm.chart" . }}
{{ include "nim-llm.selectorLabels" . }}
app.kubernetes.io/component: inference
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nim-llm.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nim-llm.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Selector labels as a kubectl label selector string
*/}}
{{- define "nim-llm.selectorLabelsString" -}}
app.kubernetes.io/name={{ include "nim-llm.name" . }},app.kubernetes.io/instance={{ .Release.Name }}
{{- end }}

{{/*
Image reference
*/}}
{{- define "nim-llm.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- $repo := .Values.image.repository }}
{{- with (coalesce .Values.global.imageRegistry .Values.image.registry) }}
{{- printf "%s/%s:%s" . $repo $tag }}
{{- else }}
{{- printf "%s:%s" $repo $tag }}
{{- end }}
{{- end }}

{{/*
Return the proper Docker Image Registry Secret Names.
Merges global.imagePullSecrets and per-chart imagePullSecrets (both lists
are concatenated so chart-level defaults like suse-registry-creds are never
silently dropped when a global override is set).
*/}}
{{- define "nim-llm.imagePullSecrets" -}}
{{- $secrets := list }}
{{- range .Values.imagePullSecrets }}
  {{- $secrets = append $secrets . }}
{{- end }}
{{- if .Values.global }}
  {{- range .Values.global.imagePullSecrets }}
    {{- if kindIs "string" . }}
      {{- $secrets = append $secrets (dict "name" .) }}
    {{- else }}
      {{- $secrets = append $secrets . }}
    {{- end }}
  {{- end }}
{{- end }}
{{- if $secrets }}
imagePullSecrets:
  {{- toYaml $secrets | nindent 2 }}
{{- end }}
{{- end -}}
