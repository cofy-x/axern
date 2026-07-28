package axern

import "testing"

func TestConfigFromEnvParsesOptionalSettings(t *testing.T) {
	t.Setenv("AXERN_ENDPOINT", " 127.0.0.1:24000 ")
	t.Setenv("AXERN_TEMPLATE_ID", " python311 ")
	t.Setenv("AXERN_NAMESPACE", " bench ")
	t.Setenv("AXERN_RUNTIME_CLASS", " runc ")
	t.Setenv("AXERN_REQUEST_CPU", " 100m ")
	t.Setenv("AXERN_REQUEST_MEMORY", " 512MiB ")
	t.Setenv("AXERN_LIMIT_CPU", " 1 ")
	t.Setenv("AXERN_LIMIT_MEMORY", " 1GiB ")
	t.Setenv("AXERN_TLS_CA_CERT", " ca.pem ")
	t.Setenv("AXERN_TLS_CERT", " cert.pem ")
	t.Setenv("AXERN_TLS_KEY", " key.pem ")
	t.Setenv("AXERN_TLS_SERVER_NAME", " controld.local ")

	config := ConfigFromEnv()
	if config.Endpoint != "127.0.0.1:24000" ||
		config.TemplateID != "python311" ||
		config.Namespace != "bench" ||
		config.RuntimeClass != "runc" ||
		config.RequestCPU != "100m" ||
		config.RequestMemory != "512MiB" ||
		config.LimitCPU != "1" ||
		config.LimitMemory != "1GiB" ||
		config.TLSCACert != "ca.pem" ||
		config.TLSCert != "cert.pem" ||
		config.TLSKey != "key.pem" ||
		config.TLSServerName != "controld.local" {
		t.Fatalf("config = %#v", config)
	}
}

func TestConfigValidateRequiresEndpoint(t *testing.T) {
	err := Config{TemplateID: "python311"}.Validate()
	if err == nil {
		t.Fatal("Validate error = nil, want missing gateway endpoint error")
	}
}

func TestConfigValidateRequiresOneSource(t *testing.T) {
	for name, config := range map[string]Config{
		"none": {Endpoint: "127.0.0.1:24000"},
		"both": {Endpoint: "127.0.0.1:24000",
			TemplateID: "python311",
			Image:      "example.com/task:latest",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := config.Validate(); err == nil {
				t.Fatal("Validate error = nil, want source error")
			}
		})
	}
}

func TestConfigValidateAcceptsTemplateOrImage(t *testing.T) {
	for name, config := range map[string]Config{
		"template": {Endpoint: "127.0.0.1:24000", TemplateID: "python311"},
		"image":    {Endpoint: "127.0.0.1:24000", Image: "example.com/task:latest"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := config.Validate(); err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
		})
	}
}

func TestConfigValidateRejectsInvalidResources(t *testing.T) {
	config := Config{
		Endpoint:      "127.0.0.1:24000",
		TemplateID:    "python311",
		RequestMemory: "-1",
	}
	if err := config.Validate(); err == nil {
		t.Fatal("Validate error = nil, want invalid resource error")
	}
}

func TestConfigNamespaceDefault(t *testing.T) {
	if got := (Config{}).NamespaceOrDefault(); got != "default" {
		t.Fatalf("NamespaceOrDefault = %q", got)
	}
	if got := (Config{Namespace: " bench "}).NamespaceOrDefault(); got != "bench" {
		t.Fatalf("NamespaceOrDefault = %q", got)
	}
}
