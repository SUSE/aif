{{/*
Expand the name of the chart.
*/}}
{{- define "generic-container.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "generic-container.fullname" -}}
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
{{- define "generic-container.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "generic-container.labels" -}}
helm.sh/chart: {{ include "generic-container.chart" . }}
{{ include "generic-container.selectorLabels" . }}
app.kubernetes.io/component: workload
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "generic-container.selectorLabels" -}}
app.kubernetes.io/name: {{ include "generic-container.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Selector labels as a kubectl label selector string
*/}}
{{- define "generic-container.selectorLabelsString" -}}
app.kubernetes.io/name={{ include "generic-container.name" . }},app.kubernetes.io/instance={{ .Release.Name }}
{{- end }}

{{/*
Image reference with conditional registry prefix
*/}}
{{- define "generic-container.image" -}}
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
{{- define "generic-container.imagePullSecrets" -}}
{{- $secrets := list }}
{{- $seen := dict }}
{{- range .Values.imagePullSecrets }}
  {{- $secrets = append $secrets . }}
  {{- $seen = set $seen .name "true" }}
{{- end }}
{{- if .Values.global }}
  {{- range .Values.global.imagePullSecrets }}
    {{- $entry := . }}
    {{- if kindIs "string" . }}
      {{- $entry = dict "name" . }}
    {{- end }}
    {{- if not (hasKey $seen $entry.name) }}
      {{- $secrets = append $secrets $entry }}
      {{- $seen = set $seen $entry.name "true" }}
    {{- end }}
  {{- end }}
{{- end -}}
{{- if $secrets -}}
imagePullSecrets:
  {{- toYaml $secrets | nindent 2 }}
{{- end }}
{{- end -}}

