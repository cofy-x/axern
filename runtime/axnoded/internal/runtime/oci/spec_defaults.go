package oci

import (
	"github.com/cofy-x/axern/runtime/axnoded/config"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	ignoreResourceFieldAnnoKey = "io.axnoded/ignore-resource-field"
	linuxCapabilitiesAnnoKey   = "linux-capabilities"
)

var defaultLinuxCapabilities = []string{
	"CAP_CHOWN",
	"CAP_DAC_OVERRIDE",
	"CAP_FOWNER",
	"CAP_FSETID",
	"CAP_KILL",
	"CAP_SETGID",
	"CAP_SETUID",
	"CAP_SETPCAP",
	"CAP_NET_BIND_SERVICE",
	"CAP_NET_RAW",
	"CAP_SYS_CHROOT",
	"CAP_MKNOD",
	"CAP_AUDIT_WRITE",
	"CAP_SETFCAP",
}

func defaultBundleSpec() *spec.Spec {
	return &spec.Spec{
		Version: "1.0.0",
		Process: &spec.Process{
			User: spec.User{
				UID: 0,
				GID: 0,
			},
			Env: []string{
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"TERM=xterm",
			},
			Cwd: "/",
			Capabilities: &spec.LinuxCapabilities{
				Bounding:    append([]string(nil), defaultLinuxCapabilities...),
				Effective:   append([]string(nil), defaultLinuxCapabilities...),
				Inheritable: append([]string(nil), defaultLinuxCapabilities...),
				Permitted:   append([]string(nil), defaultLinuxCapabilities...),
			},
			Rlimits: []spec.POSIXRlimit{
				{
					Type: "RLIMIT_NOFILE",
					Hard: defaultNoFileLimit,
					Soft: defaultNoFileLimit,
				},
			},
		},
		Root: &spec.Root{
			Path:     "rootfs",
			Readonly: true,
		},
		Hostname: "axnoded",
		Mounts: []spec.Mount{
			{
				Destination: "/proc",
				Type:        "proc",
				Source:      "proc",
			},
			{
				Destination: "/dev",
				Type:        "tmpfs",
				Source:      "tmpfs",
			},
			{
				Destination: "/dev/pts",
				Type:        "devpts",
				Source:      "devpts",
				Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"},
			},
			{
				Destination: "/sys",
				Type:        "sysfs",
				Source:      "sysfs",
				Options:     []string{"nosuid", "noexec", "nodev", "ro"},
			},
		},
		Annotations: map[string]string{
			"netac-rules": config.NetAcRule,
		},
		Linux: &spec.Linux{
			Namespaces: []spec.LinuxNamespace{
				{Type: spec.PIDNamespace},
				{Type: spec.NetworkNamespace},
				{Type: spec.IPCNamespace},
				{Type: spec.UTSNamespace},
				{Type: spec.MountNamespace},
			},
		},
	}
}
