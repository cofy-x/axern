package observability

import (
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

const (
	EnvEnabled = "AXERN_OTEL_ENABLED"

	defaultMetricInterval = 15 * time.Second
)

type Config struct {
	Enabled         bool
	ServiceName     string
	ServiceVersion  string
	Component       string
	NodeID          string
	OTLPEndpointURL string
	OTLPInsecure    bool
	ResourceAttrs   []attribute.KeyValue
	MetricInterval  time.Duration
}

type ConfigOption func(*configDefaults)

type configDefaults struct {
	serviceName    string
	serviceVersion string
	component      string
	nodeID         string
	metricInterval time.Duration
}

func WithServiceName(serviceName string) ConfigOption {
	return func(defaults *configDefaults) {
		defaults.serviceName = strings.TrimSpace(serviceName)
	}
}

func WithServiceVersion(serviceVersion string) ConfigOption {
	return func(defaults *configDefaults) {
		defaults.serviceVersion = strings.TrimSpace(serviceVersion)
	}
}

func WithComponent(component string) ConfigOption {
	return func(defaults *configDefaults) {
		defaults.component = strings.TrimSpace(component)
	}
}

func WithNodeID(nodeID string) ConfigOption {
	return func(defaults *configDefaults) {
		defaults.nodeID = strings.TrimSpace(nodeID)
	}
}

func WithMetricInterval(interval time.Duration) ConfigOption {
	return func(defaults *configDefaults) {
		if interval > 0 {
			defaults.metricInterval = interval
		}
	}
}

func ConfigFromEnv(options ...ConfigOption) Config {
	defaults := configDefaults{
		serviceVersion: Version(),
		metricInterval: defaultMetricInterval,
	}
	for _, option := range options {
		if option != nil {
			option(&defaults)
		}
	}
	return Config{
		Enabled:         parseBool(os.Getenv(EnvEnabled)),
		ServiceName:     defaultString(os.Getenv("OTEL_SERVICE_NAME"), defaults.serviceName),
		ServiceVersion:  defaultString(os.Getenv("OTEL_SERVICE_VERSION"), defaults.serviceVersion),
		Component:       defaults.component,
		NodeID:          defaults.nodeID,
		OTLPEndpointURL: strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		OTLPInsecure:    parseBool(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")),
		ResourceAttrs:   parseResourceAttributes(os.Getenv("OTEL_RESOURCE_ATTRIBUTES")),
		MetricInterval:  durationEnv("AXERN_OTEL_METRIC_INTERVAL", defaults.metricInterval),
	}
}

func parseResourceAttributes(raw string) []attribute.KeyValue {
	parts := strings.Split(raw, ",")
	attrs := make([]attribute.KeyValue, 0, len(parts))
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" {
			continue
		}
		attrs = append(attrs, StringAttr(key, value))
	}
	return attrs
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
