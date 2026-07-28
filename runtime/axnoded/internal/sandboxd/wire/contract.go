package wire

func CoreCapabilities() []string {
	return []string{
		CapabilityDiagnostics,
		CapabilityHealth,
		CapabilityMounts,
		CapabilityPorts,
		CapabilityProbe,
		CapabilityStatus,
		CapabilitySupervisor,
	}
}

func FileCapabilities() []string {
	return []string{
		CapabilityArchive,
		CapabilityFile,
	}
}

func ProcessCapabilities() []string {
	return []string{
		CapabilityManagedProxy,
		CapabilityProcess,
		CapabilityPTY,
	}
}

func BaselineCapabilities() []string {
	return []string{
		CapabilityArchive,
		CapabilityDiagnostics,
		CapabilityFile,
		CapabilityHealth,
		CapabilityManagedProxy,
		CapabilityMounts,
		CapabilityPorts,
		CapabilityProbe,
		CapabilityProcess,
		CapabilityPTY,
		CapabilityStatus,
		CapabilitySupervisor,
	}
}

func OptionalCapabilities() []string {
	return []string{
		CapabilityBrowser,
		CapabilityComputerUse,
	}
}
