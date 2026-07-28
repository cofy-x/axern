package ociimage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	"github.com/cofy-x/axern/lib/go/imageref"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Resolver struct{}

func NewResolver() *Resolver {
	return &Resolver{}
}

func (r *Resolver) Resolve(ctx context.Context, imageRef string, opts environmentkernel.ResolveOptions) (*environmentkernel.ResolvedImage, error) {
	normalized := normalizeImageRef(imageRef)
	if normalized == "" {
		return nil, fmt.Errorf("image ref is required")
	}
	parseOpts := []name.Option{}
	if useHTTPFor(normalized) {
		parseOpts = append(parseOpts, name.Insecure)
	}
	ref, err := name.ParseReference(normalized, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("parse image ref %q: %w", imageRef, err)
	}
	// Digest refs are already content-addressed; tag and name refs below still
	// go through registry HEAD resolution to obtain a stable descriptor.
	if digestRef, ok := ref.(name.Digest); ok {
		return &environmentkernel.ResolvedImage{
			Ref: digestRef.Name(),
			Descriptor: &catalogv1.OciImageDescriptor{
				Digest: digestRef.DigestStr(),
				Annotations: map[string]string{
					"org.opencontainers.image.ref.name": digestRef.Name(),
				},
			},
		}, nil
	}
	remoteOpts := []remote.Option{remote.WithContext(ctx)}
	if strings.TrimSpace(opts.DockerConfigJSON) != "" {
		keychain, err := newInlineKeychain(opts.DockerConfigJSON)
		if err != nil {
			return nil, fmt.Errorf("parse registry credential payload for %q: %w", ref.Context().RegistryStr(), err)
		}
		remoteOpts = append(remoteOpts, remote.WithAuthFromKeychain(keychain))
	}
	desc, err := remote.Head(ref, remoteOpts...)
	if err != nil {
		return nil, classifyResolveError(ref.Name(), ref.Context().RegistryStr(), strings.TrimSpace(opts.DockerConfigJSON) != "", err)
	}
	return &environmentkernel.ResolvedImage{
		Ref: ref.Name(),
		Descriptor: &catalogv1.OciImageDescriptor{
			Digest:    desc.Digest.String(),
			MediaType: string(desc.MediaType),
			SizeBytes: desc.Size,
			Annotations: map[string]string{
				"org.opencontainers.image.ref.name": ref.Name(),
			},
		},
	}, nil
}

func classifyResolveError(imageRef, registryHost string, usedCredential bool, err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case containsAnyToken(message, "unauthorized", "authentication required", "denied", "403", "401"):
		if usedCredential {
			return fmt.Errorf("resolve image ref %q from registry %q: authentication failed or access was denied; check the referenced docker-config-json secret and repository permissions: %w", imageRef, registryHost, err)
		}
		return fmt.Errorf("resolve image ref %q from registry %q: authentication is required or access was denied; provide a registry credential if this image is private: %w", imageRef, registryHost, err)
	case containsAnyToken(message, "not found", "manifest unknown", "manifest_unknown", "name unknown", "name_unknown", "404"):
		return fmt.Errorf("resolve image ref %q from registry %q: image or tag was not found: %w", imageRef, registryHost, err)
	default:
		return fmt.Errorf("resolve image ref %q from registry %q: %w", imageRef, registryHost, err)
	}
}

func containsAnyToken(message string, tokens ...string) bool {
	lower := strings.ToLower(message)
	for _, token := range tokens {
		if strings.Contains(lower, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

type inlineRegistryAuthConfig map[string]inlineRegistryAuthEntry

type inlineRegistryAuthEntry struct {
	Auth string `json:"Auth"`
}

type rawInlineRegistryEntry struct {
	AuthUpper string `json:"Auth"`
	AuthLower string `json:"auth"`
}

type inlineKeychain struct {
	auths inlineRegistryAuthConfig
}

func (k *inlineKeychain) Resolve(res authn.Resource) (authn.Authenticator, error) {
	host := res.RegistryStr()
	candidates := []string{host}
	if host == name.DefaultRegistry {
		candidates = append(candidates, "docker.io")
	}
	for _, candidate := range candidates {
		if entry, ok := k.auths[candidate]; ok && entry.Auth != "" {
			return authn.FromConfig(authn.AuthConfig{Auth: entry.Auth}), nil
		}
		for key, entry := range k.auths {
			if strings.HasPrefix(key, candidate+"/") && entry.Auth != "" {
				return authn.FromConfig(authn.AuthConfig{Auth: entry.Auth}), nil
			}
		}
	}
	return authn.Anonymous, nil
}

func newInlineKeychain(dockerConfigJSON string) (authn.Keychain, error) {
	auths, err := parseInlineRegistryAuths([]byte(dockerConfigJSON))
	if err != nil {
		return nil, err
	}
	return &inlineKeychain{auths: auths}, nil
}

func parseInlineRegistryAuths(data []byte) (inlineRegistryAuthConfig, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	entries := root
	if auths, ok := root["auths"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(auths, &nested); err != nil {
			return nil, fmt.Errorf("invalid auths section: %w", err)
		}
		entries = nested
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(inlineRegistryAuthConfig, len(keys))
	for _, key := range keys {
		var raw rawInlineRegistryEntry
		if err := json.Unmarshal(entries[key], &raw); err != nil {
			return nil, fmt.Errorf("invalid auth entry for %q: %w", key, err)
		}
		auth := raw.AuthUpper
		if auth == "" {
			auth = raw.AuthLower
		}
		normalized := normalizeRegistryKey(key)
		if normalized == "" {
			continue
		}
		out[normalized] = inlineRegistryAuthEntry{Auth: auth}
	}
	return out, nil
}

func normalizeRegistryKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.TrimSuffix(key, "/")
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		u, err := url.Parse(key)
		if err == nil && u.Host != "" {
			path := strings.Trim(u.Path, "/")
			if path == "" || path == "v1" || path == "v2" {
				return u.Host
			}
			return u.Host + "/" + path
		}
	}
	return key
}

func normalizeImageRef(imageRef string) string {
	return imageref.Normalize(imageRef)
}

func useHTTPFor(imageRef string) bool {
	return imageref.UseHTTPFor(imageRef, insecureRegistriesFromEnv())
}

func insecureRegistriesFromEnv() map[string]struct{} {
	raw := os.Getenv("CONTROLD_INSECURE_REGISTRIES")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return imageref.HostSetFromCSV(raw)
}

func registryHost(imageRef string) string {
	return imageref.RegistryHost(imageRef)
}
