package observability

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

const instrumentationName = "github.com/cofy-x/axern/lib/go/observability"

var processServiceInstanceID = newServiceInstanceID()

type Handle struct {
	enabled bool
	tracer  trace.Tracer
	meter   metric.Meter
	tp      *sdktrace.TracerProvider
	mp      *sdkmetric.MeterProvider
	lp      *sdklog.LoggerProvider
}

func Init(ctx context.Context, cfg Config) (*Handle, error) {
	setMetricIdentityNodeID("")
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	if !cfg.Enabled {
		return noopHandle(), nil
	}
	setMetricIdentityNodeID(cfg.NodeID)
	resourceAttrs := []attribute.KeyValue{
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String(AttrServiceInstanceID, serviceInstanceID(cfg.ResourceAttrs)),
		attribute.String(AttrComponent, cfg.Component),
		attribute.String(AttrNodeID, cfg.NodeID),
	}
	for _, attr := range cfg.ResourceAttrs {
		if attr.Key != AttrServiceInstanceID {
			resourceAttrs = append(resourceAttrs, attr)
		}
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			SafeAttrs(resourceAttrs...)...,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	traceOptions := traceExporterOptions(cfg)
	metricOptions := metricExporterOptions(cfg)
	logOptions := logExporterOptions(cfg)
	if cfg.OTLPInsecure {
		traceOptions = append(traceOptions, otlptracegrpc.WithInsecure())
		metricOptions = append(metricOptions, otlpmetricgrpc.WithInsecure())
		logOptions = append(logOptions, otlploggrpc.WithInsecure())
	}
	traceExporter, err := otlptracegrpc.New(ctx, traceOptions...)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	metricExporter, err := otlpmetricgrpc.New(ctx, metricOptions...)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}
	logExporter, err := otlploggrpc.New(ctx, logOptions...)
	if err != nil {
		return nil, fmt.Errorf("create log exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			metricExporter,
			sdkmetric.WithInterval(cfg.MetricInterval),
			sdkmetric.WithProducer(otelruntime.NewProducer()),
		)),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	global.SetLoggerProvider(lp)
	if err := otelruntime.Start(otelruntime.WithMeterProvider(mp)); err != nil {
		_ = lp.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("start Go runtime metrics: %w", err)
	}

	return &Handle{
		enabled: true,
		tracer:  tp.Tracer(instrumentationName),
		meter:   mp.Meter(instrumentationName),
		tp:      tp,
		mp:      mp,
		lp:      lp,
	}, nil
}

func serviceInstanceID(attrs []attribute.KeyValue) string {
	for _, attr := range attrs {
		if attr.Key == AttrServiceInstanceID {
			if value := strings.TrimSpace(attr.Value.AsString()); value != "" {
				return value
			}
		}
	}
	return processServiceInstanceID
}

func newServiceInstanceID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fmt.Sprintf("pid-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

func noopHandle() *Handle {
	return &Handle{
		tracer: otel.Tracer(instrumentationName),
		meter:  otel.Meter(instrumentationName),
	}
}

func traceExporterOptions(cfg Config) []otlptracegrpc.Option {
	endpoint := strings.TrimSpace(cfg.OTLPEndpointURL)
	if endpoint == "" {
		return nil
	}
	if strings.Contains(endpoint, "://") {
		return []otlptracegrpc.Option{otlptracegrpc.WithEndpointURL(endpoint)}
	}
	return []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
}

func metricExporterOptions(cfg Config) []otlpmetricgrpc.Option {
	endpoint := strings.TrimSpace(cfg.OTLPEndpointURL)
	if endpoint == "" {
		return nil
	}
	if strings.Contains(endpoint, "://") {
		return []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpointURL(endpoint)}
	}
	return []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
}

func logExporterOptions(cfg Config) []otlploggrpc.Option {
	endpoint := strings.TrimSpace(cfg.OTLPEndpointURL)
	if endpoint == "" {
		return nil
	}
	if strings.Contains(endpoint, "://") {
		return []otlploggrpc.Option{otlploggrpc.WithEndpointURL(endpoint)}
	}
	return []otlploggrpc.Option{otlploggrpc.WithEndpoint(endpoint)}
}

func (h *Handle) Enabled() bool {
	return h != nil && h.enabled
}

func (h *Handle) Shutdown(ctx context.Context) error {
	if h == nil || !h.enabled {
		return nil
	}
	var first error
	if h.lp != nil {
		if err := h.lp.Shutdown(ctx); err != nil {
			first = err
		}
	}
	if h.mp != nil {
		if err := h.mp.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	if h.tp != nil {
		if err := h.tp.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (h *Handle) Tracer() trace.Tracer {
	if h == nil || h.tracer == nil {
		return otel.Tracer(instrumentationName)
	}
	return h.tracer
}

func (h *Handle) Meter() metric.Meter {
	if h == nil || h.meter == nil {
		return otel.Meter(instrumentationName)
	}
	return h.meter
}

func (h *Handle) Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return h.Tracer().Start(ctx, name, trace.WithAttributes(SafeAttrs(attrs...)...))
}

func (h *Handle) HTTPHandler(handler http.Handler, operation string) http.Handler {
	if h == nil || !h.enabled {
		return handler
	}
	return otelhttp.NewHandler(handler, operation)
}

func (h *Handle) GRPCServerStatsHandler() stats.Handler {
	if h == nil || !h.enabled {
		return nil
	}
	return otelgrpc.NewServerHandler()
}

func (h *Handle) GRPCDialOptions() []grpc.DialOption {
	if h == nil || !h.enabled {
		return nil
	}
	return []grpc.DialOption{grpc.WithStatsHandler(otelgrpc.NewClientHandler())}
}

func TraceFields(ctx context.Context) map[string]string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return nil
	}
	return map[string]string{
		"trace_id": spanCtx.TraceID().String(),
		"span_id":  spanCtx.SpanID().String(),
	}
}

func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(instrumentationName).Start(ctx, name, trace.WithAttributes(SafeAttrs(attrs...)...))
}

func GRPCDialOptions() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithStatsHandler(otelgrpc.NewClientHandler())}
}

func HTTPHandler(handler http.Handler, operation string) http.Handler {
	return otelhttp.NewHandler(handler, operation)
}
