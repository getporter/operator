{{- define "mongodb.url" -}}
{{- if .Values.mongodb.enabled }}
    {{- printf "%s.%s.%s" "mongodb://mongodb" .Release.Namespace "svc.cluster.local" -}}
{{- else  }}
    {{- printf "%s" .Values.mongodb.url -}}
{{- end -}}
{{- end -}}