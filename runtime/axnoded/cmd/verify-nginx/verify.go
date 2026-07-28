package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	demonginx "github.com/cofy-x/axern/runtime/axnoded/internal/demo/nginx"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
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
	if _, err := demonginx.WriteConfig(configDir); err != nil {
		return fmt.Errorf("prepare nginx config: %w", err)
	}
	defer os.RemoveAll(configDir)

	spec := demonginx.InstanceSpec{
		RuntimeName: cfg.runtimeName,
		SandboxID:   cfg.runtimeID,
		RootfsPath:  cfg.rootfs,
		ConfigDir:   configDir,
		StdoutPath:  cfg.stdoutPath,
		StderrPath:  cfg.stderrPath,
		HostPort:    cfg.listenPort,
	}
	resolvedSpec := demonginx.BuildResolvedExecutionConfig(spec)

	startupBefore, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", cfg.runtimeName, "local")
	if err != nil {
		return fmt.Errorf("capture startup metrics before nginx start: %w", err)
	}
	handle, err := verifyutil.CreateAllocation(ctx, clients, verifyutil.NewSandboxID(cfg.runtimeID), resolvedSpec)
	if err != nil {
		return fmt.Errorf("create nginx sandbox: %w", err)
	}
	startupAfter, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", cfg.runtimeName, "local")
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
		if status.State.LocalhostCompat {
			if err := verifyutil.AssertIptablesRule("nat", "OUTPUT", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
				return err
			}
		} else {
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
		}
		if err := verifyutil.AssertTCFiltersAttached(status.Attachment.UplinkDevices); err != nil {
			return err
		}
		if !status.State.LocalhostCompat {
			if err := assertPinnedLocalhostLinks(cfg.bpfnetPin); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported nat backend %q", cfg.natBackend)
	}

	if !cfg.skipLocalhost {
		if err := assertReachable(cfg.listenPort); err != nil {
			return err
		}
		if cfg.natBackend == config.NatBackendEBPF && !status.State.LocalhostCompat {
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
		report, err := runExternalTCPIngressBenchmark(cfg.natBackend, cfg.runtimeName, cfg.bpfnetPin, cfg.externalNetNS, target, cfg.requests, cfg.concurrency, cfg.warmupRequests, cfg.timeout, startupSummary, localitySummary)
		if err != nil {
			return fmt.Errorf("benchmark external tcp ingress: %w", err)
		}
		if cfg.benchmarkOut != "" {
			if err := natbench.WriteReport(cfg.benchmarkOut, report); err != nil {
				return fmt.Errorf("write nginx benchmark report: %w", err)
			}
		}
	}

	statusResp, err := verifyutil.GetAllocationStatus(ctx, clients, containerID)
	if err != nil {
		return fmt.Errorf("get nginx sandbox status: %w", err)
	}
	if statusResp.GetStatus().String() == "" {
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

func isExpectedTCPBPFNetMode(mode string) bool {
	switch mode {
	case bpfnet.ModeIngressTCPUDPDNATEgressSNATLocalhostTCP,
		bpfnet.ModeIngressTCPUDPDNATEgressSNATLocalCompat:
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
