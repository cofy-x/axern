package executionkernel

import (
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestNormalizeConfigDefaultsUnsetResources(t *testing.T) {
	cfg := NormalizeConfig(&commonv1.ExecutionConfig{})
	if cfg.GetResources().GetRequests().GetCpuMilli() != DefaultCPUMilli {
		t.Fatalf("cpu = %d, want %d", cfg.GetResources().GetRequests().GetCpuMilli(), DefaultCPUMilli)
	}
	if cfg.GetResources().GetRequests().GetMemoryBytes() != DefaultMemoryBytes {
		t.Fatalf("memory = %d, want %d", cfg.GetResources().GetRequests().GetMemoryBytes(), DefaultMemoryBytes)
	}
	if cfg.GetResources().GetLimits() != nil {
		t.Fatalf("limits = %#v, want nil", cfg.GetResources().GetLimits())
	}
}

func TestNormalizeConfigPreservesExplicitResources(t *testing.T) {
	cfg := NormalizeConfig(&commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{
				CpuMilli:    250,
				MemoryBytes: 128 * 1024 * 1024,
			},
			Limits: &commonv1.ResourceQuantity{
				CpuMilli:    1000,
				MemoryBytes: 512 * 1024 * 1024,
			},
		},
	})
	if cfg.GetResources().GetRequests().GetCpuMilli() != 250 {
		t.Fatalf("cpu = %d, want 250", cfg.GetResources().GetRequests().GetCpuMilli())
	}
	if cfg.GetResources().GetRequests().GetMemoryBytes() != 128*1024*1024 {
		t.Fatalf("memory = %d, want 128MiB", cfg.GetResources().GetRequests().GetMemoryBytes())
	}
	if cfg.GetResources().GetLimits().GetCpuMilli() != 1000 {
		t.Fatalf("cpu limit = %d, want 1000", cfg.GetResources().GetLimits().GetCpuMilli())
	}
}

func TestNormalizeConfigDefaultsLimitOnlyRequestsFromLimit(t *testing.T) {
	cfg := NormalizeConfig(&commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{
			Limits: &commonv1.ResourceQuantity{
				CpuMilli:    250,
				MemoryBytes: 512 * 1024 * 1024,
			},
		},
	})
	if cfg.GetResources().GetRequests().GetCpuMilli() != 250 {
		t.Fatalf("cpu request = %d, want 250", cfg.GetResources().GetRequests().GetCpuMilli())
	}
	if cfg.GetResources().GetRequests().GetMemoryBytes() != 512*1024*1024 {
		t.Fatalf("memory request = %d, want 512MiB", cfg.GetResources().GetRequests().GetMemoryBytes())
	}
}

func TestNormalizeConfigCanonicalizesNetworkPolicy(t *testing.T) {
	cfg := NormalizeConfig(&commonv1.ExecutionConfig{Network: &commonv1.NetworkSpec{
		Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT,
		EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{
			DeniedDomains: []string{"Example.COM.", "example.com"},
		}}},
	}})
	if got := cfg.GetNetwork().GetEgressPolicy().GetDnsDeny().GetDeniedDomains(); len(got) != 1 || got[0] != "example.com" {
		t.Fatalf("normalized domains = %v", got)
	}
}

func TestDenyAllSurvivesRunConfigStorageRoundTrip(t *testing.T) {
	config := NormalizeConfig(&commonv1.ExecutionConfig{Network: &commonv1.NetworkSpec{
		EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}},
	}})
	if config.GetNetwork().GetMode() != commonv1.NetworkMode_NETWORK_MODE_ISOLATED || config.GetNetwork().GetEgressPolicy() != nil {
		t.Fatalf("deny-all normalized to %#v, want isolated mode without parallel egress policy", config.GetNetwork())
	}
	payload, err := protojson.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var read commonv1.ExecutionConfig
	if err := protojson.Unmarshal(payload, &read); err != nil {
		t.Fatal(err)
	}
	if read.GetNetwork().GetMode() != commonv1.NetworkMode_NETWORK_MODE_ISOLATED {
		t.Fatalf("stored Run read lost deny-all: %s", payload)
	}
	defaultConfig := NormalizeConfig(&commonv1.ExecutionConfig{})
	if defaultConfig.GetNetwork().GetMode() == commonv1.NetworkMode_NETWORK_MODE_ISOLATED {
		t.Fatal("omitted policy unexpectedly became isolated")
	}
}

func TestValidateResourcesRejectsLimitBelowRequest(t *testing.T) {
	err := ValidateResources(&commonv1.ResourceSpec{
		Requests: &commonv1.ResourceQuantity{CpuMilli: 1000},
		Limits:   &commonv1.ResourceQuantity{CpuMilli: 500},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", grpcstatus.Code(err))
	}
	if got, want := grpcstatus.Convert(err).Message(), "request=1000 limit=500"; !strings.Contains(got, want) {
		t.Fatalf("message = %q, want to contain %q", got, want)
	}
}
