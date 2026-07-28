package process

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/cofy-x/axern/lib/go/llmproxy"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

const (
	managedProxyBaseURLEnv     = "AXERN_MANAGED_PROXY_BASE_URL"
	managedProxyTokenEnv       = "AXERN_MANAGED_PROXY_TOKEN"
	managedProxyReportMaxBytes = 1 << 20
)

type managedProxySession struct {
	proxy    *llmproxy.Proxy
	recorder *llmproxy.Recorder
	provider string
}

func startManagedProxy(spec *ManagedProxySpec) (*managedProxySession, []string, error) {
	if spec == nil {
		return nil, nil, nil
	}
	provider := llmproxy.ProviderForName(spec.Provider)
	if provider == nil {
		return nil, nil, fmt.Errorf("unsupported managed proxy provider %q", spec.Provider)
	}
	upstream, err := url.Parse(strings.TrimSpace(spec.UpstreamBaseURL))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, nil, fmt.Errorf("managed proxy upstream_base_url is invalid")
	}
	localToken, err := generateManagedProxyToken()
	if err != nil {
		return nil, nil, err
	}
	recorder := llmproxy.NewRecorder(provider)
	proxy, err := llmproxy.New(llmproxy.Options{
		Upstream:      upstream,
		Provider:      provider,
		UpstreamToken: spec.UpstreamBearerToken,
		LocalToken:    localToken,
		Recorder:      recorder,
	})
	if err != nil {
		return nil, nil, err
	}
	env := []string{
		managedProxyBaseURLEnv + "=" + proxy.BaseURL(),
		managedProxyTokenEnv + "=" + localToken,
		"NO_PROXY=" + localNoProxyValue(""),
		"no_proxy=" + localNoProxyValue(""),
	}
	return &managedProxySession{proxy: proxy, recorder: recorder, provider: provider.Name()}, env, nil
}

func generateManagedProxyToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generate managed proxy token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func withManagedProxyEnv(env []string, proxyEnv []string) []string {
	if len(proxyEnv) == 0 {
		return env
	}
	merged := proc.MergeEnv(env, proxyEnv)
	return proc.MergeEnv(merged, []string{
		"NO_PROXY=" + localNoProxyValue(envValue(merged, "NO_PROXY")),
		"no_proxy=" + localNoProxyValue(envValue(merged, "no_proxy")),
	})
}

func localNoProxyValue(current string) string {
	parts := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, item := range strings.Split(current, ",") {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		parts = append(parts, item)
	}
	for _, item := range []string{"127.0.0.1", "localhost"} {
		if seen[item] {
			continue
		}
		seen[item] = true
		parts = append(parts, item)
	}
	return strings.Join(parts, ",")
}

func envValue(env []string, name string) string {
	for _, item := range env {
		key, value, ok := proc.CutEnv(item)
		if ok && key == name {
			return value
		}
	}
	return ""
}

func (s *managedProxySession) closeAndReport() *ManagedProxyReport {
	if s == nil {
		return nil
	}
	if s.proxy != nil {
		_ = s.proxy.Close()
	}
	report, reportJSON, err := s.recorder.MarshalTransportReport(managedProxyReportMaxBytes)
	if err != nil {
		reportJSON = nil
	}
	return &ManagedProxyReport{
		Provider:      report.Provider,
		RequestCount:  report.RequestCount,
		ResponseCount: report.ResponseCount,
		ErrorCount:    report.ErrorCount,
		ReportJSON:    reportJSON,
	}
}
