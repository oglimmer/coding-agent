{{/* Chart name */}}
{{- define "coding-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Fully qualified app name */}}
{{- define "coding-agent.fullname" -}}
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

{{- define "coding-agent.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Common labels */}}
{{- define "coding-agent.labels" -}}
helm.sh/chart: {{ include "coding-agent.chart" . }}
{{ include "coding-agent.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "coding-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "coding-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Per-component fullnames */}}
{{- define "coding-agent.backend.fullname" -}}
{{- printf "%s-backend" (include "coding-agent.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- define "coding-agent.frontend.fullname" -}}
{{- printf "%s-frontend" (include "coding-agent.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- define "coding-agent.postgres.fullname" -}}
{{- printf "%s-postgres" (include "coding-agent.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Service account name */}}
{{- define "coding-agent.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "coding-agent.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* Secret name */}}
{{- define "coding-agent.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- printf "%s-secret" (include "coding-agent.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/* Database URL: external DATABASE_URL secret key or bundled Postgres DSN */}}
{{- define "coding-agent.databaseEnv" -}}
{{- if .Values.postgres.enabled }}
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "coding-agent.secretName" . }}
      key: POSTGRES_PASSWORD
- name: DATABASE_URL
  value: "postgres://{{ .Values.postgres.user }}:$(POSTGRES_PASSWORD)@{{ include "coding-agent.postgres.fullname" . }}:5432/{{ .Values.postgres.database }}?sslmode=disable"
{{- else }}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "coding-agent.secretName" . }}
      key: DATABASE_URL
{{- end }}
{{- end }}
