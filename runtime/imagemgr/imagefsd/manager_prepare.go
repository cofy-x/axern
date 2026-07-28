package imagefsd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/registryauth"
)

func (mgr *manager) loadExistedDaemons() error {
	daemonConfigDir := filepath.Join(mgr.root, "daemon_configs")
	entries, err := os.ReadDir(daemonConfigDir)
	if err != nil {
		return fmt.Errorf("failed to read daemon configs: %w", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		d := &Daemon{ctx: mgr.ctx, binPath: mgr.binPath, nodeID: mgr.nodeID, cgroupCtrl: mgr.cgroupCtrl}
		metaFilePath := filepath.Join(daemonConfigDir, entry.Name())
		if err = d.LoadExisted(metaFilePath); err != nil {
			logrus.Errorf("failed to load daemon from meta file %s: %v", metaFilePath, err)
			continue
		}
		d.savedPath = filepath.Join(daemonConfigDir, d.meta.ID+".json")
		if err = mgr.reconcileNydusRuntimePolicy(d); err != nil {
			return err
		}
		mgr.daemons[d.meta.ID] = d
	}
	return nil
}

// addExistingDaemonsToCgroup adds alive daemon PIDs to the cgroup on restart.
func (mgr *manager) addExistingDaemonsToCgroup() {
	if !mgr.cgroupCtrl.Enabled() {
		return
	}
	for _, d := range mgr.daemons {
		if !d.IsAlive() {
			continue
		}
		pid := d.getPid()
		if pid <= 0 {
			continue
		}
		if err := mgr.cgroupCtrl.AddPID(pid); err != nil {
			logrus.Warnf("cgroup: failed to add existing daemon %s (pid %d): %v", d.meta.ID, pid, err)
		} else {
			logrus.Infof("cgroup: added existing daemon %s (pid %d) to cgroup", d.meta.ID, pid)
		}
	}
}

func (mgr *manager) prepare(ossCfgPath string, nydusCfgPath string, ossAuthsPath string, registryAuthsPath string) error {
	// chunk db
	err := os.MkdirAll(filepath.Join(mgr.root, "chunk_db"), 0755)
	if err != nil {
		return fmt.Errorf("failed to create chunk_db dir: %w", err)
	}
	// meta db root dir
	err = os.MkdirAll(filepath.Join(mgr.root, "image_metas"), 0755)
	if err != nil {
		return fmt.Errorf("failed to create image_meta dir: %w", err)
	}
	// daemons root dir
	err = os.MkdirAll(filepath.Join(mgr.root, "daemons"), 0755)
	if err != nil {
		return fmt.Errorf("failed to create daemons dir: %w", err)
	}
	// daemon config root dir
	err = os.MkdirAll(filepath.Join(mgr.root, "daemon_configs"), 0755)
	if err != nil {
		return fmt.Errorf("failed to create daemon configs dir: %w", err)
	}
	// daemon log staging dir (for extracting WARN/ERROR before cleanup)
	err = os.MkdirAll(filepath.Join(mgr.root, daemonLogStagingDir), 0755)
	if err != nil {
		return fmt.Errorf("failed to create daemon_log_staging dir: %w", err)
	}
	// Clean up any stale staged daemon logs from previous runs
	if entries, readErr := os.ReadDir(filepath.Join(mgr.root, daemonLogStagingDir)); readErr == nil {
		for _, e := range entries {
			if e.Type().IsRegular() {
				os.Remove(filepath.Join(mgr.root, daemonLogStagingDir, e.Name()))
			}
		}
	}

	// Load OSS config template if provided
	if ossCfgPath != "" {
		file, err := os.Open(ossCfgPath)
		if err != nil {
			return fmt.Errorf("failed to open oss config template file: %w", err)
		}
		defer file.Close()
		if err = json.NewDecoder(file).Decode(&mgr.ossCfgTemplate); err != nil {
			return fmt.Errorf("failed to load oss config template file: %w", err)
		}
	} else {
		return fmt.Errorf("oss config template path is required")
	}

	// Load Nydus config template if provided
	if nydusCfgPath != "" {
		file, err := os.Open(nydusCfgPath)
		if err != nil {
			return fmt.Errorf("failed to open nydus config template file: %w", err)
		}
		defer file.Close()
		if err = json.NewDecoder(file).Decode(&mgr.nydusCfgTemplate); err != nil {
			return fmt.Errorf("failed to load nydus config template file: %w", err)
		}
	} else {
		return fmt.Errorf("nydus config template path is required")
	}

	// Load OSS auths
	if ossAuthsPath != "" {
		file, err := os.Open(ossAuthsPath)
		if err != nil {
			return fmt.Errorf("failed to open OSS auths file: %w", err)
		}
		defer file.Close()
		mgr.ossAuths = make(OSSAuthsConfig)
		if err = json.NewDecoder(file).Decode(&mgr.ossAuths); err != nil {
			return fmt.Errorf("failed to load OSS auths file: %w", err)
		}
		logrus.Infof("loaded OSS auths for %d endpoint/bucket pairs", len(mgr.ossAuths))
	} else {
		return fmt.Errorf("oss auths path is required")
	}

	// Load registry auths
	if registryAuthsPath != "" {
		auths, err := registryauth.Load(registryAuthsPath)
		if err != nil {
			return fmt.Errorf("failed to load registry auths file: %w", err)
		}
		mgr.registryAuths = auths
		logrus.Infof("loaded registry auths for %d hosts/repos", len(mgr.registryAuths))
	} else {
		return fmt.Errorf("registry auths path is required")
	}

	return nil
}
