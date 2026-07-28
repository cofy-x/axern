package imagefsd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
)

func (d *Daemon) saveMeta() error {
	file, err := os.OpenFile(d.savedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", d.savedPath, err)
	}
	defer file.Close()
	if err = json.NewEncoder(file).Encode(&d.meta); err != nil {
		return fmt.Errorf("failed to dump json format: %w", err)
	}
	return nil
}

func (d *Daemon) loadBackendConfig() error {
	file, err := os.Open(d.meta.CfgPath)
	if err != nil {
		return fmt.Errorf("failed to load backend config, err = %w", err)
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(d.config)
}

func (d *Daemon) LoadExisted(metaFilePath string) error {
	file, err := os.Open(metaFilePath)
	if err != nil {
		return fmt.Errorf("failed to load from meta file, %w", err)
	}
	defer file.Close()
	if err = json.NewDecoder(file).Decode(&d.meta); err != nil {
		return fmt.Errorf("invalid json format, %w", err)
	}

	if d.meta.SourceType == "" {
		d.meta.SourceType = SourceTypeOSS
	}

	// Load backend config (works for both OSS and Nydus)
	d.config = &BackendConfig{}
	if err = d.loadBackendConfig(); err != nil {
		return err
	}

	if d.IsAlive() {
		d.kickStop = NewStopper()
		d.stopChan = make(chan struct{})
		d.setState(DaemonStateRunning)
		d.startWatch()
		logrus.WithFields(d.daemonLogFields()).Info("load active daemon")
	} else {
		d.setState(DaemonStateStopped)
		logrus.WithFields(d.daemonLogFields()).Info("load non-active daemon")
	}
	return nil
}

func (d *Daemon) applyConfig() error {
	file, err := os.OpenFile(d.meta.CfgPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create config file, err = %w", err)
	}
	defer file.Close()

	// Write backend config (works for both OSS and Nydus)
	return json.NewEncoder(file).Encode(d.config)
}
