package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

func runResponder(listenAddress string) error {
	if listenAddress == "" {
		return fmt.Errorf("listen address is required")
	}
	addr, err := net.ResolveUDPAddr("udp4", listenAddress)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	fmt.Println("responder_ready=true")
	buf := make([]byte, 2048)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		payload := strings.TrimSpace(string(buf[:n]))
		if payload == "" {
			payload = "axern-udp-ingress"
		}
		if _, err := conn.WriteToUDP([]byte(payload), remote); err != nil {
			return err
		}
	}
}

func runProbe(targetAddress string, timeout time.Duration) error {
	if targetAddress == "" {
		return fmt.Errorf("target address is required")
	}
	remote, err := net.ResolveUDPAddr("udp4", targetAddress)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp4", nil, remote)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	payload := []byte("axern-udp-ingress")
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if string(buf[:n]) != string(payload) {
		return fmt.Errorf("unexpected udp responder payload %q", string(buf[:n]))
	}
	fmt.Println("udp_probe_ok=true")
	return nil
}

func runBenchmarkProbe(targetAddress string, requests, concurrency, warmupRequests int, timeout time.Duration) (natbench.WorkloadSummary, error) {
	if targetAddress == "" {
		return natbench.WorkloadSummary{}, fmt.Errorf("target address is required")
	}
	return natbench.RunWorkloadWarmup(requests, concurrency, warmupRequests, func() error {
		remote, err := net.ResolveUDPAddr("udp4", targetAddress)
		if err != nil {
			return err
		}
		conn, err := net.DialUDP("udp4", nil, remote)
		if err != nil {
			return err
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
			return err
		}
		payload := []byte("axern-udp-ingress")
		if _, err := conn.Write(payload); err != nil {
			return err
		}
		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}
		if string(buf[:n]) != string(payload) {
			return fmt.Errorf("unexpected udp responder payload %q", string(buf[:n]))
		}
		return nil
	}), nil
}

func runBenchmarkProbeFromNamespace(namespace, targetAddress string, requests, concurrency, warmupRequests int, timeout time.Duration) (natbench.WorkloadSummary, error) {
	self, err := os.Executable()
	if err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("resolve verify-udp binary: %w", err)
	}
	cmd := exec.Command(
		"ip", "netns", "exec", namespace,
		self,
		"-mode", "benchmark-probe",
		"-target-address", targetAddress,
		"-benchmark-requests", fmt.Sprintf("%d", requests),
		"-benchmark-concurrency", fmt.Sprintf("%d", concurrency),
		"-benchmark-warmup-requests", fmt.Sprintf("%d", warmupRequests),
		"-timeout", timeout.String(),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	var summary natbench.WorkloadSummary
	if err := json.Unmarshal(output, &summary); err != nil {
		return natbench.WorkloadSummary{}, fmt.Errorf("decode udp benchmark summary: %w", err)
	}
	return summary, nil
}

func assertReachableFromNamespace(namespace, host string, listenPort int, timeout time.Duration) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve verify-udp binary: %w", err)
	}

	target := net.JoinHostPort(host, fmt.Sprintf("%d", listenPort))
	var lastErr error
	for attempt := 0; attempt < 15; attempt++ {
		cmd := exec.Command("ip", "netns", "exec", namespace, self, "-mode", "probe", "-target-address", target, "-timeout", timeout.String())
		if output, err := cmd.CombinedOutput(); err == nil {
			if strings.Contains(string(output), "udp_probe_ok=true") {
				return nil
			}
			lastErr = fmt.Errorf("missing udp probe success marker: %s", strings.TrimSpace(string(output)))
		} else {
			lastErr = fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("external udp probe %s from netns %s failed after retries: %v", target, namespace, lastErr)
}
