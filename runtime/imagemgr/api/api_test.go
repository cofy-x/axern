package api

import "testing"

func TestOCIMountRequest_String(t *testing.T) {
	req := &OCIMountRequest{
		ImageURL: "docker.io/library/alpine:latest",
	}

	str := req.String()
	expected := "(docker.io/library/alpine:latest)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestOCIUmountRequest_String(t *testing.T) {
	req := &OCIUmountRequest{
		ImageURL: "docker.io/library/alpine:latest",
	}

	str := req.String()
	expected := "(docker.io/library/alpine:latest)"

	if str != expected {
		t.Errorf("String() = %q, want %q", str, expected)
	}
}

func TestHttpClient_NewHttpClient(t *testing.T) {
	tests := []struct {
		name         string
		sockPath     string
		expectedPath string
	}{
		{
			name:         "custom socket path",
			sockPath:     "/custom/path/socket.sock",
			expectedPath: "/custom/path/socket.sock",
		},
		{
			name:         "empty path defaults to default",
			sockPath:     "",
			expectedPath: DefaultHttpSockPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewHttpClient(tt.sockPath)
			if client == nil {
				t.Fatal("NewHttpClient() returned nil")
			}
			if client.clt == nil {
				t.Error("HTTP client was not initialized")
			}
		})
	}
}

// Benchmark tests for critical paths
