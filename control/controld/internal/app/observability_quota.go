package app

import (
	"context"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func (a *App) observeNamespaceResources(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	if a.namespacePG == nil {
		return nil
	}
	quotas, err := a.namespacePG.List(ctx)
	if err != nil {
		return err
	}
	for _, quota := range quotas {
		observeNamespaceQuotaResource(observe, quota, "cpu_milli", quota.GetCpuMilliLimit(), quota.GetReservedCpuMilli(), quota.GetAvailableCpuMilli())
		observeNamespaceQuotaResource(observe, quota, "memory_bytes", quota.GetMemoryBytesLimit(), quota.GetReservedMemoryBytes(), quota.GetAvailableMemoryBytes())
		observeNamespaceQuotaResource(observe, quota, "ephemeral_storage_bytes", quota.GetEphemeralStorageBytesLimit(), quota.GetReservedEphemeralStorageBytes(), quota.GetAvailableEphemeralStorageBytes())
	}
	return nil
}

func observeNamespaceQuotaResource(observe sdkobs.Int64GaugeObserver, quota *quotav1.NamespaceQuota, resource string, limit *wrapperspb.Int64Value, reserved int64, available *wrapperspb.Int64Value) {
	if quota == nil {
		return
	}
	if limit != nil {
		observe(limit.GetValue(), namespaceResourceAttrs(quota.GetNamespace(), resource, "limit")...)
	}
	observe(reserved, namespaceResourceAttrs(quota.GetNamespace(), resource, "reserved")...)
	if available != nil {
		observe(available.GetValue(), namespaceResourceAttrs(quota.GetNamespace(), resource, "available")...)
	}
}

func namespaceResourceAttrs(namespace, resource, state string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(sdkobs.AttrNamespace, namespace),
		attribute.String(sdkobs.AttrResource, resource),
		attribute.String(sdkobs.AttrState, state),
	}
}
