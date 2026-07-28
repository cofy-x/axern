package imagefsd

import "testing"

func TestDaemon_BuildMountArgs(t *testing.T) {
	tests := []struct {
		name        string
		daemon      *Daemon
		contains    []string
		notContains []string
	}{
		{
			name: "nydus with bounded readahead",
			daemon: &Daemon{
				nodeID: "node-test",
				meta: DaemonMeta{
					SourceType:           SourceTypeNydus,
					CacheDir:             "/var/cache/nydus",
					BootstrapPath:        "/var/bootstrap",
					ReadaheadWorkers:     2,
					ReadaheadWindowBytes: 33554432,
					DecodedCacheBytes:    8388608,
				},
			},
			contains: []string{"--node-id", "node-test", "--nydus-readahead-workers", "2", "--nydus-readahead-window-bytes", "33554432", "--nydus-decoded-cache-bytes", "8388608"},
		},
		{
			name: "OSS daemon",
			daemon: &Daemon{
				nodeID: "node-test",
				meta: DaemonMeta{
					ID:            "oss-daemon",
					Name:          "test-oss",
					MountPoint:    "/mnt/oss",
					DaemonLogPath: "/var/log/daemon.log",
					PidFilePath:   "/var/run/daemon.pid",
					CachePath:     "/var/cache/daemon",
					CfgPath:       "/etc/daemon/config.json",
					ChunkDBDir:    "/var/chunkdb",
					ImageMetaDir:  "/var/meta",
					SourceType:    "oss",
				},
			},
			contains: []string{
				"mount",
				"--daemon",
				"--src", "oss",
				"--cache-file", "/var/cache/daemon",
				"--name", "test-oss",
				"--mountpoint", "/mnt/oss",
			},
			notContains: []string{"--bootstrap", "--cache-dir"},
		},
		{
			name: "Nydus daemon",
			daemon: &Daemon{
				nodeID: "node-test",
				meta: DaemonMeta{
					ID:            "nydus-daemon",
					Name:          "test-nydus",
					MountPoint:    "/mnt/nydus",
					DaemonLogPath: "/var/log/daemon.log",
					PidFilePath:   "/var/run/daemon.pid",
					CacheDir:      "/var/cache/nydus",
					BootstrapPath: "/var/bootstrap/image.boot",
					CfgPath:       "/etc/daemon/config.json",
					ChunkDBDir:    "/var/chunkdb",
					ImageMetaDir:  "/var/meta",
					SourceType:    "nydus",
				},
			},
			contains: []string{
				"mount",
				"--daemon",
				"--src", "nydus",
				"--cache-dir", "/var/cache/nydus",
				"--bootstrap", "/var/bootstrap/image.boot",
				"--name", "test-nydus",
				"--mountpoint", "/mnt/nydus",
			},
			notContains: []string{"--cache-file"},
		},
		{
			name: "Default to OSS when SourceType empty",
			daemon: &Daemon{
				nodeID: "node-test",
				meta: DaemonMeta{
					Name:          "default-daemon",
					MountPoint:    "/mnt/default",
					DaemonLogPath: "/var/log/daemon.log",
					PidFilePath:   "/var/run/daemon.pid",
					CachePath:     "/var/cache/daemon",
					CfgPath:       "/etc/daemon/config.json",
					ChunkDBDir:    "/var/chunkdb",
					ImageMetaDir:  "/var/meta",
					SourceType:    "",
				},
			},
			contains: []string{
				"--src", "oss",
				"--cache-file", "/var/cache/daemon",
			},
			notContains: []string{"--bootstrap"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.daemon.buildMountArgs()

			for _, s := range tt.contains {
				found := false
				for _, arg := range args {
					if arg == s {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected arg %q not found in %v", s, args)
				}
			}

			for _, s := range tt.notContains {
				for _, arg := range args {
					if arg == s {
						t.Errorf("Unexpected arg %q found in %v", s, args)
					}
				}
			}
		})
	}
}
