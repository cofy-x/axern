package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/lib/go/observability/logrusotel"
	"github.com/cofy-x/axern/runtime/imagemgr/api"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
)

// Run parses daemon flags, builds dependencies, and serves the imagemgr API.
func Run(args []string) error {
	flags := flag.NewFlagSet("imagemgr", flag.ContinueOnError)
	rootWorkDir := flags.String("root", ".", "The root directory of the imagemgr workspace.")
	debug := flags.Bool("debug", false, "Output debug info or not.")
	httpSockPath := flags.String("http_sock", api.DefaultHttpSockPath, "Http api socket path")
	registryAuthsPath := flags.String("registry_auths_path", "", "Path to registry authentication credentials file (registry_auths.json)")
	snapshotRepository := flags.String("snapshot_repository", "", "Platform-owned OCI repository for immutable Allocation rootfs snapshots")
	registryMirrorURL := flags.String("registry_mirror_url", "", "Dynamic registry mirror origin used for OCI pulls")
	enableTracing := flags.Bool("enable_tracing", false, "Enable OpenTelemetry tracing for mount/unmount operations")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if repository := strings.TrimSpace(*snapshotRepository); repository != "" {
		if _, err := name.NewRepository(repository, name.WeakValidation); err != nil {
			return fmt.Errorf("invalid snapshot_repository %q: %w", repository, err)
		}
	}

	if err := validateRetiredImageState(*rootWorkDir); err != nil {
		return err
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

	// OCI pulls and snapshots share one authenticated transport and connection pool.
	sharedRegistryClient, err := imageregistry.NewClient(
		*registryAuthsPath,
		imageregistry.WithRegistryMirror(*registryMirrorURL),
	)
	if err != nil {
		return fmt.Errorf("initialize registry client: %w", err)
	}

	// Keep the authority store locked throughout backend recovery and serving.
	// Revalidate after acquiring its lock so another process cannot introduce
	// Nydus authority between the read-only preflight and recovery.
	dbPath := filepath.Join(*rootWorkDir, "resource_leases.db")
	mountStore, err := mountstore.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open mount store: %w", err)
	}
	defer mountStore.Close()
	if err := mountStore.ValidateOCIOnly(); err != nil {
		return fmt.Errorf("retired or invalid image authority: %w", err)
	}
	if err := validateRetiredDaemonState(*rootWorkDir); err != nil {
		return err
	}

	// initialize OCI manager (local layer extraction + overlay mount)
	ociMgr, err := oci.NewManager(*rootWorkDir, sharedRegistryClient)
	if err != nil {
		return fmt.Errorf("create oci manager: %w", err)
	}
	defer ociMgr.Close()

	worker, err := api.NewHttpWorker(&api.HttpWorkerConfig{
		LifecycleContext:   ctx,
		OCIManager:         ociMgr,
		MountStore:         mountStore,
		Registry:           sharedRegistryClient,
		SnapshotRepository: *snapshotRepository,
	})
	if err != nil {
		return fmt.Errorf("create http worker: %w", err)
	}

	if err := worker.ServeHTTP(ctx, *httpSockPath); err != nil {
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}

func validateRetiredImageState(root string) error {
	if err := mountstore.ValidateOCIOnlyFile(filepath.Join(root, "resource_leases.db")); err != nil {
		return fmt.Errorf("retired or invalid image authority: %w", err)
	}
	return validateRetiredDaemonState(root)
}

// Retired state is inspected before recovery, logging or cache initialization.
// Its contents may still own mounts or bytes; absence is never cleanup proof.
func validateRetiredDaemonState(root string) error {
	for _, name := range []string{"daemon_configs", "daemons", "chunk_db", "image_metas"} {
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect retired image state: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("retired image state %s requires explicit operator resolution", name)
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read retired image state: %w", err)
		}
		if len(entries) != 0 {
			return fmt.Errorf("retired image state %s requires explicit operator resolution", name)
		}
	}
	return nil
}
