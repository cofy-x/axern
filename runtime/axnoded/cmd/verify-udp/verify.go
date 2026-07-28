package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

func runVerifyUDP(cfg verifyUDPConfig) error {
	if cfg.externalNetNS == "" || cfg.externalAddress == "" {
		return fmt.Errorf("external probe netns and address are required")
	}

	clients, err := verifyutil.DialNodeClients(cfg.address)
	if err != nil {
		return fmt.Errorf("dial axnoded: %w", err)
	}
	defer clients.Close()

	listenAddress := net.JoinHostPort("0.0.0.0", fmt.Sprintf("%d", cfg.targetPort))
	resolvedSpec := &privatenodev1.ResolvedExecutionConfig{
		RuntimeClass: cfg.runtimeName,
		Argv:         []string{"/axnoded-bin/verify-udp", "-mode", "responder", "-listen-address", listenAddress},
		Cwd:          "/",
		Mounts: []*privatenodev1.SandboxMount{
			{
				Type:    "bind",
				Source:  cfg.helperDir,
				Target:  "/axnoded-bin",
				Options: []string{"rbind", "ro"},
			},
			{
				Type:    "tmpfs",
				Source:  "tmpfs",
				Target:  "/tmp",
				Options: []string{"nosuid", "nodev", "mode=1777"},
			},
		},
		LocalRootfsPath: cfg.rootfs,
		StdoutPath:      cfg.stdoutPath,
		StderrPath:      cfg.stderrPath,
		Ports: []*commonv1.PortSpec{{
			Protocol:      commonv1.PortProtocol_PORT_PROTOCOL_UDP,
			HostPort:      int32(cfg.listenPort),
			ContainerPort: int32(cfg.targetPort),
		}},
	}

	startupBefore, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", cfg.runtimeName, "local")
	if err != nil {
		return fmt.Errorf("capture startup metrics before udp start: %w", err)
	}
	startCtx, cancelStart := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelStart()
	handle, err := verifyutil.CreateAllocation(startCtx, clients, verifyutil.NewSandboxID(cfg.runtimeID), resolvedSpec)
	if err != nil {
		return fmt.Errorf("create udp responder sandbox: %w", err)
	}
	startupAfter, err := natbench.CaptureStartupSnapshot("http://127.0.0.1:23001/debug/metricsz", cfg.runtimeName, "local")
	if err != nil {
		return fmt.Errorf("capture startup metrics after udp start: %w", err)
	}
	startupSummary := natbench.DiffStartupSummary(startupBefore, startupAfter)
	localitySummary, err := natbench.CaptureLocalitySummary(
		"http://127.0.0.1:23001/inventoryz",
		natbench.LocalRootfsKey(cfg.rootfs),
	)
	if err != nil {
		return fmt.Errorf("capture locality summary after udp start: %w", err)
	}
	containerID := handle.SandboxID
	fmt.Printf("udp_container_id=%s\n", containerID)

	defer func() {
		deleteCtx, cancelDelete := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelDelete()
		_ = handle.Delete(deleteCtx, 0)
	}()

	switch cfg.natBackend {
	case config.NatBackendIptables:
		if err := verifyutil.AssertIptablesRule("nat", "PREROUTING", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
			return err
		}
		if err := verifyutil.AssertIptablesRule("filter", "FORWARD", fmt.Sprintf("--dport %d -j ACCEPT", cfg.targetPort)); err != nil {
			return err
		}
	case config.NatBackendEBPF:
		status, err := verifyutil.LoadBPFNetStatus(cfg.bpfnetPin)
		if err != nil {
			return err
		}
		if _, err := verifyutil.FindBPFNetService(status, "udp", uint16(cfg.listenPort)); err != nil {
			return err
		}
		if !isExpectedEBPFMode(status.State.Mode) {
			return fmt.Errorf("unexpected bpfnet mode %q", status.State.Mode)
		}
		if err := bpfnetstatus.RequireIngressUDPReady(status); err != nil {
			return err
		}
		if err := verifyutil.AssertIptablesRuleAbsent("nat", "PREROUTING", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
			return err
		}
		if err := verifyutil.AssertIptablesRuleAbsent("filter", "FORWARD", fmt.Sprintf("--dport %d -j ACCEPT", cfg.targetPort)); err != nil {
			return err
		}
		if err := verifyutil.AssertIptablesRuleAbsent("nat", "OUTPUT", fmt.Sprintf("--dport %d -j DNAT", cfg.listenPort)); err != nil {
			return err
		}
		if err := verifyutil.AssertTCFiltersAttached(status.Attachment.UplinkDevices); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported nat backend %q", cfg.natBackend)
	}

	if err := assertReachableFromNamespace(cfg.externalNetNS, cfg.externalAddress, cfg.listenPort, cfg.timeout); err != nil {
		return err
	}
	if cfg.requests > 0 {
		report, err := runExternalUDPIngressBenchmark(cfg.natBackend, cfg.runtimeName, cfg.bpfnetPin, cfg.externalNetNS, cfg.externalAddress, cfg.listenPort, cfg.requests, cfg.concurrency, cfg.warmupRequests, cfg.timeout, startupSummary, localitySummary)
		if err != nil {
			return err
		}
		if cfg.benchmarkOut != "" {
			if err := natbench.WriteReport(cfg.benchmarkOut, report); err != nil {
				return fmt.Errorf("write udp benchmark report: %w", err)
			}
		}
	}

	statusResp, err := verifyutil.GetAllocationStatus(context.Background(), clients, containerID)
	if err != nil {
		return fmt.Errorf("get udp sandbox status: %w", err)
	}
	if statusResp.GetStatus().String() != "" {
		fmt.Println("udp_ingress_smoke_ok=true")
		return nil
	}
	return fmt.Errorf("udp sandbox %s returned empty status", containerID)
}

func isExpectedEBPFMode(mode string) bool {
	switch mode {
	case bpfnet.ModeIngressTCPUDPDNATEgressSNAT,
		bpfnet.ModeIngressTCPUDPDNATEgressSNATLocalhostTCP,
		bpfnet.ModeIngressTCPUDPDNATEgressSNATLocalCompat:
		return true
	default:
		return false
	}
}
