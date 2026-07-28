package oci

import (
	"context"
	"time"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/observability/timedtrace"

	"go.opentelemetry.io/otel/attribute"
)

// OCITimedOperation wraps an OCI operation with tracing metadata.
type OCITimedOperation struct {
	op *timedtrace.Operation
}

// StartOCITimedOperation creates a timed OCI operation span.
func StartOCITimedOperation(ctx context.Context, operation, identifier string) (*OCITimedOperation, context.Context) {
	op, nextCtx := timedtrace.Start(ctx, timedtrace.Config{
		TracerName:      "oci",
		Operation:       operation,
		IdentifierKey:   "identifier",
		IdentifierValue: identifier,
		LogPrefix:       "OCI trace",
		Attributes: []attribute.KeyValue{
			attribute.String("oci.operation", operation),
			attribute.String("oci.identifier", identifier),
		},
	})
	return &OCITimedOperation{op: op}, nextCtx
}

// Stage records a stage duration and emits an event.
func (t *OCITimedOperation) Stage(stageName string, duration time.Duration) {
	t.op.Stage(stageName, duration)
}

// RecordError attaches an error to the span without ending it.
func (t *OCITimedOperation) RecordError(err error) {
	t.op.RecordError(err)
}

// End completes tracing and logs timing summary.
func (t *OCITimedOperation) End() {
	t.op.End()
}
