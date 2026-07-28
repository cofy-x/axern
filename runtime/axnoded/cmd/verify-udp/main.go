package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

type verifyUDPConfig struct {
	mode            string
	listenAddress   string
	targetAddress   string
	address         string
	rootfs          string
	runtimeName     string
	runtimeID       string
	stdoutPath      string
	stderrPath      string
	listenPort      int
	targetPort      int
	natBackend      string
	bpfnetPin       string
	externalNetNS   string
	externalAddress string
	helperDir       string
	timeout         time.Duration
	requests        int
	concurrency     int
	warmupRequests  int
	benchmarkOut    string
}

func main() {
	cfg := parseFlags()
	switch cfg.mode {
	case "verify":
		if err := runVerifyUDP(cfg); err != nil {
			fatalf("%v", err)
		}
	case "responder":
		if err := runResponder(cfg.listenAddress); err != nil {
			fatalf("udp responder: %v", err)
		}
	case "probe":
		if err := runProbe(cfg.targetAddress, cfg.timeout); err != nil {
			fatalf("udp probe: %v", err)
		}
	case "benchmark-probe":
		summary, err := runBenchmarkProbe(cfg.targetAddress, cfg.requests, cfg.concurrency, cfg.warmupRequests, cfg.timeout)
		if err != nil {
			fatalf("udp benchmark probe: %v", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
			fatalf("encode udp benchmark summary: %v", err)
		}
	default:
		fatalf("unsupported mode %q", cfg.mode)
	}
}

func parseFlags() verifyUDPConfig {
	cfg := verifyUDPConfig{}
	flag.StringVar(&cfg.mode, "mode", "verify", "verify, responder, probe, or benchmark-probe")
	flag.StringVar(&cfg.listenAddress, "listen-address", "", "listen address for responder mode")
	flag.StringVar(&cfg.targetAddress, "target-address", "", "target address for probe mode")
	flag.StringVar(&cfg.address, "address", config.DefaultSocketAddress, "axnoded unix socket path")
	flag.StringVar(&cfg.rootfs, "rootfs", "/opt/sample-rootfs", "LOCAL sample rootfs path")
	flag.StringVar(&cfg.runtimeName, "runtime", config.RuntimeNameRunsc, "sandbox runtime name under test")
	flag.StringVar(&cfg.runtimeID, "runtime-id", "verify-udp-runtime", "function runtime id")
	flag.StringVar(&cfg.stdoutPath, "stdout", "/tmp/axnoded-udp.stdout", "container stdout path")
	flag.StringVar(&cfg.stderrPath, "stderr", "/tmp/axnoded-udp.stderr", "container stderr path")
	flag.IntVar(&cfg.listenPort, "listen-port", 15353, "host UDP port exposed via DNAT")
	flag.IntVar(&cfg.targetPort, "target-port", 1053, "sandbox UDP port")
	flag.StringVar(&cfg.natBackend, "nat-backend", config.NatBackendIptables, "nat backend under test")
	flag.StringVar(&cfg.bpfnetPin, "bpfnet-pin-path", config.DefaultBPFNetPinPath, "bpfnet pin root")
	flag.StringVar(&cfg.externalNetNS, "external-probe-netns", "", "network namespace used to prove external ingress reachability")
	flag.StringVar(&cfg.externalAddress, "external-probe-address", "", "host IP to probe from the external namespace")
	flag.StringVar(&cfg.helperDir, "helper-dir", "/usr/local/libexec/axnoded", "directory bind-mounted into the sandbox with helper binaries")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-probe timeout")
	flag.IntVar(&cfg.requests, "benchmark-requests", 0, "number of benchmark requests to issue")
	flag.IntVar(&cfg.concurrency, "benchmark-concurrency", 16, "number of concurrent benchmark workers")
	flag.IntVar(&cfg.warmupRequests, "benchmark-warmup-requests", 0, "number of warmup requests to issue before benchmarking")
	flag.StringVar(&cfg.benchmarkOut, "benchmark-output", "", "optional JSON output path for benchmark report")
	flag.Parse()
	return cfg
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
