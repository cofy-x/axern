package nodeinventory

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultKubernetesServiceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	defaultKubernetesAPITimeout        = 3 * time.Second
)

type NodeResources struct {
	Capacity    NodeResourceQuantity
	Allocatable NodeResourceQuantity
	Labels      map[string]string
}

type NodeResourceProvider interface {
	Collect(context.Context) (NodeResources, error)
}

type hostNodeResourceProvider struct{}
type errorNodeResourceProvider struct {
	err error
}

func NewHostNodeResourceProvider() NodeResourceProvider {
	return hostNodeResourceProvider{}
}

func NewErrorNodeResourceProvider(err error) NodeResourceProvider {
	if err == nil {
		err = fmt.Errorf("node resource provider unavailable")
	}
	return errorNodeResourceProvider{err: err}
}

func defaultNodeResourceProvider(provider NodeResourceProvider) NodeResourceProvider {
	if provider != nil {
		return provider
	}
	return NewHostNodeResourceProvider()
}

func (hostNodeResourceProvider) Collect(context.Context) (NodeResources, error) {
	capacity := detectNodeCapacity()
	return NodeResources{Capacity: capacity, Allocatable: capacity}, nil
}

func (p errorNodeResourceProvider) Collect(context.Context) (NodeResources, error) {
	return NodeResources{}, p.err
}

type KubernetesNodeResourceProviderOptions struct {
	NodeName   string
	APIServer  string
	TokenPath  string
	CACertPath string
	Timeout    time.Duration
}

type kubernetesNodeResourceProvider struct {
	nodeName  string
	apiServer string
	tokenPath string
	client    *http.Client
}

func NewKubernetesNodeResourceProvider(options KubernetesNodeResourceProviderOptions) (NodeResourceProvider, error) {
	nodeName := strings.TrimSpace(options.NodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("kubernetes node name is required")
	}
	apiServer := strings.TrimRight(strings.TrimSpace(options.APIServer), "/")
	if apiServer == "" {
		host := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST"))
		port := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT"))
		if host == "" || port == "" {
			return nil, fmt.Errorf("kubernetes service host and port are required")
		}
		apiServer = "https://" + host + ":" + port
	}
	tokenPath := strings.TrimSpace(options.TokenPath)
	if tokenPath == "" {
		tokenPath = filepath.Join(defaultKubernetesServiceAccountDir, "token")
	}
	caCertPath := strings.TrimSpace(options.CACertPath)
	if caCertPath == "" {
		caCertPath = filepath.Join(defaultKubernetesServiceAccountDir, "ca.crt")
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultKubernetesAPITimeout
	}
	client, err := kubernetesHTTPClient(apiServer, caCertPath, timeout)
	if err != nil {
		return nil, err
	}
	return &kubernetesNodeResourceProvider{
		nodeName:  nodeName,
		apiServer: apiServer,
		tokenPath: tokenPath,
		client:    client,
	}, nil
}

func kubernetesHTTPClient(apiServer string, caCertPath string, timeout time.Duration) (*http.Client, error) {
	client := &http.Client{Timeout: timeout}
	if strings.HasPrefix(apiServer, "http://") {
		return client, nil
	}
	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("read kubernetes service account CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse kubernetes service account CA")
	}
	client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	return client, nil
}

func (p *kubernetesNodeResourceProvider) Collect(ctx context.Context) (NodeResources, error) {
	token, err := os.ReadFile(p.tokenPath)
	if err != nil {
		return NodeResources{}, fmt.Errorf("read kubernetes service account token: %w", err)
	}
	endpoint := p.apiServer + "/api/v1/nodes/" + url.PathEscape(p.nodeName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return NodeResources{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return NodeResources{}, fmt.Errorf("get kubernetes node %s: %w", p.nodeName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NodeResources{}, fmt.Errorf("get kubernetes node %s: status %s", p.nodeName, resp.Status)
	}

	var node kubernetesNodeResourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		return NodeResources{}, fmt.Errorf("decode kubernetes node %s: %w", p.nodeName, err)
	}
	resources, err := resourcesFromKubernetesNode(node)
	if err != nil {
		return NodeResources{}, fmt.Errorf("kubernetes node %s resources: %w", p.nodeName, err)
	}
	return resources, nil
}

type kubernetesNodeResourceResponse struct {
	Metadata struct {
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		Capacity    map[string]string `json:"capacity"`
		Allocatable map[string]string `json:"allocatable"`
	} `json:"status"`
}

func resourcesFromKubernetesNode(node kubernetesNodeResourceResponse) (NodeResources, error) {
	capacity, err := nodeResourceQuantityFromKubernetes(node.Status.Capacity)
	if err != nil {
		return NodeResources{}, fmt.Errorf("capacity: %w", err)
	}
	allocatable, err := nodeResourceQuantityFromKubernetes(node.Status.Allocatable)
	if err != nil {
		return NodeResources{}, fmt.Errorf("allocatable: %w", err)
	}
	return NodeResources{
		Capacity:    capacity,
		Allocatable: allocatable,
		Labels:      cloneStringMap(node.Metadata.Labels),
	}, nil
}

func nodeResourceQuantityFromKubernetes(values map[string]string) (NodeResourceQuantity, error) {
	cpu, err := parseKubernetesCPUQuantity(values["cpu"])
	if err != nil {
		return NodeResourceQuantity{}, err
	}
	memory, err := parseKubernetesMemoryQuantity(values["memory"])
	if err != nil {
		return NodeResourceQuantity{}, err
	}
	return NodeResourceQuantity{CpuMilli: cpu, MemoryBytes: memory}, nil
}

func parseKubernetesCPUQuantity(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("cpu is required")
	}
	if strings.HasSuffix(value, "m") {
		return parseScaledQuantity(strings.TrimSuffix(value, "m"), 1, "cpu")
	}
	return parseScaledQuantity(value, 1000, "cpu")
}

func parseKubernetesMemoryQuantity(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("memory is required")
	}
	units := []struct {
		suffix string
		scale  int64
	}{
		{suffix: "Ki", scale: 1024},
		{suffix: "Mi", scale: 1024 * 1024},
		{suffix: "Gi", scale: 1024 * 1024 * 1024},
		{suffix: "Ti", scale: 1024 * 1024 * 1024 * 1024},
		{suffix: "K", scale: 1000},
		{suffix: "M", scale: 1000 * 1000},
		{suffix: "G", scale: 1000 * 1000 * 1000},
		{suffix: "T", scale: 1000 * 1000 * 1000 * 1000},
	}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			return parseScaledQuantity(strings.TrimSuffix(value, unit.suffix), unit.scale, "memory")
		}
	}
	return parseScaledQuantity(value, 1, "memory")
}

func parseScaledQuantity(value string, scale int64, label string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") || strings.Contains(value, "/") {
		return 0, fmt.Errorf("%s must be non-negative", label)
	}
	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0, fmt.Errorf("%s must be a decimal number", label)
	}
	rat.Mul(rat, big.NewRat(scale, 1))
	if !rat.IsInt() {
		return 0, fmt.Errorf("%s must resolve to a whole unit", label)
	}
	out := rat.Num()
	if !out.IsInt64() {
		return 0, fmt.Errorf("%s is too large", label)
	}
	return out.Int64(), nil
}
