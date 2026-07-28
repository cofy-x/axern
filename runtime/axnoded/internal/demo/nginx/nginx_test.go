package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestManagedSpec(t *testing.T) {
	runsc, ok := ManagedSpec(config.RuntimeNameRunsc)
	if !ok {
		t.Fatalf("expected managed spec for runsc")
	}
	if runsc.SandboxID != "dashboard-nginx-runsc" {
		t.Fatalf("sandbox id = %q, want %q", runsc.SandboxID, "dashboard-nginx-runsc")
	}
	if runsc.HostPort != 18080 {
		t.Fatalf("host port = %d, want %d", runsc.HostPort, 18080)
	}

	runc, ok := ManagedSpec(config.RuntimeNameRunc)
	if !ok {
		t.Fatalf("expected managed spec for runc")
	}
	if runc.SandboxID != "dashboard-nginx-runc" {
		t.Fatalf("sandbox id = %q, want %q", runc.SandboxID, "dashboard-nginx-runc")
	}
	if runc.HostPort != 18081 {
		t.Fatalf("host port = %d, want %d", runc.HostPort, 18081)
	}
}

func TestBuildResolvedExecutionConfig(t *testing.T) {
	spec := InstanceSpec{
		RuntimeName: config.RuntimeNameRunsc,
		SandboxID:   "dashboard-nginx-runsc",
		RootfsPath:  "/opt/nginx-rootfs",
		ConfigDir:   "/tmp/demo-nginx/runsc",
		StdoutPath:  "/tmp/runsc.stdout",
		StderrPath:  "/tmp/runsc.stderr",
		HostPort:    18080,
	}

	resolved := BuildResolvedExecutionConfig(spec)
	if resolved.GetStdoutPath() != spec.StdoutPath {
		t.Fatalf("stdout = %q, want %q", resolved.GetStdoutPath(), spec.StdoutPath)
	}
	if resolved.GetStderrPath() != spec.StderrPath {
		t.Fatalf("stderr = %q, want %q", resolved.GetStderrPath(), spec.StderrPath)
	}
	if len(resolved.GetPorts()) != 1 || resolved.GetPorts()[0].GetHostPort() != 18080 || resolved.GetPorts()[0].GetContainerPort() != 80 {
		t.Fatalf("ports = %v, want [tcp:18080:80]", resolved.GetPorts())
	}
	if resolved.GetRuntimeClass() != config.RuntimeNameRunsc {
		t.Fatalf("runtime class = %q, want %q", resolved.GetRuntimeClass(), config.RuntimeNameRunsc)
	}
	if resolved.GetLocalRootfsPath() != spec.RootfsPath {
		t.Fatalf("rootfs path = %q, want %q", resolved.GetLocalRootfsPath(), spec.RootfsPath)
	}
	if got := resolved.GetArgv(); len(got) != 5 || got[0] != "/usr/sbin/nginx" || got[4] != "daemon off;" {
		t.Fatalf("command = %v", got)
	}
	if got := resolved.GetEnv()["PATH"]; got == "" {
		t.Fatalf("expected PATH to be set")
	}
	if len(resolved.GetMounts()) != 4 {
		t.Fatalf("mount count = %d, want 4", len(resolved.GetMounts()))
	}
	if resolved.GetMounts()[0].GetSource() != spec.ConfigDir {
		t.Fatalf("config mount source = %q, want %q", resolved.GetMounts()[0].GetSource(), spec.ConfigDir)
	}
}

func TestWriteConfig(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "nginx")
	configPath, err := WriteConfig(configDir)
	if err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}
	if filepath.Base(configPath) != "nginx.conf" {
		t.Fatalf("config path = %q", configPath)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "listen 80;") {
		t.Fatalf("config missing listen directive: %s", content)
	}
	if !strings.Contains(content, "root /usr/share/nginx/html;") {
		t.Fatalf("config missing document root: %s", content)
	}
}
