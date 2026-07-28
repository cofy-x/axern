package nodeinventory

import (
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
)

func (s *AxnodedSource) collectBPFNetInventory(now time.Time, snapshot *NodeInventorySnapshot) {
	if s.natBackend != "ebpf" {
		snapshot.Sources["bpfnet"] = SourceStatus{Status: StatusDisabled}
		snapshot.Components.BPFNet.Status = StatusDisabled
		return
	}

	status, err := s.loadBPFNet(s.bpfnetPin)
	if err != nil {
		snapshot.Sources["bpfnet"] = errorSource(err)
		snapshot.Components.BPFNet.Status = StatusError
		snapshot.Components.BPFNet.Enabled = true
		snapshot.Components.BPFNet.Error = err.Error()
		return
	}

	snapshot.Sources["bpfnet"] = readySource(now)
	snapshot.Components.BPFNet = bpfnetComponentInventory(status)
	snapshot.Components.BPFNet.Status = StatusReady
}

func bpfnetComponentInventory(status bpfnet.Status) BPFNetComponentInventory {
	return BPFNetComponentInventory{
		Enabled:               true,
		Ready:                 status.State.TCReady && !status.State.FullFallback,
		Mode:                  status.State.Mode,
		NeedsSNATFallback:     !status.State.TCReady,
		NeedsFullDNATFallback: status.State.FullFallback || !status.State.TCReady,
		NeedsLocalhostCompat:  status.State.LocalhostCompat,
	}
}
