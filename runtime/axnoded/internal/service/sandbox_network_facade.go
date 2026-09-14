package service

import (
	"context"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	servicenetworking "github.com/cofy-x/axern/runtime/axnoded/internal/service/networking"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"go.opentelemetry.io/otel/attribute"
)

func (h *sandboxService) configureNetworking() {
	h.networking = servicenetworking.NewCoordinator(servicenetworking.Options{
		NatBackend: h.config.NatBackend,
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return h.containerManager.CollectResourceByID(id)
		},
		ContainerExists: func(id string) bool {
			_, err := h.containerManager.Get(id)
			return err == nil
		},
	})
}

func (h *sandboxService) NetworkForSandbox(containerID string) (*SandboxNetwork, error) {
	network, err := h.sandboxNetworking().NetworkForSandbox(containerID)
	if err != nil {
		return nil, err
	}
	return &SandboxNetwork{IP: network.IP, NetNSPath: network.NetNSPath}, nil
}

func (h *sandboxService) ProxyHTTP(stream HTTPProxyServer) error {
	targetID := ""
	parent := context.Background()
	if stream != nil {
		targetID = stream.TargetID()
		parent = stream.Context()
	}
	ctx, op := sdkobs.StartOperation(parent, sdkobs.OperationConfig{
		Name:        sandboxobs.SpanHTTPProxy,
		SpanAttrs:   []attribute.KeyValue{attribute.String(sdkobs.AttrAllocationID, targetID)},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "http_proxy")},
		Counter:     sandboxobs.MetricHTTPProxyTotal,
		Duration:    sandboxobs.MetricHTTPProxyDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	if stream == nil || stream.TargetID() == "" || stream.Port() <= 0 || stream.Port() > 65535 {
		opErr = errord.ToGRPC(errord.ErrInvalidArgument)
		return opErr
	}
	_, err := h.sandboxTargetResolver().Running(stream.TargetID())
	if err != nil {
		opErr = errord.ToGRPC(err)
		return opErr
	}
	op.AddMetricAttributes(attribute.String(sdkobs.AttrRuntime, "runsc"))
	err = h.sandboxNetworking().ProxyHTTP(stream)
	if err != nil && ctx.Err() != nil {
		op.SetResult(sdkobs.ResultTimeout)
	}
	opErr = errord.ToGRPC(err)
	return opErr
}

func (h *sandboxService) sandboxNetworking() *servicenetworking.Coordinator {
	if h == nil {
		return nil
	}
	if h.networking != nil {
		return h.networking
	}
	h.configureNetworking()
	return h.networking
}
