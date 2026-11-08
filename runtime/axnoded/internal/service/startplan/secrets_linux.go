//go:build linux

package startplan

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"golang.org/x/sys/unix"
)

const secretStorageName = "allocation-secrets"

var errSecretStorageAbsent = errors.New("allocation secret storage is absent")

type secretStorage struct {
	root, vault, allocation *os.File
	rootPath, key           string
}

func secretKey(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func openSecretNodeRoot(root string) (*os.File, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.TrimSpace(root) != root || root == "/" {
		return nil, errors.New("secret storage requires a canonical node root")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return nil, fmt.Errorf("open trusted secret storage ancestor: %w", err)
		}
		fd = next
		var info unix.Stat_t
		if err := unix.Fstat(fd, &info); err != nil {
			unix.Close(fd)
			return nil, err
		}
		owned := info.Uid == 0 || info.Uid == uint32(os.Geteuid())
		writable := info.Mode&0022 != 0
		// A root-owned sticky ancestor such as /tmp cannot rename another
		// user's owned child. The configured root itself is never shared.
		if !owned || (writable && (info.Uid != 0 || info.Mode&unix.S_ISVTX == 0)) {
			unix.Close(fd)
			return nil, errors.New("secret storage ancestor is not trusted")
		}
	}
	file := os.NewFile(uintptr(fd), root)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil || info.Uid != uint32(os.Geteuid()) || info.Mode&0022 != 0 {
		file.Close()
		return nil, errors.New("secret node root must be owned by the node and not writable by others")
	}
	return file, nil
}

func secretDirectoryIdentity(file *os.File) (unix.Statx_t, error) {
	var info unix.Statx_t
	err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BASIC_STATS|unix.STATX_MNT_ID, &info)
	if err != nil {
		return info, err
	}
	if info.Mask&unix.STATX_MNT_ID == 0 || info.Mnt_id == 0 || info.Mode&unix.S_IFMT != unix.S_IFDIR {
		return info, errors.New("secret directory identity is unavailable")
	}
	return info, nil
}

func sameSecretDirectory(a, b unix.Statx_t) bool {
	return a.Ino == b.Ino && a.Dev_major == b.Dev_major && a.Dev_minor == b.Dev_minor && a.Mnt_id == b.Mnt_id
}

func openSecretDirectory(parent *os.File, name string, create bool) (*os.File, error) {
	if create {
		if err := unix.Mkdirat(int(parent.Fd()), name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, err
		}
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), filepath.Join(parent.Name(), name))
	info, err := secretDirectoryIdentity(dir)
	parentInfo, parentErr := secretDirectoryIdentity(parent)
	if err != nil || parentErr != nil || info.Uid != uint32(os.Geteuid()) || info.Mode&0777 != 0700 || info.Mnt_id != parentInfo.Mnt_id {
		dir.Close()
		return nil, errors.New("secret directory ownership, private mode or mount identity changed")
	}
	return dir, nil
}

func openSecretStorage(rootDir, allocationID string, create bool) (*secretStorage, error) {
	root, err := openSecretNodeRoot(rootDir)
	if err != nil {
		return nil, err
	}
	s := &secretStorage{root: root, rootPath: rootDir, key: secretKey(allocationID)}
	ok := false
	defer func() {
		if !ok {
			s.close()
		}
	}()
	s.vault, err = openSecretDirectory(root, secretStorageName, create)
	if err != nil {
		if !create && errors.Is(err, os.ErrNotExist) {
			return nil, errSecretStorageAbsent
		}
		return nil, err
	}
	if create {
		// A prior partial materialization is cleanup debt, never permission to
		// replace potentially mounted plaintext on a repeated Admit.
		if err := unix.Mkdirat(int(s.vault.Fd()), s.key, 0700); err != nil {
			return nil, fmt.Errorf("create fresh allocation secret directory: %w", err)
		}
	}
	s.allocation, err = openSecretDirectory(s.vault, s.key, false)
	if err != nil {
		if !create && errors.Is(err, os.ErrNotExist) {
			return nil, errSecretStorageAbsent
		}
		return nil, err
	}
	if err := s.verify(); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

func (s *secretStorage) close() {
	for _, file := range []*os.File{s.allocation, s.vault, s.root} {
		if file != nil {
			file.Close()
		}
	}
}

func verifyNamedSecretDirectory(parent, dir *os.File, name string) error {
	current, err := openSecretDirectory(parent, name, false)
	if err != nil {
		return err
	}
	defer current.Close()
	a, err := secretDirectoryIdentity(current)
	b, priorErr := secretDirectoryIdentity(dir)
	if err != nil || priorErr != nil || !sameSecretDirectory(a, b) {
		return errors.New("secret directory was replaced during operation")
	}
	return nil
}

func (s *secretStorage) verify() error {
	current, err := openSecretNodeRoot(s.rootPath)
	if err != nil {
		return err
	}
	a, err := secretDirectoryIdentity(current)
	current.Close()
	b, priorErr := secretDirectoryIdentity(s.root)
	if err != nil || priorErr != nil || !sameSecretDirectory(a, b) {
		return errors.New("secret node root was replaced during operation")
	}
	if err := verifyNamedSecretDirectory(s.root, s.vault, secretStorageName); err != nil {
		return err
	}
	return verifyNamedSecretDirectory(s.vault, s.allocation, s.key)
}

func materializeSecretStorage(rootDir string, request *runtime.StartRequest) ([]*runtime.Mount, func() error, error) {
	allocationID := request.GetAllocationID()
	s, err := openSecretStorage(rootDir, allocationID, true)
	if err != nil {
		return nil, nil, err
	}
	defer s.close()
	want, err := secretDirectoryIdentity(s.allocation)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() error { return cleanupSecretStorageIdentity(rootDir, allocationID, &want) }
	mounts := make([]*runtime.Mount, 0, len(request.GetSecretFiles()))
	for _, item := range request.GetSecretFiles() {
		target := path.Clean(strings.TrimSpace(item.GetPath()))
		name := secretKey(target)
		mode := os.FileMode(item.GetMode())
		if mode == 0 {
			mode = 0400
		}
		if err := s.write(name, item.GetContent(), mode); err != nil {
			return nil, cleanup, err
		}
		mounts = append(mounts, &runtime.Mount{Type: "bind", Source: filepath.Join(rootDir, secretStorageName, s.key, name), Target: target, Options: []string{"ro"}})
	}
	if err := errors.Join(s.allocation.Sync(), s.vault.Sync(), s.root.Sync(), s.verify()); err != nil {
		return nil, cleanup, err
	}
	return mounts, cleanup, nil
}

func (s *secretStorage) write(name string, content []byte, mode os.FileMode) error {
	if err := s.verify(); err != nil {
		return err
	}
	fd, err := unix.Openat(int(s.allocation.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("create private secret file: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	_, writeErr := file.Write(content)
	// The private host parent contains even guest-readable files. Set the
	// requested guest permissions exactly, independent of the service umask.
	modeErr := file.Chmod(mode)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, modeErr, syncErr, closeErr, s.verify()); err != nil {
		return fmt.Errorf("materialize private secret file: %w", err)
	}
	return nil
}

func cleanupSecretStorage(rootDir, allocationID string) error {
	return cleanupSecretStorageIdentity(rootDir, allocationID, nil)
}

func cleanupSecretStorageIdentity(rootDir, allocationID string, want *unix.Statx_t) error {
	s, err := openSecretStorage(rootDir, allocationID, false)
	if errors.Is(err, errSecretStorageAbsent) {
		return nil
	}
	if err != nil {
		return err
	}
	defer s.close()
	if want != nil {
		current, err := secretDirectoryIdentity(s.allocation)
		if err != nil || !sameSecretDirectory(current, *want) {
			return errors.New("owned secret directory changed before cleanup")
		}
	}
	return s.remove()
}

func (s *secretStorage) remove() error {
	if err := s.verify(); err != nil {
		return err
	}
	for {
		entries, err := s.allocation.ReadDir(128)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			var info unix.Stat_t
			if err := unix.Fstatat(int(s.allocation.Fd()), entry.Name(), &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != uint32(os.Geteuid()) || info.Nlink != 1 || info.Mode&0222 != 0 {
				return errors.New("secret cleanup encountered untrusted file ownership or type")
			}
			if err := s.verify(); err != nil {
				return err
			}
			if err := unix.Unlinkat(int(s.allocation.Fd()), entry.Name(), 0); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
	}
	if err := errors.Join(s.allocation.Sync(), s.verify()); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(s.vault.Fd()), s.key, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	return s.vault.Sync()
}
