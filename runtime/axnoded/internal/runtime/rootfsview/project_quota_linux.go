//go:build linux

package rootfsview

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	fsIOCFSGetXAttr    = uintptr(0x801c581f)
	fsXFlagProjInherit = uint32(0x00000200)
	qXGetQuota         = 0x5803
	projectQuota       = 2
)

type fsXAttr struct {
	XFlags     uint32
	ExtSize    uint32
	Nextents   uint32
	ProjectID  uint32
	CowExtSize uint32
	Pad        [8]byte
}

// fsDiskQuota mirrors Linux's fs_disk_quota ABI used by XFS Q_XGETQUOTA.
type fsDiskQuota struct {
	Version       uint8
	Flags         uint8
	FieldMask     uint16
	ID            uint32
	BlkHardLimit  uint64
	BlkSoftLimit  uint64
	InoHardLimit  uint64
	InoSoftLimit  uint64
	BlockCount    uint64
	InodeCount    uint64
	InodeTimer    int32
	BlockTimer    int32
	InodeWarnings uint16
	BlockWarnings uint16
	Padding2      int32
	RTBHardLimit  uint64
	RTBSoftLimit  uint64
	RTBCount      uint64
	RTBTimer      int32
	RTBWarnings   uint16
	Padding3      uint16
	Padding4      [8]byte
}

func VerifyProjectQuota(filestoreDir, projectRoot string, projectID uint32, limitBytes int64) error {
	if projectID == 0 || limitBytes <= 0 {
		return fmt.Errorf("expected project ID and hard limit must be positive")
	}
	for _, candidate := range []string{projectRoot, filepath.Join(projectRoot, "upper"), filepath.Join(projectRoot, "work")} {
		file, err := os.Open(candidate)
		if err != nil {
			return err
		}
		var attr fsXAttr
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), fsIOCFSGetXAttr, uintptr(unsafe.Pointer(&attr)))
		closeErr := file.Close()
		if errno != 0 {
			return fmt.Errorf("FS_IOC_FSGETXATTR %s: %w", candidate, errno)
		}
		if closeErr != nil {
			return fmt.Errorf("close project quota path %s: %w", candidate, closeErr)
		}
		if attr.ProjectID != projectID || attr.XFlags&fsXFlagProjInherit == 0 {
			return fmt.Errorf("rootfs view project assignment changed at %s: project=%d flags=%#x, expected project=%d with inherit", candidate, attr.ProjectID, attr.XFlags, projectID)
		}
	}
	// XFS Q_XGETQUOTA expects the mounted block-device identity. Passing the
	// mountpoint happens to work on some quota implementations, but XFS returns
	// ENOTBLK on kernels that enforce the documented special-device contract.
	// Resolve it from the effective mount instead of trusting a configured path.
	quotaDevice, err := xfsQuotaDevice(filestoreDir)
	if err != nil {
		return err
	}
	special, err := syscall.BytePtrFromString(quotaDevice)
	if err != nil {
		return err
	}
	var quota fsDiskQuota
	command := uintptr((qXGetQuota << 8) | projectQuota)
	_, _, errno := syscall.Syscall6(syscall.SYS_QUOTACTL, command, uintptr(unsafe.Pointer(special)), uintptr(projectID), uintptr(unsafe.Pointer(&quota)), 0, 0)
	if errno != 0 {
		return fmt.Errorf("quotactl Q_XGETQUOTA project %d: %w", projectID, errno)
	}
	expectedBlocks := uint64((limitBytes + 511) / 512)
	if quota.ID != projectID || quota.BlkHardLimit != expectedBlocks {
		return fmt.Errorf("kernel project quota changed: id=%d hard_blocks=%d, expected id=%d hard_blocks=%d", quota.ID, quota.BlkHardLimit, projectID, expectedBlocks)
	}
	return nil
}

func xfsQuotaDevice(filestoreDir string) (string, error) {
	facts, err := InspectBacking(filestoreDir)
	if err != nil {
		return "", fmt.Errorf("inspect filestore mount for project quota: %w", err)
	}
	if !strings.EqualFold(facts.FSType, "xfs") {
		return "", fmt.Errorf("project quota requires XFS, got %q", facts.FSType)
	}
	device := filepath.Clean(strings.TrimSpace(facts.Source))
	if !filepath.IsAbs(device) {
		return "", fmt.Errorf("XFS mount source %q is not an absolute block-device path", facts.Source)
	}
	info, err := os.Stat(device)
	if err != nil {
		return "", fmt.Errorf("stat XFS quota device %q: %w", device, err)
	}
	if info.Mode()&os.ModeDevice == 0 {
		return "", fmt.Errorf("XFS mount source %q is not a block device", device)
	}
	return device, nil
}

func applyProjectQuota(filestoreDir, projectRoot string, projectID uint32, limitBytes int64) error {
	if projectID == 0 || limitBytes <= 0 {
		return fmt.Errorf("runc writable rootfs requires a positive XFS project ID and limit")
	}
	if !safeXFSQuotaPath(filestoreDir) || !safeXFSQuotaPath(projectRoot) {
		return fmt.Errorf("XFS project quota paths contain unsupported characters")
	}
	project := strconv.FormatUint(uint64(projectID), 10)
	commands := []string{
		// OverlayFS creates copy-up temporaries in work/ before moving them to
		// upper/. XFS rejects that move with EXDEV when the directories belong
		// to different projects, so the allocation project owns the complete
		// private view and all of its copy-up metadata.
		"project -s -p " + projectRoot + " " + project,
		"limit -p bhard=" + strconv.FormatInt(limitBytes, 10) + " bsoft=" + strconv.FormatInt(limitBytes, 10) + " " + project,
	}
	for _, command := range commands {
		output, err := exec.Command("xfs_quota", "-x", "-c", command, filestoreDir).CombinedOutput()
		if err != nil {
			return fmt.Errorf("apply XFS project quota (%s): %s: %w", command, strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

func clearProjectQuota(filestoreDir, projectRoot string, projectID uint32) error {
	if projectID == 0 {
		return nil
	}
	if !safeXFSQuotaPath(filestoreDir) || !safeXFSQuotaPath(projectRoot) {
		return fmt.Errorf("XFS project quota paths contain unsupported characters")
	}
	project := strconv.FormatUint(uint64(projectID), 10)
	var result error
	for _, command := range []string{
		"project -C -p " + projectRoot + " " + project,
		"limit -p bhard=0 bsoft=0 " + project,
	} {
		output, err := exec.Command("xfs_quota", "-x", "-c", command, filestoreDir).CombinedOutput()
		if err != nil {
			result = errors.Join(result, fmt.Errorf("clear XFS project quota (%s): %s: %w", command, strings.TrimSpace(string(output)), err))
		}
	}
	return result
}

func safeXFSQuotaPath(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '/' || character == '.' || character == '_' || character == '-' || character == '+' {
			continue
		}
		return false
	}
	return true
}
