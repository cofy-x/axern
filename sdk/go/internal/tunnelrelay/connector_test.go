package tunnelrelay

import (
	"context"
	"strings"
	"testing"
)

func TestRunConnectorRejectsUnknownProxyModeBeforeConnecting(t *testing.T) {
	err := RunConnector(context.Background(), ConnectorConfig{ProxyMode: "tunnel"})
	if err == nil || !strings.Contains(err.Error(), "proxy mode") {
		t.Fatalf("RunConnector() error = %v", err)
	}
}
