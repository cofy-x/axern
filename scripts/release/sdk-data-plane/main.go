package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	axern "github.com/cofy-x/axern/sdk/go"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
)

func main() {
	configPath := required("AXERN_SDK_ACCEPTANCE_CONFIG")
	contextName := required("AXERN_SDK_ACCEPTANCE_CONTEXT")
	version := required("AXERN_SDK_ACCEPTANCE_VERSION")
	handshake := required("AXERN_SDK_ACCEPTANCE_HANDSHAKE_DIR")
	marker := "axern-go-sdk-release-ok"
	if axern.Version() != version {
		panic(fmt.Sprintf("unexpected Go SDK version: %s", axern.Version()))
	}

	_, profile, found, err := clientconfig.Resolve(configPath, contextName)
	check(err)
	if !found {
		panic("Axern acceptance context was not found")
	}
	proxyMode := profile.ProxyMode
	if proxyMode == "" {
		proxyMode = clientconfig.ProxyModeEnv
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, err := axern.NewClient(
		ctx,
		profile.Endpoint,
		axern.WithTLS(profile.TLS.CACert, profile.TLS.Cert, profile.TLS.Key, profile.TLS.ServerName),
		axern.WithProxyMode(proxyMode),
	)
	check(err)
	defer client.Close()

	sandbox, err := axern.NewSandbox(axern.SandboxOptions{
		Client:        client,
		TemplateID:    "python311",
		RuntimeClass:  "runsc",
		RequestCPU:    "100m",
		RequestMemory: "512MiB",
		ReadyTimeout:  3 * time.Minute,
		Labels:        map[string]string{"axern.release.acceptance": "go"},
	})
	check(err)
	check(sandbox.Start(ctx))
	closed := false
	defer func() {
		if !closed {
			_ = sandbox.Close(context.Background())
		}
	}()

	result, err := sandbox.Exec(ctx, axern.Args("python", "-c", fmt.Sprintf("print(%q)", marker)), axern.ExecOptions{Check: true})
	check(err)
	if strings.TrimSpace(result.StdoutString()) != marker {
		panic(fmt.Sprintf("unexpected Go SDK exec output: %q", result.StdoutString()))
	}
	metadata, err := sandbox.Metadata()
	check(err)
	check(os.WriteFile(filepath.Join(handshake, "go.service-id"), []byte(metadata.ServiceID), 0o600))
	waitVerified(filepath.Join(handshake, "go.verified"))
	fmt.Printf("sdk_data_plane=go service_id=%s ok=true\n", metadata.ServiceID)
	check(sandbox.Close(context.Background()))
	closed = true
}

func waitVerified(path string) {
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			check(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	panic("CLI did not verify the Go SDK service")
}

func required(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(name + " is required")
	}
	return value
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
