package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func assertSandboxdReady(ctx context.Context, bundlePath string) error {
	socketPath := runtimeoci.SandboxdBundleSocketPath(bundlePath)
	client := runtimesandboxd.NewClient(socketPath)
	snapshot, err := client.WaitReady(ctx, runtimesandboxd.DefaultReadyTimeout, runtimesandboxd.DefaultPollInterval)
	if err != nil {
		return err
	}
	if !snapshot.Ready.Ready {
		return fmt.Errorf("sandboxd ready response was false")
	}
	if snapshot.Status.UserProcess.State == "" {
		return fmt.Errorf("sandboxd status user state is empty")
	}
	if snapshot.Capabilities.ProtocolVersion != 1 {
		return fmt.Errorf("sandboxd capability protocol version = %d, want 1", snapshot.Capabilities.ProtocolVersion)
	}
	if snapshot.Capabilities.Summary.Total == 0 || snapshot.Capabilities.Summary.Available == 0 {
		return fmt.Errorf("sandboxd capability summary is empty: %#v", snapshot.Capabilities.Summary)
	}
	capabilities := map[string]bool{}
	for _, capability := range snapshot.Capabilities.Capabilities {
		capabilities[capability] = true
	}
	for _, want := range []string{"archive", "diagnostics", "health", "status", "supervisor", "file", "process", "pty", "probe", "ports", "mounts"} {
		if !capabilities[want] {
			return fmt.Errorf("sandboxd capabilities %q missing %q", strings.Join(snapshot.Capabilities.Capabilities, ","), want)
		}
	}
	providers := map[string]wire.CapabilityProvider{}
	for _, item := range snapshot.Capabilities.Providers {
		providers[item.Name] = item
	}
	for _, want := range []string{"core", "file", "process", "computer_use"} {
		if _, ok := providers[want]; !ok {
			return fmt.Errorf("sandboxd providers missing %q: %#v", want, snapshot.Capabilities.Providers)
		}
	}
	if providers["computer_use"].Available {
		return fmt.Errorf("computer_use provider should not be available in base OCI e2e: %#v", providers["computer_use"])
	}
	if capabilities["computer_use"] {
		return fmt.Errorf("computer_use should not be a baseline capability: %#v", snapshot.Capabilities.Capabilities)
	}
	diagnostics, err := client.Diagnostics(ctx)
	if err != nil {
		return fmt.Errorf("sandboxd diagnostics: %w", err)
	}
	if diagnostics.ProtocolVersion != 1 {
		return fmt.Errorf("sandboxd diagnostics protocol version = %d, want 1", diagnostics.ProtocolVersion)
	}
	if !diagnostics.Ready {
		return fmt.Errorf("sandboxd diagnostics ready=false: %#v", diagnostics)
	}
	if diagnostics.GeneratedAt.IsZero() {
		return fmt.Errorf("sandboxd diagnostics missing generatedAt: %#v", diagnostics)
	}
	if diagnostics.Status.UserProcess.State == "" {
		return fmt.Errorf("sandboxd diagnostics missing user process state: %#v", diagnostics)
	}
	if diagnostics.ProviderSummary.Total == 0 || diagnostics.ProviderSummary.Available == 0 {
		return fmt.Errorf("sandboxd diagnostics provider summary is empty: %#v", diagnostics.ProviderSummary)
	}
	if !containsString(diagnostics.Capabilities, "archive") || !containsString(diagnostics.Capabilities, "file") || !containsString(diagnostics.Capabilities, "process") {
		return fmt.Errorf("sandboxd diagnostics capabilities = %#v", diagnostics.Capabilities)
	}
	for _, want := range []string{"probe", "ports", "mounts"} {
		if !containsString(diagnostics.Capabilities, want) {
			return fmt.Errorf("sandboxd diagnostics missing %s capability: %#v", want, diagnostics.Capabilities)
		}
	}
	if len(diagnostics.Providers) == 0 {
		return fmt.Errorf("sandboxd diagnostics missing provider details")
	}
	if diagnostics.Processes == nil || diagnostics.Ports == nil || diagnostics.Mounts == nil {
		return fmt.Errorf("sandboxd diagnostics missing full diagnostic snapshots")
	}
	return nil
}

func assertSandboxdProbePortsMounts(ctx context.Context, bundlePath string) error {
	client := runtimesandboxd.NewClient(runtimeoci.SandboxdBundleSocketPath(bundlePath))
	probe, err := client.Probe(ctx, wire.ProbeRequest{TCP: &wire.TCPProbe{Port: 1}, TimeoutMS: 500})
	if err != nil {
		return fmt.Errorf("sandboxd tcp probe: %w", err)
	}
	if probe.OK || probe.Kind != "tcp" || probe.Detail == "" {
		return fmt.Errorf("sandboxd tcp probe failure shape = %#v", probe)
	}
	ports, err := client.Ports(ctx)
	if err != nil {
		return fmt.Errorf("sandboxd ports: %w", err)
	}
	if ports.Ports == nil {
		return fmt.Errorf("sandboxd ports returned nil list")
	}
	mounts, err := client.Mounts(ctx)
	if err != nil {
		return fmt.Errorf("sandboxd mounts: %w", err)
	}
	for _, want := range []string{"/", "/mnt", "/proc"} {
		if !hasMountPath(mounts.Paths, want) {
			return fmt.Errorf("sandboxd mounts missing path %s: %#v", want, mounts.Paths)
		}
	}
	return nil
}

func assertInjectedSpec(ociSpec any) error {
	data, err := json.Marshal(ociSpec)
	if err != nil {
		return err
	}
	body := string(data)
	if !strings.Contains(body, runtimeoci.SandboxdGuestBinaryPath) {
		return fmt.Errorf("generated OCI spec does not reference sandboxd binary")
	}
	if !strings.Contains(body, runtimeoci.SandboxdGuestEntrypointPath) {
		return fmt.Errorf("generated OCI spec does not reference sandboxd entrypoint")
	}
	return nil
}

func assertOutput(path string, runtimeOutput string, needles []string) error {
	if len(needles) == 0 {
		return nil
	}
	var bodies []string
	if runtimeOutput != "" {
		bodies = append(bodies, runtimeOutput)
	}
	data, err := os.ReadFile(path)
	if err == nil {
		bodies = append(bodies, string(data))
	} else if len(bodies) == 0 {
		return fmt.Errorf("read stdout: %w", err)
	}
	body := strings.Join(bodies, "\n")
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			return fmt.Errorf("stdout missing %q: %s", needle, body)
		}
	}
	return nil
}

func caseLogs(stdoutPath, stderrPath, runtimeOutput string) string {
	var b strings.Builder
	if runtimeOutput != "" {
		b.WriteString("runtime output:\n")
		b.WriteString(runtimeOutput)
	}
	appendLog := func(name, path string) {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(name)
		b.WriteString(":\n")
		b.Write(data)
	}
	appendLog("stdout", stdoutPath)
	appendLog("stderr", stderrPath)
	if b.Len() == 0 {
		return "<no runtime, stdout, or stderr output>"
	}
	return b.String()
}

func caseDiagnostics(bundlePath string, stdoutPath string, stderrPath string, runtimeOutput string) string {
	var b strings.Builder
	b.WriteString(caseLogs(stdoutPath, stderrPath, runtimeOutput))
	diagCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	socketPath := runtimeoci.SandboxdBundleSocketPath(bundlePath)
	appendSandboxdSocketInfo(&b, socketPath)
	if _, err := os.Stat(socketPath); err != nil {
		b.WriteString("\nsandboxd diagnostics:\n")
		b.WriteString(fmt.Sprintf("socket unavailable at %s: %v", socketPath, err))
		appendBundleFiles(&b, bundlePath)
		return b.String()
	}
	client := runtimesandboxd.NewClient(socketPath)
	diagnostics, err := client.Diagnostics(diagCtx)
	if err != nil {
		b.WriteString("\nsandboxd diagnostics:\n")
		b.WriteString(err.Error())
		appendBundleFiles(&b, bundlePath)
		return b.String()
	}
	data, err := json.MarshalIndent(diagnostics, "", "  ")
	if err != nil {
		b.WriteString("\nsandboxd diagnostics marshal error:\n")
		b.WriteString(err.Error())
		appendBundleFiles(&b, bundlePath)
		return b.String()
	}
	b.WriteString("\nsandboxd diagnostics:\n")
	b.Write(data)
	appendBundleFiles(&b, bundlePath)
	return b.String()
}

func appendSandboxdSocketInfo(b *strings.Builder, socketPath string) {
	info, err := os.Stat(socketPath)
	b.WriteString("\nsandboxd socket:\n")
	if err != nil {
		b.WriteString(fmt.Sprintf("%s stat error: %v", socketPath, err))
		return
	}
	b.WriteString(fmt.Sprintf("%s mode=%s size=%d", socketPath, info.Mode(), info.Size()))
}

func appendBundleFiles(b *strings.Builder, bundlePath string) {
	for _, name := range []string{"config.json", "axern/sandboxd/entrypoint.json"} {
		path := filepath.Join(bundlePath, name)
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		b.WriteString("\n")
		b.WriteString(name)
		b.WriteString(":\n")
		b.Write(data)
	}
}

func hasMountPath(paths []wire.Path, want string) bool {
	for _, path := range paths {
		if path.Path == want && path.Exists {
			return true
		}
	}
	return false
}
