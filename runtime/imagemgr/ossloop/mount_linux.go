//go:build linux

package ossloop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/moby/sys/mountinfo"
)

func defaultRootfsMount(imagePath, lowerPath, targetPath, supportPath string) error {
	if err := mountLoopLower(imagePath, lowerPath); err != nil {
		return err
	}
	if err := mountReadonlyOverlay(lowerPath, targetPath, supportPath); err != nil {
		_ = defaultLoopUnmount(lowerPath)
		return err
	}
	return nil
}

func mountLoopLower(imagePath, lowerPath string) error {
	if err := os.MkdirAll(lowerPath, 0755); err != nil {
		return err
	}
	cmd := exec.Command("mount", "-t", "ext4", "-o", "loop,ro", imagePath, lowerPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func mountReadonlyOverlay(lowerPath, targetPath, supportPath string) error {
	data := "lowerdir=" + lowerPath
	if supportPath != "" {
		data += ":" + supportPath
	}
	cmd := exec.Command("mount", "-t", "overlay", "-o", data, "overlay", targetPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func defaultRootfsUnmount(lowerPath, targetPath string) error {
	if err := defaultLoopUnmount(targetPath); err != nil {
		return err
	}
	return defaultLoopUnmount(lowerPath)
}

func defaultLoopUnmount(targetPath string) error {
	cmd := exec.Command("umount", targetPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func defaultMounted(targetPath string) (bool, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return false, fmt.Errorf("failed to resolve mount path %s: %w", targetPath, err)
	}
	return mountinfo.Mounted(absPath)
}
