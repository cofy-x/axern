package observability

import (
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

const AttrServiceInstanceID = "service.instance.id"

const (
	AttrServiceID        = "axern.service_id"
	AttrRunID            = "axern.run_id"
	AttrAllocationID     = "axern.allocation_id"
	AttrNodeID           = "axern.node_id"
	AttrEnvironmentID    = "axern.environment_id"
	AttrFunctionID       = "axern.function_id"
	AttrInvocationID     = "axern.invocation_id"
	AttrRuntime          = "axern.runtime"
	AttrRootFSType       = "axern.rootfs_type"
	AttrImageRef         = "axern.image_ref"
	AttrMountType        = "axern.mount_type"
	AttrSource           = "axern.source"
	AttrResult           = "axern.result"
	AttrDiagnosticCode   = "axern.diagnostic_code"
	AttrErrorClass       = "axern.error_class"
	AttrComponent        = "axern.component"
	AttrOperation        = "axern.operation"
	AttrEvent            = "axern.event"
	AttrNamespace        = "axern.namespace"
	AttrContainerPort    = "axern.container_port"
	AttrPortRef          = "axern.port_ref"
	AttrServiceEventType = "axern.service_event_type"
	AttrStatus           = "axern.status"
	AttrOwnerType        = "axern.owner_type"
	AttrResource         = "axern.resource"
	AttrStorage          = "axern.storage"
	AttrState            = "axern.state"
	AttrKind             = "axern.kind"
	AttrTrigger          = "axern.trigger"
	AttrReady            = "axern.ready"
	AttrReason           = "axern.reason"
	AttrStage            = "axern.stage"
	AttrPhase            = "axern.phase"
	AttrStep             = "axern.step"
	AttrStartClass       = "axern.start_class"
	AttrPath             = "axern.path"
	AttrProbeKind        = "axern.probe_kind"
	AttrProbeType        = "axern.probe_type"
	AttrRouteType        = "axern.route_type"
	AttrHTTPMethod       = "http.request.method"
	AttrHTTPStatusCode   = "http.status_code"
)

var sensitiveKeyFragments = []string{
	"token",
	"secret",
	"private_key",
	"public_key",
	"authorized_key",
	"password",
	"stdin",
	"stdout",
	"stderr",
}

func StringAttr(key, value string) attribute.KeyValue {
	if strings.TrimSpace(value) == "" {
		return attribute.String(key, "")
	}
	if SensitiveKey(key) {
		return attribute.String(key, "[redacted]")
	}
	return attribute.String(key, SanitizeValue(value))
}

func SensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func SanitizeValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 256 {
		return value
	}
	return value[:256] + "...[truncated]"
}

func SanitizeLogBody(value string) string {
	value = strings.TrimSpace(value)
	normalized := strings.ToLower(strings.ReplaceAll(value, "-", "_"))
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return "[redacted]"
		}
	}
	return SanitizeValue(value)
}

func SafeAttrs(attrs ...attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		if SensitiveKey(string(attr.Key)) {
			out = append(out, attribute.String(string(attr.Key), "[redacted]"))
			continue
		}
		out = append(out, attr)
	}
	return out
}
