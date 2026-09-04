//go:build linux

package dataplane

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
)

var pinnedMapNames = []string{
	serviceMapName,
	statsMapName,
	localAddrMapName,
	revNatMapName,
	configMapName,
	hostNetnsCookieMapName,
	uplinkAddrMapName,
	nativeRouteMapName,
	snatFwdMapName,
	snatRevMapName,
	snatRevMarkerMapName,
	localhostSockMapName,
}

func (d *linuxDataplane) UpsertService(service Service) error {
	if !d.loaded {
		return nil
	}

	proto, ok := serviceProtocolNumber(service.Protocol)
	if !ok {
		return nil
	}

	key := tcprog.DataplaneServiceKey{
		Proto:    proto,
		HostPort: service.HostPort,
	}
	value := tcprog.DataplaneServiceValue{
		TargetIp:   ipv4ToUint32(service.TargetIP),
		TargetPort: service.TargetPort,
	}
	return d.objects.ServiceMap.Update(key, value, ebpf.UpdateAny)
}

func (d *linuxDataplane) DeleteService(service Service) error {
	if !d.loaded {
		return nil
	}

	proto, ok := serviceProtocolNumber(service.Protocol)
	if !ok {
		return nil
	}

	key := tcprog.DataplaneServiceKey{
		Proto:    proto,
		HostPort: service.HostPort,
	}
	if err := d.objects.ServiceMap.Delete(key); err != nil && !isMapKeyNotExist(err) {
		return err
	}
	return nil
}

func (d *linuxDataplane) loadObjects() error {
	spec, err := tcprog.LoadDataplane()
	if err != nil {
		return fmt.Errorf("load dataplane spec: %w", err)
	}

	if err := os.MkdirAll(d.cfg.PinPath, 0755); err != nil {
		return fmt.Errorf("create bpfnet pin path: %w", err)
	}

	mapSize := d.cfg.MapSize
	snatMapSize := d.cfg.SNATMapSize
	if snatMapSize <= 0 {
		snatMapSize = mapSize
	}
	for name, specMap := range spec.Maps {
		switch name {
		case snatFwdMapName, snatRevMapName, snatRevMarkerMapName:
			specMap.MaxEntries = uint32(snatMapSize)
		case serviceMapName, revNatMapName, localhostSockMapName:
			specMap.MaxEntries = uint32(mapSize)
		}
		specMap.Pinning = ebpf.PinByName
	}

	opts := &ebpf.CollectionOptions{
		Maps: ebpf.MapOptions{
			PinPath: d.cfg.PinPath,
		},
	}
	if err := d.loadAndAssign(spec, opts); err != nil {
		if reconcileErr := d.clearPinnedArtifacts(); reconcileErr != nil {
			return fmt.Errorf("reconcile stale pinned dataplane artifacts after load failure: %w", reconcileErr)
		}
		if retryErr := d.loadAndAssign(spec, opts); retryErr != nil {
			return retryErr
		}
	}
	d.loaded = true
	return nil
}

func (d *linuxDataplane) loadAndAssign(spec *ebpf.CollectionSpec, opts *ebpf.CollectionOptions) error {
	if err := spec.LoadAndAssign(&d.objects, opts); err != nil {
		var verifierErr *ebpf.VerifierError
		if errors.As(err, &verifierErr) {
			return fmt.Errorf("load dataplane objects: %+v", verifierErr)
		}
		return fmt.Errorf("load dataplane objects: %w", err)
	}
	return nil
}

func (d *linuxDataplane) pinPrograms() error {
	programDir := filepath.Join(d.cfg.PinPath, "programs")
	if err := os.MkdirAll(programDir, 0755); err != nil {
		return fmt.Errorf("create program pin path: %w", err)
	}

	pins := map[string]*ebpf.Program{
		filepath.Join(programDir, "ingress"):            d.objects.DataplaneIngress,
		filepath.Join(programDir, "egress"):             d.objects.DataplaneEgress,
		filepath.Join(programDir, "localhost-connect4"): d.objects.LocalhostConnect4,
		filepath.Join(programDir, "localhost-getpeer4"): d.objects.LocalhostGetpeername4,
		filepath.Join(programDir, "localhost-release"):  d.objects.LocalhostSockRelease,
	}
	for path, program := range pins {
		if program == nil {
			continue
		}
		if err := pinProgram(path, program); err != nil {
			return fmt.Errorf("pin program %s: %w", path, err)
		}
	}
	return nil
}

func pinProgram(path string, program *ebpf.Program) error {
	if err := program.Pin(path); err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		// The Program may still remember this path after an older reconcile
		// removed it. Clear the remembered path so Pin recreates the bpffs node.
		if err := program.Unpin(); err != nil {
			return err
		}
		return program.Pin(path)
	} else if !pinPathExists(err) {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace stale pinned program: %w", err)
	}
	return program.Pin(path)
}

func pinPathExists(err error) bool {
	return errors.Is(err, os.ErrExist)
}

func (d *linuxDataplane) clearPinnedArtifacts() error {
	for _, name := range pinnedMapNames {
		path := filepath.Join(d.cfg.PinPath, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove pinned map %s: %w", path, err)
		}
	}

	for _, dir := range []string{
		filepath.Join(d.cfg.PinPath, "programs"),
		filepath.Join(d.cfg.PinPath, "links"),
	} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove pinned dataplane dir %s: %w", dir, err)
		}
	}

	d.objects = tcprog.DataplaneObjects{}
	d.loaded = false
	return nil
}
