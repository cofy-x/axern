package agent

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/lib/go/agentprofile"
	"github.com/cofy-x/axern/lib/go/llmproxy"
)

type localProxy struct {
	proxy *llmproxy.Proxy
	token string
}

func startLocalProxy(profile agentprofile.Profile) (*localProxy, error) {
	provider := llmproxy.ProviderForName(string(profile.ProviderType))
	if provider == nil {
		return nil, fmt.Errorf("unsupported provider %q", profile.ProviderType)
	}
	token, err := generateLocalToken()
	if err != nil {
		return nil, err
	}
	proxy, err := llmproxy.New(llmproxy.Options{
		Upstream:                profile.Upstream,
		Provider:                provider,
		UpstreamToken:           profile.Token,
		LocalToken:              token,
		DisableEnvironmentProxy: configBool(profile.Config, "upstream_no_proxy"),
	})
	if err != nil {
		return nil, err
	}
	return &localProxy{proxy: proxy, token: token}, nil
}

func configBool(values map[string]string, key string) bool {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return false
	}
	parsed, err := strconv.ParseBool(raw)
	return err == nil && parsed
}

func (p *localProxy) Addr() string {
	if p == nil || p.proxy == nil {
		return ""
	}
	return p.proxy.Addr()
}

func (p *localProxy) Token() string {
	if p == nil {
		return ""
	}
	return p.token
}

func (p *localProxy) Close() error {
	if p == nil || p.proxy == nil {
		return nil
	}
	return p.proxy.Close()
}

func generateLocalToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate agent local token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
