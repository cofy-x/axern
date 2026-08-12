//go:build linux

package image

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const pageCacheDropVerificationAttempts = 3

func dropMountedFilePageCache(mountPath, containerPath string, offsetBytes, lengthBytes int64) error {
	pageSize := int64(os.Getpagesize())
	if mountPath == "" || !filepath.IsAbs(mountPath) || filepath.Clean(mountPath) != mountPath {
		return fmt.Errorf("mount path must be a clean absolute path")
	}
	if containerPath == "" || !strings.HasPrefix(containerPath, "/") || filepath.Clean(containerPath) != containerPath || containerPath == "/" {
		return fmt.Errorf("container path must be a clean absolute non-root path")
	}
	if offsetBytes < 0 || lengthBytes <= 0 || lengthBytes > maxPageCacheDropBytes {
		return fmt.Errorf("page-cache range is invalid")
	}
	if offsetBytes%pageSize != 0 || lengthBytes%pageSize != 0 {
		return fmt.Errorf("page-cache range must be aligned to the %d-byte host page size", pageSize)
	}

	rootFD, err := unix.Open(
		mountPath,
		unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open mount root: %w", err)
	}
	defer unix.Close(rootFD)

	relativePath := strings.TrimPrefix(containerPath, "/")
	fileFD, err := unix.Openat2(rootFD, relativePath, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: uint64(
			unix.RESOLVE_BENEATH |
				unix.RESOLVE_NO_MAGICLINKS |
				unix.RESOLVE_NO_SYMLINKS,
		),
	})
	if err != nil {
		return fmt.Errorf("open image file without following links: %w", err)
	}
	defer unix.Close(fileFD)

	var stat unix.Stat_t
	if err := unix.Fstat(fileFD, &stat); err != nil {
		return fmt.Errorf("stat image file: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("image path is not a regular file")
	}
	if offsetBytes > stat.Size || lengthBytes > stat.Size-offsetBytes {
		return fmt.Errorf(
			"requested range [%d,%d) exceeds file size %d",
			offsetBytes,
			offsetBytes+lengthBytes,
			stat.Size,
		)
	}

	residentPages := 0
	for attempt := 1; attempt <= pageCacheDropVerificationAttempts; attempt++ {
		if err := unix.Fadvise(fileFD, offsetBytes, lengthBytes, unix.FADV_DONTNEED); err != nil {
			return fmt.Errorf("fadvise DONTNEED: %w", err)
		}
		residentPages, err = residentPageCount(fileFD, offsetBytes, lengthBytes)
		if err != nil {
			return err
		}
		if residentPages == 0 {
			return nil
		}
		if attempt < pageCacheDropVerificationAttempts {
			time.Sleep(20 * time.Millisecond)
			continue
		}
	}
	return fmt.Errorf(
		"fadvise DONTNEED left %d pages resident after %d attempts",
		residentPages,
		pageCacheDropVerificationAttempts,
	)
}

func residentPageCount(fileFD int, offsetBytes, lengthBytes int64) (int, error) {
	mapping, err := unix.Mmap(
		fileFD,
		offsetBytes,
		int(lengthBytes),
		unix.PROT_READ,
		unix.MAP_SHARED,
	)
	if err != nil {
		return 0, fmt.Errorf("map image file range for residency verification: %w", err)
	}
	defer unix.Munmap(mapping)

	residency := make([]byte, (len(mapping)+os.Getpagesize()-1)/os.Getpagesize())
	_, _, errno := unix.Syscall(
		unix.SYS_MINCORE,
		uintptr(unsafe.Pointer(&mapping[0])),
		uintptr(len(mapping)),
		uintptr(unsafe.Pointer(&residency[0])),
	)
	if errno != 0 {
		return 0, fmt.Errorf("mincore image file range: %w", errno)
	}
	resident := 0
	for _, state := range residency {
		if state&1 != 0 {
			resident++
		}
	}
	return resident, nil
}
