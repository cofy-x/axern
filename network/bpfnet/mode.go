package bpfnet

const (
	ModeIPTablesFullFallback                    = "iptables-full-fallback"
	ModeIngressTCPUDPDNATEgressSNAT             = "ingress-tcp-udp-dnat+egress-snat+iptables-safety-fallback"
	ModeIngressTCPUDPDNATEgressSNATLocalhostTCP = "ingress-tcp-udp-dnat+egress-snat+localhost-tcp-dnat+iptables-safety-fallback"
	ModeIngressTCPUDPDNATEgressSNATLocalCompat  = "ingress-tcp-udp-dnat+egress-snat+localhost-tcp-iptables-compat+iptables-safety-fallback"
)

const (
	KernelStatAttachSuccess uint32 = iota
	KernelStatAttachError
	KernelStatServiceHit
	KernelStatRevNATHit
	KernelStatFallbackHit
	KernelStatMapConflict
	KernelStatSNATHit
	KernelStatSNATRevHit
	KernelStatSNATFwdHit
	KernelStatSNATUDPSamePortHit
	KernelStatSNATUDPPortRewriteHit
	KernelStatSNATUDPChecksumPresentHit
	KernelStatSNATMappingProgrammed
	KernelStatSNATAllocCollision
	KernelStatSNATFallbackHit
	KernelStatSNATAllocExhausted
	KernelStatSNATTCPNonSynMiss
	KernelStatSNATTCPNonSynMissFIN
	KernelStatSNATTCPNonSynMissRST
	KernelStatSNATTCPNonSynMissACK
	KernelStatSNATTCPNonSynMissOther
	KernelStatSNATFullCloseReclaim
	KernelStatSNATFullCloseMark
	KernelStatSNATTCPFullCloseDelete
	KernelStatSNATTCPFullCloseDeleteFwd
	KernelStatSNATTCPFullCloseDeleteRev
	KernelStatSNATTCPNonSynMissFwdLookup
	KernelStatSNATTCPNonSynMissFwdHostMismatch
	KernelStatSNATTCPRevMiss
	KernelStatSNATTCPRevMissSynACK
	KernelStatSNATTCPRevMissFIN
	KernelStatSNATTCPRevMissRST
	KernelStatSNATTCPRevMissACK
	KernelStatSNATTCPRevMissOther
	KernelStatNativeRouteSkip
	KernelStatLocalhostConnectHit
	KernelStatLocalhostGetPeerHit
	KernelStatLocalhostFallbackHit
)
