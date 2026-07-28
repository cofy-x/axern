package nginx

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

const (
	DefaultRootfsPath = "/opt/nginx-rootfs"
	DefaultTargetPort = 80
)

type InstanceSpec struct {
	RuntimeName string
	SandboxID   string
	RootfsPath  string
	ConfigDir   string
	StdoutPath  string
	StderrPath  string
	HostPort    int
}

func ManagedSpec(runtimeName string) (InstanceSpec, bool) {
	baseDir := filepath.Join(os.TempDir(), "axnoded-dashboard-nginx", runtimeName)
	switch runtimeName {
	case config.RuntimeNameRunsc:
		return InstanceSpec{
			RuntimeName: runtimeName,
			SandboxID:   "dashboard-nginx-runsc",
			RootfsPath:  DefaultRootfsPath,
			ConfigDir:   baseDir,
			StdoutPath:  filepath.Join(os.TempDir(), "axnoded-dashboard-nginx-runsc.stdout"),
			StderrPath:  filepath.Join(os.TempDir(), "axnoded-dashboard-nginx-runsc.stderr"),
			HostPort:    18080,
		}, true
	case config.RuntimeNameRunc:
		return InstanceSpec{
			RuntimeName: runtimeName,
			SandboxID:   "dashboard-nginx-runc",
			RootfsPath:  DefaultRootfsPath,
			ConfigDir:   baseDir,
			StdoutPath:  filepath.Join(os.TempDir(), "axnoded-dashboard-nginx-runc.stdout"),
			StderrPath:  filepath.Join(os.TempDir(), "axnoded-dashboard-nginx-runc.stderr"),
			HostPort:    18081,
		}, true
	default:
		return InstanceSpec{}, false
	}
}

func BrowserURL(hostPort int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", hostPort)
}

func WriteConfig(configDir string) (string, error) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}
	configPath := filepath.Join(configDir, "nginx.conf")
	if err := os.WriteFile(configPath, []byte(nginxConfig), 0644); err != nil {
		return "", err
	}
	return configPath, nil
}

func BuildResolvedExecutionConfig(spec InstanceSpec) *privatenodev1.ResolvedExecutionConfig {
	return &privatenodev1.ResolvedExecutionConfig{
		RuntimeClass: spec.RuntimeName,
		Argv:         []string{"/usr/sbin/nginx", "-c", "/axnoded-conf/nginx.conf", "-g", "daemon off;"},
		Cwd:          "/",
		Env: map[string]string{
			"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Mounts: []*privatenodev1.SandboxMount{
			{
				Type:    "bind",
				Source:  spec.ConfigDir,
				Target:  "/axnoded-conf",
				Options: []string{"rbind", "ro"},
			},
			{
				Type:    "tmpfs",
				Source:  "tmpfs",
				Target:  "/tmp",
				Options: []string{"nosuid", "nodev", "mode=1777"},
			},
			{
				Type:    "tmpfs",
				Source:  "tmpfs",
				Target:  "/var/run",
				Options: []string{"nosuid", "nodev", "mode=0755"},
			},
			{
				Type:    "tmpfs",
				Source:  "tmpfs",
				Target:  "/var/cache/nginx",
				Options: []string{"nosuid", "nodev", "mode=0755"},
			},
		},
		LocalityKey:     spec.RootfsPath,
		LocalRootfsPath: spec.RootfsPath,
		Ports: []*commonv1.PortSpec{{
			Protocol:      commonv1.PortProtocol_PORT_PROTOCOL_TCP,
			HostPort:      int32(spec.HostPort),
			ContainerPort: int32(DefaultTargetPort),
		}},
		StdoutPath: spec.StdoutPath,
		StderrPath: spec.StderrPath,
	}
}

func PortMapping(hostPort, targetPort int) string {
	return fmt.Sprintf("tcp:%d:%d", hostPort, targetPort)
}

const nginxConfig = `user root;
master_process off;
worker_processes 1;
error_log /dev/stderr info;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
}

http {
    access_log /dev/stdout;
    client_body_temp_path /var/cache/nginx/client_temp;
    proxy_temp_path /var/cache/nginx/proxy_temp;
    fastcgi_temp_path /var/cache/nginx/fastcgi_temp;
    uwsgi_temp_path /var/cache/nginx/uwsgi_temp;
    scgi_temp_path /var/cache/nginx/scgi_temp;

    server {
        listen 80;
        server_name _;

        location / {
            root /usr/share/nginx/html;
            index index.html;
        }
    }
}
`
