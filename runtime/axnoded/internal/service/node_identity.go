package service

import (
	"context"
	"path/filepath"

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
	identity := workloadtls.Identity{Cluster: cfg.WorkloadCluster, Role: "axnoded", NodeID: cfg.ControlPlaneNodeID}
	bundle := filepath.Join(h.config.RootDir, "identity", "node.pem")
	manager := &controlplane.NodeEnrollment{
		Client:   controlplane.NewNodeEnrollmentClient(cfg.ControlPlaneEnrollmentTarget, cfg.ControlPlaneTLSCACertValue(), bundle, identity),
		Identity: identity, BundlePath: bundle, TrustPath: cfg.ControlPlaneTLSCACertValue(),
	}
	ctx, cancel := context.WithCancel(parent)
	h.nodeIdentityCancel = cancel
	h.nodeIdentityWG.Add(1)
	bootstrapTokenFile := h.nodeBootstrapTokenFile
	h.nodeBootstrapTokenFile = ""
	go func() {
		defer h.nodeIdentityWG.Done()
		manager.Maintain(ctx, bootstrapTokenFile, func(err error) {
			logrus.WithError(err).WithField("node_id", identity.NodeID).Error("node certificate maintenance failed; authenticated admission remains fail-closed")
		})
	}()
}
