package dataplane

type Config struct {
	PinPath          string
	MapSize          int
	SNATMapSize      int
	LocalOutCompat   bool
	IptablesFallback bool
}

type Service struct {
	Protocol   string
	HostPort   uint16
	TargetIP   string
	TargetPort uint16
}

type Attachment struct {
	LocalAddresses       []string
	LocalhostTCPDNAT     bool
	LocalhostAttachError string
}

type KernelStats struct {
	AttachSuccesses                    uint64
	ServiceHits                        uint64
	RevNATHits                         uint64
	SNATHits                           uint64
	SNATRevHits                        uint64
	SNATFwdHits                        uint64
	SNATUDPSamePortHits                uint64
	SNATUDPPortRewriteHits             uint64
	SNATUDPChecksumHits                uint64
	SNATMappingsProgrammed             uint64
	SNATAllocCollisions                uint64
	SNATFallbackHits                   uint64
	SNATAllocExhausted                 uint64
	SNATTCPNonSynMisses                uint64
	SNATTCPNonSynMissFINs              uint64
	SNATTCPNonSynMissRSTs              uint64
	SNATTCPNonSynMissACKs              uint64
	SNATTCPNonSynMissOther             uint64
	SNATFullCloseReclaims              uint64
	SNATFullCloseMarks                 uint64
	SNATTCPFullCloseDeletes            uint64
	SNATTCPFullCloseDeletesFwd         uint64
	SNATTCPFullCloseDeletesRev         uint64
	SNATTCPNonSynMissFwdLookups        uint64
	SNATTCPNonSynMissFwdHostMismatches uint64
	SNATTCPReverseMisses               uint64
	SNATTCPReverseMissSynACKs          uint64
	SNATTCPReverseMissFINs             uint64
	SNATTCPReverseMissRSTs             uint64
	SNATTCPReverseMissACKs             uint64
	SNATTCPReverseMissOther            uint64
	NativeRouteSkips                   uint64
	LocalhostConnectHits               uint64
	LocalhostGetpeerHits               uint64
	FallbackHits                       uint64
	LocalhostFallbackHits              uint64
	AttachErrors                       uint64
}

type SNATMapStats struct {
	FwdEntries             uint64
	FwdTCPEntries          uint64
	FwdUDPEntries          uint64
	FwdICMPEntries         uint64
	FwdActiveEntries       uint64
	FwdClosingEntries      uint64
	FwdOrigClosingEntries  uint64
	FwdReplyClosingEntries uint64
	FwdFullClosingEntries  uint64
	RevEntries             uint64
	RevTCPEntries          uint64
	RevUDPEntries          uint64
	RevICMPEntries         uint64
	RevActiveEntries       uint64
	RevClosingEntries      uint64
	RevOrigClosingEntries  uint64
	RevReplyClosingEntries uint64
	RevFullClosingEntries  uint64
	RevReverseEntries      uint64
	RevAliasEntries        uint64
	TranslatedPortsUsed    uint64
	UDPTranslatedPortsUsed uint64
}

type SNATGCPolicy struct {
	TCPIdleNanos      uint64
	TCPClosingNanos   uint64
	DatagramIdleNanos uint64
}

type SNATGCResult struct {
	FwdScanned uint64
	FwdDeleted uint64
	RevScanned uint64
	RevDeleted uint64
}

type AttachmentReadiness struct {
	UplinkDevices          []string
	LocalAddresses         []string
	IngressTCAttached      bool
	EgressTCAttached       bool
	LocalhostLinksAttached bool
	PinnedMapsReady        bool
	PinnedProgramsReady    bool
}

type Interface interface {
	EnsureAttached(uplinks []string, ipRange string, nativeRoutingCIDRs []string, services []Service) (Attachment, error)
	UpsertService(service Service) error
	DeleteService(service Service) error
	CleanupStaleSNATMappings(policy SNATGCPolicy) (SNATGCResult, error)
}
