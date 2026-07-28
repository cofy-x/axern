package imagefsd

import (
	"context"
	"time"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/observability/timedtrace"

	"go.opentelemetry.io/otel/attribute"
)

// TimedOperation wraps an operation with tracing and logs timing on completion
type TimedOperation struct {
	op *timedtrace.Operation
}

// StartTimedOperation creates a new timed operation with tracing from context
func StartTimedOperation(ctx context.Context, operation string, daemonID string) (*TimedOperation, context.Context) {
	op, nextCtx := timedtrace.Start(ctx, timedtrace.Config{
		TracerName:      "imagefsd",
		Operation:       operation,
		IdentifierKey:   "daemon",
		IdentifierValue: daemonID,
		LogPrefix:       "imagefsd trace",
		Attributes: []attribute.KeyValue{
			attribute.String("daemon.id", daemonID),
			attribute.String("operation", operation),
		},
	})
	return &TimedOperation{op: op}, nextCtx
}

// Stage records a stage timing and creates a span event
func (t *TimedOperation) Stage(stageName string, duration time.Duration) {
	t.op.Stage(stageName, duration)
}

// End completes the operation and logs all timing information
func (t *TimedOperation) End() {
	t.op.End()
}

// Fail marks the operation as failed with an error
func (t *TimedOperation) Fail(err error) {
	t.op.Fail(err)
}
