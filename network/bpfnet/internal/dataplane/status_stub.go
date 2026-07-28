//go:build !linux

package dataplane

func CollectKernelStats(Config) KernelStats {
	return KernelStats{}
}

func CollectSNATMapStats(Config) SNATMapStats {
	return SNATMapStats{}
}

func CollectAttachmentReadiness(_ Config, uplinks []string, localAddresses []string, _ bool) AttachmentReadiness {
	return AttachmentReadiness{
		UplinkDevices:  append([]string(nil), uplinks...),
		LocalAddresses: append([]string(nil), localAddresses...),
	}
}
