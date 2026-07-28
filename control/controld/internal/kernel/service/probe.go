package servicekernel

import (
	"strings"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func defaultProbeHTTPPath() string {
	return "/"
}

func defaultProbe() *servicev1.ServiceProbe {
	return &servicev1.ServiceProbe{
		Period:           durationpb.New(5 * time.Second),
		Timeout:          durationpb.New(2 * time.Second),
		SuccessThreshold: 1,
		FailureThreshold: 1,
	}
}

func normalizeProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	if in == nil {
		return nil
	}
	out := proto.Clone(in).(*servicev1.ServiceProbe)
	defaults := defaultProbe()
	if out.GetPeriod() == nil {
		out.Period = proto.Clone(defaults.GetPeriod()).(*durationpb.Duration)
	}
	if out.GetTimeout() == nil {
		out.Timeout = proto.Clone(defaults.GetTimeout()).(*durationpb.Duration)
	}
	if out.GetSuccessThreshold() <= 0 {
		out.SuccessThreshold = defaults.GetSuccessThreshold()
	}
	if out.GetFailureThreshold() <= 0 {
		out.FailureThreshold = defaults.GetFailureThreshold()
	}
	if http := out.GetHttp(); http != nil {
		if strings.TrimSpace(http.GetPath()) == "" {
			http.Path = defaultProbeHTTPPath()
		}
		if http.GetScheme() == servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_UNSPECIFIED {
			http.Scheme = servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTP
		}
	}
	return out
}

func validateAndNormalizeProbe(field string, in *servicev1.ServiceProbe) (*servicev1.ServiceProbe, error) {
	if in == nil {
		return nil, nil
	}
	actionCount := 0
	if in.GetHttp() != nil {
		actionCount++
	}
	if in.GetTcp() != nil {
		actionCount++
	}
	if actionCount != 1 {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "service %s must set exactly one of http or tcp", field)
	}
	if err := validateProbeDuration(field+".initial_delay", in.GetInitialDelay(), true); err != nil {
		return nil, err
	}
	if err := validateProbeDuration(field+".period", in.GetPeriod(), false); err != nil {
		return nil, err
	}
	if err := validateProbeDuration(field+".timeout", in.GetTimeout(), false); err != nil {
		return nil, err
	}
	if in.GetSuccessThreshold() < 0 {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "service %s.success_threshold must be > 0 when set", field)
	}
	if in.GetFailureThreshold() < 0 {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "service %s.failure_threshold must be > 0 when set", field)
	}
	if http := in.GetHttp(); http != nil {
		if http.GetPort() <= 0 || http.GetPort() > 65535 {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "service %s.http.port must be in 1..65535", field)
		}
	}
	if tcp := in.GetTcp(); tcp != nil {
		if tcp.GetPort() <= 0 || tcp.GetPort() > 65535 {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "service %s.tcp.port must be in 1..65535", field)
		}
	}
	return normalizeProbe(in), nil
}

func validateProbeDuration(field string, value *durationpb.Duration, allowZero bool) error {
	if value == nil {
		return nil
	}
	if err := value.CheckValid(); err != nil {
		return grpcstatus.Errorf(codes.InvalidArgument, "service %s is invalid: %v", field, err)
	}
	duration := value.AsDuration()
	if duration < 0 || (!allowZero && duration == 0) {
		operator := "> 0"
		if allowZero {
			operator = ">= 0"
		}
		return grpcstatus.Errorf(codes.InvalidArgument, "service %s must be %s", field, operator)
	}
	if duration%time.Millisecond != 0 {
		return grpcstatus.Errorf(codes.InvalidArgument, "service %s must use whole milliseconds", field)
	}
	return nil
}

func ValidateAndNormalizeReadinessProbe(in *servicev1.ServiceProbe) (*servicev1.ServiceProbe, error) {
	return validateAndNormalizeProbe("readiness_probe", in)
}

func ValidateAndNormalizeLivenessProbe(in *servicev1.ServiceProbe) (*servicev1.ServiceProbe, error) {
	return validateAndNormalizeProbe("liveness_probe", in)
}

func cloneReadinessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return cloneProbe(in)
}

func CloneReadinessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return cloneReadinessProbe(in)
}

func cloneLivenessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return cloneProbe(in)
}

func CloneLivenessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return cloneLivenessProbe(in)
}

func normalizeReadinessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return normalizeProbe(in)
}

func NormalizeReadinessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return normalizeReadinessProbe(in)
}

func normalizeLivenessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return normalizeProbe(in)
}

func NormalizeLivenessProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	return normalizeLivenessProbe(in)
}
