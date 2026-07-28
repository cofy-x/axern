package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/moby/sys/mountinfo"
	"github.com/sirupsen/logrus"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/diskusage"
)

func generateContainerID(imageURL string) string {
	cs := sha256.Sum256([]byte(imageURL))
	hash := hex.EncodeToString(cs[:])[:16]
	cleanName := strings.ReplaceAll(imageURL, "/", "-")
	cleanName = strings.ReplaceAll(cleanName, ":", "-")
	if len(cleanName) > 50 {
		cleanName = cleanName[:50]
	}
	return fmt.Sprintf("%s%s-%s", containerNamePrefix, cleanName, hash)
}

func reverseCopy(items []string) []string {
	out := make([]string, len(items))
	copy(out, items)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (m *Manager) buildMountLowerDirs(chainLowerDirs []string) []string {
	lowerDirs := append([]string(nil), chainLowerDirs...)
	if m == nil || m.supportDir == "" {
		return lowerDirs
	}
	return append(lowerDirs, m.supportDir)
}

func defaultOverlayMount(target string, lowerDirs []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("overlay mount is only supported on linux")
	}
	if len(lowerDirs) == 1 {
		source := lowerDirs[0]
		logrus.Infof(
			"OCI mount params: type=bind target=%s source=%s readonly=true",
			target,
			source,
		)
		if err := mountReadonlyBind(source, target); err != nil {
			return fmt.Errorf("readonly bind mount failed: source=%s target=%s: %w", source, target, err)
		}
		return nil
	}

	mountData, err := buildOverlayMountData(lowerDirs)
	if err != nil {
		return err
	}
	logrus.Infof(
		"OCI overlay mount params: target=%s lowerdir_count=%d opts=%s",
		target,
		len(lowerDirs),
		mountData,
	)
	if err := mountReadonlyOverlay(target, mountData); err != nil {
		return fmt.Errorf("overlay mount failed: target=%s opts=%s: %w", target, mountData, err)
	}
	return nil
}

func buildOverlayMountData(lowerDirs []string) (string, error) {
	if len(lowerDirs) == 0 {
		return "", fmt.Errorf("lowerDirs is empty")
	}
	return "lowerdir=" + strings.Join(lowerDirs, ":"), nil
}

func defaultOverlayUnmount(target string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("overlay unmount is only supported on linux")
	}
	if err := unmountOverlay(target); err != nil {
		return fmt.Errorf("overlay unmount failed: target=%s: %w", target, err)
	}
	return nil
}

type managedMountSnapshot struct {
	paths         map[string]struct{}
	authoritative bool
}

func readManagedMounts(mountsDir string) (managedMountSnapshot, error) {
	if runtime.GOOS != "linux" {
		return managedMountSnapshot{paths: map[string]struct{}{}}, nil
	}

	mountsRoot := filepath.Clean(mountsDir) + string(os.PathSeparator)
	mountsInfo, err := mountinfo.GetMounts(func(info *mountinfo.Info) (skip, stop bool) {
		cleanMP := filepath.Clean(info.Mountpoint)
		return !strings.HasPrefix(cleanMP, mountsRoot), false
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return managedMountSnapshot{paths: map[string]struct{}{}}, nil
		}
		return managedMountSnapshot{}, err
	}

	mounts := make(map[string]struct{}, len(mountsInfo))
	for _, info := range mountsInfo {
		mounts[info.Mountpoint] = struct{}{}
	}
	return managedMountSnapshot{paths: mounts, authoritative: true}, nil
}

func defaultDiskUsage(p string) (float64, error) {
	return diskusage.UsedRatioByAvailable(p)
}
