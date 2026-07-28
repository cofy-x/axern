package tunnel

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTerminalConnectorError(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.NotFound} {
		if !terminalConnectorError(status.Error(code, "terminal")) {
			t.Fatalf("terminalConnectorError(%s) = false, want true", code)
		}
	}
	if terminalConnectorError(status.Error(codes.Unavailable, "transient")) {
		t.Fatal("terminalConnectorError(Unavailable) = true, want false")
	}
	if terminalConnectorError(fmt.Errorf("plain error")) {
		t.Fatal("terminalConnectorError(plain error) = true, want false")
	}
}
