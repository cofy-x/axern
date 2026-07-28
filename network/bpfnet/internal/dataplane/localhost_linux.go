//go:build linux

package dataplane

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

const cgroupV2Root = "/sys/fs/cgroup"

type cgroupLinkSpec struct {
	name    string
	attach  ebpf.AttachType
	program *ebpf.Program
}

func (d *linuxDataplane) ensureLocalhostLinks() (bool, error) {
	if err := ensureCgroupV2Root(); err != nil {
		return false, err
	}

	linkDir := filepath.Join(d.cfg.PinPath, "links")
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		return false, fmt.Errorf("create localhost link pin path: %w", err)
	}

	specs := []cgroupLinkSpec{
		{
			name:    "localhost-connect4",
			attach:  ebpf.AttachCGroupInet4Connect,
			program: d.objects.LocalhostConnect4,
		},
		{
			name:    "localhost-getpeer4",
			attach:  ebpf.AttachCgroupInet4GetPeername,
			program: d.objects.LocalhostGetpeername4,
		},
		{
			name:    "localhost-release",
			attach:  ebpf.AttachCgroupInetSockRelease,
			program: d.objects.LocalhostSockRelease,
		},
	}

	for _, spec := range specs {
		if err := ensurePinnedCgroupLink(filepath.Join(linkDir, spec.name), spec.attach, spec.program); err != nil {
			return false, err
		}
	}

	return true, nil
}

func ensureCgroupV2Root() error {
	if _, err := os.Stat(filepath.Join(cgroupV2Root, "cgroup.controllers")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("localhost tcp dnat requires cgroup v2 root at %s", cgroupV2Root)
		}
		return fmt.Errorf("stat cgroup v2 root: %w", err)
	}
	return nil
}

func ensurePinnedCgroupLink(path string, attach ebpf.AttachType, program *ebpf.Program) error {
	if program == nil {
		return fmt.Errorf("missing localhost program for %s", path)
	}

	existing, err := link.LoadPinnedLink(path, nil)
	if err == nil {
		defer existing.Close()
		if err := existing.Update(program); err != nil {
			return fmt.Errorf("update pinned localhost link %s: %w", path, err)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return fmt.Errorf("remove stale localhost link pin %s: %w", path, removeErr)
		}
	}

	attached, err := link.AttachCgroup(link.CgroupOptions{
		Path:    cgroupV2Root,
		Attach:  attach,
		Program: program,
	})
	if err != nil {
		return fmt.Errorf("attach localhost cgroup program %s: %w", path, err)
	}
	defer attached.Close()

	if err := attached.Pin(path); err != nil {
		if errors.Is(err, link.ErrNotSupported) {
			return fmt.Errorf("pin localhost cgroup link %s: %w", path, err)
		}
		return fmt.Errorf("pin localhost cgroup link %s: %w", path, err)
	}
	return nil
}
