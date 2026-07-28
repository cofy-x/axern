package quota

import (
	"context"

	"github.com/cofy-x/axern/apps/cli/internal/workloaddiagnostic"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc"
)

type Client interface {
	GetNamespaceQuota(context.Context, *quotav1.GetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.GetNamespaceQuotaResponse, error)
	ListNamespaceQuotas(context.Context, *quotav1.ListNamespaceQuotasRequest, ...grpc.CallOption) (*quotav1.ListNamespaceQuotasResponse, error)
	ListNamespaceQuotaEvents(context.Context, *quotav1.ListNamespaceQuotaEventsRequest, ...grpc.CallOption) (*quotav1.ListNamespaceQuotaEventsResponse, error)
	SetNamespaceQuota(context.Context, *quotav1.SetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.SetNamespaceQuotaResponse, error)
	UnsetNamespaceQuota(context.Context, *quotav1.UnsetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.UnsetNamespaceQuotaResponse, error)
}

type ServiceLister interface {
	ListServices(context.Context, *servicev1.ListServicesRequest, ...grpc.CallOption) (*servicev1.ListServicesResponse, error)
}

type Control struct {
	client Client
}

type DescribeResult struct {
	Quota                    *quotav1.NamespaceQuota
	AdmissionBlockedServices []*servicev1.Service
}

func New(client Client) Control {
	return Control{client: client}
}

func (c Control) Get(ctx context.Context, namespace string) (*quotav1.GetNamespaceQuotaResponse, error) {
	return c.client.GetNamespaceQuota(ctx, &quotav1.GetNamespaceQuotaRequest{Namespace: namespace})
}

func (c Control) List(ctx context.Context, options ListOptions) (*quotav1.ListNamespaceQuotasResponse, error) {
	resp, err := c.client.ListNamespaceQuotas(ctx, &quotav1.ListNamespaceQuotasRequest{})
	if err != nil {
		return nil, err
	}
	quotas, err := PrepareList(resp.GetQuotas(), options)
	if err != nil {
		return nil, err
	}
	return &quotav1.ListNamespaceQuotasResponse{Quotas: quotas}, nil
}

func (c Control) ListEvents(ctx context.Context, namespace string, limit int) (*quotav1.ListNamespaceQuotaEventsResponse, error) {
	return c.client.ListNamespaceQuotaEvents(ctx, &quotav1.ListNamespaceQuotaEventsRequest{
		Namespace: namespace,
		Limit:     int32(limit),
	})
}

func (c Control) Set(ctx context.Context, namespace string, limits *quotav1.NamespaceQuotaLimits) (*quotav1.SetNamespaceQuotaResponse, error) {
	return c.client.SetNamespaceQuota(ctx, &quotav1.SetNamespaceQuotaRequest{Namespace: namespace, Limits: limits})
}

func (c Control) Unset(ctx context.Context, namespace string) (*quotav1.UnsetNamespaceQuotaResponse, error) {
	return c.client.UnsetNamespaceQuota(ctx, &quotav1.UnsetNamespaceQuotaRequest{Namespace: namespace})
}

func (c Control) Describe(ctx context.Context, namespace string, services ServiceLister) (DescribeResult, error) {
	resp, err := c.Get(ctx, namespace)
	if err != nil {
		return DescribeResult{}, err
	}
	result := DescribeResult{Quota: resp.GetQuota()}
	if services == nil {
		return result, nil
	}
	serviceResp, err := services.ListServices(ctx, &servicev1.ListServicesRequest{
		Filter: &servicev1.ServiceListFilter{Namespace: namespace},
	})
	if err != nil {
		return DescribeResult{}, err
	}
	for _, service := range serviceResp.GetServices() {
		if service == nil || workloaddiagnostic.AdmissionBlockedSummary(service.GetMessage()) == "" {
			continue
		}
		result.AdmissionBlockedServices = append(result.AdmissionBlockedServices, service)
	}
	return result, nil
}
