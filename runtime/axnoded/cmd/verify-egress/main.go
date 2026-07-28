package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

const (
	startTimeout  = 90 * time.Second
	waitTimeout   = 90 * time.Second
	deleteTimeout = 15 * time.Second
)

type verifyEgressConfig struct {
	mode                 string
	listenAddress        string
	address              string
	rootfs               string
	runtimeName          string
	runtimeID            string
	stdoutPath           string
	stderrPath           string
	natBackend           string
	bpfnetPinPath        string
	externalNetNS        string
	externalAddress      string
	expectedSourceIP     string
	helperDir            string
	timeout              time.Duration
	tcpPort              int
	udpPort              int
	requests             int
	concurrency          int
	warmupRequests       int
	benchmarkClientCount int
	snatPostGCWait       time.Duration
	benchmarkTransports  string
	benchmarkOut         string
}

func main() {
	cfg := parseFlags()
	switch cfg.mode {
	case "verify":
		if err := runVerifyEgress(cfg); err != nil {
			fatalf("%v", err)
		}
	case "tcp-responder":
		if err := runTCPResponder(cfg.listenAddress); err != nil {
			fatalf("tcp responder: %v", err)
		}
	case "udp-responder":
		if err := runUDPResponder(cfg.listenAddress); err != nil {
			fatalf("udp responder: %v", err)
		}
	case "icmp-responder":
		if err := runICMPResponder(cfg.listenAddress); err != nil {
			fatalf("icmp responder: %v", err)
		}
	default:
		fatalf("unsupported mode %q", cfg.mode)
	}
}

func parseFlags() verifyEgressConfig {
	cfg := verifyEgressConfig{}
	flag.StringVar(&cfg.mode, "mode", "verify", "verify, tcp-responder, udp-responder, or icmp-responder")
	flag.StringVar(&cfg.listenAddress, "listen-address", "", "listen address for responder modes")
	flag.StringVar(&cfg.address, "address", config.DefaultSocketAddress, "axnoded unix socket path")
	flag.StringVar(&cfg.rootfs, "rootfs", "/opt/sample-rootfs", "LOCAL sample rootfs path")
	flag.StringVar(&cfg.runtimeName, "runtime", config.RuntimeNameRunsc, "sandbox runtime name under test")
	flag.StringVar(&cfg.runtimeID, "runtime-id", "verify-egress-runtime", "function runtime id")
	flag.StringVar(&cfg.stdoutPath, "stdout", "/tmp/axnoded-egress.stdout", "container stdout path")
	flag.StringVar(&cfg.stderrPath, "stderr", "/tmp/axnoded-egress.stderr", "container stderr path")
	flag.StringVar(&cfg.natBackend, "nat-backend", config.NatBackendIptables, "nat backend under test")
	flag.StringVar(&cfg.bpfnetPinPath, "bpfnet-pin-path", config.DefaultBPFNetPinPath, "bpfnet pin root")
	flag.StringVar(&cfg.externalNetNS, "external-probe-netns", "", "network namespace hosting the external responder")
	flag.StringVar(&cfg.externalAddress, "external-probe-address", "", "IP address reached by the sandbox for egress validation")
	flag.StringVar(&cfg.expectedSourceIP, "expected-source-ip", "", "source IP the external responder must observe")
	flag.StringVar(&cfg.helperDir, "helper-dir", "/usr/local/libexec/axnoded", "directory bind-mounted into the sandbox with helper binaries")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-probe timeout")
	flag.IntVar(&cfg.tcpPort, "tcp-port", 19080, "TCP responder port")
	flag.IntVar(&cfg.udpPort, "udp-port", 19081, "UDP responder port")
	flag.IntVar(&cfg.requests, "benchmark-requests", 0, "number of benchmark requests to issue")
	flag.IntVar(&cfg.concurrency, "benchmark-concurrency", 16, "number of concurrent benchmark workers")
	flag.IntVar(&cfg.warmupRequests, "benchmark-warmup-requests", 0, "number of warmup requests to issue before benchmarking")
	flag.IntVar(&cfg.benchmarkClientCount, "benchmark-client-count", 1, "number of concurrent egress benchmark client sandboxes")
	flag.DurationVar(&cfg.snatPostGCWait, "benchmark-snat-post-gc-wait", 8*time.Second, "grace period before sampling post-GC SNAT map occupancy")
	flag.StringVar(&cfg.benchmarkTransports, "benchmark-transports", "", "comma-separated benchmark transports: tcp-short,tcp-reuse,tcp-pool,udp,udp-connected")
	flag.StringVar(&cfg.benchmarkOut, "benchmark-output", "", "optional JSON output path for benchmark report")
	flag.Parse()
	return cfg
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
