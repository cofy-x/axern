package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

func runVerifyEgress(cfg verifyEgressConfig) error {
	if cfg.externalNetNS == "" || cfg.externalAddress == "" || cfg.expectedSourceIP == "" {
		return fmt.Errorf("external probe netns, address, and expected source ip are required")
	}

	tcpAddress := net.JoinHostPort(cfg.externalAddress, fmt.Sprintf("%d", cfg.tcpPort))
	udpAddress := net.JoinHostPort(cfg.externalAddress, fmt.Sprintf("%d", cfg.udpPort))

	tcpResponder, err := startResponder(cfg.externalNetNS, "tcp-responder", tcpAddress)
	if err != nil {
		return err
	}
	defer tcpResponder.stop()

	udpResponder, err := startResponder(cfg.externalNetNS, "udp-responder", udpAddress)
	if err != nil {
		return err
	}
	defer udpResponder.stop()

	icmpResponder, err := startResponder(cfg.externalNetNS, "icmp-responder", cfg.externalAddress)
	if err != nil {
		return err
	}
	defer icmpResponder.stop()

	clients, err := verifyutil.DialNodeClients(cfg.address)
	if err != nil {
		return fmt.Errorf("dial axnoded: %w", err)
	}
	defer clients.Close()

	benchmarkRequested := cfg.requests > 0
	baseSpec := &privatenodev1.ResolvedExecutionConfig{
		RuntimeClass:    cfg.runtimeName,
		Cwd:             "/",
		LocalRootfsPath: cfg.rootfs,
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
	}

	if benchmarkRequested {
		report, err := runBenchmarkReport(
			clients,
			baseSpec,
			cfg.runtimeID,
			cfg.stdoutPath,
			cfg.stderrPath,
			cfg.runtimeName,
			cfg.natBackend,
			cfg.bpfnetPinPath,
			cfg.rootfs,
			tcpAddress,
			udpAddress,
			cfg.externalAddress,
			cfg.expectedSourceIP,
			cfg.timeout,
			cfg.requests,
			cfg.concurrency,
			cfg.warmupRequests,
			cfg.benchmarkClientCount,
			cfg.snatPostGCWait,
			cfg.benchmarkTransports,
		)
		if err != nil {
			return err
		}
		if cfg.benchmarkOut != "" {
			if err := natbench.WriteReport(cfg.benchmarkOut, report); err != nil {
				return fmt.Errorf("write egress benchmark report: %w", err)
			}
		}
	} else {
		stdoutData, stderrData, err := runProbeContainer(
			clients,
			baseSpec,
			cfg.runtimeID,
			cfg.stdoutPath,
			cfg.stderrPath,
			buildProbeCommand("verify", "", "", tcpAddress, udpAddress, cfg.externalAddress, cfg.expectedSourceIP, cfg.timeout, 0, 0, 0, 0),
		)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(stderrData))) > 0 {
			fmt.Printf("egress_probe_stderr=%s\n", strings.TrimSpace(string(stderrData)))
		}
		if !strings.Contains(string(stdoutData), "egress_probe_ok=true") {
			return fmt.Errorf("egress probe stdout missing success marker: %s", strings.TrimSpace(string(stdoutData)))
		}
		if cfg.natBackend == config.NatBackendEBPF {
			if err := assertSNATStats(cfg.bpfnetPinPath); err != nil {
				return err
			}
		}
	}

	fmt.Println("egress_smoke_ok=true")
	return nil
}

func assertSNATStats(pinPath string) error {
	status, err := bpfnetstatus.Load(pinPath)
	if err != nil {
		return fmt.Errorf("load bpfnet status: %w", err)
	}
	if err := bpfnetstatus.RequireTCReady(status); err != nil {
		return err
	}
	if status.Kernel.SNATHits == 0 {
		return fmt.Errorf("expected non-zero snat hits in stats map")
	}
	if status.Kernel.SNATRevHits == 0 {
		return fmt.Errorf("expected non-zero snat reverse hits in stats map")
	}

	fmt.Printf("bpfnet_snat_hits=%d\n", status.Kernel.SNATHits)
	fmt.Printf("bpfnet_snat_rev_hits=%d\n", status.Kernel.SNATRevHits)
	return nil
}
