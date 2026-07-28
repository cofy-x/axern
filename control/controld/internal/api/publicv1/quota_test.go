package publicv1

import (
	"context"
	"testing"
	"time"

	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestQuotaSetValidatesNegativeLimits(t *testing.T) {
	server := New(Dependencies{
		Now:    func() time.Time { return time.Unix(0, 0).UTC() },
		Quotas: &fakeQuotas{},
	})
	_, err := server.SetNamespaceQuota(context.Background(), &quotav1.SetNamespaceQuotaRequest{
		Namespace: "default",
		Limits: &quotav1.NamespaceQuotaLimits{
			CpuMilli: wrapperspb.Int64(-1),
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument err=%v", grpcstatus.Code(err), err)
	}
}

func TestQuotaGetValidatesNamespace(t *testing.T) {
	server := New(Dependencies{
		Now:    func() time.Time { return time.Unix(0, 0).UTC() },
		Quotas: &fakeQuotas{},
	})
	_, err := server.GetNamespaceQuota(context.Background(), &quotav1.GetNamespaceQuotaRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument err=%v", grpcstatus.Code(err), err)
	}
}

func TestQuotaSetStoresNullableLimits(t *testing.T) {
	quotas := &fakeQuotas{}
	server := New(Dependencies{
		Now:    func() time.Time { return time.Unix(0, 0).UTC() },
		Quotas: quotas,
	})
	resp, err := server.SetNamespaceQuota(context.Background(), &quotav1.SetNamespaceQuotaRequest{
		Namespace: "default",
		Limits: &quotav1.NamespaceQuotaLimits{
			CpuMilli: wrapperspb.Int64(1000),
		},
	})
	if err != nil {
		t.Fatalf("SetNamespaceQuota returned error: %v", err)
	}
	if resp.GetQuota().GetCpuMilliLimit().GetValue() != 1000 {
		t.Fatalf("cpu limit = %d, want 1000", resp.GetQuota().GetCpuMilliLimit().GetValue())
	}
	if resp.GetQuota().GetMemoryBytesLimit() != nil {
		t.Fatalf("memory limit = %v, want nil", resp.GetQuota().GetMemoryBytesLimit())
	}
	if quotas.setNamespace != "default" {
		t.Fatalf("namespace = %q, want default", quotas.setNamespace)
	}
}

type fakeQuotas struct {
	setNamespace string
}

func (f *fakeQuotas) Get(context.Context, string) (*quotav1.NamespaceQuota, error) {
	return &quotav1.NamespaceQuota{}, nil
}

func (f *fakeQuotas) List(context.Context) ([]*quotav1.NamespaceQuota, error) {
	return []*quotav1.NamespaceQuota{}, nil
}

func (f *fakeQuotas) ListEvents(context.Context, string, int) ([]*quotav1.NamespaceQuotaEvent, error) {
	return []*quotav1.NamespaceQuotaEvent{}, nil
}

func (f *fakeQuotas) Set(_ context.Context, namespace string, limits *quotav1.NamespaceQuotaLimits, _ time.Time) (*quotav1.NamespaceQuota, error) {
	f.setNamespace = namespace
	return &quotav1.NamespaceQuota{
		Namespace:         namespace,
		CpuMilliLimit:     limits.GetCpuMilli(),
		MemoryBytesLimit:  limits.GetMemoryBytes(),
		AvailableCpuMilli: limits.GetCpuMilli(),
	}, nil
}

func (f *fakeQuotas) Unset(_ context.Context, namespace string, _ time.Time) (*quotav1.NamespaceQuota, error) {
	return &quotav1.NamespaceQuota{Namespace: namespace}, nil
}
