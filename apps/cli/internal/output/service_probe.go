package output

import (
	"fmt"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

func formatServiceProbe(probe *servicev1.ServiceProbe) string {
	if probe == nil {
		return ""
	}
	var action string
	switch typed := probe.GetAction().(type) {
	case *servicev1.ServiceProbe_Http:
		scheme := "http"
		if typed.Http != nil && typed.Http.GetScheme() == servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTPS {
			scheme = "https"
		}
		path := "/"
		if typed.Http != nil && typed.Http.GetPath() != "" {
			path = typed.Http.GetPath()
		}
		action = fmt.Sprintf("%s port=%d path=%s", scheme, typed.Http.GetPort(), path)
	case *servicev1.ServiceProbe_Tcp:
		action = fmt.Sprintf("tcp port=%d", typed.Tcp.GetPort())
	default:
		action = "unknown"
	}
	return fmt.Sprintf(
		"%s initial_delay=%s period=%s timeout=%s success_threshold=%d failure_threshold=%d",
		action,
		formatProbeDuration(probe.GetInitialDelay()),
		formatProbeDuration(probe.GetPeriod()),
		formatProbeDuration(probe.GetTimeout()),
		probe.GetSuccessThreshold(),
		probe.GetFailureThreshold(),
	)
}

func formatProbeDuration(value *durationpb.Duration) string {
	if value == nil {
		return "0s"
	}
	return value.AsDuration().String()
}
