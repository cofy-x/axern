package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestConfigFromEnvDefaultDisabled(t *testing.T) {
	t.Setenv(EnvEnabled, "")
	cfg := ConfigFromEnv(
		WithServiceName("svc"),
		WithComponent("component"),
		WithNodeID("node-a"),
	)
	if cfg.Enabled {
		t.Fatal("ConfigFromEnv().Enabled = true, want false")
	}
	if cfg.ServiceName != "svc" || cfg.Component != "component" || cfg.NodeID != "node-a" {
		t.Fatalf("ConfigFromEnv() = %#v", cfg)
	}
}

func TestConfigFromEnvEnabledAndServiceOverride(t *testing.T) {
	t.Setenv(EnvEnabled, "true")
	t.Setenv("OTEL_SERVICE_NAME", "override")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=compose,axern.node_id=node-a,token=secret")
	cfg := ConfigFromEnv(
		WithServiceName("svc"),
		WithComponent("component"),
	)
	if !cfg.Enabled {
		t.Fatal("ConfigFromEnv().Enabled = false, want true")
	}
	if cfg.ServiceName != "override" {
		t.Fatalf("ServiceName = %q, want override", cfg.ServiceName)
	}
	if cfg.OTLPEndpointURL != "http://collector:4317" || !cfg.OTLPInsecure {
		t.Fatalf("OTLP config = endpoint %q insecure %v, want http://collector:4317 true", cfg.OTLPEndpointURL, cfg.OTLPInsecure)
	}
	if len(cfg.ResourceAttrs) != 3 {
		t.Fatalf("ResourceAttrs len = %d, want 3", len(cfg.ResourceAttrs))
	}
	if got := cfg.ResourceAttrs[2].Value.AsString(); got != "[redacted]" {
		t.Fatalf("sensitive resource attribute = %q, want [redacted]", got)
	}
}

func TestConfigFromEnvOptionsAndEnvPrecedence(t *testing.T) {
	t.Setenv("OTEL_SERVICE_VERSION", "env-version")
	t.Setenv("AXERN_OTEL_METRIC_INTERVAL", "3s")
	cfg := ConfigFromEnv(
		WithServiceName("svc"),
		WithServiceVersion("option-version"),
		WithComponent("component"),
		WithNodeID("node-a"),
		WithMetricInterval(10*time.Second),
	)
	if cfg.ServiceName != "svc" || cfg.Component != "component" || cfg.NodeID != "node-a" {
		t.Fatalf("ConfigFromEnv() = %#v", cfg)
	}
	if cfg.ServiceVersion != "env-version" {
		t.Fatalf("ServiceVersion = %q, want env-version", cfg.ServiceVersion)
	}
	if cfg.MetricInterval != 3*time.Second {
		t.Fatalf("MetricInterval = %s, want 3s", cfg.MetricInterval)
	}
}

func TestSensitiveAttributeSanitizer(t *testing.T) {
	got := StringAttr("axern.execution_lease_token", "super-secret")
	if got.Value.AsString() != "[redacted]" {
		t.Fatalf("sanitized value = %q", got.Value.AsString())
	}
	if !SensitiveKey("stdout") {
		t.Fatal("stdout should be treated as sensitive")
	}
	if got := SanitizeLogBody("token=super-secret"); got != "[redacted]" {
		t.Fatalf("sanitized log body = %q, want [redacted]", got)
	}
}

func TestMetricAttrsIncludeProcessNodeIdentity(t *testing.T) {
	setMetricIdentityNodeID("node-a")
	t.Cleanup(func() { setMetricIdentityNodeID("") })

	attrs := metricAttrs(attribute.String(AttrResult, ResultOK))
	got := map[attribute.Key]string{}
	for _, attr := range attrs {
		got[attr.Key] = attr.Value.AsString()
	}
	if got[AttrNodeID] != "node-a" || got[AttrResult] != ResultOK {
		t.Fatalf("metric attrs = %#v", got)
	}
}

func TestMetricAttrsPreserveExplicitNodeIdentity(t *testing.T) {
	setMetricIdentityNodeID("node-default")
	t.Cleanup(func() { setMetricIdentityNodeID("") })

	attrs := metricAttrs(attribute.String(AttrNodeID, "node-explicit"))
	if len(attrs) != 1 || attrs[0].Value.AsString() != "node-explicit" {
		t.Fatalf("metric attrs = %#v", attrs)
	}
}

func TestServiceInstanceIDUsesProcessIdentityUnlessExplicit(t *testing.T) {
	if got := serviceInstanceID(nil); got == "" || got != processServiceInstanceID {
		t.Fatalf("serviceInstanceID(nil) = %q, want process identity", got)
	}
	attrs := []attribute.KeyValue{attribute.String(AttrServiceInstanceID, "instance-explicit")}
	if got := serviceInstanceID(attrs); got != "instance-explicit" {
		t.Fatalf("serviceInstanceID(explicit) = %q", got)
	}
}

func TestInitConfiguresTraceContextPropagator(t *testing.T) {
	_, err := Init(context.Background(), Config{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01},
		SpanID:     trace.SpanID{0x02},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if carrier.Get("traceparent") == "" {
		t.Fatal("traceparent was not injected")
	}
}

func TestNoopHandleDoesNotCreateRecordingSpan(t *testing.T) {
	handle, err := Init(context.Background(), Config{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	ctx, span := handle.Start(context.Background(), "test")
	defer span.End()
	if span.SpanContext().IsValid() && span.IsRecording() {
		t.Fatal("noop span should not be recording")
	}
	if got := trace.SpanContextFromContext(ctx); got.IsValid() && span.IsRecording() {
		t.Fatal("noop context should not contain a recording span")
	}
}

func TestOperationRecordsResultAndErrorStatus(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	handle := &Handle{
		enabled: true,
		tracer:  provider.Tracer("test"),
		meter:   noop.NewMeterProvider().Meter("test"),
	}

	_, op := handle.StartOperation(context.Background(), OperationConfig{
		Name:        "test.operation",
		SpanAttrs:   []attribute.KeyValue{attribute.String(AttrServiceID, "svc-1")},
		MetricAttrs: []attribute.KeyValue{attribute.String(AttrServiceID, "svc-1")},
	})
	op.SetResult("upstream_unavailable")
	op.SetErrorStatus("upstream unavailable")
	op.End(errors.New("boom"))

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	if ended[0].Status().Code != codes.Error {
		t.Fatalf("status = %v, want error", ended[0].Status())
	}
	var result string
	for _, attr := range ended[0].Attributes() {
		if attr.Key == AttrResult {
			result = attr.Value.AsString()
		}
	}
	if result != "upstream_unavailable" {
		t.Fatalf("result attr = %q, want upstream_unavailable", result)
	}
}
