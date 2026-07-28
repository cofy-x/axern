package imagefsd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sirupsen/logrus"
)

func (mgr *manager) setupNydusDaemon(opts *DaemonCreateOpt) (*Daemon, error) {
	d := &Daemon{
		ctx:         mgr.ctx,
		nodeID:      mgr.nodeID,
		config:      &BackendConfig{},
		nydusClient: mgr.nydusClient,
		cgroupCtrl:  mgr.cgroupCtrl,
	}
	d.meta.Name = opts.Name
	d.meta.ID = opts.ID
	d.meta.SourceType = SourceTypeNydus
	d.meta.ImageURL = opts.ImageURL
	d.dockerConfigJSON = opts.DockerConfigJSON

	if opts.MountPoint == "" {
		opts.MountPoint = filepath.Join(mgr.root, "mnt", d.meta.ID)
	}
	if err := os.MkdirAll(opts.MountPoint, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mountpoint dir: %w", err)
	}
	d.meta.MountPoint = opts.MountPoint

	*d.config = mgr.nydusCfgTemplate.DeepCopy()
	d.binPath = mgr.binPath

	if d.config.Registry != nil && d.config.Registry.Proxy != nil && d.config.Registry.Proxy.Url != "" {
		logrus.Infof("using proxy %s for Nydus blob access", d.config.Registry.Proxy.Url)
	}

	if opts.ImageURL != "" && d.config.Registry != nil {
		ref, err := name.ParseReference(opts.ImageURL)
		if err != nil {
			logrus.Warnf("failed to parse image URL %s for registry config: %v", opts.ImageURL, err)
		} else {
			host := ref.Context().RegistryStr()
			repo := ref.Context().RepositoryStr()
			d.config.Registry.Host = host
			d.config.Registry.Repo = repo
			if resolver, ok := mgr.nydusClient.(insecureRegistryResolver); ok && resolver.UseHTTPFor(opts.ImageURL) {
				d.config.Registry.Scheme = "http"
				d.config.Registry.BlobUrlScheme = "http"
				logrus.Infof("using insecure HTTP transport for Nydus registry %s", host)
			}
			logrus.Infof("populated registry host/repo from image URL: %s/%s", host, repo)

			if opts.RegistryAuth != "" {
				d.config.Registry.Auth = opts.RegistryAuth
			} else if mgr.registryAuths != nil && d.config.Registry.Auth == "" {
				hostRepo := host + "/" + repo
				if authEntry, ok := mgr.registryAuths[hostRepo]; ok {
					d.config.Registry.Auth = authEntry.Auth
					logrus.Infof("populated registry auth for %s from auth file", hostRepo)
				} else if authEntry, ok := mgr.registryAuths[host]; ok {
					d.config.Registry.Auth = authEntry.Auth
					logrus.Infof("populated registry auth for host %s from auth file", host)
				} else {
					logrus.Debugf("no registry auth found for %s or %s in auth file", hostRepo, host)
				}
			}
		}
	}

	if err := os.MkdirAll(filepath.Join(mgr.root, "image_metas", d.meta.ID), 0755); err != nil {
		return nil, fmt.Errorf("failed to create image meta dir: %w", err)
	}

	daemonDir := filepath.Join(mgr.root, "daemons", d.meta.ID)
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create daemon dir: %w", err)
	}

	cacheDir := filepath.Join(daemonDir, "cache_dir")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}

	d.meta.DaemonDir = daemonDir
	d.meta.ImageMetaDir = filepath.Join(mgr.root, "image_metas", d.meta.ID)
	d.meta.DaemonLogPath = filepath.Join(daemonDir, "daemon.log")
	d.meta.PidFilePath = filepath.Join(daemonDir, "pid")
	d.meta.ChunkDBDir = filepath.Join(mgr.root, "chunk_db")
	d.meta.CacheDir = cacheDir
	mgr.nydusRuntimePolicy().apply(&d.meta)
	d.meta.CfgPath = filepath.Join(daemonDir, "backend.cfg")
	d.savedPath = filepath.Join(mgr.root, "daemon_configs", d.meta.ID+".json")
	d.updateExpired()

	return d, nil
}
