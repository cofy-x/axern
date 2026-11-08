//go:build linux

package workspace

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

type kernelPlatform struct{}

func (kernelPlatform) space(root *os.File) (int64, int64, error) {
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(root.Fd()), &fs); err != nil {
		return 0, 0, err
	}
	bytes := func(blocks uint64) int64 {
		if fs.Bsize <= 0 {
			return 0
		}
		if blocks > math.MaxInt64/uint64(fs.Bsize) {
			return math.MaxInt64
		}
		return int64(blocks) * fs.Bsize
	}
	return bytes(fs.Blocks), bytes(fs.Bavail), nil
}

func (kernelPlatform) guestDirectory(file *os.File) error {
	if err := file.Chown(1000, 1000); err != nil {
		return err
	}
	return file.Chmod(0770)
}

// These layouts and commands are Linux UAPI from linux/fs.h and
// linux/dqblk_xfs.h. XFS disk-quota blocks are always 512-byte basic blocks.
type fsxattr struct {
	Flags, Extsize, Nextents, Project, Cowextsize uint32
	Padding                                       [8]byte
}

type diskQuota struct {
	Version, Flags                                             int8
	Fields                                                     uint16
	ID                                                         uint32
	BlockHard, BlockSoft, InodeHard, InodeSoft, Blocks, Inodes uint64
	InodeTimer, BlockTimer                                     int32
	InodeWarnings, BlockWarnings                               uint16
	TimerHigh                                                  [4]int8
	RealtimeHard, RealtimeSoft, RealtimeBlocks                 uint64
	RealtimeTimer                                              int32
	RealtimeWarnings                                           uint16
	Padding16                                                  int16
	Padding                                                    [8]byte
}

const projectInherit = 0x200

func quotaCall(fd int, command, project uint32, data unsafe.Pointer) error {
	_, _, errno := unix.Syscall6(unix.SYS_QUOTACTL_FD, uintptr(fd), uintptr((command<<8)|2), uintptr(project), uintptr(data), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func projectAttributes(fd int) (fsxattr, error) {
	var value fsxattr
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), 0x801c581f, uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return value, errno
	}
	return value, nil
}

func (kernelPlatform) root(file *os.File) (directoryIdentity, string, error) {
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(file.Fd()), &fs); err != nil {
		return directoryIdentity{}, "", err
	}
	if fs.Type != unix.XFS_SUPER_MAGIC {
		return directoryIdentity{}, "", errors.New("persistent Workspace storage requires XFS project quotas")
	}
	var status [256]byte
	status[0] = 1
	if err := quotaCall(int(file.Fd()), ('X'<<8)+8, 0, unsafe.Pointer(&status[0])); err != nil {
		return directoryIdentity{}, "", fmt.Errorf("read XFS project enforcement: %w", err)
	}
	flags := *(*uint16)(unsafe.Pointer(&status[2]))
	if flags&0x30 != 0x30 {
		return directoryIdentity{}, "", errors.New("XFS project accounting and enforcement must both be enabled")
	}
	identity, err := (kernelPlatform{}).directory(file)
	if err != nil {
		return directoryIdentity{}, "", err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil || stat.Uid != 0 {
		return directoryIdentity{}, "", errors.New("Workspace storage root must be owned by root")
	}
	return identity, fmt.Sprintf("%x:%x", fs.Fsid.Val[0], fs.Fsid.Val[1]), nil
}

func (kernelPlatform) directory(file *os.File) (directoryIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return directoryIdentity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return directoryIdentity{}, errors.New("Workspace path is not a directory")
	}
	var extended unix.Statx_t
	if err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_MNT_ID, &extended); err != nil {
		return directoryIdentity{}, err
	}
	if extended.Mask&unix.STATX_MNT_ID == 0 || extended.Mnt_id == 0 {
		return directoryIdentity{}, errors.New("Workspace mount identity is unavailable")
	}
	return directoryIdentity{uint64(stat.Dev), stat.Ino, extended.Mnt_id}, nil
}

func (kernelPlatform) quota(root *os.File, project uint32) (quota, error) {
	value := diskQuota{Version: 1}
	if err := quotaCall(int(root.Fd()), ('X'<<8)+3, project, unsafe.Pointer(&value)); err != nil {
		return quota{}, err
	}
	if value.Version != 1 || value.ID != project || value.Flags&2 == 0 {
		return quota{}, errors.New("XFS quota returned an unexpected project identity")
	}
	return quota{Project: value.ID, Flags: uint32(value.Flags), BlockHard: value.BlockHard, BlockSoft: value.BlockSoft, Blocks: value.Blocks, InodeHard: value.InodeHard, InodeSoft: value.InodeSoft, Inodes: value.Inodes}, nil
}

func (kernelPlatform) assign(file *os.File, project uint32) error {
	attributes, err := projectAttributes(int(file.Fd()))
	if err != nil {
		return err
	}
	if attributes.Project != 0 && attributes.Project != project {
		return errors.New("Workspace directory already belongs to another project")
	}
	attributes.Project = project
	attributes.Flags |= projectInherit
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, file.Fd(), 0x401c5820, uintptr(unsafe.Pointer(&attributes)))
	if errno != 0 {
		return errno
	}
	return (kernelPlatform{}).verify(file, project)
}

func (kernelPlatform) limits(root *os.File, project uint32, blocks, inodes uint64) error {
	value := diskQuota{Version: 1, Flags: 2, Fields: 0xf, ID: project, BlockHard: blocks, InodeHard: inodes}
	if err := quotaCall(int(root.Fd()), ('X'<<8)+4, project, unsafe.Pointer(&value)); err != nil {
		return err
	}
	readback, err := (kernelPlatform{}).quota(root, project)
	if blocks == 0 && inodes == 0 && errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if readback.BlockHard != blocks || readback.InodeHard != inodes || readback.BlockSoft != 0 || readback.InodeSoft != 0 {
		return errors.New("XFS Workspace quota readback differs from requested limits")
	}
	return nil
}

func (kernelPlatform) verify(file *os.File, project uint32) error {
	attributes, err := projectAttributes(int(file.Fd()))
	if err != nil {
		return err
	}
	if attributes.Project != project || attributes.Flags&projectInherit == 0 {
		return errors.New("Workspace project assignment or inheritance changed")
	}
	return nil
}

func (kernelPlatform) sync(file *os.File) error { return unix.Syncfs(int(file.Fd())) }

func (p kernelPlatform) remove(root *os.File, name string, want directoryIdentity, project uint32) error {
	fd, err := unix.Openat(int(root.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), name)
	identity, err := p.directory(dir)
	rootIdentity, rootErr := p.directory(root)
	if err != nil || rootErr != nil || identity.Inode != want.Inode || identity.Device != rootIdentity.Device || identity.Mount != rootIdentity.Mount {
		dir.Close()
		return errors.New("Workspace deletion directory identity changed")
	}
	if err := p.removeChildren(dir, identity, project); err != nil {
		dir.Close()
		return err
	}
	if err := dir.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(int(root.Fd()), name, unix.AT_REMOVEDIR)
}

func (p kernelPlatform) removeChildren(dir *os.File, root directoryIdentity, project uint32) error {
	if err := p.verify(dir, project); err != nil {
		return err
	}
	for {
		entries, err := dir.ReadDir(128)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			var stat unix.Stat_t
			if err := unix.Fstatat(int(dir.Fd()), entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
				fd, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if err != nil {
					return err
				}
				child := os.NewFile(uintptr(fd), entry.Name())
				identity, err := p.directory(child)
				if err != nil || identity.Device != root.Device || identity.Mount != root.Mount {
					child.Close()
					return errors.New("Workspace cleanup cannot cross filesystem or mount identity")
				}
				if err := p.removeChildren(child, root, project); err != nil {
					child.Close()
					return err
				}
				if err := child.Close(); err != nil {
					return err
				}
				if err := unix.Unlinkat(int(dir.Fd()), entry.Name(), unix.AT_REMOVEDIR); err != nil {
					return err
				}
			} else {
				// Never follow symlinks or open FIFOs/devices. The private parent
				// is no longer mounted in any runtime when deletion is permitted.
				if err := unix.Unlinkat(int(dir.Fd()), entry.Name(), 0); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

func openDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.TrimSpace(path) != path || path == "/" {
		return nil, errors.New("Workspace path must be a canonical absolute directory")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openRegular(path string) (*os.File, error) {
	parent, err := openDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxOwnerRecordBytes {
		f.Close()
		return nil, errors.New("Workspace ownership must be a bounded private regular file")
	}
	return f, nil
}

func ensureMetadataDirectory(path string) error {
	parent, err := openDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := unix.Mkdirat(int(parent.Fd()), filepath.Base(path), 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	dir, err := openDirectory(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(dir.Fd()), &stat); err != nil {
		return err
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 {
		return errors.New("Workspace ownership directory has unsafe owner or permissions")
	}
	return parent.Sync()
}

func operatorReserveForFilesystem(root *os.File, filestore string, reserve, budget int64) (int64, error) {
	if filestore == "" {
		return 1 << 30, nil
	}
	other, err := openDirectory(filestore)
	if err != nil {
		return 0, err
	}
	defer other.Close()
	var a, b unix.Statfs_t
	if err := unix.Fstatfs(int(root.Fd()), &a); err != nil {
		return 0, err
	}
	if err := unix.Fstatfs(int(other.Fd()), &b); err != nil {
		return 0, err
	}
	if a.Fsid == b.Fsid && a.Type == b.Type {
		if reserve < budget || reserve-budget < 1<<30 {
			return 0, errors.New("shared Workspace filesystem requires its full capacity budget plus 1 GiB operator reserve excluded from ephemeral storage")
		}
		return reserve - budget, nil
	}
	return 1 << 30, nil
}

func openChildDirectory(parent *os.File, name string) (*os.File, error) {
	if !identityName.MatchString(name) {
		return nil, errors.New("invalid Workspace directory identity")
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Join(parent.Name(), name)), nil
}
