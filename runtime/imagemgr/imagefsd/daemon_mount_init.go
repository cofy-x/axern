package imagefsd

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// initializeDaemon performs the full daemon initialization sequence:
// - Downloads Nydus bootstrap if needed
// - Cleans mount point
// - Applies configuration
// - Starts daemon process
// - Saves metadata
// - Initializes channels and starts watcher
func (d *Daemon) initializeDaemon(ctx context.Context, timing *TimedOperation) error {
	if err := d.ensureBootstrapAndEnv(ctx, timing); err != nil {
		return err
	}

	stageStart := time.Now()
	if err := d.cleanMountPoint(); err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to clean mountpoint")
		timing.Fail(err)
		return err
	}
	timing.Stage("clean_mount_point", time.Since(stageStart))

	stageStart = time.Now()
	if err := d.applyConfig(); err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to apply config")
		timing.Fail(err)
		return err
	}
	timing.Stage("apply_config", time.Since(stageStart))

	if err := d.startMountCommand(ctx, timing); err != nil {
		return err
	}

	stageStart = time.Now()
	if err := d.saveMeta(); err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to save daemon meta")
		timing.Fail(err)
		return err
	}
	timing.Stage("save_metadata", time.Since(stageStart))

	d.stopChan = make(chan struct{})
	d.kickStop = NewStopper()
	d.startWatch()

	return nil
}

func (d *Daemon) ensureBootstrapAndEnv(ctx context.Context, timing *TimedOperation) error {
	if d.meta.SourceType == SourceTypeNydus && d.meta.BootstrapPath == "" {
		if d.meta.ImageURL == "" || d.nydusClient == nil {
			err := fmt.Errorf("ImageURL and nydusClient required for Nydus daemon")
			logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to start daemon")
			timing.Fail(err)
			return err
		}

		stageStart := time.Now()
		logrus.WithFields(d.daemonLogFields()).Info("fetching Nydus bootstrap")
		extractedPath, envVars, err := d.fetchNydusBootstrap(ctx)
		if err != nil {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to fetch and extract bootstrap")
			timing.Fail(err)
			return err
		}
		d.meta.BootstrapPath = extractedPath
		d.meta.Env = envVars
		d.meta.EnvResolved = true
		d.dockerConfigJSON = ""
		logrus.WithFields(d.daemonLogFields()).WithField("bootstrap_path", extractedPath).Info("extracted bootstrap")
		timing.Stage("fetch_bootstrap", time.Since(stageStart))

		if err = d.saveMeta(); err != nil {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to save daemon meta after bootstrap download")
			timing.Fail(err)
			return err
		}
	} else if d.meta.SourceType == SourceTypeNydus && d.meta.BootstrapPath != "" && !d.meta.EnvResolved {
		if d.nydusClient != nil && d.meta.ImageURL != "" {
			stageStart := time.Now()
			_, envVars, err := d.fetchNydusBootstrap(ctx)
			if err != nil {
				logrus.WithFields(d.daemonLogFields()).WithError(err).Debug("failed to fetch env for existing daemon")
			} else {
				d.meta.Env = envVars
				d.meta.EnvResolved = true
				d.dockerConfigJSON = ""
				if saveErr := d.saveMeta(); saveErr != nil {
					logrus.WithFields(d.daemonLogFields()).WithError(saveErr).Warn("failed to persist env to daemon meta")
				}
			}
			timing.Stage("fetch_env_for_existing_daemon", time.Since(stageStart))
		}
	}

	return nil
}

func (d *Daemon) fetchNydusBootstrap(ctx context.Context) (string, []string, error) {
	if client, ok := d.nydusClient.(authenticatedNydusClient); ok {
		return client.FetchAndExtractBootstrapWithDockerConfigJSON(
			ctx, d.meta.ImageURL, d.meta.DaemonDir, d.dockerConfigJSON,
		)
	}
	return d.nydusClient.FetchAndExtractBootstrap(ctx, d.meta.ImageURL, d.meta.DaemonDir)
}
