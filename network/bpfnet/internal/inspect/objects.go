package inspect

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
)

func ListObjects(pinPath string) []ObjectInfo {
	var out []ObjectInfo
	for _, name := range knownMaps {
		out = append(out, inspectMap(pinPath, name))
	}
	for _, name := range programPins {
		out = append(out, inspectProgram(pinPath, name))
	}
	for _, name := range linkPins {
		out = append(out, inspectLink(pinPath, name))
	}
	return out
}

func inspectMap(pinPath, name string) ObjectInfo {
	path := filepath.Join(pinPath, name)
	info := ObjectInfo{Kind: "map", Name: name, Path: path}
	if _, err := os.Stat(path); err == nil {
		info.Present = true
	} else if !errors.Is(err, os.ErrNotExist) {
		info.Error = err.Error()
	}

	m, err := ebpf.LoadPinnedMap(path, nil)
	if err != nil {
		if info.Present && info.Error == "" && !errors.Is(err, os.ErrNotExist) {
			info.Error = err.Error()
		}
		return info
	}
	defer m.Close()

	info.Openable = true
	if mapInfo, err := m.Info(); err == nil {
		info.Type = mapInfo.Type.String()
		info.KeySize = mapInfo.KeySize
		info.ValueSize = mapInfo.ValueSize
		info.MaxEntries = mapInfo.MaxEntries
	}
	if count, err := countEntries(m, name); err == nil {
		info.Entries = count
	}
	return info
}

func inspectProgram(pinPath, name string) ObjectInfo {
	path := filepath.Join(pinPath, "programs", name)
	info := ObjectInfo{Kind: "program", Name: name, Path: path}
	if _, err := os.Stat(path); err == nil {
		info.Present = true
	} else if !errors.Is(err, os.ErrNotExist) {
		info.Error = err.Error()
	}

	program, err := ebpf.LoadPinnedProgram(path, nil)
	if err != nil {
		if info.Present && info.Error == "" && !errors.Is(err, os.ErrNotExist) {
			info.Error = err.Error()
		}
		return info
	}
	defer program.Close()

	info.Openable = true
	if programInfo, err := program.Info(); err == nil {
		info.Type = programInfo.Type.String()
	}
	return info
}

func countEntries(m *ebpf.Map, name string) (int, error) {
	entries, _, err := dumpEntries(m, name, 0, highChurnMaps[name] || name == MapHostNetNS)
	return len(entries), err
}
