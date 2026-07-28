package axernsdk

import (
	"context"
	"strings"
	"testing"
)

func TestWithProxyModeRejectsUnknownModeBeforeDial(t *testing.T) {
	_, err := NewClient(context.Background(), "unused", WithProxyMode("tunnel"))
	if err == nil || !strings.Contains(err.Error(), "proxy mode") {
		t.Fatalf("NewClient() error = %v", err)
	}
}

func TestWithProxyModeUsesLastOption(t *testing.T) {
	config := clientConfig{}
	for _, option := range []ClientOption{WithProxyMode(ProxyModeDirect), WithProxyMode(ProxyModeEnv)} {
		if err := option(&config); err != nil {
			t.Fatal(err)
		}
	}
	if config.relayOptions.ProxyMode != ProxyModeEnv {
		t.Fatalf("proxy mode = %q, want %q", config.relayOptions.ProxyMode, ProxyModeEnv)
	}
	if len(config.dialOptions) != 0 {
		t.Fatalf("proxy options were applied before final resolution: %d", len(config.dialOptions))
	}
}
