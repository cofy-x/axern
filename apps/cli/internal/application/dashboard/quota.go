package dashboard

import (
	"context"

	appquota "github.com/cofy-x/axern/apps/cli/internal/application/quota"
)

func (c Control) Quotas(ctx context.Context) ([]QuotaDTO, error) {
	resp, err := c.quotas.List(ctx, appquota.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]QuotaDTO, 0, len(resp.GetQuotas()))
	for _, quota := range resp.GetQuotas() {
		out = append(out, NewQuotaDTO(quota))
	}
	return out, nil
}

func (c Control) QuotaEvents(ctx context.Context, namespace string, limit int32) ([]QuotaEventDTO, error) {
	resp, err := c.quotas.ListEvents(ctx, namespace, int(limit))
	if err != nil {
		return nil, err
	}
	out := make([]QuotaEventDTO, 0, len(resp.GetEvents()))
	for _, event := range resp.GetEvents() {
		out = append(out, NewQuotaEventDTO(event))
	}
	return out, nil
}
