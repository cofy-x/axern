package command

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/controlv1"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/spf13/cobra"
)

type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

func Usage(err error) error {
	if err == nil {
		return nil
	}
	return UsageError{Err: err}
}

func NoArgs(cmd *cobra.Command, args []string) error { return Usage(cobra.NoArgs(cmd, args)) }
func ExactArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return Usage(cobra.ExactArgs(count)(cmd, args)) }
}

type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e ExitError) Unwrap() error { return e.Err }

type Options struct {
	ConfigPath    string
	ContextName   string
	Endpoint      string
	TLSCACert     string
	TLSCert       string
	TLSKey        string
	TLSServerName string
	ProxyMode     string
	Timeout       time.Duration
	Output        string
}

type Runtime struct {
	Options *Options
	Root    *cobra.Command
}

type ResolvedConnection struct {
	ContextName string
	Config      controlv1.Config
}

func (r Runtime) Format(allowed ...output.Format) (output.Format, error) {
	format, err := output.ParseFormat(r.Options.Output, allowed...)
	if err != nil {
		return "", Usage(err)
	}
	return format, nil
}

func (r Runtime) ValidateOutput() error {
	_, err := output.ParseFormat(r.Options.Output)
	return Usage(err)
}

func (r Runtime) ResolveContext() (string, *clientconfig.Context, bool, error) {
	return clientconfig.Resolve(r.Options.ConfigPath, r.Options.ContextName)
}

func (r Runtime) Open(ctx context.Context) (*controlv1.Session, error) {
	connection, err := r.ResolveConnection()
	if err != nil {
		return nil, Usage(err)
	}
	return connection.Open(ctx)
}

func (r Runtime) ResolveConnection() (ResolvedConnection, error) {
	contextName := ""
	resolved := controlv1.Config{
		Endpoint: controlv1.DefaultTarget, TLSCACert: controlv1.DefaultTLSCACert,
		TLSCert: controlv1.DefaultTLSCert, TLSKey: controlv1.DefaultTLSKey,
		ProxyMode: controlv1.DefaultProxyMode,
		Timeout:   r.Options.Timeout,
	}
	if !r.hasExplicitConnection() {
		name, profile, ok, err := r.ResolveContext()
		if err != nil {
			return ResolvedConnection{}, err
		}
		if ok {
			contextName = name
			resolved.Endpoint = profile.Endpoint
			resolved.TLSCACert = profile.TLS.CACert
			resolved.TLSCert = profile.TLS.Cert
			resolved.TLSKey = profile.TLS.Key
			resolved.TLSServerName = profile.TLS.ServerName
			resolved.ProxyMode = profile.ProxyMode
		}
	}
	overrideEnv("AXERN_ENDPOINT", &resolved.Endpoint)
	overrideEnv("AXERN_TLS_CA_CERT", &resolved.TLSCACert)
	overrideEnv("AXERN_TLS_CERT", &resolved.TLSCert)
	overrideEnv("AXERN_TLS_KEY", &resolved.TLSKey)
	overrideEnv("AXERN_TLS_SERVER_NAME", &resolved.TLSServerName)
	overrideEnv("AXERN_PROXY_MODE", &resolved.ProxyMode)
	overrideString(r.Root, "endpoint", &resolved.Endpoint, r.Options.Endpoint)
	overrideString(r.Root, "tls-ca-cert", &resolved.TLSCACert, r.Options.TLSCACert)
	overrideString(r.Root, "tls-cert", &resolved.TLSCert, r.Options.TLSCert)
	overrideString(r.Root, "tls-key", &resolved.TLSKey, r.Options.TLSKey)
	overrideString(r.Root, "tls-server-name", &resolved.TLSServerName, r.Options.TLSServerName)
	overrideString(r.Root, "proxy-mode", &resolved.ProxyMode, r.Options.ProxyMode)
	if strings.TrimSpace(resolved.Endpoint) == "" {
		return ResolvedConnection{}, fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(resolved.TLSCACert) == "" || strings.TrimSpace(resolved.TLSCert) == "" || strings.TrimSpace(resolved.TLSKey) == "" {
		return ResolvedConnection{}, fmt.Errorf("gateway mTLS requires tls ca cert, cert, and key")
	}
	switch resolved.ProxyMode {
	case "", controlv1.ProxyModeEnv:
		resolved.ProxyMode = controlv1.ProxyModeEnv
	case controlv1.ProxyModeDirect:
	default:
		return ResolvedConnection{}, fmt.Errorf("proxy mode must be %q or %q", controlv1.ProxyModeEnv, controlv1.ProxyModeDirect)
	}
	return ResolvedConnection{ContextName: contextName, Config: resolved}, nil
}

func (c ResolvedConnection) Open(ctx context.Context) (*controlv1.Session, error) {
	return controlv1.Open(ctx, c.Config)
}

func (r Runtime) hasExplicitConnection() bool {
	return r.hasExplicitValue("endpoint", "AXERN_ENDPOINT", r.Options.Endpoint) &&
		r.hasExplicitValue("tls-ca-cert", "AXERN_TLS_CA_CERT", r.Options.TLSCACert) &&
		r.hasExplicitValue("tls-cert", "AXERN_TLS_CERT", r.Options.TLSCert) &&
		r.hasExplicitValue("tls-key", "AXERN_TLS_KEY", r.Options.TLSKey)
}

func (r Runtime) hasExplicitValue(flagName, envName, value string) bool {
	if r.Root.PersistentFlags().Changed(flagName) {
		return strings.TrimSpace(value) != ""
	}
	return strings.TrimSpace(os.Getenv(envName)) != ""
}

func overrideString(root *cobra.Command, name string, destination *string, value string) {
	if root.PersistentFlags().Changed(name) {
		*destination = value
	}
}

func overrideEnv(name string, destination *string) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		*destination = value
	}
}

func Resources(requestCPU, requestMemory, limitCPU, limitMemory string) (*commonv1.ResourceSpec, error) {
	rc, err := parse.CPU(requestCPU)
	if err != nil {
		return nil, fmt.Errorf("request-cpu: %w", err)
	}
	rm, err := parse.Memory(requestMemory)
	if err != nil {
		return nil, fmt.Errorf("request-memory: %w", err)
	}
	lc, err := parse.CPU(limitCPU)
	if err != nil {
		return nil, fmt.Errorf("limit-cpu: %w", err)
	}
	lm, err := parse.Memory(limitMemory)
	if err != nil {
		return nil, fmt.Errorf("limit-memory: %w", err)
	}
	if rc == 0 && rm == 0 && lc == 0 && lm == 0 {
		return nil, nil
	}
	value := &commonv1.ResourceSpec{}
	if rc != 0 || rm != 0 {
		value.Requests = &commonv1.ResourceQuantity{CpuMilli: rc, MemoryBytes: rm}
	}
	if lc != 0 || lm != 0 {
		value.Limits = &commonv1.ResourceQuantity{CpuMilli: lc, MemoryBytes: lm}
	}
	return value, nil
}
