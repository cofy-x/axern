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
	_ = readJSONFile(c.statsFile, &status.Stats)
	_ = readJSONFile(c.svcFile, &status.Services)
	status.Kernel = collectKernelStats(c.cfg)
	status.SNATMaps = collectSNATMapStats(c.cfg)
	status.Attachment = collectAttachmentReadiness(c.cfg, status.State)
	return status, nil
}

func (c *Controller) loadServicesLocked() (map[string]Service, error) {
	var services []Service
	if err := readJSONFile(c.svcFile, &services); err != nil {
		return nil, err
	}
	out := make(map[string]Service, len(services))
	for _, svc := range services {
		out[serviceKey(svc.Protocol, svc.HostPort)] = svc
	}
	return out, nil
}

func (c *Controller) bumpStats(mutate func(*Stats)) {
	var stats Stats
	_ = readJSONFile(c.statsFile, &stats)
	mutate(&stats)
	stats.UpdatedAt = time.Now().UTC()
	_ = writeJSONFile(c.statsFile, stats)
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

func (c *Controller) fallbackState(uplinks []string, attachErr error) DataplaneState {
	tcProbeErr, reconcileErr := splitAttachError(attachErr)
	return DataplaneState{
		Mode:               ModeIPTablesFullFallback,
		IPRange:            c.ipRange,
		UplinkDevices:      uplinks,
		PinPath:            c.cfg.PinPath,
		MapSize:            c.cfg.MapSize,
		SNATMapSize:        c.cfg.SNATMapSize,
		SNATPortMin:        SNATAllocatorPortMin,
		SNATPortMax:        SNATAllocatorPortMax,
		SNATPortAttempts:   SNATAllocatorPortAttempts,
		LocalOutCompat:     c.cfg.LocalOutCompat,
		NativeRoutingCIDRs: append([]string(nil), c.cfg.NativeRoutingCIDRs...),
		IptablesFallback:   c.cfg.IptablesFallback,
		TCReady:            false,
		LocalhostTCPDNAT:   false,
		LocalhostPathReady: false,
		FullFallback:       true,
		LocalhostCompat:    false,
		LastAttachError:    attachErr.Error(),
		LastTCProbeError:   tcProbeErr,
		LastReconcileError: reconcileErr,
		UpdatedAt:          time.Now().UTC(),
	}
}

func (c *Controller) readyState(uplinks []string, attachment dataplaneAttachment) DataplaneState {
	mode := ModeIngressTCPUDPDNATEgressSNAT
	localhostCompat := false
	if c.cfg.LocalOutCompat {
		if attachment.LocalhostTCPDNAT {
			mode = ModeIngressTCPUDPDNATEgressSNATLocalhostTCP
		} else {
			mode = ModeIngressTCPUDPDNATEgressSNATLocalCompat
			localhostCompat = c.cfg.IptablesFallback && attachment.LocalhostAttachError != ""
		}
	}

	lastAttachError := ""
	if attachment.LocalhostAttachError != "" {
		lastAttachError = attachment.LocalhostAttachError
	}

	return DataplaneState{
		Mode:               mode,
		IPRange:            c.ipRange,
		UplinkDevices:      uplinks,
		LocalAddresses:     append([]string(nil), attachment.LocalAddresses...),
		PinPath:            c.cfg.PinPath,
		MapSize:            c.cfg.MapSize,
		SNATMapSize:        c.cfg.SNATMapSize,
		SNATPortMin:        SNATAllocatorPortMin,
		SNATPortMax:        SNATAllocatorPortMax,
		SNATPortAttempts:   SNATAllocatorPortAttempts,
		LocalOutCompat:     c.cfg.LocalOutCompat,
		NativeRoutingCIDRs: append([]string(nil), c.cfg.NativeRoutingCIDRs...),
		IptablesFallback:   c.cfg.IptablesFallback,
		IngressTCPDNAT:     true,
		IngressUDPDNAT:     true,
		EgressSNAT:         true,
		TCReady:            true,
		LocalhostTCPDNAT:   attachment.LocalhostTCPDNAT,
		LocalhostPathReady: attachment.LocalhostTCPDNAT,
		FullFallback:       false,
		LocalhostCompat:    localhostCompat,
		LastAttachError:    lastAttachError,
		LastLocalhostError: attachment.LocalhostAttachError,
		UpdatedAt:          time.Now().UTC(),
	}
}
