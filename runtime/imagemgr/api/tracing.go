package api

import (
	"context"
	"time"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/observability/timedtrace"

	"go.opentelemetry.io/otel/attribute"
)

// APITimedOperation wraps an API operation with tracing
type APITimedOperation struct {
	op *timedtrace.Operation
}

// StartAPITimedOperation creates a new timed API operation with tracing
func StartAPITimedOperation(ctx context.Context, operation string, identifier string) (*APITimedOperation, context.Context) {
	op, nextCtx := timedtrace.Start(ctx, timedtrace.Config{
		TracerName:      "api",
		Operation:       operation,
		IdentifierKey:   "identifier",
		IdentifierValue: identifier,
		LogPrefix:       "API trace",
		Attributes: []attribute.KeyValue{
			attribute.String("api.operation", operation),
			attribute.String("api.identifier", identifier),
		},
	})
	return &APITimedOperation{op: op}, nextCtx
}

// Stage records a stage timing and creates a span event
func (t *APITimedOperation) Stage(stageName string, duration time.Duration) {
	t.op.Stage(stageName, duration)
}

// End completes the operation and logs all timing information
func (t *APITimedOperation) End() {
	t.op.End()
}

// Fail marks the operation as failed with an error
func (t *APITimedOperation) Fail(err error) {
	t.op.Fail(err)
}
