//go:build linux

package hostlinux

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

const FilestoreCapabilitiesFile = ".axern-filestore-capabilities.json"

type FilestoreCapabilities struct {
	OverlayReady      bool      `json:"overlay_ready"`
	EROFSReady        bool      `json:"erofs_ready"`
	ProjectQuotaReady bool      `json:"project_quota_ready"`
	FilesystemType    string    `json:"filesystem_type"`
	MountIdentity     string    `json:"mount_identity"`
	EROFSProbeError   string    `json:"erofs_probe_error,omitempty"`
	ProbedAt          time.Time `json:"probed_at"`
}

func CurrentBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("kernel boot ID is empty")
	}
	return value, nil
}

// ProbeCgroupMemoryLimit verifies the node-level prerequisites for admitting a
// memory-limited sandbox. Runtime-specific PID attribution is verified later,
// after each sandbox starts.
func ProbeCgroupMemoryLimit(rootName string) (result error) {
	driver, err := os2.DefaultCgroupDriver()
	if err != nil {
		return fmt.Errorf("load cgroup driver: %w", err)
	}
	return probeCgroupMemoryLimit(driver, rootName, VerifyCgroupMemoryLimit)
}

func VerifyCgroupMemoryLimit(cgroupPath string, want int64) error {
	if want <= 0 {
		return nil
	}
	dir := resourceDirForCgroupPath(cgroupPath)
	paths := []string{filepath.Join(dir, "memory.max"), filepath.Join(dir, "memory.limit_in_bytes")}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		got, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if got != want {
			return fmt.Errorf("memory limit readback mismatch: got=%d want=%d", got, want)
		}
		return nil
	}
	return fmt.Errorf("memory controller is unavailable for %s", cgroupPath)
}

func VerifyPIDInCgroup(cgroupPath string, pid int) error {
	if cgroupPath == "" || pid <= 0 {
		return fmt.Errorf("cgroup path and pid are required")
	}
	file, err := os.Open(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return err
	}
	defer file.Close()
	want := "/" + strings.Trim(strings.TrimSpace(cgroupPath), "/")
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 3)
		if len(parts) != 3 {
			continue
		}
		got := filepath.Clean(parts[2])
		if got == want || strings.HasPrefix(got, strings.TrimSuffix(want, "/")+"/") {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("pid %d is not attributed to cgroup %s", pid, want)
}

func VerifyCgroupPIDs(cgroupPath string, requiredPID, minimum int) error {
	if err := VerifyPIDInCgroup(cgroupPath, requiredPID); err != nil {
		return err
	}
	seen, err := cgroupPIDs(cgroupPath)
	if err != nil {
		return err
	}
	if len(seen) < minimum {
		return fmt.Errorf("cgroup %s has %d attributed host pids, need at least %d", cgroupPath, len(seen), minimum)
	}
	return nil
}

func cgroupPIDs(cgroupPath string) (map[int]struct{}, error) {
	seen := map[int]struct{}{}
	root := resourceDirForCgroupPath(cgroupPath)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "cgroup.procs" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(line)
			if err == nil && pid > 0 {
				seen[pid] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inventory cgroup pids: %w", err)
	}
	return seen, nil
}

func VerifyRunscCgroupProcesses(cgroupPath string, sentryPID int) error {
	if err := VerifyPIDInCgroup(cgroupPath, sentryPID); err != nil {
		return err
	}
	pids, err := cgroupPIDs(cgroupPath)
	if err != nil {
		return err
	}
	sentryFound, goferFound := false, false
	for pid := range pids {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
		if err != nil {
			continue
		}
		command := strings.ToLower(strings.ReplaceAll(string(data), "\x00", " "))
		if pid == sentryPID && (strings.Contains(command, "sandbox") || strings.Contains(command, " boot")) {
			sentryFound = true
		}
		if strings.Contains(command, "gofer") {
			goferFound = true
		}
	}
	if !sentryFound || !goferFound {
		return fmt.Errorf("runsc cgroup %s attribution incomplete: sentry=%t gofer=%t pids=%d", cgroupPath, sentryFound, goferFound, len(pids))
	}
	return nil
}

func ReadCgroupMemoryBreakdown(cgroupPath string) (map[string]int64, error) {
	dir := resourceDirForCgroupPath(cgroupPath)
	values := make(map[string]int64)
	for _, file := range []string{"memory.stat", "memory.events"} {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse %s %q: %w", file, line, err)
			}
			key := fields[0]
			if file == "memory.events" {
				key = "event_" + key
			}
			values[key] = value
		}
	}
	return values, nil
}

func cloneResource(resource *runtimeapi.LinuxContainerResources) *runtimeapi.LinuxContainerResources {
	if resource == nil {
		return nil
	}
	return proto.Clone(resource).(*runtimeapi.LinuxContainerResources)
}

func hasCPUResource(resource *runtimeapi.LinuxContainerResources) bool {
	return resource != nil && (resource.CpuShares > 0 || resource.CpuQuota > 0 || resource.CpuPeriod > 0 || resource.CpusetCpus != "" || resource.CpusetMems != "")
}

func hasMemoryResource(resource *runtimeapi.LinuxContainerResources) bool {
	return resource != nil && (resource.MemoryLimitInBytes > 0 || resource.MemorySwapLimitInBytes > 0)
}

func resourceDirForCgroupPath(cgroupPath string) string {
	return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(filepath.Clean("/"+cgroupPath), "/"))
}

func RuntimeCgroupPath(driver os2.CgroupDriver, cgroupPath string) string {
	if cgroupPath == "" {
		return ""
	}
	mode := ""
	if driver != nil {
		mode = driver.Mode()
	}
	return os2.WorkloadGroup(cgroupPath, mode)
}

func SanitizeResourceForDriver(driver os2.CgroupDriver, cgroupPath string, resource *runtimeapi.LinuxContainerResources) *runtimeapi.LinuxContainerResources {
	if resource == nil {
		return nil
	}
	if driver == nil || driver.Mode() != os2.CgroupModeV2 {
		return resource
	}

	sanitized := cloneResource(resource)
	resourceDir := resourceDirForCgroupPath(cgroupPath)
	if hasCPUResource(sanitized) {
		if _, err := os.Stat(filepath.Join(resourceDir, "cpu.weight")); err != nil {
			sanitized.CpuShares = 0
			sanitized.CpuQuota = 0
			sanitized.CpuPeriod = 0
			sanitized.CpusetCpus = ""
			sanitized.CpusetMems = ""
		}
	}
	if hasMemoryResource(sanitized) {
		if _, err := os.Stat(filepath.Join(resourceDir, "memory.max")); err != nil {
			sanitized.MemoryLimitInBytes = 0
			sanitized.MemorySwapLimitInBytes = 0
		}
	}

	if !hasCPUResource(sanitized) && !hasMemoryResource(sanitized) && len(sanitized.HugepageLimits) == 0 && len(sanitized.Unified) == 0 {
		return nil
	}
	return sanitized
}

func UpdateCgroup(cgroupPath string, resource *runtimeapi.LinuxContainerResources) error {
	if resource == nil {
		return nil
	}

	driver, err := os2.DefaultCgroupDriver()
	if err != nil {
		return err
	}
	cgroup, err := driver.Load(cgroupPath)
	if err != nil {
		return err
	}

	var cpu spec.LinuxCPU
	if resource.CpuShares > 0 {
		cpu.Shares = &resource.CpuShares
	}
	if resource.CpuQuota > 0 {
		cpu.Quota = &resource.CpuQuota
	}
	if resource.CpuPeriod > 0 {
		cpu.Period = &resource.CpuPeriod
	}
	if resource.CpusetCpus != "" {
		cpu.Cpus = resource.CpusetCpus
	}
	if resource.CpusetMems != "" {
		cpu.Mems = resource.CpusetMems
	}
	var mem spec.LinuxMemory
	if resource.MemoryLimitInBytes > 0 {
		mem.Limit = &resource.MemoryLimitInBytes
	}
	if resource.MemorySwapLimitInBytes > 0 {
		mem.Swap = &resource.MemorySwapLimitInBytes
	}

	cgroupResource := &spec.LinuxResources{
		CPU:    &cpu,
		Memory: &mem,
	}

	return cgroup.Update(cgroupResource)
}

func IsPathReadOnly(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("path is empty")
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false, fmt.Errorf("statfs %s: %w", path, err)
	}
	return stat.Flags&unix.ST_RDONLY != 0, nil
}

func mountedFilesystem(dir string) (string, bool, error) {
	fsType, mounted, _, err := mountedFilesystemFacts(dir)
	return fsType, mounted, err
}

func mountedFilesystemFacts(dir string) (string, bool, string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", false, "", fmt.Errorf("read mountinfo: %v", err)
	}
	clean := filepath.Clean(dir)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		pre, post := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(pre) >= 5 && len(post) >= 2 && filepath.Clean(unescapeMountInfoField(pre[4])) == clean {
			return post[0], true, fmt.Sprintf("%s:%s:%s", pre[0], unescapeMountInfoField(post[1]), clean), nil
		}
	}
	return "", false, "", nil
}

func unescapeMountInfoField(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}

func mountedSource(dir string) (string, bool, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", false, err
	}
	clean := filepath.Clean(dir)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		pre, post := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(pre) >= 5 && len(post) >= 2 && filepath.Clean(unescapeMountInfoField(pre[4])) == clean {
			return unescapeMountInfoField(post[1]), true, nil
		}
	}
	return "", false, nil
}

func verifyLoopbackMount(filestoreDir, image string) error {
	source, mounted, err := mountedSource(filestoreDir)
	if err != nil {
		return err
	}
	if !mounted || !strings.HasPrefix(filepath.Base(source), "loop") {
		return fmt.Errorf("loopback_dev filestore %s is not mounted from a loop device", filestoreDir)
	}
	data, err := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(source), "loop/backing_file"))
	if err != nil {
		return fmt.Errorf("read filestore loop backing file: %w", err)
	}
	actual := strings.TrimSpace(string(data))
	if !filepath.IsAbs(actual) {
		actual = "/" + actual
	}
	expected, err := filepath.EvalSymlinks(image)
	if err != nil {
		return err
	}
	actual, err = filepath.EvalSymlinks(actual)
	if err != nil {
		return err
	}
	if filepath.Clean(actual) != filepath.Clean(expected) {
		return fmt.Errorf("filestore loop backing mismatch: mounted=%s configured=%s", actual, expected)
	}
	return nil
}

func PrepareFilestore(filestoreDir, mode, image string, loopbackSizeBytes, systemReserveBytes int64) (result error) {
	mountedByCall := false
	defer func() {
		if result != nil && mountedByCall {
			result = errors.Join(result, CleanupFilestore(filestoreDir, mode, image))
		}
	}()
	if filestoreDir == "" {
		return fmt.Errorf("filestore_dir is required")
	}
	if !filepath.IsAbs(filestoreDir) {
		return fmt.Errorf("filestore_dir must be absolute: %s", filestoreDir)
	}
	switch mode {
	case "existing":
		info, err := os.Stat(filestoreDir)
		if err != nil {
			return fmt.Errorf("inspect existing filestore %s: %w", filestoreDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("existing filestore %s must be a directory", filestoreDir)
		}
	case "loopback_dev":
		if image == "" || !filepath.IsAbs(image) {
			return fmt.Errorf("filestore_loopback_image must be an absolute path")
		}
		if loopbackSizeBytes <= 0 {
			return fmt.Errorf("filestore_loopback_size_bytes must be positive")
		}
		if err := os.MkdirAll(filestoreDir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filestoreDir, err)
		}
		if _, err := os.Stat(image); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(image), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(image, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return fmt.Errorf("create loopback image: %w", err)
			}
			if err := file.Truncate(loopbackSizeBytes); err != nil {
				_ = file.Close()
				return fmt.Errorf("size loopback image: %w", err)
			}
			if err := file.Close(); err != nil {
				return err
			}
			if out, err := exec.Command("mkfs.xfs", "-f", "-m", "reflink=1", image).CombinedOutput(); err != nil {
				return fmt.Errorf("mkfs.xfs %s (%s bytes): %s: %w", image, strconv.FormatInt(loopbackSizeBytes, 10), out, err)
			}
		} else if err != nil {
			return fmt.Errorf("inspect loopback image: %w", err)
		} else {
			info, err := os.Stat(image)
			if err != nil {
				return fmt.Errorf("inspect loopback image: %w", err)
			}
			if !info.Mode().IsRegular() || info.Size() != loopbackSizeBytes {
				return fmt.Errorf("existing loopback image must be a regular file of %d bytes, got mode=%s size=%d", loopbackSizeBytes, info.Mode(), info.Size())
			}
		}
		if _, mounted, err := mountedFilesystem(filestoreDir); err != nil {
			return err
		} else if !mounted {
			if out, err := exec.Command("mount", "-o", "loop,defaults,discard,prjquota", image, filestoreDir).CombinedOutput(); err != nil {
				return fmt.Errorf("mount loopback filestore %s: %s: %w", filestoreDir, out, err)
			}
			mountedByCall = true
		}
		if err := verifyLoopbackMount(filestoreDir, image); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported filestore_mode %q", mode)
	}
	fsType, mounted, err := mountedFilesystem(filestoreDir)
	if err != nil {
		return err
	}
	if !mounted {
		return fmt.Errorf("filestore_dir must be an independent mount: %s", filestoreDir)
	}
	if fsType != "xfs" && fsType != "ext4" {
		return fmt.Errorf("filestore filesystem must be xfs or ext4, got %s", fsType)
	}
	readonly, err := IsPathReadOnly(filestoreDir)
	if err != nil || readonly {
		return fmt.Errorf("filestore must be writable: readonly=%t: %w", readonly, err)
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(filestoreDir, &stat); err != nil {
		return fmt.Errorf("stat filestore: %w", err)
	}
	capacity := StatfsBytes(uint64(stat.Blocks), int64(stat.Bsize))
	available := StatfsBytes(uint64(stat.Bavail), int64(stat.Bsize))
	if systemReserveBytes < 0 || systemReserveBytes >= capacity || systemReserveBytes >= available {
		return fmt.Errorf("filestore_system_reserve_bytes %d is invalid for capacity=%d available=%d", systemReserveBytes, capacity, available)
	}
	for _, subdir := range []string{"runsc", "runc", "projections"} {
		if err := os.MkdirAll(filepath.Join(filestoreDir, subdir), 0755); err != nil {
			return fmt.Errorf("create filestore partition %s: %w", subdir, err)
		}
	}
	capabilities, err := probeFilestoreCapabilities(filestoreDir, fsType)
	if err != nil {
		return err
	}
	_, _, capabilities.MountIdentity, err = mountedFilesystemFacts(filestoreDir)
	if err != nil {
		return err
	}
	if capabilities.MountIdentity == "" {
		return fmt.Errorf("filestore mount identity is unavailable for %s", filestoreDir)
	}
	if err := writeFilestoreCapabilities(filestoreDir, capabilities); err != nil {
		return err
	}
	return nil
}

func ReadFilestoreCapabilities(filestoreDir string) (FilestoreCapabilities, error) {
	data, err := os.ReadFile(filepath.Join(filestoreDir, FilestoreCapabilitiesFile))
	if err != nil {
		return FilestoreCapabilities{}, err
	}
	var out FilestoreCapabilities
	if err := json.Unmarshal(data, &out); err != nil {
		return FilestoreCapabilities{}, err
	}
	fsType, mounted, identity, err := mountedFilesystemFacts(filestoreDir)
	if err != nil {
		return FilestoreCapabilities{}, err
	}
	if !mounted || identity == "" || identity != out.MountIdentity || fsType != out.FilesystemType {
		return FilestoreCapabilities{}, fmt.Errorf("filestore mount identity changed: stored=%s current=%s", out.MountIdentity, identity)
	}
	return out, nil
}

func writeFilestoreCapabilities(dir string, capabilities FilestoreCapabilities) error {
	data, err := json.MarshalIndent(capabilities, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".capabilities-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(name)
	}()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, FilestoreCapabilitiesFile)); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func probeFilestoreCapabilities(filestoreDir, fsType string) (FilestoreCapabilities, error) {
	result := FilestoreCapabilities{FilesystemType: fsType, ProbedAt: time.Now().UTC()}
	probeRoot, err := os.MkdirTemp(filestoreDir, ".axern-overlay-probe-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(probeRoot)
	lower, upper := filepath.Join(probeRoot, "lower"), filepath.Join(probeRoot, "upper")
	work, merged := filepath.Join(probeRoot, "work"), filepath.Join(probeRoot, "merged")
	for _, dir := range []string{lower, upper, work, merged} {
		if err := os.Mkdir(dir, 0700); err != nil {
			return result, err
		}
	}
	if err := os.WriteFile(filepath.Join(lower, "lower.txt"), []byte("lower"), 0600); err != nil {
		return result, err
	}
	if err := unix.Setxattr(upper, "user.axern.probe", []byte("1"), 0); err != nil {
		return result, fmt.Errorf("filestore xattr probe: %w", err)
	}
	options := "lowerdir=" + lower + ",upperdir=" + upper + ",workdir=" + work
	if err := unix.Mount("overlay", merged, "overlay", 0, options); err != nil {
		return result, fmt.Errorf("filestore overlay scratch mount: %w", err)
	}
	mounted := true
	defer func() {
		if mounted {
			_ = unix.Unmount(merged, unix.MNT_DETACH)
		}
	}()
	if err := os.WriteFile(filepath.Join(merged, "created.txt"), []byte("created"), 0600); err != nil {
		return result, fmt.Errorf("filestore overlay write probe: %w", err)
	}
	if err := os.Rename(filepath.Join(merged, "created.txt"), filepath.Join(merged, "renamed.txt")); err != nil {
		return result, err
	}
	if err := os.Remove(filepath.Join(merged, "lower.txt")); err != nil {
		return result, err
	}
	if err := unix.Unmount(merged, 0); err != nil {
		return result, err
	}
	mounted = false
	result.OverlayReady = true
	if fsType == "xfs" {
		result.ProjectQuotaReady = probeXFSProjectQuota(filestoreDir, upper)
	}
	if fixture := erofsFixturePath(); fixture != "" {
		if err := probeEROFSLower(filestoreDir, fixture); err != nil {
			result.EROFSProbeError = err.Error()
		} else {
			result.EROFSReady = true
		}
	}
	return result, nil
}

func probeXFSProjectQuota(filestoreDir, path string) (ready bool) {
	id := strconv.FormatUint(uint64(FilestoreProbeProjectID), 10)
	defer func() {
		for _, command := range []string{"project -C -p " + path + " " + id, "limit -p bhard=0 bsoft=0 " + id} {
			if _, err := exec.Command("xfs_quota", "-x", "-c", command, filestoreDir).CombinedOutput(); err != nil {
				ready = false
			}
		}
	}()
	for _, command := range []string{"project -s -p " + path + " " + id, "limit -p bhard=1048576 bsoft=1048576 " + id} {
		if _, err := exec.Command("xfs_quota", "-x", "-c", command, filestoreDir).CombinedOutput(); err != nil {
			return false
		}
	}
	file, err := os.OpenFile(filepath.Join(path, "quota-enforcement-probe"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	payload := make([]byte, 2*1024*1024)
	written, writeErr := file.Write(payload)
	if writeErr == nil && written < len(payload) {
		_, writeErr = file.Write(payload[written:])
	}
	syncErr := file.Sync()
	if writeErr == nil && syncErr == nil {
		return false
	}
	return quotaBoundaryError(writeErr) || quotaBoundaryError(syncErr)
}

func quotaBoundaryError(err error) bool {
	// XFS may surface an enforced project hard limit as EDQUOT or ENOSPC,
	// depending on the kernel and backing device. Both are valid fail-closed
	// outcomes after the probe has established ample filesystem capacity.
	return errors.Is(err, unix.EDQUOT) || errors.Is(err, unix.ENOSPC)
}

func erofsFixturePath() string {
	for _, candidate := range []string{strings.TrimSpace(os.Getenv("AXERN_EROFS_FIXTURE")), "/usr/share/axnoded/fixtures/minimal.erofs"} {
		if candidate != "" {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate
			}
		}
	}
	return ""
}

func probeEROFSLower(filestoreDir, fixture string) error {
	fixtureBefore, err := os.ReadFile(fixture)
	if err != nil {
		return err
	}
	fixtureHash := sha256.Sum256(fixtureBefore)
	root, err := os.MkdirTemp(filestoreDir, ".axern-erofs-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	lower, upper, work, merged := filepath.Join(root, "lower"), filepath.Join(root, "upper"), filepath.Join(root, "work"), filepath.Join(root, "merged")
	for _, dir := range []string{lower, upper, work, merged} {
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
	}
	if output, err := exec.Command("mount", "-t", "erofs", "-o", "loop,ro", fixture, lower).CombinedOutput(); err != nil {
		return fmt.Errorf("mount EROFS fixture: %s: %w", output, err)
	}
	lowerMounted := true
	defer func() {
		if lowerMounted {
			_ = unix.Unmount(lower, unix.MNT_DETACH)
		}
	}()
	if err := unix.Mount("overlay", merged, "overlay", 0, "lowerdir="+lower+",upperdir="+upper+",workdir="+work); err != nil {
		return fmt.Errorf("overlay on EROFS lower: %w", err)
	}
	overlayMounted := true
	defer func() {
		if overlayMounted {
			_ = unix.Unmount(merged, unix.MNT_DETACH)
		}
	}()
	var regular string
	_ = filepath.WalkDir(merged, func(path string, entry os.DirEntry, err error) error {
		if err == nil && regular == "" && entry.Type().IsRegular() {
			regular = path
		}
		return nil
	})
	if regular == "" {
		return fmt.Errorf("EROFS fixture has no regular file")
	}
	if _, err := os.ReadFile(regular); err != nil {
		return err
	}
	if err := os.WriteFile(regular, []byte("copy-up"), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(merged, "created"), []byte("create"), 0600); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(merged, "directory"), 0700); err != nil {
		return err
	}
	if err := os.Remove(regular); err != nil {
		return err
	}
	if err := unix.Unmount(merged, 0); err != nil {
		return err
	}
	overlayMounted = false
	if err := unix.Unmount(lower, 0); err != nil {
		return err
	}
	lowerMounted = false
	fixtureAfter, err := os.ReadFile(fixture)
	if err != nil {
		return err
	}
	if sha256.Sum256(fixtureAfter) != fixtureHash {
		return fmt.Errorf("EROFS fixture changed during lower compatibility probe")
	}
	return nil
}

func CleanupFilestore(filestoreDir, mode, image string) error {
	if filestoreDir == "" || mode != "loopback_dev" {
		return nil
	}
	_, mounted, err := mountedFilesystem(filestoreDir)
	if err != nil {
		return err
	}
	if !mounted {
		return nil
	}
	if err := verifyLoopbackMount(filestoreDir, image); err != nil {
		return err
	}
	if out, err := exec.Command("umount", filestoreDir).CombinedOutput(); err != nil {
		return fmt.Errorf("umount %s: %s: %v", filestoreDir, out, err)
	}
	return nil
}
