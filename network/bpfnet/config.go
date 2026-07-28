package bpfnet

import (
	"path/filepath"
	"strings"
)

const (
	DefaultPinPath            = "/sys/fs/bpf/axern/bpfnet"
	DefaultStatePath          = "/var/run/axern/bpfnet"
	DefaultMapSize            = 16384
	DefaultSNATMapSize        = 262144
	SNATAllocatorPortMin      = 10000
	SNATAllocatorPortMax      = 65535
	SNATAllocatorPortAttempts = 256
)

type Config struct {
	UplinkDevices      []string
	PinPath            string
	StatePath          string
	MapSize            int
	SNATMapSize        int
	LocalOutCompat     bool
	NativeRoutingCIDRs []string
	IptablesFallback   bool
}

func (c Config) WithDefaults() Config {
	if c.PinPath == "" {
		c.PinPath = DefaultPinPath
	}
	if c.StatePath == "" {
		c.StatePath = defaultStatePath(c.PinPath)
	}
	if c.MapSize <= 0 {
		c.MapSize = DefaultMapSize
	}
	if c.SNATMapSize <= 0 {
		c.SNATMapSize = DefaultSNATMapSize
	}
	return c
}

func defaultStatePath(pinPath string) string {
	clean := filepath.Clean(pinPath)
	if clean == "." || clean == "" {
		return DefaultStatePath
	}
	if clean == "/sys/fs/bpf" || strings.HasPrefix(clean, "/sys/fs/bpf/") {
		return DefaultStatePath
	}
	return clean
}
