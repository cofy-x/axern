package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/sdk/go/clientconfig"
)

type KubernetesImportParams struct {
	Name            string
	ConfigPath      string
	CertDir         string
	Endpoint        string
	ServiceURL      string
	SSHEndpoint     string
	SSHIdentityFile string
	TLSServerName   string
	ProxyMode       string
	Current         bool
}

type kubernetesSecret struct {
	Data map[string]string `json:"data"`
}

func ImportKubernetesSecret(secretJSON []byte, params KubernetesImportParams) error {
	if strings.TrimSpace(params.Name) == "" || filepath.Base(params.Name) != params.Name || params.Name == "." || params.Name == ".." {
		return fmt.Errorf("context name must be a non-empty path-safe name")
	}
	if strings.TrimSpace(params.CertDir) == "" {
		return fmt.Errorf("certificate directory is required")
	}
	var value kubernetesSecret
	if err := json.Unmarshal(secretJSON, &value); err != nil {
		return fmt.Errorf("parse Kubernetes Secret: %w", err)
	}
	if value.Data == nil {
		return fmt.Errorf("Kubernetes Secret has no data")
	}

	decoded := make(map[string][]byte, 3)
	paths := make(map[string]string, 3)
	for _, key := range []string{"ca.crt", "client.crt", "client.key"} {
		encoded, ok := value.Data[key]
		if !ok || encoded == "" {
			return fmt.Errorf("Kubernetes Secret is missing %q", key)
		}
		contents, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return fmt.Errorf("decode Kubernetes Secret key %q: %w", key, err)
		}
		decoded[key] = contents
		paths[key] = filepath.Join(params.CertDir, key)
	}

	contextValue := &clientconfig.Context{
		Endpoint:        params.Endpoint,
		ServiceURL:      params.ServiceURL,
		SSHEndpoint:     params.SSHEndpoint,
		SSHIdentityFile: params.SSHIdentityFile,
		TLS: clientconfig.TLS{
			CACert:     paths["ca.crt"],
			Cert:       paths["client.crt"],
			Key:        paths["client.key"],
			ServerName: params.TLSServerName,
		},
		ProxyMode: params.ProxyMode,
	}
	if err := clientconfig.Validate(contextValue); err != nil {
		return err
	}
	cfg, err := Load(params.ConfigPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(params.CertDir, 0o700); err != nil {
		return fmt.Errorf("create certificate directory: %w", err)
	}
	if err := os.Chmod(params.CertDir, 0o700); err != nil {
		return fmt.Errorf("protect certificate directory: %w", err)
	}
	for _, key := range []string{"ca.crt", "client.crt", "client.key"} {
		if err := writePrivateFile(paths[key], decoded[key]); err != nil {
			return err
		}
	}

	cfg.Contexts[params.Name] = contextValue
	if params.Current || cfg.CurrentContext == "" {
		cfg.CurrentContext = params.Name
	}
	return Save(params.ConfigPath, cfg)
}

func writePrivateFile(path string, contents []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".axern-cert-*")
	if err != nil {
		return fmt.Errorf("create temporary certificate: %w", err)
	}
	tmpPath := file.Name()
	defer os.Remove(tmpPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("protect temporary certificate: %w", err)
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return fmt.Errorf("write certificate %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close certificate %q: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install certificate %q: %w", path, err)
	}
	return nil
}
