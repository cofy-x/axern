package service

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/sirupsen/logrus"
)

// Credential maintenance must not gate runtime recovery. If registration or
// renewal is unavailable, ordinary RPCs fail closed while the execution-lease
// watchdog remains alive to fence existing sandboxes. Exiting before starting
// the watchdog would leave surviving runsc sandboxes without lease enforcement.
func (h *sandboxService) startNodeIdentity(parent context.Context) {
	cfg := h.config.PluginConfig
	if cfg.ControlPlaneTargetValue() == "" {
		return
	}
	hostname, _ := os.Hostname()
	identity := workloadtls.Identity{Cluster: cfg.WorkloadCluster, Role: "axnoded", NodeID: cfg.ControlPlaneNodeIDValue(hostname)}
	bundle := filepath.Join(h.config.RootDir, "identity", "node.pem")
	manager := &controlplane.NodeEnrollment{
		Client:   controlplane.NewNodeEnrollmentClient(cfg.ControlPlaneEnrollmentTarget, cfg.ControlPlaneTLSCACertValue(), bundle, identity),
		Identity: identity, BundlePath: bundle, TrustPath: cfg.ControlPlaneTLSCACertValue(),
	}
	ctx, cancel := context.WithCancel(parent)
	h.nodeIdentityCancel = cancel
	h.nodeIdentityWG.Add(1)
	go func() {
		defer h.nodeIdentityWG.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			attempt, stop := context.WithTimeout(ctx, 10*time.Second)
			err := manager.Ensure(attempt, cfg.ControlPlaneEnrollmentTokenValue())
			if err == nil {
				err = manager.RenewIfDue(attempt)
			}
			stop()
			if err != nil && ctx.Err() == nil {
				logrus.WithError(err).WithField("node_id", identity.NodeID).Error("node certificate maintenance failed; authenticated admission remains fail-closed")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
