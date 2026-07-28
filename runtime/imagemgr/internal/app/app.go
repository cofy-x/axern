package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/lib/go/observability/logrusotel"
	"github.com/cofy-x/axern/runtime/imagemgr/api"
	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/nydus"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"github.com/cofy-x/axern/runtime/imagemgr/ossloop"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
)

// Run parses daemon flags, builds dependencies, and serves the imagemgr API.
func Run(args []string) error {
	flags := flag.NewFlagSet("imagemgr", flag.ContinueOnError)
	rootWorkDir := flags.String("root", ".", "The root directory of the imagemgr workspace.")
	nodeID := flags.String("node_id", "", "Stable control-plane node ID attached to imagefsd metrics.")
	imagefsdBinPath := flags.String("imagefsd_bin", "/usr/local/bin/imagefsd",
		"imagefsd binary path.")
	ossTemplate := flags.String("oss_template", "", "Path to the OSS configuration template file.")
	nydusTemplate := flags.String("nydus_template", "", "Path to the Nydus backend configuration template file.")
	debug := flags.Bool("debug", false, "Output debug info or not.")
	httpSockPath := flags.String("http_sock", api.DefaultHttpSockPath, "Http api socket path")
	nydusSuffix := flags.String("nydus_suffix", "", "Tag suffix to try when detecting Nydus images (e.g., '-nydus')")
	ossAuthsPath := flags.String("oss_auths_path", "", "Path to OSS authentication credentials file (oss_auths.json)")
	registryAuthsPath := flags.String("registry_auths_path", "", "Path to registry authentication credentials file (registry_auths.json)")
	registryMirrorURL := flags.String("registry_mirror_url", "", "Dynamic registry mirror origin used for OCI pulls and Nydus bootstrap fetches")
	enableTracing := flags.Bool("enable_tracing", false, "Enable OpenTelemetry tracing for mount/unmount operations")
	cgroupMemoryLimitStr := flags.String("cgroup_memory_limit", "0", "Memory limit for imagefsd cgroup, e.g. 512MiB, 2GiB, or raw bytes (0 = no limit)")
	nydusReadaheadWorkers := flags.Int("nydus_readahead_workers", 0, "Background workers for demand-triggered Nydus cache readahead (0 disables readahead)")
	nydusReadaheadWindowBytes := flags.Int("nydus_readahead_window_bytes", 32*1024*1024, "Maximum bytes scheduled after a foreground Nydus read")
	nydusDecodedCacheBytes := flags.Int("nydus_decoded_cache_bytes", 8*1024*1024, "Per-mount byte limit for decoded Nydus chunks")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*nodeID) == "" {
		return fmt.Errorf("node_id is required")
	}

	cgroupMemoryLimit, err := parseMemorySize(*cgroupMemoryLimitStr)
	if err != nil {
		return fmt.Errorf("invalid cgroup_memory_limit %q: %w", *cgroupMemoryLimitStr, err)
	}
	if *nydusReadaheadWorkers < 0 {
		return fmt.Errorf("nydus_readahead_workers must not be negative")
	}
	if *nydusReadaheadWindowBytes < 0 {
		return fmt.Errorf("nydus_readahead_window_bytes must not be negative")
	}
	if *nydusDecodedCacheBytes < 0 {
		return fmt.Errorf("nydus_decoded_cache_bytes must not be negative")
	}

	logLevel := logrus.InfoLevel
	if *debug {
		logLevel = logrus.DebugLevel
	}
	logrus.SetLevel(logLevel)
	logsDir := filepath.Join(*rootWorkDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("create logs directory: %w", err)
	}
	lumberjackLogger := &lumberjack.Logger{
		Filename:   filepath.Join(logsDir, "imagemgr.log"),
		MaxBackups: 5,
		MaxAge:     30, // 30 days
	}
	logrus.SetOutput(lumberjackLogger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	otelConfig := sdkobs.ConfigFromEnv(
		sdkobs.WithServiceName("imagemgr"),
		sdkobs.WithComponent("imagemgr"),
	)
	otelConfig.Enabled = otelConfig.Enabled || *enableTracing
	obs, err := sdkobs.Init(ctx, otelConfig)
	if err != nil {
		return fmt.Errorf("initialize OpenTelemetry: %w", err)
	}
	if obs.Enabled() {
		logrus.AddHook(logrusotel.New("imagemgr"))
		logrus.Info("OpenTelemetry enabled")
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := obs.Shutdown(shutdownCtx); err != nil {
			logrus.Errorf("failed to shutdown OpenTelemetry: %v", err)
		}
	}()

	// OCI and Nydus share one authenticated transport and connection pool.
	sharedRegistryClient, err := imageregistry.NewClient(
		*registryAuthsPath,
		imageregistry.WithRegistryMirror(*registryMirrorURL),
	)
	if err != nil {
		return fmt.Errorf("initialize registry client: %w", err)
	}
	nydusClient := nydus.NewRegistryClientFromShared(sharedRegistryClient)
	logrus.Info("Registry and Nydus clients initialized successfully")

	// Create manager with both OSS and Nydus config templates
	mgr, err := imagefsd.NewManager(&imagefsd.ManagerConfig{
		Context:                   ctx,
		NodeID:                    *nodeID,
		Root:                      *rootWorkDir,
		OSSCfgPath:                *ossTemplate,
		NydusCfgPath:              *nydusTemplate,
		BinPath:                   *imagefsdBinPath,
		NydusClient:               nydusClient,
		OSSAuthsPath:              *ossAuthsPath,
		RegistryAuthsPath:         *registryAuthsPath,
		CgroupMemoryLimit:         cgroupMemoryLimit,
		NydusReadaheadWorkers:     *nydusReadaheadWorkers,
		NydusReadaheadWindowBytes: *nydusReadaheadWindowBytes,
		NydusDecodedCacheBytes:    *nydusDecodedCacheBytes,
	})
	if err != nil {
		return fmt.Errorf("create imagefsd manager: %w", err)
	}

	// initialize OCI manager (local layer extraction + overlay mount)
	ociMgr, err := oci.NewManager(*rootWorkDir, *nydusTemplate, sharedRegistryClient)
	if err != nil {
		return fmt.Errorf("create oci manager: %w", err)
	}
	defer ociMgr.Close()
	registryProxyURL, err := registryProxyURLFromTemplate(*nydusTemplate)
	if err != nil {
		return err
	}

	ossLoopMgr, err := ossloop.NewManager(&ossloop.Config{
		Root: filepath.Join(*rootWorkDir, "oss_rootfs"),
	})
	if err != nil {
		return fmt.Errorf("create oss loop manager: %w", err)
	}

	dbPath := filepath.Join(*rootWorkDir, "mount_records.db")
	mountStore, err := mountstore.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open mount store: %w", err)
	}
	defer mountStore.Close()

	worker, err := api.NewHttpWorker(&api.HttpWorkerConfig{
		LifecycleContext: ctx,
		Manager:          mgr,
		OCIManager:       ociMgr,
		NydusClient:      nydusClient,
		NydusSuffix:      *nydusSuffix,
		RegistryProxyURL: registryProxyURL,
		OSSLoopManager:   ossLoopMgr,
		MountStore:       mountStore,
	})
	if err != nil {
		return fmt.Errorf("create http worker: %w", err)
	}

	if err := worker.ServeHTTP(ctx, *httpSockPath); err != nil {
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}

func registryProxyURLFromTemplate(templatePath string) (string, error) {
	if templatePath == "" {
		return "", nil
	}
	var cfg imagefsd.BackendConfig
	if err := cfg.LoadTemplate(templatePath); err != nil {
		return "", fmt.Errorf("load registry proxy config: %w", err)
	}
	if cfg.Registry == nil || cfg.Registry.Proxy == nil {
		return "", nil
	}
	return cfg.Registry.Proxy.Url, nil
}

// parseMemorySize parses a human-readable memory size string (e.g. "5GiB",
// "64MiB", "1024KiB", "512MB", "2GB") into bytes. Plain numeric strings
// are treated as raw byte values.
func parseMemorySize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	// Pure numeric value: treat as raw bytes
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}

	re := regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*(B|KiB|MiB|GiB|TiB|KB|MB|GB|TB)$`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("unsupported format, use e.g. 512MiB, 2GiB, or plain bytes")
	}

	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}

	var multiplier float64
	switch strings.ToUpper(m[2]) {
	case "B":
		multiplier = 1
	case "KIB":
		multiplier = 1024
	case "MIB":
		multiplier = 1024 * 1024
	case "GIB":
		multiplier = 1024 * 1024 * 1024
	case "TIB":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "KB":
		multiplier = 1000
	case "MB":
		multiplier = 1000 * 1000
	case "GB":
		multiplier = 1000 * 1000 * 1000
	case "TB":
		multiplier = 1000 * 1000 * 1000 * 1000
	}

	return int64(val * multiplier), nil
}
