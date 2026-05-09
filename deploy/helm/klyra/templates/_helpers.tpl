{{/* deploy/helm/klyra/templates/_helpers.tpl */}}
{{- define "klyra.name" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "klyra.namespace" -}}
{{- default .Release.Namespace .Values.namespace }}
{{- end }}

{{- define "klyra.labels" -}}
app.kubernetes.io/name: klyra
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
