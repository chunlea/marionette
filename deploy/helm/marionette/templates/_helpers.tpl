{{/*
Expand the name of the chart.
*/}}
{{- define "marionette.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "marionette.fullname" -}}
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
{{- define "marionette.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "marionette.labels" -}}
helm.sh/chart: {{ include "marionette.chart" . }}
{{ include "marionette.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "marionette.selectorLabels" -}}
app.kubernetes.io/name: {{ include "marionette.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Server labels
*/}}
{{- define "marionette.server.labels" -}}
{{ include "marionette.labels" . }}
app.kubernetes.io/component: server
{{- end }}

{{/*
Server selector labels
*/}}
{{- define "marionette.server.selectorLabels" -}}
{{ include "marionette.selectorLabels" . }}
app.kubernetes.io/component: server
{{- end }}

{{/*
Agent labels
*/}}
{{- define "marionette.agent.labels" -}}
{{ include "marionette.labels" . }}
app.kubernetes.io/component: agent
{{- end }}

{{/*
Agent selector labels
*/}}
{{- define "marionette.agent.selectorLabels" -}}
{{ include "marionette.selectorLabels" . }}
app.kubernetes.io/component: agent
{{- end }}

{{/*
Create the name of the server service account
*/}}
{{- define "marionette.server.serviceAccountName" -}}
{{- if .Values.server.serviceAccount.create }}
{{- default (printf "%s-server" (include "marionette.fullname" .)) .Values.server.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.server.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the agent service account
*/}}
{{- define "marionette.agent.serviceAccountName" -}}
{{- if .Values.agent.serviceAccount.create }}
{{- default (printf "%s-agent" (include "marionette.fullname" .)) .Values.agent.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.agent.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Server image
*/}}
{{- define "marionette.server.image" -}}
{{- $tag := default .Chart.AppVersion .Values.server.image.tag }}
{{- printf "%s:%s" .Values.server.image.repository $tag }}
{{- end }}

{{/*
Agent image
*/}}
{{- define "marionette.agent.image" -}}
{{- $tag := default .Chart.AppVersion .Values.agent.image.tag }}
{{- printf "%s:%s" .Values.agent.image.repository $tag }}
{{- end }}

{{/*
Database URL - uses subchart if enabled, otherwise uses secret
*/}}
{{- define "marionette.databaseUrl" -}}
{{- if .Values.postgresql.enabled }}
{{- printf "postgres://%s:%s@%s-postgresql:5432/%s?sslmode=disable" .Values.postgresql.auth.username .Values.postgresql.auth.password (include "marionette.fullname" .) .Values.postgresql.auth.database }}
{{- else }}
{{- .Values.secrets.databaseUrl }}
{{- end }}
{{- end }}
