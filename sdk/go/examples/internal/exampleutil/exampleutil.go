package exampleutil

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/lib/go/clientconfig"
	axern "github.com/cofy-x/axern/sdk/go"
)

type Config struct {
	ConfigPath    string
	ContextName   string
	Endpoint      string
	TLSCACert     string
	TLSCert       string
	TLSKey        string
	TLSServerName string
	TemplateID    string
	RuntimeClass  string
	ProxyMode     string
	resolved      bool
}

func Flags() *Config {
	config := &Config{
		ConfigPath:    env("AXERN_CONFIG", defaultConfigPath()),
		ContextName:   os.Getenv("AXERN_CONTEXT"),
		Endpoint:      env("AXERN_ENDPOINT", "127.0.0.1:25000"),
		TLSCACert:     os.Getenv("AXERN_TLS_CA_CERT"),
		TLSCert:       os.Getenv("AXERN_TLS_CERT"),
		TLSKey:        os.Getenv("AXERN_TLS_KEY"),
		TLSServerName: os.Getenv("AXERN_TLS_SERVER_NAME"),
		TemplateID:    env("AXERN_TEMPLATE_ID", "python311"),
		RuntimeClass:  os.Getenv("AXERN_RUNTIME_CLASS"),
		ProxyMode:     env("AXERN_PROXY_MODE", clientconfig.ProxyModeEnv),
	}
	flag.StringVar(&config.ConfigPath, "config", config.ConfigPath, "path to the local axern CLI config file")
	flag.StringVar(&config.ContextName, "context", config.ContextName, "named axern context from the local config file")
	flag.StringVar(&config.Endpoint, "endpoint", config.Endpoint, "gateway gRPC endpoint")
	flag.StringVar(&config.TLSCACert, "tls-ca-cert", config.TLSCACert, "control plane TLS CA certificate")
	flag.StringVar(&config.TLSCert, "tls-cert", config.TLSCert, "control plane TLS client certificate")
	flag.StringVar(&config.TLSKey, "tls-key", config.TLSKey, "control plane TLS client key")
	flag.StringVar(&config.TLSServerName, "tls-server-name", config.TLSServerName, "control plane TLS server name")
	flag.StringVar(&config.TemplateID, "template-id", config.TemplateID, "sandbox template id")
	flag.StringVar(&config.RuntimeClass, "runtime-class", config.RuntimeClass, "sandbox runtime class")
	flag.StringVar(&config.ProxyMode, "proxy-mode", config.ProxyMode, "gRPC proxy mode: env or direct")
	return config
}

func NewClient(ctx context.Context, config *Config) (*axern.Client, error) {
	if err := config.Resolve(); err != nil {
		return nil, err
	}
	var options []axern.ClientOption
	if config.TLSCACert != "" || config.TLSCert != "" || config.TLSKey != "" || config.TLSServerName != "" {
		options = append(options, axern.WithTLS(config.TLSCACert, config.TLSCert, config.TLSKey, config.TLSServerName))
	}
	options = append(options, axern.WithProxyMode(config.ProxyMode))
	return axern.NewClient(ctx, config.Endpoint, options...)
}

func StartSandbox(ctx context.Context, client *axern.Client, config *Config) (*axern.Sandbox, error) {
	if err := config.Resolve(); err != nil {
		return nil, err
	}
	sandbox, err := axern.NewSandbox(axern.SandboxOptions{
		Client:       client,
		TemplateID:   config.TemplateID,
		RuntimeClass: config.RuntimeClass,
		ReadyTimeout: 3 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	if err := sandbox.Start(ctx); err != nil {
		_ = sandbox.Close(ctx)
		return nil, err
	}
	return sandbox, nil
}

func PrintMetadata(sandbox *axern.Sandbox) error {
	metadata, err := sandbox.Metadata()
	if err != nil {
		return err
	}
	fmt.Printf("service=%s allocation=%s node=%s\n", metadata.ServiceID, metadata.AllocationID, metadata.NodeID)
	return nil
}

func (c *Config) Resolve() error {
	if c == nil || c.resolved {
		return nil
	}
	c.resolved = true
	return c.applyContextDefaults(explicitFlags(), os.Getenv)
}

func (c *Config) applyContextDefaults(explicit map[string]bool, getenv func(string) string) error {
	file, err := clientconfig.Load(c.ConfigPath)
	if err != nil {
		return err
	}
	contextName := c.ContextName
	if contextName == "" {
		contextName = file.CurrentContext
	}
	if contextName == "" {
		return nil
	}
	profile, ok := file.Contexts[contextName]
	if !ok || profile == nil {
		return fmt.Errorf("axern context %q not found in %s", contextName, c.ConfigPath)
	}
	if !explicit["context"] {
		c.ContextName = contextName
	}
	applyStringDefault(&c.Endpoint, profile.Endpoint, "endpoint", "AXERN_ENDPOINT", explicit, getenv)
	applyStringDefault(&c.TLSCACert, profile.TLS.CACert, "tls-ca-cert", "AXERN_TLS_CA_CERT", explicit, getenv)
	applyStringDefault(&c.TLSCert, profile.TLS.Cert, "tls-cert", "AXERN_TLS_CERT", explicit, getenv)
	applyStringDefault(&c.TLSKey, profile.TLS.Key, "tls-key", "AXERN_TLS_KEY", explicit, getenv)
	applyStringDefault(&c.TLSServerName, profile.TLS.ServerName, "tls-server-name", "AXERN_TLS_SERVER_NAME", explicit, getenv)
	applyStringDefault(&c.ProxyMode, profile.ProxyMode, "proxy-mode", "AXERN_PROXY_MODE", explicit, getenv)
	return nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func applyStringDefault(target *string, value, flagName, envName string, explicit map[string]bool, getenv func(string) string) {
	if strings.TrimSpace(value) == "" || explicit[flagName] || strings.TrimSpace(getenv(envName)) != "" {
		return
	}
	*target = value
}

func explicitFlags() map[string]bool {
	out := map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		out[f.Name] = true
	})
	return out
}

func defaultConfigPath() string {
	return clientconfig.DefaultPath()
}
