package imageregistry

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cofy-x/axern/lib/go/imageref"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/registryauth"
)

// Client handles image fetching from container registries with authentication.
type Client struct {
	keychain           authn.Keychain
	transportCache     map[string]*registryTransport
	transportCacheLock sync.RWMutex
	insecureRegistries map[string]struct{}
	registryMirror     *url.URL
}

type ClientOption func(*Client) error

type registryRoute string

const (
	registryRouteDirect       registryRoute = "direct"
	registryRouteForwardProxy registryRoute = "forward_proxy"
	registryRouteMirror       registryRoute = "mirror"
)

func WithRegistryMirror(rawURL string) ClientOption {
	return func(client *Client) error {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return nil
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("invalid registry mirror URL %q", rawURL)
		}
		if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("registry mirror URL must be an origin without credentials, path, query, or fragment")
		}
		parsed.Path = ""
		client.registryMirror = parsed
		return nil
	}
}

// NewClient creates a registry client by loading credentials from registry_auths.json.
func NewClient(registryAuthsPath string, options ...ClientOption) (*Client, error) {
	client := &Client{
		transportCache:     make(map[string]*registryTransport),
		insecureRegistries: insecureRegistriesFromEnv(),
	}
	if registryAuthsPath == "" {
		client.keychain = &registryKeychain{}
	} else {
		auths, err := registryauth.Load(registryAuthsPath)
		if err != nil {
			return nil, err
		}
		client.keychain = &registryKeychain{auths: auths}
	}
	for _, option := range options {
		if option != nil {
			if err := option(client); err != nil {
				return nil, err
			}
		}
	}
	return client, nil
}

// NormalizeImageRef removes protocol prefixes and returns a canonical image reference.
func NormalizeImageRef(imageRef string) string {
	return imageref.Normalize(imageRef)
}

// FetchImageWithFallback prefers an explicit mirror, then an explicit forward
// proxy, and finally direct registry access. Registry TLS remains enabled when
// a forward proxy is used.
func (c *Client) FetchImageWithFallback(ctx context.Context, imageRef string, proxyURL string) (v1.Image, error) {
	if c == nil {
		return nil, fmt.Errorf("registry client is nil")
	}
	normalized := NormalizeImageRef(imageRef)

	return c.fetchImageWithFallback(ctx, normalized, proxyURL, c.keychain)
}

func (c *Client) FetchImageWithFallbackWithDockerConfigJSON(ctx context.Context, imageRef string, proxyURL string, dockerConfigJSON string) (v1.Image, error) {
	if c == nil {
		return nil, fmt.Errorf("registry client is nil")
	}
	normalized := NormalizeImageRef(imageRef)
	keychain, err := c.keychainForDockerConfigJSON(dockerConfigJSON)
	if err != nil {
		return nil, err
	}
	return c.fetchImageWithFallback(ctx, normalized, proxyURL, keychain)
}

// FetchImageDirect fetches an image from its origin registry without using a
// configured mirror or forward proxy.
func (c *Client) FetchImageDirect(ctx context.Context, imageRef string) (v1.Image, error) {
	if c == nil {
		return nil, fmt.Errorf("registry client is nil")
	}
	normalized := NormalizeImageRef(imageRef)
	return c.fetchImage(ctx, normalized, c.useHTTPFor(normalized), c.keychain, c.getOrCreateTransport(registryRouteDirect, ""))
}

// FetchImageDirectWithDockerConfigJSON is FetchImageDirect with request-scoped
// registry credentials.
func (c *Client) FetchImageDirectWithDockerConfigJSON(ctx context.Context, imageRef, dockerConfigJSON string) (v1.Image, error) {
	if c == nil {
		return nil, fmt.Errorf("registry client is nil")
	}
	keychain, err := c.keychainForDockerConfigJSON(dockerConfigJSON)
	if err != nil {
		return nil, err
	}
	normalized := NormalizeImageRef(imageRef)
	return c.fetchImage(ctx, normalized, c.useHTTPFor(normalized), keychain, c.getOrCreateTransport(registryRouteDirect, ""))
}

func (c *Client) fetchImageWithFallback(ctx context.Context, imageRef, proxyURL string, keychain authn.Keychain) (v1.Image, error) {
	if c.useHTTPFor(imageRef) {
		return c.fetchImage(ctx, imageRef, true, keychain, c.getOrCreateTransport(registryRouteDirect, ""))
	}
	if c.registryMirror != nil {
		img, err := c.fetchImage(ctx, imageRef, false, keychain, c.getOrCreateTransport(registryRouteMirror, ""))
		if err == nil {
			return img, nil
		}
		logrus.Warnf("failed to fetch image %s via registry mirror: %v", imageRef, err)
	}
	if proxyURL != "" {
		img, err := c.fetchImage(ctx, imageRef, false, keychain, c.getOrCreateTransport(registryRouteForwardProxy, proxyURL))
		if err == nil {
			return img, nil
		}
		logrus.Warnf("failed to fetch image %s via forward proxy: %v", imageRef, err)
	}
	return c.fetchImage(ctx, imageRef, false, keychain, c.getOrCreateTransport(registryRouteDirect, ""))
}

// FetchImage fetches an image from the registry with authentication and optional proxy.
func (c *Client) FetchImage(ctx context.Context, imageRef string, proxyURL string, useHTTP bool) (v1.Image, error) {
	if c == nil {
		return nil, fmt.Errorf("registry client is nil")
	}
	route := registryRouteDirect
	if proxyURL != "" {
		route = registryRouteForwardProxy
	}
	return c.fetchImage(ctx, imageRef, useHTTP, c.keychain, c.getOrCreateTransport(route, proxyURL))
}

func (c *Client) fetchImage(ctx context.Context, imageRef string, useHTTP bool, keychain authn.Keychain, transport *registryTransport) (img v1.Image, retErr error) {
	if c == nil {
		return nil, fmt.Errorf("registry client is nil")
	}
	fetchStart := time.Now()
	defer func() {
		if span := trace.SpanFromContext(ctx); span.IsRecording() {
			result := "success"
			if retErr != nil {
				result = "error"
			}
			span.AddEvent("registry_fetch",
				trace.WithAttributes(
					attribute.String("axern.registry.route", transport.route),
					attribute.String("axern.result", result),
					attribute.Int64("axern.duration_ms", time.Since(fetchStart).Milliseconds()),
				),
			)
		}
	}()

	stageStart := time.Now()
	parseOpts := []name.Option{}
	if useHTTP {
		parseOpts = append(parseOpts, name.Insecure)
	}
	ref, err := name.ParseReference(imageRef, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %s: %w", imageRef, err)
	}
	parseDuration := time.Since(stageStart)

	stageStart = time.Now()
	options := []remote.Option{
		remote.WithAuthFromKeychain(keychain),
		remote.WithPlatform(defaultRemotePlatform()),
	}
	options = append(options, remote.WithTransport(transport))
	transportDuration := time.Since(stageStart)

	stageStart = time.Now()
	img, err = remote.Image(ref, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image %s: %w", imageRef, err)
	}
	fetchDuration := time.Since(stageStart)

	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.AddEvent("fetch_image_details",
			trace.WithAttributes(
				attribute.Int64("parse_reference_ms", parseDuration.Milliseconds()),
				attribute.Int64("setup_transport_ms", transportDuration.Milliseconds()),
				attribute.Int64("registry_fetch_ms", fetchDuration.Milliseconds()),
			),
		)
	}

	return img, nil
}

func insecureRegistriesFromEnv() map[string]struct{} {
	raw := os.Getenv("IMAGEMGR_INSECURE_REGISTRIES")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return imageref.HostSetFromCSV(raw)
}

func (c *Client) useHTTPFor(imageRef string) bool {
	if c == nil {
		return false
	}
	return imageref.UseHTTPFor(imageRef, c.insecureRegistries)
}

// UseHTTPFor reports whether imageRef is configured to use an insecure HTTP
// registry. Callers that construct secondary registry data paths (for example
// the Nydus blob backend) must use the same transport policy as this client.
func (c *Client) UseHTTPFor(imageRef string) bool {
	return c.useHTTPFor(imageRef)
}

func registryHost(imageRef string) string {
	return imageref.RegistryHost(imageRef)
}

func (c *Client) keychainForDockerConfigJSON(dockerConfigJSON string) (authn.Keychain, error) {
	if strings.TrimSpace(dockerConfigJSON) == "" {
		return c.keychain, nil
	}
	auths, err := registryauth.Parse([]byte(dockerConfigJSON))
	if err != nil {
		return nil, fmt.Errorf("parse inline docker config json: %w", err)
	}
	return &registryKeychain{auths: auths}, nil
}

func (c *Client) getOrCreateTransport(route registryRoute, proxyURL string) *registryTransport {
	cacheKey := string(route) + ":" + proxyURL

	c.transportCacheLock.RLock()
	if transport, exists := c.transportCache[cacheKey]; exists {
		c.transportCacheLock.RUnlock()
		return transport
	}
	c.transportCacheLock.RUnlock()

	c.transportCacheLock.Lock()
	defer c.transportCacheLock.Unlock()

	if transport, exists := c.transportCache[cacheKey]; exists {
		return transport
	}

	direct := route != registryRouteForwardProxy
	base := newRegistryBaseTransport(proxyURL, direct)
	transport := &registryTransport{base: base, route: string(route)}
	if route == registryRouteMirror {
		transport.roundTripper = &registryMirrorTransport{base: base, mirror: c.registryMirror}
	}
	c.transportCache[cacheKey] = transport
	return transport
}

func defaultRemotePlatform() v1.Platform {
	return v1.Platform{
		OS:           "linux",
		Architecture: runtime.GOARCH,
	}
}
