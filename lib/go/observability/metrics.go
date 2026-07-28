package observability

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Counter struct {
	counter metric.Int64Counter
}

type UpDownCounter struct {
	counter metric.Int64UpDownCounter
}

type Histogram struct {
	histogram metric.Float64Histogram
}

type Gauge struct {
	gauge metric.Float64Gauge
}

type Int64GaugeObserver func(value int64, attrs ...attribute.KeyValue)

type Int64GaugeCallback func(context.Context, Int64GaugeObserver) error

type Float64GaugeObserver func(value float64, attrs ...attribute.KeyValue)

type Float64GaugeCallback func(context.Context, Float64GaugeObserver) error

type ObservableRegistration interface {
	Unregister() error
}

var (
	counterCache   sync.Map
	gaugeCache     sync.Map
	histogramCache sync.Map
	metricIdentity atomic.Pointer[metricIdentityAttrs]
)

type metricIdentityAttrs struct {
	attrs []attribute.KeyValue
}

var defaultDurationHistogramBuckets = []float64{
	0.0005,
	0.001,
	0.0025,
	0.005,
	0.01,
	0.025,
	0.05,
	0.1,
	0.25,
	0.5,
	0.75,
	1,
	1.25,
	1.5,
	2,
	2.5,
	3,
	4,
	5,
	7.5,
	10,
	30,
	60,
}

func (h *Handle) Int64Counter(name, description string) Counter {
	counter, _ := h.Meter().Int64Counter(name, metric.WithDescription(description))
	return Counter{counter: counter}
}

func (h *Handle) Int64UpDownCounter(name, description string) UpDownCounter {
	counter, _ := h.Meter().Int64UpDownCounter(name, metric.WithDescription(description))
	return UpDownCounter{counter: counter}
}

func (h *Handle) DurationHistogram(name, description string) Histogram {
	return h.DurationHistogramWithBuckets(name, description, defaultDurationHistogramBuckets...)
}

func (h *Handle) DurationHistogramWithBuckets(name, description string, boundaries ...float64) Histogram {
	histogram, _ := h.Meter().Float64Histogram(
		name,
		metric.WithDescription(description),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(boundaries...),
	)
	return Histogram{histogram: histogram}
}

func (h *Handle) Float64Gauge(name, description string) Gauge {
	gauge, _ := h.Meter().Float64Gauge(name, metric.WithDescription(description))
	return Gauge{gauge: gauge}
}

func Int64Counter(name, description string) Counter {
	key := name + "\x00" + description
	if cached, ok := counterCache.Load(key); ok {
		return cached.(Counter)
	}
	counter, _ := otel.Meter(instrumentationName).Int64Counter(name, metric.WithDescription(description))
	wrapped := Counter{counter: counter}
	actual, _ := counterCache.LoadOrStore(key, wrapped)
	return actual.(Counter)
}

func Float64Gauge(name, description string) Gauge {
	key := name + "\x00" + description
	if cached, ok := gaugeCache.Load(key); ok {
		return cached.(Gauge)
	}
	gauge, _ := otel.Meter(instrumentationName).Float64Gauge(name, metric.WithDescription(description))
	wrapped := Gauge{gauge: gauge}
	actual, _ := gaugeCache.LoadOrStore(key, wrapped)
	return actual.(Gauge)
}

func DurationHistogram(name, description string) Histogram {
	return DurationHistogramWithBuckets(name, description, defaultDurationHistogramBuckets...)
}

func DurationHistogramWithBuckets(name, description string, boundaries ...float64) Histogram {
	key := histogramCacheKey(name, description, boundaries)
	if cached, ok := histogramCache.Load(key); ok {
		return cached.(Histogram)
	}
	histogram, _ := otel.Meter(instrumentationName).Float64Histogram(
		name,
		metric.WithDescription(description),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(boundaries...),
	)
	wrapped := Histogram{histogram: histogram}
	actual, _ := histogramCache.LoadOrStore(key, wrapped)
	return actual.(Histogram)
}

func histogramCacheKey(name, description string, boundaries []float64) string {
	var key strings.Builder
	key.WriteString(name)
	key.WriteByte(0)
	key.WriteString(description)
	for _, boundary := range boundaries {
		key.WriteByte(0)
		key.WriteString(strconv.FormatFloat(boundary, 'g', -1, 64))
	}
	return key.String()
}

func RegisterInt64ObservableGauge(name, description string, callback Int64GaugeCallback) (ObservableRegistration, error) {
	return registerInt64ObservableGauge(otel.Meter(instrumentationName), name, description, callback)
}

func RegisterFloat64ObservableGauge(name, description string, callback Float64GaugeCallback) (ObservableRegistration, error) {
	return registerFloat64ObservableGauge(otel.Meter(instrumentationName), name, description, callback)
}

func (h *Handle) RegisterInt64ObservableGauge(name, description string, callback Int64GaugeCallback) (ObservableRegistration, error) {
	return registerInt64ObservableGauge(h.Meter(), name, description, callback)
}

func (h *Handle) RegisterFloat64ObservableGauge(name, description string, callback Float64GaugeCallback) (ObservableRegistration, error) {
	return registerFloat64ObservableGauge(h.Meter(), name, description, callback)
}

func registerInt64ObservableGauge(meter metric.Meter, name, description string, callback Int64GaugeCallback) (ObservableRegistration, error) {
	gauge, err := meter.Int64ObservableGauge(name, metric.WithDescription(description))
	if err != nil {
		return nil, err
	}
	return meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		if callback == nil {
			return nil
		}
		return callback(ctx, func(value int64, attrs ...attribute.KeyValue) {
			observer.ObserveInt64(gauge, value, metric.WithAttributes(metricAttrs(attrs...)...))
		})
	}, gauge)
}

func registerFloat64ObservableGauge(meter metric.Meter, name, description string, callback Float64GaugeCallback) (ObservableRegistration, error) {
	gauge, err := meter.Float64ObservableGauge(name, metric.WithDescription(description))
	if err != nil {
		return nil, err
	}
	return meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		if callback == nil {
			return nil
		}
		return callback(ctx, func(value float64, attrs ...attribute.KeyValue) {
			observer.ObserveFloat64(gauge, value, metric.WithAttributes(metricAttrs(attrs...)...))
		})
	}, gauge)
}

func (c Counter) Add(ctx context.Context, value int64, attrs ...attribute.KeyValue) {
	if c.counter == nil {
		return
	}
	c.counter.Add(ctx, value, metric.WithAttributes(metricAttrs(attrs...)...))
}

func (c UpDownCounter) Add(ctx context.Context, value int64, attrs ...attribute.KeyValue) {
	if c.counter == nil {
		return
	}
	c.counter.Add(ctx, value, metric.WithAttributes(metricAttrs(attrs...)...))
}

func (g Gauge) Record(ctx context.Context, value float64, attrs ...attribute.KeyValue) {
	if g.gauge == nil {
		return
	}
	g.gauge.Record(ctx, value, metric.WithAttributes(metricAttrs(attrs...)...))
}

func (h Histogram) RecordDuration(ctx context.Context, value time.Duration, attrs ...attribute.KeyValue) {
	if h.histogram == nil {
		return
	}
	h.histogram.Record(ctx, value.Seconds(), metric.WithAttributes(metricAttrs(attrs...)...))
}

func setMetricIdentityNodeID(nodeID string) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		metricIdentity.Store(nil)
		return
	}
	metricIdentity.Store(&metricIdentityAttrs{attrs: []attribute.KeyValue{
		attribute.String(AttrNodeID, SanitizeValue(nodeID)),
	}})
}

func metricAttrs(attrs ...attribute.KeyValue) []attribute.KeyValue {
	attrs = SafeAttrs(attrs...)
	identity := metricIdentity.Load()
	if identity == nil || len(identity.attrs) == 0 {
		return attrs
	}
	for _, attr := range attrs {
		if attr.Key == AttrNodeID {
			return attrs
		}
	}
	out := make([]attribute.KeyValue, 0, len(identity.attrs)+len(attrs))
	out = append(out, identity.attrs...)
	out = append(out, attrs...)
	return out
}
