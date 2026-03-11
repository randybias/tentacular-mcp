{{/*
Expand the name of the chart.
*/}}
{{- define "tentacular-platform.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "tentacular-platform.fullname" -}}
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
{{- define "tentacular-platform.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "tentacular-platform.labels" -}}
helm.sh/chart: {{ include "tentacular-platform.chart" . }}
{{ include "tentacular-platform.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "tentacular-platform.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tentacular-platform.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Namespace name helpers - resolve configured namespace names with defaults.
*/}}
{{- define "tentacular-platform.namespace.system" -}}
{{- default "tentacular-system" .Values.namespaces.system.name }}
{{- end }}

{{- define "tentacular-platform.namespace.exoskeleton" -}}
{{- default "tentacular-exoskeleton" .Values.namespaces.exoskeleton.name }}
{{- end }}

{{- define "tentacular-platform.namespace.support" -}}
{{- default "tentacular-support" .Values.namespaces.support.name }}
{{- end }}
