package oci

import (
	"strings"
	"unicode"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	defaultSandboxHostname = "sandbox"
	maxDNSLabelLength      = 63
)

func applyHostname(ociSpec *spec.Spec, request *apipb.CreateContainerRequest, containerID string) {
	if ociSpec == nil {
		return
	}
	ociSpec.Hostname = workloadHostname(request, containerID)
}

func workloadHostname(request *apipb.CreateContainerRequest, containerID string) string {
	allocationID := ""
	if request != nil {
		allocationID = strings.TrimSpace(request.GetID())
	}
	if allocationID == "" {
		allocationID = strings.TrimSpace(containerID)
	}

	if suffix := shortAllocationIdentity(allocationID); suffix != "" {
		return joinHostnameParts("alloc", suffix)
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

func shortAllocationIdentity(value string) string {
	value = sanitizeDNSLabel(value)
	value = strings.TrimPrefix(value, "alloc-")
	return shortIdentity(value)
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
