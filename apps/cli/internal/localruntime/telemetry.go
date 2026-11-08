package localruntime

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The development stack exports to an explicitly operated OTLP receiver. It
// never supplies a telemetry backend or infers a local receiver address.
func localTelemetryEnv() (map[string]string, error) {
	values := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
	}
	for _, name := range []string{"AXERN_OTEL_ENABLED", "OTEL_EXPORTER_OTLP_INSECURE"} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			value = "false"
		}
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%s must be a boolean", name)
		}
		values[name] = strconv.FormatBool(enabled)
	}
	if values["AXERN_OTEL_ENABLED"] == "true" && values["OTEL_EXPORTER_OTLP_ENDPOINT"] == "" {
		return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT is required when AXERN_OTEL_ENABLED is true")
	}
	return values, nil
}

func (m *Manager) localTelemetryConfigurationChanged() bool {
	desired, err := localTelemetryEnv()
	if err != nil {
		return true
	}
	for name, value := range desired {
		applied, err := readMaterializedEnvValue(m.envPath(), name)
		if err != nil || applied != value {
			return true
		}
	}
	return false
}
