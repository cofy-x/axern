//go:build !linux

package dataplane

func CollectKernelStats(Config) KernelStats {
	return KernelStats{}
}

func CollectSNATMapStats(Config) SNATMapStats {
	return SNATMapStats{}
}

func CollectAttachmentReadiness(_ Config, uplinks []string) AttachmentReadiness {
	return AttachmentReadiness{
		UplinkDevices: append([]string(nil), uplinks...),
	}
}
