{{/*
Expand the name of the chart.
*/}}
{{- define "tentacular-observability.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated at 63 chars because some Kubernetes name fields are limited to this.
*/}}
{{- define "tentacular-observability.fullname" -}}
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
{{- define "tentacular-observability.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "tentacular-observability.labels" -}}
helm.sh/chart: {{ include "tentacular-observability.chart" . }}
{{ include "tentacular-observability.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "tentacular-observability.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tentacular-observability.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
The fixed namespace for all observability resources.
This is hardcoded to satisfy the well-known DNS contract:
  otel-collector.tentacular-observability.svc.cluster.local
*/}}
{{- define "tentacular-observability.namespace" -}}
tentacular-observability
{{- end }}

{{/*
ServiceAccount name for the OTel Collector.
*/}}
{{- define "tentacular-observability.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- printf "%s-collector" (include "tentacular-observability.fullname" .) }}
{{- else }}
default
{{- end }}
{{- end }}
