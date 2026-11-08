package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeRequiresRegistryAPI(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		header string
		ready  bool
	}{
		{"ready", http.StatusOK, "registry/2.0", true},
		{"unrelated HTTP service", http.StatusOK, "", false},
		{"unavailable", http.StatusServiceUnavailable, "registry/2.0", false},
		{"redirect", http.StatusTemporaryRedirect, "registry/2.0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/v2/" || request.Method != http.MethodGet {
					t.Errorf("unexpected API probe: %s %s", request.Method, request.URL.Path)
				}
				writer.Header().Set("Docker-Distribution-Api-Version", test.header)
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			err := probe(context.Background(), server.Client(), server.URL+"/v2/")
			if (err == nil) != test.ready {
				t.Fatalf("ready=%t, error=%v", test.ready, err)
			}
		})
	}
}

func TestProbeHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := probe(ctx, &http.Client{}, "http://127.0.0.1:5000/v2/"); err == nil {
		t.Fatal("canceled probe succeeded")
	}
}
