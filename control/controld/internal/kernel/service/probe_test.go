package servicekernel

import (
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestValidateAndNormalizeReadinessProbeSupportsSubsecondPeriod(t *testing.T) {
	probe, err := ValidateAndNormalizeReadinessProbe(&servicev1.ServiceProbe{
		Action: &servicev1.ServiceProbe_Http{Http: &servicev1.HttpProbe{Port: 8080}},
		Period: durationpb.New(100 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("ValidateAndNormalizeReadinessProbe() error = %v", err)
	}
	if got := probe.GetPeriod().AsDuration(); got != 100*time.Millisecond {
		t.Fatalf("period = %s, want 100ms", got)
	}
	if got := probe.GetTimeout().AsDuration(); got != 2*time.Second {
		t.Fatalf("default timeout = %s, want 2s", got)
	}
}

func TestValidateAndNormalizeReadinessProbeRejectsInvalidDurations(t *testing.T) {
	tests := map[string]*durationpb.Duration{
		"zero":              durationpb.New(0),
		"negative":          durationpb.New(-time.Millisecond),
		"fractional_millis": durationpb.New(1500 * time.Microsecond),
	}
	for name, period := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateAndNormalizeReadinessProbe(&servicev1.ServiceProbe{
				Action: &servicev1.ServiceProbe_Tcp{Tcp: &servicev1.TcpProbe{Port: 8080}},
				Period: period,
			})
			if grpcstatus.Code(err) != codes.InvalidArgument {
				t.Fatalf("error = %v, want InvalidArgument", err)
			}
		})
	}
}
