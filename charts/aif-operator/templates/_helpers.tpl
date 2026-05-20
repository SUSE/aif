{{/*
Expand the name of the chart.
*/}}
{{- define "aif-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "aif-operator.fullname" -}}
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
{{- define "aif-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "aif-operator.labels" -}}
helm.sh/chart: {{ include "aif-operator.chart" . }}
{{ include "aif-operator.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "aif-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aif-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "aif-operator.serviceAccountName" -}}
{{- default (include "aif-operator.fullname" .) .Values.serviceAccount.name }}
{{- end }}

{{/*
Selector labels as a kubectl label selector string
*/}}
{{- define "aif-operator.selectorLabelsString" -}}
app.kubernetes.io/name={{ include "aif-operator.name" . }},app.kubernetes.io/instance={{ .Release.Name }}
{{- end }}

{{/*
Image reference with conditional registry prefix
*/}}
{{- define "aif-operator.image" -}}
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
Merges global.imagePullSecrets and per-chart imagePullSecrets.
*/}}
{{- define "aif-operator.imagePullSecrets" -}}
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
