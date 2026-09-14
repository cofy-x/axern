package bpfnet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func (c *Controller) Status() (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var status Status
	_ = readJSONFile(c.stateFile, &status.State)
	status.Kernel = collectKernelStats(c.cfg)
	status.SNATMaps = collectSNATMapStats(c.cfg)
	status.Attachment = collectAttachmentReadiness(c.cfg, status.State)
	return status, nil
}

func writeJSONFile(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSONFile(path string, value interface{}) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func (c *Controller) failedState(uplinks []string, attachErr error) DataplaneState {
	tcProbeErr, reconcileErr := splitAttachError(attachErr)
	return DataplaneState{
		Mode:               ModeAttachFailed,
		IPRange:            c.ipRange,
		UplinkDevices:      uplinks,
		PinPath:            c.cfg.PinPath,
		SNATMapSize:        c.cfg.SNATMapSize,
		SNATPortMin:        SNATAllocatorPortMin,
		SNATPortMax:        SNATAllocatorPortMax,
		SNATPortAttempts:   SNATAllocatorPortAttempts,
		NativeRoutingCIDRs: append([]string(nil), c.cfg.NativeRoutingCIDRs...),
		TCReady:            false,
		LastAttachError:    attachErr.Error(),
		LastTCProbeError:   tcProbeErr,
		LastReconcileError: reconcileErr,
		UpdatedAt:          time.Now().UTC(),
	}
}

func (c *Controller) readyState(uplinks []string) DataplaneState {
	return DataplaneState{
		Mode:               ModeEgressSNAT,
		IPRange:            c.ipRange,
		UplinkDevices:      uplinks,
		PinPath:            c.cfg.PinPath,
		SNATMapSize:        c.cfg.SNATMapSize,
		SNATPortMin:        SNATAllocatorPortMin,
		SNATPortMax:        SNATAllocatorPortMax,
		SNATPortAttempts:   SNATAllocatorPortAttempts,
		NativeRoutingCIDRs: append([]string(nil), c.cfg.NativeRoutingCIDRs...),
		EgressSNAT:         true,
		TCReady:            true,
		UpdatedAt:          time.Now().UTC(),
	}
}
