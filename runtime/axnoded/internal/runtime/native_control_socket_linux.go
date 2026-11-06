//go:build linux

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type nativeControlSocket struct {
	parent, fd             int
	parentPath, name, root string
}

// The pinned runsc writes the successful candidate to sandbox.controlSocketPath.
// Only that exact socket may be retired; never sweep the other fallback paths.
func openNativeControlSocket(root, id, saved string) (*nativeControlSocket, error) {
	if !nativeStateAllocationID.MatchString(id) {
		return nil, errors.New("native control socket requires an exact allocation identity")
	}
	name := "runsc-" + id + ".sock"
	allowed := false
	for _, parent := range []string{root, "/var/run", "/run", "/tmp"} {
		if saved == filepath.Join(parent, name) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, errors.New("saved native control socket is not an exact pinned runtime candidate")
	}
	parentPath := filepath.Dir(saved)
	parent, err := openNativeSocketParent(parentPath, root)
	if err != nil {
		return nil, err
	}
	socket := &nativeControlSocket{parent: parent, fd: -1, parentPath: parentPath, name: name, root: root}
	fd, err := unix.Openat(parent, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return socket, nil
	}
	if err != nil {
		socket.close()
		return nil, fmt.Errorf("open exact native control socket: %w", err)
	}
	socket.fd = fd
	if err := socket.verify(); err != nil {
		socket.close()
		return nil, err
	}
	return socket, nil
}

func openNativeSocketParent(parent, runtimeRoot string) (int, error) {
	if parent == runtimeRoot {
		return openNativeStateRoot(runtimeRoot)
	}
	resolved := parent
	if parent == "/var/run" {
		info, err := os.Lstat(parent)
		if err != nil {
			return -1, fmt.Errorf("inspect native control socket parent: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(parent)
			if err != nil || (target != "/run" && target != "../run") {
				return -1, errors.New("native control socket /var/run alias is not the standard /run mapping")
			}
			resolved = "/run"
		}
	}
	if resolved != "/run" && resolved != "/var/run" && resolved != "/tmp" {
		return -1, errors.New("native control socket parent is not a pinned system directory")
	}
	fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open native control socket parent: %w", err)
	}
	var info unix.Stat_t
	err = unix.Fstat(fd, &info)
	trusted := err == nil && info.Uid == 0
	if resolved == "/tmp" {
		trusted = trusted && info.Mode&unix.S_ISVTX != 0
	} else {
		trusted = trusted && info.Mode&0022 == 0
	}
	if !trusted {
		unix.Close(fd)
		return -1, errors.New("native control socket parent has untrusted ownership or permissions")
	}
	return fd, nil
}

func (s *nativeControlSocket) close() {
	if s.fd >= 0 {
		unix.Close(s.fd)
	}
	unix.Close(s.parent)
}

func (s *nativeControlSocket) verify() error {
	currentParent, err := openNativeSocketParent(s.parentPath, s.root)
	if err != nil {
		return err
	}
	defer unix.Close(currentParent)
	var openedParent, currentParentInfo unix.Stat_t
	if err := errors.Join(unix.Fstat(s.parent, &openedParent), unix.Fstat(currentParent, &currentParentInfo)); err != nil {
		return err
	}
	if openedParent.Dev != currentParentInfo.Dev || openedParent.Ino != currentParentInfo.Ino {
		return errors.New("native control socket parent was replaced")
	}
	var current unix.Stat_t
	err = unix.Fstatat(s.parent, s.name, &current, unix.AT_SYMLINK_NOFOLLOW)
	if s.fd < 0 {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return errors.New("native control socket appeared during termination verification")
	}
	if err != nil {
		return fmt.Errorf("recheck native control socket: %w", err)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(s.fd, &opened); err != nil {
		return err
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFSOCK || opened.Uid != uint32(os.Geteuid()) || opened.Nlink != 1 || opened.Mode&0022 != 0 {
		return errors.New("native control socket is not a trusted single-link socket")
	}
	if opened.Dev != current.Dev || opened.Ino != current.Ino {
		return errors.New("native control socket was replaced during termination verification")
	}
	return nil
}

func (s *nativeControlSocket) retire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.verify(); err != nil {
		return err
	}
	if s.fd < 0 {
		return nil
	}
	probe, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open native control socket liveness probe: %w", err)
	}
	defer unix.Close(probe)
	// O_PATH pins the exact socket dentry and avoids Unix pathname length
	// limits. A successful or uncertain connection never proves termination.
	err = unix.Connect(probe, &unix.SockaddrUnix{Name: fmt.Sprintf("/proc/self/fd/%d", s.fd)})
	if !errors.Is(err, unix.ECONNREFUSED) {
		return fmt.Errorf("native control socket still has a live or unverifiable endpoint: %v", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.verify(); err != nil {
		return err
	}
	if err := unix.Unlinkat(s.parent, s.name, 0); err != nil {
		return fmt.Errorf("retire exact native control socket: %w", err)
	}
	if err := unix.Fsync(s.parent); err != nil {
		return fmt.Errorf("persist native control socket retirement: %w", err)
	}
	return nil
}
