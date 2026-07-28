package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	axnodedconfig "github.com/cofy-x/axern/runtime/axnoded/config"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func newVerifyRuntimeHandlerWithRoot(cfg config, rootDir string) (contract.RuntimeHandler, error) {
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		return nil, err
	}
	runtimeCfg := axnodedconfig.RuntimeInstanceConfig{Binary: filepath.Join(rootDir, "missing-runtime-binary")}
	baseCfg := axnodedconfig.Config{RootDir: rootDir}
	switch cfg.runtimeName {
	case "runc":
		handler, err := runtimecore.NewRuncServiceHandler(baseCfg, axnodedconfig.RuntimeNameRunc, runtimeCfg, loader)
		if err != nil {
			return nil, err
		}
		return handler, nil
	case "runsc":
		handler, err := runtimecore.NewRunscServiceHandler(baseCfg, axnodedconfig.RuntimeNameRunsc, runtimeCfg, loader)
		if err != nil {
			return nil, err
		}
		return handler, nil
	default:
		return nil, fmt.Errorf("unsupported runtime for sandboxd-backed exec e2e: %s", cfg.runtimeName)
	}
}

func runtimeArgs(cfg config, runtimeRoot string, args ...string) []string {
	out := []string{"--root", runtimeRoot}
	if cfg.runtimeName == "runsc" {
		out = append(out, "--ignore-cgroups", "--host-uds=create")
		if cfg.runscOverlay2 != "" {
			out = append(out, "--overlay2", cfg.runscOverlay2)
		}
	}
	out = append(out, args...)
	return out
}

func killContainer(cfg config, runtimeRoot, containerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := runtimeArgs(cfg, runtimeRoot, "kill", containerID, "TERM")
	output, err := exec.CommandContext(ctx, cfg.runtimeBinary, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func deleteContainer(cfg config, runtimeRoot, containerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := runtimeArgs(cfg, runtimeRoot, "delete", "--force", containerID)
	output, err := exec.CommandContext(ctx, cfg.runtimeBinary, args...).CombinedOutput()
	if err != nil && !strings.Contains(string(output), "does not exist") {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyRootfs(src, dst string) error {
	cmd := exec.Command("cp", "-a", src+string(os.PathSeparator)+".", dst)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitErr.ExitCode()
	}
	return -1
}

func safeName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return strings.Trim(b.String(), "-")
}
