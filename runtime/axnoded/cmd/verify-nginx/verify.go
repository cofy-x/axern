package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

func runVerifyNginx(cfg verifyNginxConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	clients, err := verifyutil.DialNodeClients(cfg.address)
	if err != nil {
		return fmt.Errorf("dial axnoded: %w", err)
	}
	defer clients.Close()

	configDir, err := os.MkdirTemp("", "axnoded-nginx-config-*")
	if err != nil {
		return fmt.Errorf("create nginx config dir: %w", err)
	}
	if err := writeNginxConfig(configDir); err != nil {
		return fmt.Errorf("prepare nginx config: %w", err)
	}
	defer os.RemoveAll(configDir)

	resolvedSpec := nginxExecutionConfig(cfg, configDir)

	startupBefore, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", config.RuntimeNameRunsc, "local")
	if err != nil {
		return fmt.Errorf("capture startup metrics before nginx start: %w", err)
	}
	handle, err := verifyutil.CreateAllocation(ctx, clients, verifyutil.NewSandboxID(cfg.environmentID), resolvedSpec)
	if err != nil {
		return fmt.Errorf("create nginx sandbox: %w", err)
	}
	startupAfter, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", config.RuntimeNameRunsc, "local")
	if err != nil {
		return fmt.Errorf("capture startup metrics after nginx start: %w", err)
	}
	startupSummary := natbench.DiffStartupSummary(startupBefore, startupAfter)
	localitySummary, err := natbench.CaptureLocalitySummary(
		"http://127.0.0.1:23001/inventoryz",
		natbench.LocalRootfsKey(cfg.rootfs),
	)
	if err != nil {
		return fmt.Errorf("capture locality summary after nginx start: %w", err)
	}
	containerID := handle.SandboxID
	fmt.Printf("nginx_container_id=%s\n", containerID)

	defer func() {
		if err := handle.Delete(context.Background(), 0); err != nil {
			fmt.Fprintf(os.Stderr, "delete nginx sandbox: %v\n", err)
		}
	}()

	var status bpfnet.Status
	switch cfg.natBackend {
	case config.NatBackendIptables:
		if err := verifyutil.AssertIptablesRule("nat", "PREROUTING", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
			return err
		}
		if err := verifyutil.AssertIptablesRule("nat", "OUTPUT", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
			return err
		}
	case config.NatBackendEBPF:
		status, err = verifyutil.LoadBPFNetStatus(cfg.bpfnetPin)
		if err != nil {
			return fmt.Errorf("load bpfnet status: %w", err)
		}
		service, err := verifyutil.FindBPFNetService(status, "tcp", uint16(cfg.listenPort))
		if err != nil {
			return err
		}
		if !isExpectedTCPBPFNetMode(status.State.Mode) {
			return fmt.Errorf("unexpected bpfnet mode %q: %#v", status.State.Mode, status.State)
		}
		if err := bpfnetstatus.RequireTCReady(status); err != nil {
			return err
		}
		if err := verifyutil.AssertIptablesRuleAbsent("nat", "PREROUTING", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
			return err
		}
		if err := bpfnetstatus.RequireLocalhostTCPReady(status); err != nil {
			return err
		}
		if err := verifyutil.AssertIptablesRuleAbsent("nat", "OUTPUT", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
			return err
		}
		if err := assertIptablesRuleAbsentAll("nat", "POSTROUTING",
			"-s 127.0.0.1/32",
			fmt.Sprintf("-d %s/32", service.TargetIP),
			fmt.Sprintf("--dport %d", service.TargetPort),
			"-j MASQUERADE",
		); err != nil {
			return err
		}
		if err := verifyutil.AssertTCFiltersAttached(status.Attachment.UplinkDevices); err != nil {
			return err
		}
		if err := assertPinnedLocalhostLinks(cfg.bpfnetPin); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported nat backend %q", cfg.natBackend)
	}

	if !cfg.skipLocalhost {
		if err := assertReachable(cfg.listenPort); err != nil {
			return err
		}
		if cfg.natBackend == config.NatBackendEBPF {
			if err := assertGetpeernameAlias(cfg.listenPort); err != nil {
				return err
			}
			if err := assertLocalhostStats(mustLoadBPFNetStatus(cfg.bpfnetPin)); err != nil {
				return err
			}
		}
	}
	if cfg.natBackend == config.NatBackendEBPF && cfg.externalNetNS != "" && cfg.externalAddr != "" {
		if err := assertReachableFromNamespace(cfg.externalNetNS, cfg.externalAddr, cfg.listenPort); err != nil {
			return err
		}
	}

	if cfg.requests > 0 {
		if cfg.externalNetNS == "" || cfg.externalAddr == "" {
			return fmt.Errorf("benchmark requires -external-probe-netns and -external-probe-address")
		}
		target := fmt.Sprintf("http://%s:%d/", cfg.externalAddr, cfg.listenPort)
		report, err := runExternalTCPIngressBenchmark(cfg.natBackend, config.RuntimeNameRunsc, cfg.bpfnetPin, cfg.externalNetNS, target, cfg.requests, cfg.concurrency, cfg.warmupRequests, cfg.timeout, startupSummary, localitySummary)
		if err != nil {
			return fmt.Errorf("benchmark external tcp ingress: %w", err)
		}
		if cfg.benchmarkOut != "" {
			if err := natbench.WriteReport(cfg.benchmarkOut, report); err != nil {
				return fmt.Errorf("write nginx benchmark report: %w", err)
			}
		}
	}

	statusResp, err := verifyutil.GetAllocationLifecycle(ctx, clients, containerID)
	if err != nil {
		return fmt.Errorf("get nginx sandbox status: %w", err)
	}
	if statusResp.GetState().String() == "" {
		return fmt.Errorf("nginx sandbox %s returned empty status", containerID)
	}

	fmt.Println("nginx_smoke_ok=true")
	if cfg.holdOpen {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
	}
	return nil
}

func writeNginxConfig(configDir string) error {
	return os.WriteFile(filepath.Join(configDir, "nginx.conf"), []byte(nginxConfig), 0644)
}

func nginxExecutionConfig(cfg verifyNginxConfig, configDir string) *privatenodev1.ResolvedExecutionConfig {
	return &privatenodev1.ResolvedExecutionConfig{
		Argv: []string{"/usr/sbin/nginx", "-c", "/axnoded-conf/nginx.conf", "-g", "daemon off;"},
		Cwd:  "/",
		Env:  map[string]string{"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		Mounts: []*privatenodev1.SandboxMount{
			{Type: "bind", Source: configDir, Target: "/axnoded-conf", Options: []string{"rbind", "ro"}},
			{Type: "tmpfs", Source: "tmpfs", Target: "/tmp", Options: []string{"nosuid", "nodev", "mode=1777"}},
			{Type: "tmpfs", Source: "tmpfs", Target: "/var/run", Options: []string{"nosuid", "nodev", "mode=0755"}},
			{Type: "tmpfs", Source: "tmpfs", Target: "/var/cache/nginx", Options: []string{"nosuid", "nodev", "mode=0755"}},
		},
		LocalityKey: cfg.rootfs, LocalRootfsPath: cfg.rootfs,
		Ports:      []*commonv1.PortSpec{{Protocol: commonv1.PortProtocol_PORT_PROTOCOL_TCP, HostPort: int32(cfg.listenPort), ContainerPort: 80}},
		StdoutPath: cfg.stdoutPath, StderrPath: cfg.stderrPath,
	}
}

const nginxConfig = `user root;
master_process off;
worker_processes 1;
error_log /dev/stderr info;
pid /var/run/nginx.pid;
events { worker_connections 1024; }
http {
  access_log /dev/stdout;
  client_body_temp_path /var/cache/nginx/client_temp;
  proxy_temp_path /var/cache/nginx/proxy_temp;
  fastcgi_temp_path /var/cache/nginx/fastcgi_temp;
  uwsgi_temp_path /var/cache/nginx/uwsgi_temp;
  scgi_temp_path /var/cache/nginx/scgi_temp;
  server { listen 80; server_name _; location / { root /usr/share/nginx/html; index index.html; } }
}
`

func isExpectedTCPBPFNetMode(mode string) bool {
	switch mode {
	case bpfnet.ModeIngressTCPUDPDNATEgressSNATLocalhostTCP:
		return true
	default:
		return false
	}
}

func mustLoadBPFNetStatus(pinPath string) bpfnet.Status {
	status, err := verifyutil.LoadBPFNetStatus(pinPath)
	if err != nil {
		fatalf("load bpfnet status: %v", err)
	}
	return status
}
