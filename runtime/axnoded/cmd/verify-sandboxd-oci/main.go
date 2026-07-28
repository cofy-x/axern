package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type config struct {
	runtimeName    string
	runtimeBinary  string
	rootfs         string
	sandboxdBinary string
	workDir        string
	netnsPath      string
	runscOverlay2  string
	shellCommand   string
	useTemplate    bool
	withResource   bool
	noCopyRootfs   bool
}

type runCase struct {
	name             string
	argv             []string
	env              []*apipb.KeyValue
	cwd              string
	expected         int
	expectOut        []string
	expectReady      bool
	expectProcessAPI bool
	signalAfter      time.Duration
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "verify-sandboxd-oci: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("verify_sandboxd_oci_ok=true")
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.runtimeName, "runtime", "runsc", "runtime name under test")
	flag.StringVar(&cfg.runtimeBinary, "runtime-binary", "", "OCI runtime binary path")
	flag.StringVar(&cfg.rootfs, "rootfs", "/opt/sample-rootfs", "rootfs path")
	flag.StringVar(&cfg.sandboxdBinary, "sandboxd-binary", "/usr/local/libexec/axnoded/axern-sandboxd", "host axern-sandboxd binary path")
	flag.StringVar(&cfg.workDir, "work-dir", "", "temporary work directory")
	flag.StringVar(&cfg.netnsPath, "netns-path", "", "optional host network namespace path to inject into the OCI spec")
	flag.StringVar(&cfg.runscOverlay2, "runsc-overlay2", "", "optional runsc --overlay2 value")
	flag.StringVar(&cfg.shellCommand, "shell-command", "", "optional single shell command case to run instead of the default suite")
	flag.BoolVar(&cfg.useTemplate, "use-template", false, "materialize the OCI bundle through a prepared bundle template")
	flag.BoolVar(&cfg.withResource, "with-resource", false, "include representative OCI resource limits")
	flag.BoolVar(&cfg.noCopyRootfs, "no-copy-rootfs", false, "use the supplied rootfs path directly")
	flag.Parse()
	if cfg.runtimeBinary == "" {
		cfg.runtimeBinary = "/usr/local/bin/" + cfg.runtimeName
	}
	return cfg
}

func run(cfg config) error {
	if _, err := os.Stat(cfg.runtimeBinary); err != nil {
		return fmt.Errorf("runtime binary %s is unavailable: %w", cfg.runtimeBinary, err)
	}
	if _, err := os.Stat(cfg.sandboxdBinary); err != nil {
		return fmt.Errorf("sandboxd binary %s is unavailable: %w", cfg.sandboxdBinary, err)
	}
	workDir := cfg.workDir
	cleanup := func() {}
	if workDir == "" {
		dir, err := os.MkdirTemp("", "axern-sandboxd-oci-*")
		if err != nil {
			return err
		}
		workDir = dir
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	defer cleanup()

	cases := []runCase{
		{
			name:        "exit7",
			argv:        []string{"/bin/sh", "-c", "printf 'pid1=%s env=%s cwd=%s\\n' \"$(cat /proc/1/comm)\" \"$AXERN_SANDBOXD_E2E\" \"$(pwd)\"; sleep 1; exit 7"},
			env:         []*apipb.KeyValue{{Key: "AXERN_SANDBOXD_E2E", Value: "ok"}},
			cwd:         "/tmp",
			expected:    7,
			expectOut:   []string{"pid1=axern-sandboxd env=ok cwd=/tmp"},
			expectReady: true,
		},
		{
			name:     "missing",
			argv:     []string{"/tmp/axern-sandboxd-missing-binary"},
			expected: 127,
		},
		{
			name:             "signal",
			argv:             []string{"/bin/sh", "-c", "while true; do sleep 1; done"},
			expected:         143,
			expectReady:      true,
			expectProcessAPI: true,
			signalAfter:      2 * time.Second,
		},
	}
	if cfg.shellCommand != "" {
		cases = []runCase{
			{
				name:        "custom",
				argv:        []string{"/bin/sh", "-c", cfg.shellCommand},
				expected:    143,
				expectReady: true,
				signalAfter: 2 * time.Second,
			},
		}
	}
	for _, tc := range cases {
		if err := runOne(workDir, cfg, tc); err != nil {
			return err
		}
	}
	return nil
}

func runOne(workDir string, cfg config, tc runCase) error {
	caseDir := filepath.Join(workDir, safeName(cfg.runtimeName+"-"+tc.name))
	if err := os.RemoveAll(caseDir); err != nil {
		return err
	}
	for _, dir := range []string{
		filepath.Join(caseDir, "rootfs"),
		filepath.Join(caseDir, "containers"),
		filepath.Join(caseDir, "runtime-root"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	rootfs := cfg.rootfs
	if !cfg.noCopyRootfs {
		rootfs = filepath.Join(caseDir, "rootfs")
		if err := copyRootfs(cfg.rootfs, rootfs); err != nil {
			return fmt.Errorf("%s: copy rootfs: %w", tc.name, err)
		}
		if err := os.MkdirAll(filepath.Join(rootfs, "mnt"), 0755); err != nil {
			return err
		}
	}

	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(caseDir, "containers"))
	if err != nil {
		return err
	}
	containerID := safeName("axern-sandboxd-" + cfg.runtimeName + "-" + tc.name)
	stdoutPath := filepath.Join(caseDir, "stdout.log")
	stderrPath := filepath.Join(caseDir, "stderr.log")
	cwd := tc.cwd
	if cwd == "" {
		cwd = "/"
	}
	labels := map[string]string(nil)
	if cfg.netnsPath != "" {
		labels = map[string]string{
			resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName): (&resourcemanager.NetResource{NetNSPath: cfg.netnsPath}).ToString(),
		}
	}
	request := &apipb.CreateContainerRequest{
		Command: tc.argv,
		Cwd:     cwd,
		Envs:    tc.env,
		Labels:  labels,
		Stdout:  stdoutPath,
		Stderr:  stderrPath,
		Rootfs: &apipb.Rootfs{
			RootDir:  rootfs,
			Readonly: false,
		},
	}
	if cfg.withResource {
		request.Resource = &apipb.LinuxContainerResources{
			CpuShares:          512,
			MemoryLimitInBytes: 4 * 1024 * 1024 * 1024,
		}
	}
	loadOptions := runtimeoci.LoadOptions{
		ContainerID:       containerID,
		Request:           request,
		SandboxdInjection: &runtimeoci.SandboxdInjectionOptions{HostBinaryPath: cfg.sandboxdBinary},
	}
	var bundlePath string
	var ociSpec *spec.Spec
	if cfg.useTemplate {
		template, err := loader.PrepareBundleTemplate(runtimeoci.TemplateOptions{Request: request})
		if err != nil {
			return fmt.Errorf("%s: prepare bundle template: %w", tc.name, err)
		}
		bundlePath, ociSpec, err = loader.MaterializeBundle(template, loadOptions)
	} else {
		bundlePath, ociSpec, err = loader.Generate(loadOptions)
	}
	if err != nil {
		return fmt.Errorf("%s: generate bundle: %w", tc.name, err)
	}
	if err := assertInjectedSpec(ociSpec); err != nil {
		return fmt.Errorf("%s: %w", tc.name, err)
	}

	runtimeRoot := filepath.Join(caseDir, "runtime-root")
	args := runtimeArgs(cfg, runtimeRoot, "run", "--bundle", bundlePath, containerID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.runtimeBinary, args...)
	var runtimeOut bytes.Buffer
	cmd.Stdout = &runtimeOut
	cmd.Stderr = &runtimeOut
	caseFailure := func(err error) error {
		return fmt.Errorf("%s: %w\n%s", tc.name, err, caseDiagnostics(bundlePath, stdoutPath, stderrPath, runtimeOut.String()))
	}
	if tc.signalAfter > 0 {
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("%s: start runtime: %w: %s", tc.name, err, runtimeOut.String())
		}
		if tc.expectReady {
			if err := assertSandboxdReady(ctx, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
		}
		if tc.expectProcessAPI {
			if err := assertSandboxdProcessAPI(ctx, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdFileAPI(ctx, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdProbePortsMounts(ctx, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdBackedExecContainer(ctx, cfg, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdBackedExecSession(ctx, cfg, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdBackedFileService(ctx, cfg, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
		}
		time.Sleep(tc.signalAfter)
		if err := killContainer(cfg, runtimeRoot, containerID); err != nil {
			_ = cmd.Wait()
			return fmt.Errorf("%s: signal container: %w", tc.name, err)
		}
		err = cmd.Wait()
		code := exitCode(err)
		if code != tc.expected {
			err := caseFailure(fmt.Errorf("exit code = %d, want %d", code, tc.expected))
			_ = deleteContainer(cfg, runtimeRoot, containerID)
			return err
		}
		_ = deleteContainer(cfg, runtimeRoot, containerID)
		return nil
	}

	if tc.expectReady {
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("%s: start runtime: %w: %s", tc.name, err, runtimeOut.String())
		}
		if err := assertSandboxdReady(ctx, bundlePath); err != nil {
			_ = killContainer(cfg, runtimeRoot, containerID)
			_ = cmd.Wait()
			_ = deleteContainer(cfg, runtimeRoot, containerID)
			return caseFailure(err)
		}
		if tc.expectProcessAPI {
			if err := assertSandboxdProcessAPI(ctx, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdFileAPI(ctx, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdProbePortsMounts(ctx, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdBackedExecContainer(ctx, cfg, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdBackedExecSession(ctx, cfg, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
			if err := assertSandboxdBackedFileService(ctx, cfg, bundlePath); err != nil {
				_ = killContainer(cfg, runtimeRoot, containerID)
				_ = cmd.Wait()
				_ = deleteContainer(cfg, runtimeRoot, containerID)
				return caseFailure(err)
			}
		}
		err = cmd.Wait()
	} else {
		err = cmd.Run()
	}
	code := exitCode(err)
	if code != tc.expected {
		err := caseFailure(fmt.Errorf("exit code = %d, want %d", code, tc.expected))
		_ = deleteContainer(cfg, runtimeRoot, containerID)
		return err
	}
	_ = deleteContainer(cfg, runtimeRoot, containerID)
	if err := assertOutput(stdoutPath, runtimeOut.String(), tc.expectOut); err != nil {
		return fmt.Errorf("%s: %w", tc.name, err)
	}
	return nil
}
