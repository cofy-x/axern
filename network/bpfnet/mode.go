package bpfnet

const (
	ModeAttachFailed                            = "attach-failed"
	ModeIngressTCPUDPDNATEgressSNAT             = "ingress-tcp-udp-dnat+egress-snat"
	ModeIngressTCPUDPDNATEgressSNATLocalhostTCP = "ingress-tcp-udp-dnat+egress-snat+localhost-tcp-dnat"
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
