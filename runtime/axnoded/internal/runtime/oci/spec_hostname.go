package oci

import (
	"regexp"
	"strings"
	"unicode"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	defaultSandboxHostname = "sandbox"
	maxDNSLabelLength      = 63
	maxServiceNameLength   = 32
)

var serviceUUIDPattern = regexp.MustCompile(`^svc-([0-9a-f]{8})-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func runtimeHostnameAnnotationKey() string {
	return workloadidentity.LabelKeyHostname
}

func requestedHostnameAnnotation(request *apipb.CreateContainerRequest, additionalAnnotations map[string]string) string {
	hostname := ""
	if request != nil {
		hostname = strings.TrimSpace(request.GetLabels()[workloadidentity.LabelKeyHostname])
	}
	if value := strings.TrimSpace(additionalAnnotations[workloadidentity.LabelKeyHostname]); value != "" {
		hostname = value
	}
	return hostname
}

func applyHostname(ociSpec *spec.Spec, request *apipb.CreateContainerRequest, containerID string, explicitHostname string) {
	if ociSpec == nil {
		return
	}
	hostname := workloadHostname(ociSpec.Annotations, request, containerID, explicitHostname)
	ociSpec.Hostname = hostname
	ociSpec.Annotations = combineAnnotations(ociSpec.Annotations, map[string]string{
		workloadidentity.LabelKeyHostname: hostname,
	})
}

func workloadHostname(annotations map[string]string, request *apipb.CreateContainerRequest, containerID string, explicitHostname string) string {
	if explicit := sanitizeDNSLabel(explicitHostname); explicit != "" {
		return explicit
	}

	allocationID := strings.TrimSpace(annotations[workloadidentity.LabelKeyAllocationID])
	if allocationID == "" && request != nil {
		allocationID = strings.TrimSpace(request.GetID())
	}
	if allocationID == "" {
		allocationID = strings.TrimSpace(containerID)
	}

	serviceID := shortServiceIdentity(annotations[workloadidentity.LabelKeyServiceID])
	if serviceID != "" {
		if suffix := shortIdentity(allocationID); suffix != "" {
			return joinHostnameParts(serviceID, suffix)
		}
		return serviceID
	}
	if suffix := shortIdentity(allocationID); suffix != "" {
		return joinHostnameParts("alloc", suffix)
	}
	if runtimeID := sanitizeDNSLabel(annotations[workloadidentity.LabelKeyRuntimeID]); runtimeID != "" {
		return runtimeID
	}
	return defaultSandboxHostname
}

func joinHostnameParts(prefix, suffix string) string {
	prefix = sanitizeDNSLabel(prefix)
	suffix = sanitizeDNSLabel(suffix)
	switch {
	case prefix == "":
		return firstDNSLabel(suffix)
	case suffix == "":
		return firstDNSLabel(prefix)
	}
	maxPrefix := maxDNSLabelLength - len(suffix) - 1
	if maxPrefix < 1 {
		return firstDNSLabel(suffix)
	}
	if len(prefix) > maxPrefix {
		prefix = trimDNSLabel(prefix[:maxPrefix])
	}
	return firstDNSLabel(prefix + "-" + suffix)
}

func shortIdentity(value string) string {
	value = sanitizeDNSLabel(value)
	if value == "" {
		return ""
	}
	if len(value) <= 12 {
		return value
	}
	return trimDNSLabel(value[:12])
}

func shortServiceIdentity(value string) string {
	value = sanitizeDNSLabel(value)
	if value == "" {
		return ""
	}
	if matches := serviceUUIDPattern.FindStringSubmatch(value); len(matches) == 2 {
		return "svc-" + matches[1]
	}
	if len(value) <= maxServiceNameLength {
		return value
	}
	return trimDNSLabel(value[:maxServiceNameLength])
}

func firstDNSLabel(value string) string {
	value = sanitizeDNSLabel(value)
	if len(value) <= maxDNSLabelLength {
		return value
	}
	return trimDNSLabel(value[:maxDNSLabelLength])
}

func sanitizeDNSLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	lastHyphen := false
	for _, r := range value {
		valid := unicode.IsLetter(r) || unicode.IsDigit(r)
		if valid && r <= unicode.MaxASCII {
			out.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	return trimDNSLabel(out.String())
}

func trimDNSLabel(value string) string {
	value = strings.Trim(value, "-")
	for len(value) > maxDNSLabelLength {
		value = strings.TrimRight(value[:maxDNSLabelLength], "-")
	}
	return value
}
