package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	ResultOK      = "ok"
	ResultError   = "error"
	ResultSkipped = "skipped"
	ResultTimeout = "timeout"
	ResultExit    = "exit"
)

type Instrument struct {
	Name        string
	Description string
}

type OperationConfig struct {
	Name        string
	SpanAttrs   []attribute.KeyValue
	MetricAttrs []attribute.KeyValue
	Counter     Instrument
	Duration    Instrument
}

type Operation struct {
	ctx                    context.Context
	span                   trace.Span
	startedAt              time.Time
	result                 string
	errorStatusDescription string
	metricAttrs            []attribute.KeyValue
	counter                Instrument
	duration               Instrument
}

func (h *Handle) StartOperation(ctx context.Context, cfg OperationConfig) (context.Context, *Operation) {
	ctx, span := h.Start(ctx, cfg.Name, cfg.SpanAttrs...)
	return ctx, newOperation(ctx, span, cfg)
}

func StartOperation(ctx context.Context, cfg OperationConfig) (context.Context, *Operation) {
	ctx, span := Start(ctx, cfg.Name, cfg.SpanAttrs...)
	return ctx, newOperation(ctx, span, cfg)
}

func newOperation(ctx context.Context, span trace.Span, cfg OperationConfig) *Operation {
	return &Operation{
		ctx:         ctx,
		span:        span,
		startedAt:   time.Now(),
		result:      ResultOK,
		metricAttrs: SafeAttrs(cfg.MetricAttrs...),
		counter:     cfg.Counter,
		duration:    cfg.Duration,
	}
}

func (o *Operation) SetResult(result string) {
	if o == nil || result == "" {
		return
	}
	o.result = result
	o.SetAttributes(attribute.String(AttrResult, result))
}

func (o *Operation) SetAttributes(attrs ...attribute.KeyValue) {
	if o == nil || o.span == nil {
		return
	}
	o.span.SetAttributes(SafeAttrs(attrs...)...)
}

func (o *Operation) SetErrorClass(errorClass string) {
	if o == nil || errorClass == "" {
		return
	}
	o.SetAttributes(attribute.String(AttrErrorClass, errorClass))
	o.AddMetricAttributes(attribute.String(AttrErrorClass, errorClass))
}

func (o *Operation) SetHTTPStatusCode(status int) {
	if o == nil || status <= 0 {
		return
	}
	attr := attribute.Int(AttrHTTPStatusCode, status)
	o.SetAttributes(attr)
	o.AddMetricAttributes(attr)
}

func (o *Operation) AddMetricAttributes(attrs ...attribute.KeyValue) {
	if o == nil {
		return
	}
	o.metricAttrs = append(o.metricAttrs, SafeAttrs(attrs...)...)
}

func (o *Operation) SetErrorStatus(description string) {
	if o == nil || o.span == nil {
		return
	}
	o.errorStatusDescription = description
	o.span.SetStatus(codes.Error, description)
}

func (o *Operation) End(err error) {
	if o == nil {
		return
	}
	if err != nil {
		if o.result == ResultOK {
			o.result = ResultError
		}
		if o.span != nil {
			o.span.RecordError(err)
			description := o.errorStatusDescription
			if description == "" {
				description = ResultError
			}
			o.span.SetStatus(codes.Error, description)
		}
	}
	if o.span != nil {
		o.span.SetAttributes(attribute.String(AttrResult, o.result))
	}
	attrs := append([]attribute.KeyValue{attribute.String(AttrResult, o.result)}, o.metricAttrs...)
	if o.counter.Name != "" {
		Int64Counter(o.counter.Name, o.counter.Description).Add(o.ctx, 1, attrs...)
	}
	if o.duration.Name != "" {
		DurationHistogram(o.duration.Name, o.duration.Description).RecordDuration(o.ctx, time.Since(o.startedAt), attrs...)
	}
	if o.span != nil {
		o.span.End()
	}
}
