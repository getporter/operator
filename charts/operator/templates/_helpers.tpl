{{- define "mongodb.url" -}}
{{- if .Values.mongodb.enabled }}
    {{- printf "%s%s%s-%s.%s.%s" "mongodb" "://" .Release.Name "mongodb" .Release.Namespace "svc.cluster.local" -}}
{{- else  }}
    {{- printf "%s" .Values.mongodb.url -}}
{{- end -}}
{{- end -}}