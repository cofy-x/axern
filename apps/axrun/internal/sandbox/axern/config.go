package axern

import (
	"fmt"
	"os"
	"strings"

	axernsdk "github.com/cofy-x/axern/sdk/go"
)

type Config struct {
	Endpoint        string
	TemplateID      string
	Image           string
	Namespace       string
	RuntimeClass    string
	RequestCPU      string
	RequestMemory   string
	LimitCPU        string
	LimitMemory     string
	TLSCACert       string
	TLSCert         string
	TLSKey          string
	TLSServerName   string
	ProxyMode       string
	WorkspaceVolume bool
	ImageMounts     []axernsdk.ImageMount
	WorkspaceImage  *axernsdk.WorkspaceImageSource
}

const validationTemplateID = "axrun-validation-source"

func ConfigFromEnv() Config {
	return Config{
		Endpoint:      strings.TrimSpace(os.Getenv("AXERN_ENDPOINT")),
		TemplateID:    strings.TrimSpace(os.Getenv("AXERN_TEMPLATE_ID")),
		Image:         strings.TrimSpace(os.Getenv("AXERN_IMAGE")),
		Namespace:     strings.TrimSpace(os.Getenv("AXERN_NAMESPACE")),
		RuntimeClass:  strings.TrimSpace(os.Getenv("AXERN_RUNTIME_CLASS")),
		RequestCPU:    strings.TrimSpace(os.Getenv("AXERN_REQUEST_CPU")),
		RequestMemory: strings.TrimSpace(os.Getenv("AXERN_REQUEST_MEMORY")),
		LimitCPU:      strings.TrimSpace(os.Getenv("AXERN_LIMIT_CPU")),
		LimitMemory:   strings.TrimSpace(os.Getenv("AXERN_LIMIT_MEMORY")),
		TLSCACert:     strings.TrimSpace(os.Getenv("AXERN_TLS_CA_CERT")),
		TLSCert:       strings.TrimSpace(os.Getenv("AXERN_TLS_CERT")),
		TLSKey:        strings.TrimSpace(os.Getenv("AXERN_TLS_KEY")),
		TLSServerName: strings.TrimSpace(os.Getenv("AXERN_TLS_SERVER_NAME")),
		ProxyMode:     strings.TrimSpace(os.Getenv("AXERN_PROXY_MODE")),
	}
}

func (c Config) Validate() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	return c.ValidateSource()
}

func (c Config) ValidateBase() error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return fmt.Errorf("axern runner requires AXERN_ENDPOINT")
	}
	if _, err := axernsdk.NewSandbox(axernsdk.SandboxOptions{
		Client:         &axernsdk.Client{},
		TemplateID:     validationTemplateID,
		ImageMounts:    c.ImageMounts,
		WorkspaceImage: c.WorkspaceImage,
		RequestCPU:     axernsdk.ResourceQuantity(c.RequestCPU),
		RequestMemory:  axernsdk.ResourceQuantity(c.RequestMemory),
		LimitCPU:       axernsdk.ResourceQuantity(c.LimitCPU),
		LimitMemory:    axernsdk.ResourceQuantity(c.LimitMemory),
	}); err != nil {
		return err
	}
	return nil
}

func (c Config) ValidateSource() error {
	sourceCount := 0
	if strings.TrimSpace(c.TemplateID) != "" {
		sourceCount++
	}
	if strings.TrimSpace(c.Image) != "" {
		sourceCount++
	}
	if sourceCount != 1 {
		return fmt.Errorf("axern runner requires exactly one of AXERN_TEMPLATE_ID or AXERN_IMAGE")
	}
	if _, err := axernsdk.NewSandbox(axernsdk.SandboxOptions{
		Client:         &axernsdk.Client{},
		TemplateID:     c.TemplateID,
		Image:          c.Image,
		ImageMounts:    c.ImageMounts,
		WorkspaceImage: c.WorkspaceImage,
		RequestCPU:     axernsdk.ResourceQuantity(c.RequestCPU),
		RequestMemory:  axernsdk.ResourceQuantity(c.RequestMemory),
		LimitCPU:       axernsdk.ResourceQuantity(c.LimitCPU),
		LimitMemory:    axernsdk.ResourceQuantity(c.LimitMemory),
	}); err != nil {
		return err
	}
	return nil
}

func (c Config) NamespaceOrDefault() string {
	if strings.TrimSpace(c.Namespace) == "" {
		return "default"
	}
	return strings.TrimSpace(c.Namespace)
}
