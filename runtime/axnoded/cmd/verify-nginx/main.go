package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

type verifyNginxConfig struct {
	mode           string
	targetURL      string
	requests       int
	concurrency    int
	warmupRequests int
	benchmarkOut   string
	timeout        time.Duration
	address        string
	rootfs         string
	runtimeName    string
	runtimeID      string
	stdoutPath     string
	stderrPath     string
	listenPort     int
	holdOpen       bool
	natBackend     string
	bpfnetPin      string
	externalNetNS  string
	externalAddr   string
	skipLocalhost  bool
}

func main() {
	cfg := parseFlags()
	switch cfg.mode {
	case "verify":
		if err := runVerifyNginx(cfg); err != nil {
			fatalf("%v", err)
		}
	case "http-benchmark-client":
		summary, err := runHTTPBenchmarkClient(cfg.targetURL, cfg.requests, cfg.concurrency, cfg.warmupRequests, cfg.timeout)
		if err != nil {
			fatalf("http benchmark client: %v", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
			fatalf("encode http benchmark summary: %v", err)
		}
	default:
		fatalf("unsupported mode %q", cfg.mode)
	}
}

func parseFlags() verifyNginxConfig {
	cfg := verifyNginxConfig{}
	flag.StringVar(&cfg.mode, "mode", "verify", "verify or http-benchmark-client")
	flag.StringVar(&cfg.targetURL, "target-url", "", "benchmark target URL for http-benchmark-client mode")
	flag.IntVar(&cfg.requests, "benchmark-requests", 0, "number of benchmark requests to issue")
	flag.IntVar(&cfg.concurrency, "benchmark-concurrency", 16, "number of concurrent benchmark workers")
	flag.IntVar(&cfg.warmupRequests, "benchmark-warmup-requests", 0, "number of warmup requests to issue before benchmarking")
	flag.StringVar(&cfg.benchmarkOut, "benchmark-output", "", "optional JSON output path for benchmark report")
	flag.DurationVar(&cfg.timeout, "timeout", 5*time.Second, "per-request timeout")
	flag.StringVar(&cfg.address, "address", config.DefaultSocketAddress, "axnoded unix socket path")
	flag.StringVar(&cfg.rootfs, "rootfs", "/opt/nginx-rootfs", "LOCAL nginx rootfs path")
	flag.StringVar(&cfg.runtimeName, "runtime", config.RuntimeNameRunsc, "sandbox runtime name under test")
	flag.StringVar(&cfg.runtimeID, "runtime-id", "verify-nginx-runtime", "function runtime id")
	flag.StringVar(&cfg.stdoutPath, "stdout", "/tmp/axnoded-nginx.stdout", "container stdout path")
	flag.StringVar(&cfg.stderrPath, "stderr", "/tmp/axnoded-nginx.stderr", "container stderr path")
	flag.IntVar(&cfg.listenPort, "listen-port", 18080, "host port exposed via DNAT")
	flag.BoolVar(&cfg.holdOpen, "hold-open", false, "keep the nginx container running until SIGINT/SIGTERM")
	flag.StringVar(&cfg.natBackend, "nat-backend", config.NatBackendIptables, "nat backend under test")
	flag.StringVar(&cfg.bpfnetPin, "bpfnet-pin-path", config.DefaultBPFNetPinPath, "bpfnet pin root")
	flag.StringVar(&cfg.externalNetNS, "external-probe-netns", "", "optional network namespace used to prove ingress tc reachability")
	flag.StringVar(&cfg.externalAddr, "external-probe-address", "", "optional host IP to probe from the external namespace")
	flag.BoolVar(&cfg.skipLocalhost, "skip-localhost-check", false, "skip localhost hostPort reachability assertions")
	flag.Parse()
	return cfg
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
