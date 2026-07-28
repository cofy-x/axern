package nydus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	remoteTransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
)

const nydusFetchRetryAttempts = 3

var (
	nydusFetchRetryDelay    = time.Second
	nydusFetchRetryMaxDelay = 5 * time.Second
)

// RegistryClient is the Nydus-facing wrapper over shared registry client.
type RegistryClient struct {
	shared *imageregistry.Client

	bootstrapCache     *bootstrapCache
	bootstrapCacheLock sync.Mutex

	// The following hooks are for unit tests only.
	fetchImageFn             func(context.Context, string, string, bool) (v1.Image, error)
	fetchImageDirectFn       func(context.Context, string, string) (v1.Image, error)
	fetchImageWithFallbackFn func(context.Context, string, string) (v1.Image, error)
	isNydusImageFn           func(v1.Image) (bool, error)
	extractBootstrapFn       func(context.Context, v1.Image, string) (string, error)
	imageEnvFn               func(v1.Image, string, bool) []string
}

func (c *RegistryClient) imageEnv(img v1.Image, imageURL string, warnOnConfigError bool) []string {
	if c != nil && c.imageEnvFn != nil {
		return c.imageEnvFn(img, imageURL, warnOnConfigError)
	}
	return envFromImageConfig(img, imageURL, warnOnConfigError)
}

// NewRegistryClientFromShared creates a Nydus registry client wrapper from an existing shared client.
func NewRegistryClientFromShared(shared *imageregistry.Client) *RegistryClient {
	if shared == nil {
		return nil
	}
	return &RegistryClient{
		shared:         shared,
		bootstrapCache: newBootstrapCache(),
	}
}

// UseHTTPFor exposes the shared registry transport policy to the Nydus blob
// backend so bootstrap and lazy blob reads cannot disagree on the scheme.
func (c *RegistryClient) UseHTTPFor(imageRef string) bool {
	return c != nil && c.shared != nil && c.shared.UseHTTPFor(imageRef)
}

// FetchImage fetches an image from the registry with authentication and optional proxy.
func (c *RegistryClient) FetchImage(ctx context.Context, imageRef string, proxyURL string, useHTTP bool) (v1.Image, error) {
	if c != nil && c.fetchImageFn != nil {
		return c.fetchImageFn(ctx, imageRef, proxyURL, useHTTP)
	}
	if c == nil || c.shared == nil {
		return nil, fmt.Errorf("registry client is not initialized")
	}
	return c.shared.FetchImage(ctx, imageRef, proxyURL, useHTTP)
}

// FetchImageWithFallback fetches image through shared fallback strategy.
func (c *RegistryClient) FetchImageWithFallback(ctx context.Context, imageRef string, proxyURL string) (v1.Image, error) {
	if c != nil && c.fetchImageWithFallbackFn != nil {
		return c.fetchImageWithFallbackFn(ctx, imageRef, proxyURL)
	}
	if c == nil || c.shared == nil {
		return nil, fmt.Errorf("registry client is not initialized")
	}
	return c.shared.FetchImageWithFallback(ctx, imageRef, proxyURL)
}

func (c *RegistryClient) fetchImageWithFallbackAndAuth(ctx context.Context, imageRef, proxyURL, dockerConfigJSON string) (v1.Image, error) {
	if strings.TrimSpace(dockerConfigJSON) == "" {
		return c.FetchImageWithFallback(ctx, imageRef, proxyURL)
	}
	if c == nil || c.shared == nil {
		return nil, fmt.Errorf("registry client is not initialized")
	}
	return c.shared.FetchImageWithFallbackWithDockerConfigJSON(ctx, imageRef, proxyURL, dockerConfigJSON)
}

func (c *RegistryClient) fetchImageDirectAndAuth(ctx context.Context, imageRef, dockerConfigJSON string) (v1.Image, error) {
	if c != nil && c.fetchImageDirectFn != nil {
		return c.fetchImageDirectFn(ctx, imageRef, dockerConfigJSON)
	}
	if c == nil || c.shared == nil {
		return nil, fmt.Errorf("registry client is not initialized")
	}
	if strings.TrimSpace(dockerConfigJSON) == "" {
		return c.shared.FetchImageDirect(ctx, imageRef)
	}
	return c.shared.FetchImageDirectWithDockerConfigJSON(ctx, imageRef, dockerConfigJSON)
}

// DetectImage fetches an image through the shared registry strategy and checks
// whether its manifest is in Nydus format.
func (c *RegistryClient) DetectImage(ctx context.Context, imageRef string, proxyURL string) (bool, error) {
	return c.DetectImageWithDockerConfigJSON(ctx, imageRef, proxyURL, "")
}

func (c *RegistryClient) DetectImageWithDockerConfigJSON(ctx context.Context, imageRef, proxyURL, dockerConfigJSON string) (bool, error) {
	img, err := c.fetchImageWithFallbackAndAuth(ctx, imageRef, proxyURL, dockerConfigJSON)
	if err != nil {
		return false, err
	}
	return c.isNydusImage(img)
}

func shouldRetryNydusFetch(err error) bool {
	if err == nil {
		return false
	}

	var transportErr *remoteTransport.Error
	if errors.As(err, &transportErr) && transportErr.StatusCode == http.StatusInternalServerError {
		return true
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	return false
}

func (c *RegistryClient) isNydusImage(img v1.Image) (bool, error) {
	if c != nil && c.isNydusImageFn != nil {
		return c.isNydusImageFn(img)
	}
	return IsNydusImage(img)
}

func (c *RegistryClient) extractBootstrap(ctx context.Context, img v1.Image, outputDir string) (bootstrapExtractionResult, error) {
	if c != nil && c.extractBootstrapFn != nil {
		path, err := c.extractBootstrapFn(ctx, img, outputDir)
		return bootstrapExtractionResult{Path: path}, err
	}
	return extractBootstrap(img, outputDir)
}

func (c *RegistryClient) getBootstrapCache() *bootstrapCache {
	if c == nil {
		return newBootstrapCache()
	}

	c.bootstrapCacheLock.Lock()
	defer c.bootstrapCacheLock.Unlock()

	if c.bootstrapCache == nil {
		c.bootstrapCache = newBootstrapCache()
	}
	return c.bootstrapCache
}
