//go:build linux

package hostlinux

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CgroupMemoryDomain is the immutable identity and control state of one
// allocation parent plus its OCI workload leaf.
type CgroupMemoryDomain struct {
	BootID        string
	MountIdentity string
	ParentInode   uint64
	LeafInode     uint64
	LimitBytes    int64
	SwapMaxBytes  int64
	OOMGroup      bool
	InitialEvents map[string]uint64
}

// CgroupMemoryObservation is the bounded host-kernel view used for reporting,
// OOM diagnosis, cleanup debt, and periodic enforcement audits.
type CgroupMemoryObservation struct {
	CurrentBytes  int64
	PeakBytes     int64
	PeakAvailable bool
	SwapCurrent   int64
	Stat          map[string]int64
	Events        map[string]uint64
	PSIAvailable  bool
	PSISomeAvg10  float64
	PSIFullAvg10  float64
	PSISomeTotal  uint64
	PSIFullTotal  uint64
}

// ConfigureCgroupMemoryDomain establishes one hard boundary at both the
// allocation parent and OCI workload leaf. The duplicate memory.max prevents
// an accidental process move between those two allocation-owned cgroups from
// escaping the declared limit. memory.oom.group is required on both levels:
// an OOM invoked by the leaf does not inherit the ancestor's grouping policy.
func ConfigureCgroupMemoryDomain(parentPath, workloadPath string, limitBytes int64) (*CgroupMemoryDomain, error) {
	if limitBytes <= 0 {
		return nil, fmt.Errorf("sandbox memory limit must be positive")
	}
	parentDir := resourceDirForCgroupPath(parentPath)
	workloadDir := resourceDirForCgroupPath(workloadPath)
	if filepath.Dir(workloadDir) != parentDir {
		return nil, fmt.Errorf("workload cgroup %s is not the direct child of allocation cgroup %s", workloadPath, parentPath)
	}

	if err := configureCgroupMemoryControls(parentDir, workloadDir, limitBytes); err != nil {
		return nil, err
	}

	domain, err := InspectCgroupMemoryDomain(parentPath, workloadPath)
	if err != nil {
		return nil, err
	}
	if domain.LimitBytes != limitBytes || domain.SwapMaxBytes != 0 || !domain.OOMGroup {
		return nil, fmt.Errorf("sandbox memory control readback mismatch: limit=%d swap=%d oom_group=%t", domain.LimitBytes, domain.SwapMaxBytes, domain.OOMGroup)
	}
	return domain, nil
}

func configureCgroupMemoryControls(parentDir, workloadDir string, limitBytes int64) error {
	for _, dir := range []string{parentDir, workloadDir} {
		if err := writeAndVerifyCgroupValue(filepath.Join(dir, "memory.max"), strconv.FormatInt(limitBytes, 10)); err != nil {
			return err
		}
		if err := writeAndVerifyCgroupValue(filepath.Join(dir, "memory.swap.max"), "0"); err != nil {
			return err
		}
		if err := writeAndVerifyCgroupValue(filepath.Join(dir, "memory.oom.group"), "1"); err != nil {
			return err
		}
	}
	return nil
}

func VerifyCgroupMemoryDomain(parentPath, workloadPath string, limitBytes int64, wantBootID, wantMountIdentity string, wantParentInode, wantLeafInode uint64) error {
	domain, err := InspectCgroupMemoryDomain(parentPath, workloadPath)
	if err != nil {
		return err
	}
	if domain.LimitBytes != limitBytes || domain.SwapMaxBytes != 0 || !domain.OOMGroup {
		return fmt.Errorf("sandbox memory controls changed: limit=%d swap=%d oom_group=%t", domain.LimitBytes, domain.SwapMaxBytes, domain.OOMGroup)
	}
	if wantBootID != "" && domain.BootID != wantBootID {
		return fmt.Errorf("cgroup boot identity changed")
	}
	if wantMountIdentity != "" && domain.MountIdentity != wantMountIdentity {
		return fmt.Errorf("cgroup mount identity changed")
	}
	if wantParentInode != 0 && domain.ParentInode != wantParentInode {
		return fmt.Errorf("allocation cgroup identity changed")
	}
	if wantLeafInode != 0 && domain.LeafInode != wantLeafInode {
		return fmt.Errorf("workload cgroup identity changed")
	}
	return nil
}

func InspectCgroupMemoryDomain(parentPath, workloadPath string) (*CgroupMemoryDomain, error) {
	domain, err := InspectCgroupMemoryParent(parentPath)
	if err != nil {
		return nil, err
	}
	workloadDir := resourceDirForCgroupPath(workloadPath)
	leafInode, err := cgroupInode(workloadDir)
	if err != nil {
		return nil, err
	}
	leafLimit, err := readCgroupInt64(filepath.Join(workloadDir, "memory.max"))
	if err != nil {
		return nil, err
	}
	if domain.LimitBytes != leafLimit {
		return nil, fmt.Errorf("allocation and workload memory.max differ: parent=%d leaf=%d", domain.LimitBytes, leafLimit)
	}
	leafSwap, err := readCgroupInt64(filepath.Join(workloadDir, "memory.swap.max"))
	if err != nil {
		return nil, err
	}
	if domain.SwapMaxBytes != leafSwap {
		return nil, fmt.Errorf("allocation and workload memory.swap.max differ: parent=%d leaf=%d", domain.SwapMaxBytes, leafSwap)
	}
	leafOOMGroup, err := readCgroupInt64(filepath.Join(workloadDir, "memory.oom.group"))
	if err != nil {
		return nil, err
	}
	if domain.OOMGroup != (leafOOMGroup == 1) {
		return nil, fmt.Errorf("allocation and workload memory.oom.group differ: parent=%t leaf=%d", domain.OOMGroup, leafOOMGroup)
	}
	domain.LeafInode = leafInode
	return domain, nil
}

// InspectCgroupMemoryParent reads the durable allocation boundary without
// requiring the runtime-owned workload leaf to exist. Runtime deletion may
// remove that leaf before page-cache reclaim and retirement of the allocation
// parent have converged.
func InspectCgroupMemoryParent(parentPath string) (*CgroupMemoryDomain, error) {
	parentDir := resourceDirForCgroupPath(parentPath)
	parentInode, err := cgroupInode(parentDir)
	if err != nil {
		return nil, err
	}
	parentLimit, err := readCgroupInt64(filepath.Join(parentDir, "memory.max"))
	if err != nil {
		return nil, err
	}
	parentSwap, err := readCgroupInt64(filepath.Join(parentDir, "memory.swap.max"))
	if err != nil {
		return nil, err
	}
	oomGroup, err := readCgroupInt64(filepath.Join(parentDir, "memory.oom.group"))
	if err != nil {
		return nil, err
	}
	bootID, err := CurrentBootID()
	if err != nil {
		return nil, err
	}
	_, mounted, mountIdentity, err := mountedFilesystemFacts("/sys/fs/cgroup")
	if err != nil {
		return nil, err
	}
	if !mounted || mountIdentity == "" {
		return nil, fmt.Errorf("unified cgroup mount identity is unavailable")
	}
	events, err := readUint64KeyValueFile(filepath.Join(parentDir, "memory.events"))
	if err != nil {
		return nil, err
	}
	return &CgroupMemoryDomain{
		BootID: bootID, MountIdentity: mountIdentity,
		ParentInode: parentInode,
		LimitBytes:  parentLimit, SwapMaxBytes: parentSwap,
		OOMGroup: oomGroup == 1, InitialEvents: events,
	}, nil
}

func ReadCgroupMemoryObservation(cgroupPath string) (*CgroupMemoryObservation, error) {
	return readCgroupMemoryObservationDir(resourceDirForCgroupPath(cgroupPath))
}

func readCgroupMemoryObservationDir(dir string) (*CgroupMemoryObservation, error) {
	current, err := readCgroupInt64(filepath.Join(dir, "memory.current"))
	if err != nil {
		return nil, err
	}
	peak := current
	peakAvailable := true
	if kernelPeak, err := readCgroupInt64(filepath.Join(dir, "memory.peak")); err != nil {
		if !optionalCgroupMemoryObservationUnavailable(err) {
			return nil, err
		}
		peakAvailable = false
	} else {
		peak = kernelPeak
	}
	if peak < current {
		return nil, fmt.Errorf("cgroup memory.peak %d is below memory.current %d", peak, current)
	}
	swapCurrent, err := readCgroupInt64(filepath.Join(dir, "memory.swap.current"))
	if err != nil {
		return nil, err
	}
	stat, err := readInt64KeyValueFile(filepath.Join(dir, "memory.stat"))
	if err != nil {
		return nil, err
	}
	events, err := readUint64KeyValueFile(filepath.Join(dir, "memory.events"))
	if err != nil {
		return nil, err
	}
	obs := &CgroupMemoryObservation{CurrentBytes: current, PeakBytes: peak, PeakAvailable: peakAvailable, SwapCurrent: swapCurrent, Stat: stat, Events: events}
	if err := readMemoryPressure(filepath.Join(dir, "memory.pressure"), obs); err != nil {
		// Per-cgroup PSI is an optional observability facility. Its absence must
		// not invalidate otherwise verified memory.max, swap, OOM-event, or PID
		// enforcement. Keep malformed or unreadable files fail-closed so a
		// provider cannot publish misleading pressure values.
		if !optionalCgroupMemoryObservationUnavailable(err) {
			return nil, err
		}
	} else {
		obs.PSIAvailable = true
	}
	return obs, nil
}

// optionalCgroupMemoryObservationUnavailable identifies kernel interfaces that
// are optional for diagnostics and do not participate in hard-limit
// enforcement. Some cgroup v2 deployments expose the files but return
// EOPNOTSUPP when the kernel or hierarchy does not provide the facility. Treat
// that exactly like an absent optional file while preserving every other error
// as a hard observation failure.
func optionalCgroupMemoryObservationUnavailable(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.EOPNOTSUPP)
}

func ReclaimCgroupMemory(cgroupPath string) error {
	path := filepath.Join(resourceDirForCgroupPath(cgroupPath), "memory.reclaim")
	current, err := readCgroupInt64(filepath.Join(resourceDirForCgroupPath(cgroupPath), "memory.current"))
	if err != nil {
		return err
	}
	if current <= 0 {
		return nil
	}
	return writeCgroupFile(path, strconv.FormatInt(current, 10))
}

func writeAndVerifyCgroupValue(path, value string) error {
	if err := writeCgroupFile(path, value); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read back %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) != value {
		return fmt.Errorf("cgroup control readback mismatch for %s: got=%q want=%q", path, strings.TrimSpace(string(data)), value)
	}
	return nil
}

func writeCgroupFile(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func readCgroupInt64(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "max" {
		return -1, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return parsed, nil
}

func readInt64KeyValueFile(path string) (map[string]int64, error) {
	values := map[string]int64{}
	err := scanKeyValueFile(path, func(key, value string) error {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		values[key] = parsed
		return nil
	})
	return values, err
}

func readUint64KeyValueFile(path string) (map[string]uint64, error) {
	values := map[string]uint64{}
	err := scanKeyValueFile(path, func(key, value string) error {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}
		values[key] = parsed
		return nil
	})
	return values, err
}

func scanKeyValueFile(path string, consume func(string, string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			return fmt.Errorf("malformed cgroup value in %s: %q", path, scanner.Text())
		}
		if err := consume(fields[0], fields[1]); err != nil {
			return fmt.Errorf("parse %s value %q: %w", path, scanner.Text(), err)
		}
	}
	return scanner.Err()
}

func readMemoryPressure(path string, obs *CgroupMemoryObservation) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	seen := map[string]bool{"some": false, "full": false}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || (fields[0] != "some" && fields[0] != "full") {
			return fmt.Errorf("malformed PSI value in %s: %q", path, scanner.Text())
		}
		if seen[fields[0]] {
			return fmt.Errorf("duplicate PSI %s record in %s", fields[0], path)
		}
		seen[fields[0]] = true
		values := map[string]string{}
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				return fmt.Errorf("malformed PSI field in %s: %q", path, field)
			}
			values[key] = value
		}
		avg10, err := strconv.ParseFloat(values["avg10"], 64)
		if err != nil {
			return fmt.Errorf("parse PSI avg10 in %s: %w", path, err)
		}
		total, err := strconv.ParseUint(values["total"], 10, 64)
		if err != nil {
			return fmt.Errorf("parse PSI total in %s: %w", path, err)
		}
		if fields[0] == "some" {
			obs.PSISomeAvg10, obs.PSISomeTotal = avg10, total
		} else {
			obs.PSIFullAvg10, obs.PSIFullTotal = avg10, total
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !seen["some"] || !seen["full"] {
		return fmt.Errorf("incomplete PSI value in %s: some=%t full=%t", path, seen["some"], seen["full"])
	}
	return nil
}

func cgroupInode(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		return 0, fmt.Errorf("cgroup inode identity unavailable for %s", path)
	}
	return stat.Ino, nil
}
