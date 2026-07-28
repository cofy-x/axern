package imagefsd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func (mgr *manager) setupOSSDaemon(opts *DaemonCreateOpt) (*Daemon, error) {
	d := &Daemon{
		ctx:        mgr.ctx,
		nodeID:     mgr.nodeID,
		config:     &BackendConfig{},
		cgroupCtrl: mgr.cgroupCtrl,
	}
	d.meta.Name = opts.Name
	d.meta.ID = opts.ID
	d.meta.SourceType = SourceTypeOSS

	if opts.MountPoint == "" {
		opts.MountPoint = filepath.Join(mgr.root, "mnt", d.meta.ID)
	}
	if err := os.MkdirAll(opts.MountPoint, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mountpoint dir: %w", err)
	}
	d.meta.MountPoint = opts.MountPoint
	*d.config = mgr.ossCfgTemplate.DeepCopy()

	store := ensureObjectStoreConfig(d.config)

	if opts.overwriteOSSConfig() {
		*store.endpoint = opts.Endpoint
		*store.bucket = opts.Bucket
		*store.objectPrefix = opts.ObjectPrefix
		logrus.Infof("overwriting %s config (%s, %s, %s)",
			store.backendLabel, *store.endpoint, *store.bucket, *store.objectPrefix)
	}
	if opts.AccessKeyID != "" {
		*store.accessKeyID = opts.AccessKeyID
	}
	if opts.AccessKeySecret != "" {
		*store.accessKeySecret = opts.AccessKeySecret
	}

	if *store.accessKeyID == "" && mgr.ossAuths != nil {
		lookupKey := *store.endpoint + "/" + *store.bucket
		if authEntry, ok := mgr.ossAuths[lookupKey]; ok {
			*store.accessKeyID = authEntry.AccessKeyID
			*store.accessKeySecret = authEntry.AccessKeySecret
			logrus.Infof("populated %s auth for %s from auth file", store.backendLabel, lookupKey)
		} else {
			logrus.Debugf("no %s auth found for %s in auth file", store.backendLabel, lookupKey)
		}
	}

	d.binPath = mgr.binPath
	if err := os.MkdirAll(filepath.Join(mgr.root, "image_metas", d.meta.ID), 0755); err != nil {
		return nil, fmt.Errorf("failed to create image meta dir: %w", err)
	}
	daemonDir := filepath.Join(mgr.root, "daemons", d.meta.ID)
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create daemon dir: %w", err)
	}
	d.meta.DaemonDir = daemonDir
	d.meta.ImageMetaDir = filepath.Join(mgr.root, "image_metas", d.meta.ID)
	d.meta.DaemonLogPath = filepath.Join(daemonDir, "daemon.log")
	d.meta.PidFilePath = filepath.Join(daemonDir, "pid")
	d.meta.ChunkDBDir = filepath.Join(mgr.root, "chunk_db")
	d.meta.CachePath = filepath.Join(daemonDir, "cache")
	d.meta.CfgPath = filepath.Join(daemonDir, "backend.cfg")
	d.savedPath = filepath.Join(mgr.root, "daemon_configs", d.meta.ID+".json")
	d.updateExpired()

	return d, nil
}

type objectStoreConfig struct {
	endpoint        *string
	bucket          *string
	objectPrefix    *string
	accessKeyID     *string
	accessKeySecret *string
	backendLabel    string
}

func ensureObjectStoreConfig(cfg *BackendConfig) objectStoreConfig {
	if cfg.BackendType == "s3" {
		if cfg.S3 == nil {
			cfg.S3 = &S3Config{}
		}
		return objectStoreConfig{
			endpoint:        &cfg.S3.Endpoint,
			bucket:          &cfg.S3.BucketName,
			objectPrefix:    &cfg.S3.ObjectPrefix,
			accessKeyID:     &cfg.S3.AccessKeyId,
			accessKeySecret: &cfg.S3.AccessKeySecret,
			backendLabel:    "S3",
		}
	}
	if cfg.Oss == nil {
		cfg.Oss = &OssConfig{}
	}
	return objectStoreConfig{
		endpoint:        &cfg.Oss.Endpoint,
		bucket:          &cfg.Oss.BucketName,
		objectPrefix:    &cfg.Oss.ObjectPrefix,
		accessKeyID:     &cfg.Oss.AccessKeyId,
		accessKeySecret: &cfg.Oss.AccessKeySecret,
		backendLabel:    "OSS",
	}
}
