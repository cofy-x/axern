{{- define "axern.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "axern.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "axern.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
app.kubernetes.io/name: {{ include "axern.name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{- define "axern.selectorLabels" -}}
app.kubernetes.io/name: {{ include "axern.name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
{{- end -}}

{{- define "axern.componentLabels" -}}
{{ include "axern.labels" .root }}
app.kubernetes.io/component: {{ .component | quote }}
{{- end -}}

{{- define "axern.componentSelectorLabels" -}}
{{ include "axern.selectorLabels" .root }}
app.kubernetes.io/component: {{ .component | quote }}
{{- end -}}

{{- define "axern.scheduling" -}}
{{- $profile := index .root.Values.scheduling .profile -}}
{{- with $profile.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $profile.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- if and $profile.topologySpread.enabled .spread }}
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: {{ $profile.topologySpread.whenUnsatisfiable }}
    labelSelector:
      matchLabels:
        {{- include "axern.componentSelectorLabels" (dict "root" .root "component" .component) | nindent 8 }}
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: {{ $profile.topologySpread.whenUnsatisfiable }}
    labelSelector:
      matchLabels:
        {{- include "axern.componentSelectorLabels" (dict "root" .root "component" .component) | nindent 8 }}
{{- end }}
{{- end -}}

{{- define "axern.image" -}}
{{- $root := .root -}}
{{- $image := .image -}}
{{- $repo := required "image.repository is required" $image.repository -}}
{{- $resolvedRepo := $repo -}}
{{- if and $root.Values.global.imageRegistry (not (contains "/" $repo | and (contains "." (first (splitList "/" $repo))))) -}}
{{- $resolvedRepo = printf "%s/%s" ($root.Values.global.imageRegistry | trimSuffix "/") $repo -}}
{{- end -}}
{{- if $image.digest -}}
{{- printf "%s@%s" $resolvedRepo $image.digest -}}
{{- else -}}
{{- printf "%s:%s" $resolvedRepo (required "image.tag is required when image.digest is empty" $image.tag) -}}
{{- end -}}
{{- end -}}

{{/* Optional backends are operator-selected artifacts, outside Axern image delivery. */}}
{{- define "axern.observabilityImage" -}}
{{- $label := printf "observability.%s.image" .name -}}
{{- $repo := required (printf "%s.repository is required when observability is enabled" $label) .image.repository -}}
{{- $registry := first (splitList "/" $repo) -}}
{{- if not (and (contains "/" $repo) (or (contains "." $registry) (contains ":" $registry) (eq "localhost" $registry))) -}}
{{- fail (printf "%s.repository must include an explicit registry hostname" $label) -}}
{{- end -}}
{{- $digest := required (printf "%s.digest is required; qualify and pin the operator-owned backend" $label) .image.digest -}}
{{- if not (regexMatch "^sha256:[a-f0-9]{64}$" $digest) -}}
{{- fail (printf "%s.digest must be a sha256 digest" $label) -}}
{{- end -}}
{{- printf "%s@%s" $repo $digest -}}
{{- end -}}

{{- define "axern.pullPolicy" -}}
{{- default .root.Values.global.imagePullPolicy .image.pullPolicy -}}
{{- end -}}

{{- define "axern.pkiSecretName" -}}
{{- required "pki.secretName is required; provision it with the repository PKI bootstrap tool" .Values.pki.secretName -}}
{{- end -}}

{{- define "axern.secretsSecretName" -}}
{{- default .Values.secrets.secretName .Values.secrets.existingSecret -}}
{{- end -}}

{{- define "axern.gatewaySSHSecretName" -}}
{{- default .Values.gatewayd.ssh.secretName .Values.gatewayd.ssh.existingSecret -}}
{{- end -}}

{{- define "axern.nodeResourceSource" -}}
{{- $source := lower (trim (default "host" .Values.node.resourceSource)) -}}
{{- if not (or (eq $source "host") (eq $source "kubernetes")) -}}
{{- fail "node.resourceSource must be either \"host\" or \"kubernetes\"" -}}
{{- end -}}
{{- $source -}}
{{- end -}}

{{- define "axern.nodeServiceAccountName" -}}
{{- $name := trim .Values.node.serviceAccount.name -}}
{{- if $name -}}
{{- $name -}}
{{- else if .Values.node.serviceAccount.create -}}
{{- printf "%s-node" (include "axern.fullname" .) -}}
{{- else if eq (include "axern.nodeResourceSource" .) "kubernetes" -}}
{{- fail "node.serviceAccount.name is required when node.serviceAccount.create=false and node.resourceSource=kubernetes" -}}
{{- else -}}
{{- printf "%s-node" (include "axern.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "axern.postgresSecretName" -}}
{{- default .Values.postgres.secretName .Values.postgres.existingSecret -}}
{{- end -}}

{{- define "axern.controldTarget" -}}
{{- printf "controld.%s.svc:24000" .Release.Namespace -}}
{{- end -}}

{{- define "axern.tunneldTarget" -}}
{{- printf "tunneld.%s.svc:24100" .Release.Namespace -}}
{{- end -}}

{{- define "axern.tunnelRelays" -}}
{{- printf "%s,%s,%s,1,%t" .Values.tunneld.relayID .Values.gatewayd.controlEdge.publicAddress (include "axern.tunneldTarget" .) .Values.tunneld.drain -}}
{{- end -}}

{{- define "axern.otelEndpoint" -}}
{{- default (printf "http://otel-collector.%s.svc:4317" .Release.Namespace) .Values.observability.otlp.endpoint -}}
{{- end -}}

{{- define "axern.otelEnv" -}}
{{- if or .Values.observability.enabled (trim .Values.observability.otlp.endpoint) }}
- name: AXERN_OTEL_ENABLED
  value: "true"
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ include "axern.otelEndpoint" . | quote }}
- name: OTEL_EXPORTER_OTLP_INSECURE
  value: {{ .Values.observability.otlp.insecure | quote }}
- name: OTEL_METRIC_EXPORT_INTERVAL
  value: {{ .Values.observability.metricExportIntervalMilliseconds | quote }}
- name: OTEL_RESOURCE_ATTRIBUTES
  value: {{ .Values.observability.resourceAttributes | quote }}
{{- else }}
- name: AXERN_OTEL_ENABLED
  value: "false"
{{- end }}
{{- end -}}
