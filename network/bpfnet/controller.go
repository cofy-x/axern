package bpfnet

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type commandRunner func(name string, args ...string) ([]byte, error)

type dataplaneFactory func(cfg Config, run commandRunner) dataplane

type dataplane interface {
	EnsureAttached(uplinks []string, ipRange string, nativeRoutingCIDRs []string, services []Service) (dataplaneAttachment, error)
	UpsertService(service Service) error
	DeleteService(service Service) error
	CleanupStaleSNATMappings(policy SNATGCPolicy) (SNATGCResult, error)
}

type dataplaneAttachment struct {
	LocalAddresses       []string
	LocalhostTCPDNAT     bool
	LocalhostAttachError string
}

type Controller struct {
	cfg       Config
	run       commandRunner
	stateDir  string
	stateFile string
	svcFile   string
	statsFile string
	dp        dataplane
	ipRange   string
	mu        sync.Mutex
}

type DataplaneState struct {
	Mode               string    `json:"mode"`
	IPRange            string    `json:"ipRange"`
	UplinkDevices      []string  `json:"uplinkDevices"`
	LocalAddresses     []string  `json:"localAddresses"`
	PinPath            string    `json:"pinPath"`
	MapSize            int       `json:"mapSize"`
	SNATMapSize        int       `json:"snatMapSize"`
	SNATPortMin        int       `json:"snatPortMin"`
	SNATPortMax        int       `json:"snatPortMax"`
	SNATPortAttempts   int       `json:"snatPortAttempts"`
	LocalOutCompat     bool      `json:"localOutCompat"`
	NativeRoutingCIDRs []string  `json:"nativeRoutingCIDRs"`
	IptablesFallback   bool      `json:"iptablesFallback"`
	IngressTCPDNAT     bool      `json:"ingressTcpDnat"`
	IngressUDPDNAT     bool      `json:"ingressUdpDnat"`
	EgressSNAT         bool      `json:"egressSnat"`
	TCReady            bool      `json:"tcReady"`
	LocalhostTCPDNAT   bool      `json:"localhostTcpDnat"`
	LocalhostPathReady bool      `json:"localhostPathReady"`
	FullFallback       bool      `json:"fullFallback"`
	LocalhostCompat    bool      `json:"localhostCompatFallback"`
	LastAttachError    string    `json:"lastAttachError,omitempty"`
	LastTCProbeError   string    `json:"lastTcProbeError,omitempty"`
	LastLocalhostError string    `json:"lastLocalhostAttachError,omitempty"`
	LastReconcileError string    `json:"lastReconcileError,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type Service struct {
	Protocol   string `json:"protocol"`
	HostPort   uint16 `json:"hostPort"`
	TargetIP   string `json:"targetIp"`
	TargetPort uint16 `json:"targetPort"`
}

type Stats struct {
	AttachSuccesses uint64    `json:"attachSuccesses"`
	Upserts         uint64    `json:"upserts"`
	Deletes         uint64    `json:"deletes"`
	Conflicts       uint64    `json:"conflicts"`
	Fallbacks       uint64    `json:"fallbacks"`
	AttachErrors    uint64    `json:"attachErrors"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type KernelStats struct {
	AttachSuccesses                    uint64 `json:"attachSuccesses"`
	ServiceHits                        uint64 `json:"serviceHits"`
	RevNATHits                         uint64 `json:"revNatHits"`
	SNATHits                           uint64 `json:"snatHits"`
	SNATRevHits                        uint64 `json:"snatRevHits"`
	SNATFwdHits                        uint64 `json:"snatFwdHits"`
	SNATUDPSamePortHits                uint64 `json:"snatUdpSamePortHits"`
	SNATUDPPortRewriteHits             uint64 `json:"snatUdpPortRewriteHits"`
	SNATUDPChecksumHits                uint64 `json:"snatUdpChecksumHits"`
	SNATMappingsProgrammed             uint64 `json:"snatMappingsProgrammed"`
	SNATAllocCollisions                uint64 `json:"snatAllocCollisions"`
	SNATFallbackHits                   uint64 `json:"snatFallbackHits"`
	SNATAllocExhausted                 uint64 `json:"snatAllocExhausted"`
	SNATTCPNonSynMisses                uint64 `json:"snatTcpNonSynMisses"`
	SNATTCPNonSynMissFINs              uint64 `json:"snatTcpNonSynMissFins"`
	SNATTCPNonSynMissRSTs              uint64 `json:"snatTcpNonSynMissRsts"`
	SNATTCPNonSynMissACKs              uint64 `json:"snatTcpNonSynMissAcks"`
	SNATTCPNonSynMissOther             uint64 `json:"snatTcpNonSynMissOther"`
	SNATFullCloseReclaims              uint64 `json:"snatFullCloseReclaims"`
	SNATFullCloseMarks                 uint64 `json:"snatFullCloseMarks"`
	SNATTCPFullCloseDeletes            uint64 `json:"snatTcpFullCloseDeletes"`
	SNATTCPFullCloseDeletesFwd         uint64 `json:"snatTcpFullCloseDeletesFwd"`
	SNATTCPFullCloseDeletesRev         uint64 `json:"snatTcpFullCloseDeletesRev"`
	SNATTCPNonSynMissFwdLookups        uint64 `json:"snatTcpNonSynMissFwdLookups"`
	SNATTCPNonSynMissFwdHostMismatches uint64 `json:"snatTcpNonSynMissFwdHostMismatches"`
	SNATTCPReverseMisses               uint64 `json:"snatTcpReverseMisses"`
	SNATTCPReverseMissSynACKs          uint64 `json:"snatTcpReverseMissSynAcks"`
	SNATTCPReverseMissFINs             uint64 `json:"snatTcpReverseMissFins"`
	SNATTCPReverseMissRSTs             uint64 `json:"snatTcpReverseMissRsts"`
	SNATTCPReverseMissACKs             uint64 `json:"snatTcpReverseMissAcks"`
	SNATTCPReverseMissOther            uint64 `json:"snatTcpReverseMissOther"`
	NativeRouteSkips                   uint64 `json:"nativeRouteSkips"`
	LocalhostConnectHits               uint64 `json:"localhostConnectHits"`
	LocalhostGetpeerHits               uint64 `json:"localhostGetpeerHits"`
	FallbackHits                       uint64 `json:"fallbackHits"`
	LocalhostFallbackHits              uint64 `json:"localhostFallbackHits"`
	AttachErrors                       uint64 `json:"attachErrors"`
}

type SNATMapStats struct {
	FwdEntries             uint64 `json:"fwdEntries"`
	FwdTCPEntries          uint64 `json:"fwdTcpEntries"`
	FwdUDPEntries          uint64 `json:"fwdUdpEntries"`
	FwdICMPEntries         uint64 `json:"fwdIcmpEntries"`
	FwdActiveEntries       uint64 `json:"fwdActiveEntries"`
	FwdClosingEntries      uint64 `json:"fwdClosingEntries"`
	FwdOrigClosingEntries  uint64 `json:"fwdOrigClosingEntries"`
	FwdReplyClosingEntries uint64 `json:"fwdReplyClosingEntries"`
	FwdFullClosingEntries  uint64 `json:"fwdFullClosingEntries"`
	RevEntries             uint64 `json:"revEntries"`
	RevTCPEntries          uint64 `json:"revTcpEntries"`
	RevUDPEntries          uint64 `json:"revUdpEntries"`
	RevICMPEntries         uint64 `json:"revIcmpEntries"`
	RevActiveEntries       uint64 `json:"revActiveEntries"`
	RevClosingEntries      uint64 `json:"revClosingEntries"`
	RevOrigClosingEntries  uint64 `json:"revOrigClosingEntries"`
	RevReplyClosingEntries uint64 `json:"revReplyClosingEntries"`
	RevFullClosingEntries  uint64 `json:"revFullClosingEntries"`
	RevReverseEntries      uint64 `json:"revReverseEntries"`
	RevAliasEntries        uint64 `json:"revAliasEntries"`
	TranslatedPortsUsed    uint64 `json:"translatedPortsUsed"`
	UDPTranslatedPortsUsed uint64 `json:"udpTranslatedPortsUsed"`
}

type SNATGCPolicy struct {
	TCPIdleTimeout      time.Duration `json:"tcpIdleTimeout"`
	TCPClosingTimeout   time.Duration `json:"tcpClosingTimeout"`
	DatagramIdleTimeout time.Duration `json:"datagramIdleTimeout"`
}

type SNATGCResult struct {
	FwdScanned uint64 `json:"fwdScanned"`
	FwdDeleted uint64 `json:"fwdDeleted"`
	RevScanned uint64 `json:"revScanned"`
	RevDeleted uint64 `json:"revDeleted"`
}

type AttachmentReadiness struct {
	UplinkDevices          []string `json:"uplinkDevices"`
	LocalAddresses         []string `json:"localAddresses"`
	IngressTCAttached      bool     `json:"ingressTcAttached"`
	EgressTCAttached       bool     `json:"egressTcAttached"`
	LocalhostLinksAttached bool     `json:"localhostLinksAttached"`
	PinnedMapsReady        bool     `json:"pinnedMapsReady"`
	PinnedProgramsReady    bool     `json:"pinnedProgramsReady"`
}

type Status struct {
	State      DataplaneState      `json:"state"`
	Services   []Service           `json:"services"`
	Stats      Stats               `json:"stats"`
	Kernel     KernelStats         `json:"kernelStats"`
	SNATMaps   SNATMapStats        `json:"snatMaps"`
	Attachment AttachmentReadiness `json:"attachment"`
}

var newDataplane dataplaneFactory = defaultDataplaneFactory

func NewController(cfg Config) *Controller {
	cfg = cfg.WithDefaults()
	return &Controller{
		cfg:       cfg,
		stateDir:  cfg.StatePath,
		run:       defaultRunner,
		stateFile: filepath.Join(cfg.StatePath, "dataplane_state.json"),
		svcFile:   filepath.Join(cfg.StatePath, "service_map.json"),
		statsFile: filepath.Join(cfg.StatePath, "stats.json"),
	}
}

func defaultRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func (c *Controller) NeedsSNATFallback() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return !c.currentStateLocked().TCReady
}

func (c *Controller) NeedsFullDNATFallback(protocol string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch strings.ToLower(protocol) {
	case "tcp", "udp":
		return !c.currentStateLocked().TCReady
	default:
		return true
	}
}

func (c *Controller) NeedsLocalhostCompat(protocol string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return strings.ToLower(protocol) == "tcp" && c.currentStateLocked().LocalhostCompat
}

func (c *Controller) EnsureAttached(ipRange string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ipRange != "" {
		c.ipRange = ipRange
	}
	if c.ipRange == "" {
		var existing DataplaneState
		if err := readJSONFile(c.stateFile, &existing); err == nil && existing.IPRange != "" {
			c.ipRange = existing.IPRange
		}
	}

	uplinks, err := c.resolveUplinks()
	if err != nil {
		c.bumpStats(func(s *Stats) {
			s.AttachErrors++
		})
		return err
	}

	if err := os.MkdirAll(c.cfg.PinPath, 0755); err != nil {
		return fmt.Errorf("create bpfnet pin path: %w", err)
	}
	if err := os.MkdirAll(c.stateDir, 0755); err != nil {
		return fmt.Errorf("create bpfnet state path: %w", err)
	}

	services, err := c.loadServicesLocked()
	if err != nil {
		return err
	}

	attachment, err := c.dataplaneLocked().EnsureAttached(
		uplinks,
		c.ipRange,
		append([]string(nil), c.cfg.NativeRoutingCIDRs...),
		flattenServices(services),
	)
	if err != nil {
		c.bumpStats(func(s *Stats) {
			s.AttachErrors++
			s.Fallbacks++
		})
		fallbackState := c.fallbackState(uplinks, err)
		if writeErr := writeJSONFile(c.stateFile, fallbackState); writeErr != nil {
			return writeErr
		}
		if !c.cfg.IptablesFallback {
			return err
		}
		return nil
	}
	c.bumpStats(func(s *Stats) {
		s.AttachSuccesses++
	})

	state := c.readyState(uplinks, attachment)
	if attachment.LocalhostAttachError != "" && !c.cfg.IptablesFallback {
		if writeErr := writeJSONFile(c.stateFile, state); writeErr != nil {
			return writeErr
		}
		c.bumpStats(func(s *Stats) {
			s.AttachErrors++
		})
		return fmt.Errorf("attach localhost tcp path: %s", attachment.LocalhostAttachError)
	}
	return writeJSONFile(c.stateFile, state)
}

func (c *Controller) Cleanup() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.Remove(c.stateFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *Controller) CleanupStaleSNATMappings(policy SNATGCPolicy) (SNATGCResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.dataplaneLocked().CleanupStaleSNATMappings(policy)
}

func (c *Controller) resolveUplinks() ([]string, error) {
	if len(c.cfg.UplinkDevices) > 0 {
		return append([]string(nil), c.cfg.UplinkDevices...), nil
	}

	output, err := c.run("ip", "-4", "route", "show", "default")
	if err != nil {
		return nil, fmt.Errorf("resolve default route uplink: %w", err)
	}
	fields := strings.Fields(string(output))
	for idx := range fields {
		if fields[idx] == "dev" && idx+1 < len(fields) {
			return []string{fields[idx+1]}, nil
		}
	}
	return nil, fmt.Errorf("resolve default route uplink: no default device found")
}

func (c *Controller) dataplaneLocked() dataplane {
	if c.dp == nil {
		c.dp = newDataplane(c.cfg, c.run)
	}
	return c.dp
}

func (c *Controller) currentStateLocked() DataplaneState {
	var state DataplaneState
	_ = readJSONFile(c.stateFile, &state)
	return state
}
