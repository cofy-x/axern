package publicv1

import (
	"context"
	"strings"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

var publicOps publicOperationStarter

type publicOperationStarter struct{}

type publicOperationAction string

const (
	publicActionCreate          publicOperationAction = "create"
	publicActionGet             publicOperationAction = "get"
	publicActionList            publicOperationAction = "list"
	publicActionSet             publicOperationAction = "set"
	publicActionUnset           publicOperationAction = "unset"
	publicActionUpdate          publicOperationAction = "update"
	publicActionDelete          publicOperationAction = "delete"
	publicActionPurge           publicOperationAction = "purge"
	publicActionCancel          publicOperationAction = "cancel"
	publicActionRevoke          publicOperationAction = "revoke"
	publicActionRenew           publicOperationAction = "renew"
	publicActionInspect         publicOperationAction = "inspect"
	publicActionListReplicas    publicOperationAction = "list_replicas"
	publicActionListEvents      publicOperationAction = "list_events"
	publicActionDeploy          publicOperationAction = "deploy"
	publicActionInvoke          publicOperationAction = "invoke"
	publicActionUpload          publicOperationAction = "upload"
	publicActionListInvocations publicOperationAction = "list_invocations"
	publicActionGetInvocation   publicOperationAction = "get_invocation"
)

type publicOperationOptions struct {
	spanAttrs []attribute.KeyValue
}

type publicOperationOption func(*publicOperationOptions)

func withServiceID(serviceID string) publicOperationOption {
	return withSpanString(sdkobs.AttrServiceID, serviceID)
}

func withEnvironmentID(environmentID string) publicOperationOption {
	return withSpanString(sdkobs.AttrEnvironmentID, environmentID)
}

func withRunID(runID string) publicOperationOption {
	return withSpanString(sdkobs.AttrRunID, runID)
}

func withAllocationID(allocationID string) publicOperationOption {
	return withSpanString(sdkobs.AttrAllocationID, allocationID)
}

func withNamespace(namespace string) publicOperationOption {
	return withSpanString(sdkobs.AttrNamespace, namespace)
}

func withSpanString(key string, value string) publicOperationOption {
	return func(opts *publicOperationOptions) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		opts.spanAttrs = append(opts.spanAttrs, attribute.String(key, value))
	}
}

func withFunctionID(functionID string) publicOperationOption {
	return withSpanString(sdkobs.AttrFunctionID, functionID)
}

func withInvocationID(invocationID string) publicOperationOption {
	return withSpanString(sdkobs.AttrInvocationID, invocationID)
}

func (publicOperationStarter) Function(ctx context.Context, spanName string, action publicOperationAction, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	return startPublicOperation(ctx, spanName, action, ctrlobs.MetricFunctionOperationTotal, ctrlobs.MetricFunctionOperationDuration, options...)
}

func (publicOperationStarter) Service(ctx context.Context, spanName string, action publicOperationAction, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	return startPublicOperation(ctx, spanName, action, ctrlobs.MetricServiceOperationTotal, ctrlobs.MetricServiceOperationDuration, options...)
}

func (publicOperationStarter) Environment(ctx context.Context, spanName string, action publicOperationAction, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	return startPublicOperation(ctx, spanName, action, ctrlobs.MetricEnvironmentOperationTotal, ctrlobs.MetricEnvironmentOperationDuration, options...)
}

func (publicOperationStarter) Run(ctx context.Context, spanName string, action publicOperationAction, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	return startPublicOperation(ctx, spanName, action, ctrlobs.MetricRunOperationTotal, ctrlobs.MetricRunOperationDuration, options...)
}

func (publicOperationStarter) Namespace(ctx context.Context, spanName string, action publicOperationAction, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	return startPublicOperation(ctx, spanName, action, ctrlobs.MetricNamespaceOperationTotal, ctrlobs.MetricNamespaceOperationDuration, options...)
}

func (publicOperationStarter) Tunnel(ctx context.Context, spanName string, action publicOperationAction, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	return startPublicOperation(ctx, spanName, action, ctrlobs.MetricTunnelOperationTotal, ctrlobs.MetricTunnelOperationDuration, options...)
}

func (publicOperationStarter) Quota(ctx context.Context, spanName string, action publicOperationAction, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	return startPublicOperation(ctx, spanName, action, ctrlobs.MetricQuotaOperationTotal, ctrlobs.MetricQuotaOperationDuration, options...)
}

func startPublicOperation(ctx context.Context, spanName string, action publicOperationAction, counter sdkobs.Instrument, duration sdkobs.Instrument, options ...publicOperationOption) (context.Context, *sdkobs.Operation) {
	actionValue := string(action)
	opts := publicOperationOptions{
		spanAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, actionValue)},
	}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        spanName,
		SpanAttrs:   opts.spanAttrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, actionValue)},
		Counter:     counter,
		Duration:    duration,
	})
}
