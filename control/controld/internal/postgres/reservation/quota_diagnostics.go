package reservation

import (
	"fmt"
	"strconv"
	"strings"

	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	namespaceQuotaExceededReason = string(resourcekernel.AdmissionRejectionNamespaceQuotaExceeded)
	namespaceQuotaErrorDomain    = resourcekernel.AdmissionErrorDomain
)

type quotaDiagnosticResource string

const (
	quotaDiagnosticResourceCPU              quotaDiagnosticResource = "cpu"
	quotaDiagnosticResourceMemory           quotaDiagnosticResource = "memory"
	quotaDiagnosticResourceEphemeralStorage quotaDiagnosticResource = "ephemeral_storage"
)

type quotaDiagnosticUnit string

const (
	quotaDiagnosticUnitMilli quotaDiagnosticUnit = "milli"
	quotaDiagnosticUnitBytes quotaDiagnosticUnit = "bytes"
)

func quotaRejectionError(namespace string, evaluation resourcekernel.QuotaEvaluation) error {
	st := grpcstatus.New(codes.ResourceExhausted, quotaRejectionMessage(namespace, evaluation))
	info := &errdetails.ErrorInfo{
		Reason:   namespaceQuotaExceededReason,
		Domain:   namespaceQuotaErrorDomain,
		Metadata: quotaRejectionMetadata(namespace, evaluation),
	}
	withDetails, err := st.WithDetails(info)
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func quotaRejectionMessage(namespace string, evaluation resourcekernel.QuotaEvaluation) string {
	parts := []string{fmt.Sprintf("namespace quota exceeded: namespace=%s", namespace)}
	if evaluation.CPU.Requested > 0 && !evaluation.CPU.Fits {
		parts = append(parts, quotaResourceMessage(quotaDiagnosticResourceCPU, quotaDiagnosticUnitMilli, evaluation.CPU))
	}
	if evaluation.Memory.Requested > 0 && !evaluation.Memory.Fits {
		parts = append(parts, quotaResourceMessage(quotaDiagnosticResourceMemory, quotaDiagnosticUnitBytes, evaluation.Memory))
	}
	if evaluation.EphemeralStorage.Requested > 0 && !evaluation.EphemeralStorage.Fits {
		parts = append(parts, quotaResourceMessage(quotaDiagnosticResourceEphemeralStorage, quotaDiagnosticUnitBytes, evaluation.EphemeralStorage))
	}
	return strings.Join(parts, " ")
}

func quotaResourceMessage(resource quotaDiagnosticResource, unit quotaDiagnosticUnit, evaluation resourcekernel.QuotaResourceEvaluation) string {
	limit := int64(0)
	if evaluation.Limit != nil {
		limit = *evaluation.Limit
	}
	available := int64(0)
	if evaluation.Available != nil {
		available = *evaluation.Available
	}
	return fmt.Sprintf("%s requested_%s=%d reserved_%s=%d limit_%s=%d available_%s=%d",
		resource,
		unit,
		evaluation.Requested,
		unit,
		evaluation.Used,
		unit,
		limit,
		unit,
		available,
	)
}

func quotaRejectionMetadata(namespace string, evaluation resourcekernel.QuotaEvaluation) map[string]string {
	metadata := map[string]string{
		"namespace":       namespace,
		"diagnostic_code": string(resourcekernel.AdmissionDiagnosticNamespaceQuotaExceeded),
	}
	resources := make([]string, 0, 3)
	if evaluation.CPU.Requested > 0 && !evaluation.CPU.Fits {
		resources = append(resources, string(quotaDiagnosticResourceCPU))
		addQuotaResourceMetadata(metadata, quotaDiagnosticResourceCPU, quotaDiagnosticUnitMilli, evaluation.CPU)
	}
	if evaluation.Memory.Requested > 0 && !evaluation.Memory.Fits {
		resources = append(resources, string(quotaDiagnosticResourceMemory))
		addQuotaResourceMetadata(metadata, quotaDiagnosticResourceMemory, quotaDiagnosticUnitBytes, evaluation.Memory)
	}
	if evaluation.EphemeralStorage.Requested > 0 && !evaluation.EphemeralStorage.Fits {
		resources = append(resources, string(quotaDiagnosticResourceEphemeralStorage))
		addQuotaResourceMetadata(metadata, quotaDiagnosticResourceEphemeralStorage, quotaDiagnosticUnitBytes, evaluation.EphemeralStorage)
	}
	if len(resources) > 0 {
		metadata["resources"] = strings.Join(resources, ",")
	}
	return metadata
}

func addQuotaResourceMetadata(metadata map[string]string, resource quotaDiagnosticResource, unit quotaDiagnosticUnit, evaluation resourcekernel.QuotaResourceEvaluation) {
	limit := int64(0)
	if evaluation.Limit != nil {
		limit = *evaluation.Limit
	}
	available := int64(0)
	if evaluation.Available != nil {
		available = *evaluation.Available
	}
	prefix := string(resource) + "_"
	metadata[prefix+"unit"] = string(unit)
	metadata[prefix+"requested"] = strconv.FormatInt(evaluation.Requested, 10)
	metadata[prefix+"reserved"] = strconv.FormatInt(evaluation.Used, 10)
	metadata[prefix+"limit"] = strconv.FormatInt(limit, 10)
	metadata[prefix+"available"] = strconv.FormatInt(available, 10)
}
