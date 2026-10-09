{{- define "aif-ui.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "aif-ui.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "aif-ui.labels" -}}
catalog.cattle.io/ui-extensions-catalog-image: {{ .Chart.Name }}
app.kubernetes.io/name: {{ include "aif-ui.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "aif-ui.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aif-ui.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "aif-ui.podLabels" -}}
{{ include "aif-ui.selectorLabels" . }}
catalog.cattle.io/ui-extensions-catalog-image: {{ .Chart.Name }}
{{- end }}

{{- define "aif-ui.image" -}}
{{- $registry := coalesce .Values.global.imageRegistry .Values.image.registry -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- if $registry -}}
{{ $registry }}/{{ .Values.image.repository }}:{{ $tag }}
{{- else -}}
{{ .Values.image.repository }}:{{ $tag }}
{{- end -}}
{{- end }}

{{- define "aif-ui.imagePullSecrets" -}}
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
{{- range .Values.imagePullSecrets }}
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
{{- end }}

{{/*
Service DNS name. The "-svc" suffix must fit inside the 63-character RFC 1035
label limit, so the fullname is truncated to 59 first — truncating afterwards
would silently produce a 64-character name for a 53-character release name.

Standalone mode uses "<chart name>-svc" instead, whatever the release is
called. In that mode Rancher installs the extension chart this server
publishes, and that chart's UIPlugin endpoint is fixed at build time by
@rancher/shell to http://<image>-svc.cattle-ui-plugin-system:8080, the name
Rancher's own "Import Extension Catalog" dialog gives the Service. The chart
name and the image name are both the extension's package name.
*/}}
{{- define "aif-ui.serviceName" -}}
{{- if .Values.standalone -}}
{{- printf "%s-svc" .Chart.Name -}}
{{- else -}}
{{- printf "%s-svc" (include "aif-ui.fullname" . | trunc 59 | trimSuffix "-") -}}
{{- end -}}
{{- end }}

{{/*
Fails a standalone render that the extension, once Rancher installs it, could
not work with. Each check is a value the extension chart's built-in endpoint
fixes (see aif-ui.serviceName), or a name Rancher's install would collide with.
*/}}
{{- define "aif-ui.validateStandalone" -}}
{{- if eq .Release.Name .Chart.Name }}
{{- fail (printf "standalone mode: the release must not be named %q. Rancher installs the extension itself as a release of that name, and would overwrite this one. Install this chart under another name, for example %s-server." .Chart.Name .Chart.Name) }}
{{- end }}
{{- if ne .Release.Namespace "cattle-ui-plugin-system" }}
{{- fail (printf "standalone mode: install into the cattle-ui-plugin-system namespace, not %q. Rancher only runs extensions from there, and the extension expects this server there." .Release.Namespace) }}
{{- end }}
{{- if ne (int .Values.service.port) 8080 }}
{{- fail (printf "standalone mode: service.port must be 8080, not %v. The extension Rancher installs expects this server on port 8080." .Values.service.port) }}
{{- end }}
{{- end }}
