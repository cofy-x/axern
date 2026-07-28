package nydus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	remoteTransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestFetchCheckAndExtractRunsImageConfigAndBootstrapConcurrently(t *testing.T) {
	configStarted := make(chan struct{})
	extractStarted := make(chan struct{})
	client := &RegistryClient{
		fetchImageDirectFn: func(context.Context, string, string) (v1.Image, error) {
			return nil, nil
		},
		isNydusImageFn: func(v1.Image) (bool, error) { return true, nil },
		imageEnvFn: func(v1.Image, string, bool) []string {
			close(configStarted)
			select {
			case <-extractStarted:
			case <-time.After(time.Second):
				t.Error("bootstrap extraction did not start while image config was blocked")
			}
			return []string{"AXERN_TEST=1"}
		},
		extractBootstrapFn: func(_ context.Context, _ v1.Image, outputDir string) (string, error) {
			close(extractStarted)
			select {
			case <-configStarted:
			case <-time.After(time.Second):
				t.Error("image config did not start while bootstrap extraction was blocked")
			}
			return outputDir + "/bootstrap", nil
		},
	}

	result, err := client.fetchCheckAndExtract(context.Background(), "registry.example/test:nydus", t.TempDir(), "")
	if err != nil {
		t.Fatalf("fetchCheckAndExtract() error = %v", err)
	}
	if len(result.Env) != 1 || result.Env[0] != "AXERN_TEST=1" {
		t.Fatalf("result.Env = %v", result.Env)
	}
}

func TestFetchAndExtractBootstrapRetriesTransientDirectFailure(t *testing.T) {
	prevDelay := nydusFetchRetryDelay
	prevMaxDelay := nydusFetchRetryMaxDelay
	nydusFetchRetryDelay = time.Millisecond
	nydusFetchRetryMaxDelay = time.Millisecond
	defer func() {
		nydusFetchRetryDelay = prevDelay
		nydusFetchRetryMaxDelay = prevMaxDelay
	}()

	var directFetches int
	var extractCalls int

	client := &RegistryClient{
		fetchImageDirectFn: func(_ context.Context, _ string, _ string) (v1.Image, error) {
			directFetches++
			return nil, nil
		},
		isNydusImageFn: func(v1.Image) (bool, error) {
			return true, nil
		},
		extractBootstrapFn: func(_ context.Context, _ v1.Image, outputDir string) (string, error) {
			extractCalls++
			if extractCalls < nydusFetchRetryAttempts {
				return "", fmt.Errorf("failed to decompress layer: %w", &remoteTransport.Error{StatusCode: http.StatusInternalServerError})
			}
			return filepath.Join(outputDir, "bootstrap"), nil
		},
	}

	got, _, err := client.FetchAndExtractBootstrap(context.Background(), "registry.example/test:nydus", t.TempDir())
	if err != nil {
		t.Fatalf("FetchAndExtractBootstrap() error = %v", err)
	}
	if got == "" {
		t.Fatalf("FetchAndExtractBootstrap() returned empty bootstrap path")
	}
	if directFetches != nydusFetchRetryAttempts {
		t.Fatalf("direct fetch count = %d, want %d", directFetches, nydusFetchRetryAttempts)
	}
	if extractCalls != nydusFetchRetryAttempts {
		t.Fatalf("extract call count = %d, want %d", extractCalls, nydusFetchRetryAttempts)
	}
}

func TestFetchAndExtractBootstrapRetriesOnUnexpectedEOF(t *testing.T) {
	prevDelay := nydusFetchRetryDelay
	prevMaxDelay := nydusFetchRetryMaxDelay
	nydusFetchRetryDelay = time.Millisecond
	nydusFetchRetryMaxDelay = time.Millisecond
	defer func() {
		nydusFetchRetryDelay = prevDelay
		nydusFetchRetryMaxDelay = prevMaxDelay
	}()

	var directFetches int

	client := &RegistryClient{
		fetchImageDirectFn: func(_ context.Context, _ string, _ string) (v1.Image, error) {
			directFetches++
			if directFetches < nydusFetchRetryAttempts {
				return nil, fmt.Errorf("failed to fetch layer: %w", io.ErrUnexpectedEOF)
			}
			return nil, nil
		},
		isNydusImageFn: func(v1.Image) (bool, error) {
			return true, nil
		},
		extractBootstrapFn: func(_ context.Context, _ v1.Image, outputDir string) (string, error) {
			return filepath.Join(outputDir, "bootstrap"), nil
		},
	}

	got, _, err := client.FetchAndExtractBootstrap(context.Background(), "registry.example/test:nydus", t.TempDir())
	if err != nil {
		t.Fatalf("FetchAndExtractBootstrap() error = %v", err)
	}
	if got == "" {
		t.Fatalf("FetchAndExtractBootstrap() returned empty bootstrap path")
	}
	if directFetches != nydusFetchRetryAttempts {
		t.Fatalf("direct fetch count = %d, want %d", directFetches, nydusFetchRetryAttempts)
	}
}

func TestFetchAndExtractBootstrapDoesNotRetryNonRetryableDirectFailure(t *testing.T) {
	prevDelay := nydusFetchRetryDelay
	prevMaxDelay := nydusFetchRetryMaxDelay
	nydusFetchRetryDelay = time.Millisecond
	nydusFetchRetryMaxDelay = time.Millisecond
	defer func() {
		nydusFetchRetryDelay = prevDelay
		nydusFetchRetryMaxDelay = prevMaxDelay
	}()

	var directFetches int

	client := &RegistryClient{
		fetchImageDirectFn: func(_ context.Context, _ string, _ string) (v1.Image, error) {
			directFetches++
			return nil, fmt.Errorf("failed to fetch manifest: %w", &remoteTransport.Error{StatusCode: http.StatusNotFound})
		},
	}

	if _, _, err := client.FetchAndExtractBootstrap(context.Background(), "registry.example/test:nydus", t.TempDir()); err == nil {
		t.Fatalf("FetchAndExtractBootstrap() error = nil, want non-nil")
	}
	if directFetches != 1 {
		t.Fatalf("direct fetch count = %d, want 1", directFetches)
	}
}

func TestDetectImageUsesFallbackFetch(t *testing.T) {
	var gotImageRef string
	var gotProxyURL string
	var checkCalls int

	client := &RegistryClient{
		fetchImageWithFallbackFn: func(_ context.Context, imageRef string, proxyURL string) (v1.Image, error) {
			gotImageRef = imageRef
			gotProxyURL = proxyURL
			return nil, nil
		},
		isNydusImageFn: func(v1.Image) (bool, error) {
			checkCalls++
			return true, nil
		},
	}

	ok, err := client.DetectImage(context.Background(), "localhost:5001/axern/nydus-smoke:dev", "http://proxy.local")
	if err != nil {
		t.Fatalf("DetectImage() error = %v", err)
	}
	if !ok {
		t.Fatal("DetectImage() = false, want true")
	}
	if gotImageRef != "localhost:5001/axern/nydus-smoke:dev" {
		t.Fatalf("image ref = %q, want localhost registry ref", gotImageRef)
	}
	if gotProxyURL != "http://proxy.local" {
		t.Fatalf("proxy URL = %q, want http://proxy.local", gotProxyURL)
	}
	if checkCalls != 1 {
		t.Fatalf("check calls = %d, want 1", checkCalls)
	}
}
