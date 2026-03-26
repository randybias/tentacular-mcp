{{/*
Expand the name of the chart.
*/}}
{{- define "tentacular-mcp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated at 63 chars because some Kubernetes name fields are limited to this.
If release name contains chart name it will be used as a full name.
*/}}
{{- define "tentacular-mcp.fullname" -}}
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
{{- define "tentacular-mcp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "tentacular-mcp.labels" -}}
helm.sh/chart: {{ include "tentacular-mcp.chart" . }}
{{ include "tentacular-mcp.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "tentacular-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tentacular-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Container image reference.
*/}}
{{- define "tentacular-mcp.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.repository $tag -}}
{{- end }}

{{/*
Auth secret name.
Returns existing secret name if set, otherwise the generated secret name.
*/}}
{{- define "tentacular-mcp.authSecretName" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else }}
{{- printf "%s-auth" (include "tentacular-mcp.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Namespace: use namespaceOverride if set, otherwise .Release.Namespace.
This allows umbrella charts to place MCP resources in a specific namespace.
*/}}
{{- define "tentacular-mcp.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "tentacular-mcp.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- include "tentacular-mcp.fullname" . }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
RustFS CA certificate secret name.
Defaults to "rustfs-root-ca-secret", matching a standard standalone RustFS
deployment (helm install rustfs charts/rustfs). Override via rustfsCa.secretName
when RustFS uses a non-standard release name or fullnameOverride.
*/}}
{{- define "tentacular-mcp.rustfsCa.secretName" -}}
{{- if .Values.rustfsCa.secretName -}}
  {{- .Values.rustfsCa.secretName -}}
{{- else -}}
  {{- "rustfs-root-ca-secret" -}}
{{- end -}}
{{- end }}
