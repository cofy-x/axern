package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

type verifyStartupConfig struct {
	address          string
	metricsURL       string
	inventoryURL     string
	scenario         string
	mode             string
	samples          int
	runtimeName      string
	runtimeID        string
	mountType        string
	rootfsSrc        string
	rootfsPath       string
	imageURL         string
	stdoutDir        string
	stderrDir        string
	omitStdio        bool
	argvJSON         string
	waitBeforeDelete bool
	expectedExit     int
}

func parseFlags() verifyStartupConfig {
	cfg := verifyStartupConfig{}
	flag.StringVar(&cfg.address, "address", config.DefaultSocketAddress, "axnoded unix socket path")
	flag.StringVar(&cfg.metricsURL, "metrics-url", "http://127.0.0.1:23001/debug/metricsz", "axnoded debug metrics snapshot endpoint")
	flag.StringVar(&cfg.inventoryURL, "inventory-url", "http://127.0.0.1:23001/inventoryz", "axnoded inventory endpoint")
	flag.StringVar(&cfg.scenario, "scenario", "", "startup matrix scenario name")
	flag.StringVar(&cfg.mode, "mode", "cold", "sample mode: cold or warm")
	flag.IntVar(&cfg.samples, "samples", 1, "number of start/delete samples to execute")
	flag.StringVar(&cfg.runtimeName, "runtime", config.RuntimeNameRunsc, "sandbox runtime name under test")
	flag.StringVar(&cfg.runtimeID, "runtime-id", "", "function runtime id")
	flag.StringVar(&cfg.mountType, "mount-type", "local", "mount type label for the scenario")
	flag.StringVar(&cfg.rootfsSrc, "rootfs-src", "local", "rootfs source: local or image")
	flag.StringVar(&cfg.rootfsPath, "rootfs", "/opt/sample-rootfs", "LOCAL rootfs path")
	flag.StringVar(&cfg.imageURL, "image-url", "", "OCI/Nydus image URL for rootfs-src=image")
	flag.StringVar(&cfg.stdoutDir, "stdout-dir", "/tmp/startup-matrix-stdout", "directory for startup sample stdout files")
	flag.StringVar(&cfg.stderrDir, "stderr-dir", "/tmp/startup-matrix-stderr", "directory for startup sample stderr files")
	flag.BoolVar(&cfg.omitStdio, "omit-stdio", false, "omit explicit stdout/stderr paths to exercise runtime-provided defaults")
	flag.StringVar(&cfg.argvJSON, "argv-json", "", "JSON argv array to execute inside the container")
	flag.BoolVar(&cfg.waitBeforeDelete, "wait-before-delete", false, "wait for the container to exit before issuing delete")
	flag.IntVar(&cfg.expectedExit, "expected-exit", 0, "expected exit code when wait-before-delete is enabled")
	flag.Parse()
	return cfg
}

func resolveCommand(argvJSON string) ([]string, error) {
	return verifyutil.ResolveArgv(argvJSON, "", "sleep 300")
}

func buildRootfsConfig(src, localPath, imageURL string) (*verifyutil.RootfsSpec, string, string, error) {
	rootfsConfig, err := verifyutil.BuildRootfsSpec(src, localPath, imageURL)
	if err != nil {
		return nil, "", "", err
	}
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "", "local":
		return rootfsConfig, natbench.LocalRootfsKey(localPath), "local", nil
	case "image":
		return rootfsConfig, natbench.ImageRootfsKey(imageURL), "image", nil
	default:
		return nil, "", "", fmt.Errorf("unsupported rootfs source %q", src)
	}
}

func filepathJoin(dir, base string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + base
	}
	return dir + "/" + base
}
