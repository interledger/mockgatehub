{{/* Expand the name of the chart. */}}
{{- define "mockgatehub.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name, truncated to 63 chars for label limits.
*/}}
{{- define "mockgatehub.fullname" -}}
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

{{- define "mockgatehub.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mockgatehub.labels" -}}
helm.sh/chart: {{ include "mockgatehub.chart" . }}
{{ include "mockgatehub.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "mockgatehub.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mockgatehub.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "mockgatehub.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "mockgatehub.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding credentials — either the one we create or the
existing one the operator pointed us at.
*/}}
{{- define "mockgatehub.secretName" -}}
{{- if .Values.credentials.existingSecret }}
{{- .Values.credentials.existingSecret }}
{{- else }}
{{- printf "%s-credentials" (include "mockgatehub.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Service name of the bundled Valkey. Mirrors the subchart's own fullname helper,
which is what its Service is named.
*/}}
{{- define "mockgatehub.valkeyServiceName" -}}
{{- if .Values.valkey.fullnameOverride }}
{{- .Values.valkey.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "valkey" .Values.valkey.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Redis connection URL. Prefers the bundled Valkey, then an external instance.
Empty means MockGatehub falls back to in-memory storage with webhook delivery
disabled, which is a legitimate lightweight mode rather than an error.
*/}}
{{- define "mockgatehub.redisUrl" -}}
{{- if .Values.valkey.enabled }}
{{- printf "redis://%s:%v" (include "mockgatehub.valkeyServiceName" .) (.Values.valkey.service.serverPort | default 6379) }}
{{- else }}
{{- .Values.externalRedis.url }}
{{- end }}
{{- end }}

{{/*
Externally reachable base URL. Falls back to the in-cluster service address,
which is right for server-side callers; a browser following a card-data link
needs publicBaseUrl set to an ingress host.
*/}}
{{- define "mockgatehub.publicBaseUrl" -}}
{{- if .Values.config.publicBaseUrl }}
{{- .Values.config.publicBaseUrl | trimSuffix "/" }}
{{- else }}
{{- printf "http://%s.%s.svc.cluster.local:%v" (include "mockgatehub.fullname" .) .Release.Namespace .Values.service.port }}
{{- end }}
{{- end }}
