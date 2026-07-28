package sandboxd

import apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"

func managedProxySpec(spec *apipb.ManagedProxySpec) *ManagedProxySpec {
	if spec == nil {
		return nil
	}
	return &ManagedProxySpec{
		Provider:            spec.GetProvider(),
		UpstreamBaseURL:     spec.GetUpstreamBaseUrl(),
		UpstreamBearerToken: spec.GetUpstreamBearerToken(),
	}
}

func managedProxyReport(report *ManagedProxyReport) *apipb.ManagedProxyReport {
	if report == nil {
		return nil
	}
	return &apipb.ManagedProxyReport{
		Provider:      report.Provider,
		RequestCount:  int32(report.RequestCount),
		ResponseCount: int32(report.ResponseCount),
		ErrorCount:    int32(report.ErrorCount),
		ReportJson:    append([]byte(nil), report.ReportJSON...),
	}
}
