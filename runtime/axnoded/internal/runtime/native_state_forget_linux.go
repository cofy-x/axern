//go:build linux

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var nativeStateAllocationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// nativeRuntimeStateAbsent proves only that the exact state file is absent
// beneath the trusted configured runtime root. The caller owns lifecycle
// serialization; this read-only check never creates a runsc lock or repairs state.
func nativeRuntimeStateAbsent(ctx context.Context, runtimeRoot, allocationID string) (bool, error) {
	if ctx == nil || !nativeStateAllocationID.MatchString(allocationID) {
		return false, errors.New("native state absence requires context and an exact allocation identity")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	root, err := openNativeStateRoot(runtimeRoot)
	if err != nil {
		return false, err
	}
	defer unix.Close(root)
	name := allocationID + "_sandbox:" + allocationID + ".state"
	var info unix.Stat_t
	err = unix.Fstatat(root, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	absent := errors.Is(err, unix.ENOENT)
	if err != nil && !absent {
		return false, fmt.Errorf("inspect exact native runtime state: %w", err)
	}
	if !absent {
		if err := validateNativeStateFileInfo(name, &info); err != nil {
			return false, err
		}
	}
	if err := verifyNativeStateRootPath(runtimeRoot, root); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return absent, nil
}

// forgetNativeRuntimeState removes only pinned runsc metadata after the caller
// proves termination against durable kernel ownership. It never invokes runsc
// or signals a PID from the saved state, whose cached process IDs may be reused.
func forgetNativeRuntimeState(ctx context.Context, runtimeRoot, allocationID, bundleDir string, verifyTerminated func(int) error) error {
	if ctx == nil || verifyTerminated == nil {
		return errors.New("native state retirement requires context and kernel termination proof")
	}
	if !nativeStateAllocationID.MatchString(allocationID) || !filepath.IsAbs(bundleDir) || filepath.Clean(bundleDir) != bundleDir {
		return errors.New("native state retirement requires exact allocation and bundle identities")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := openNativeStateRoot(runtimeRoot)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	base := allocationID + "_sandbox:" + allocationID
	stateName, lockName := base+".state", base+".lock"
	var initial unix.Stat_t
	if err := unix.Fstatat(root, stateName, &initial, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		// A crash between the two unlinks may leave the old lock behind.
		// The lifecycle owner serializes probe ID reuse; absence grants no
		// permission to guess a PID or repair a lock.
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect native runtime state: %w", err)
	}
	if err := validateNativeStateFileInfo(stateName, &initial); err != nil {
		return err
	}
	var lockInfo unix.Stat_t
	if err := unix.Fstatat(root, lockName, &lockInfo, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("inspect existing native runtime state lock: %w", err)
	}
	if err := validateNativeStateFileInfo(lockName, &lockInfo); err != nil {
		return err
	}
	lock, err := unix.Openat(root, lockName, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open existing native runtime state lock: %w", err)
	}
	defer unix.Close(lock)
	if err := verifyNativeStateFile(root, lockName, lock); err != nil {
		return err
	}
	for {
		err := unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("lock native runtime state: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("lock native runtime state: %w", ctx.Err())
		case <-timer.C:
		}
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	if err := verifyNativeStateFile(root, lockName, lock); err != nil {
		return err
	}
	stateFD, err := unix.Openat(root, stateName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open native runtime state: %w", err)
	}
	state := os.NewFile(uintptr(stateFD), stateName)
	defer state.Close()
	if err := verifyNativeStateFile(root, stateName, stateFD); err != nil {
		return err
	}
	const maximumStateBytes = 16 << 20
	payload, err := io.ReadAll(io.LimitReader(state, maximumStateBytes+1))
	if err != nil {
		return fmt.Errorf("read native runtime state: %w", err)
	}
	if len(payload) > maximumStateBytes {
		return fmt.Errorf("native runtime state exceeds %d bytes", maximumStateBytes)
	}
	pid, socketPath, err := validateNativeSavedState(payload, runtimeRoot, allocationID, bundleDir)
	if err != nil {
		return err
	}
	socket, err := openNativeControlSocket(runtimeRoot, allocationID, socketPath)
	if err != nil {
		return err
	}
	defer socket.close()
	if err := verifyTerminated(pid); err != nil {
		return fmt.Errorf("verify native kernel termination under state lock: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyNativeStateRootPath(runtimeRoot, root); err != nil {
		return err
	}
	if err := verifyNativeStateFile(root, lockName, lock); err != nil {
		return err
	}
	if err := verifyNativeStateFile(root, stateName, stateFD); err != nil {
		return err
	}
	// Retire the saved control socket before losing the only record naming
	// it. Internal probes reuse their identity after ordered cleanup; leaking
	// this socket eventually exhausts runsc's fallback binding locations.
	if err := socket.retire(ctx); err != nil {
		return err
	}
	if err := unix.Unlinkat(root, stateName, 0); err != nil {
		return fmt.Errorf("forget exact native runtime state: %w", err)
	}
	if err := verifyNativeStateFile(root, lockName, lock); err != nil {
		return err
	}
	if err := unix.Unlinkat(root, lockName, 0); err != nil {
		return fmt.Errorf("forget exact native runtime lock: %w", err)
	}
	if err := unix.Fsync(root); err != nil {
		return fmt.Errorf("persist native runtime state retirement: %w", err)
	}
	return nil
}

func openNativeStateRoot(root string) (int, error) {
	if !filepath.IsAbs(root) || root == "/" || filepath.Clean(root) != root {
		return -1, errors.New("native runtime root must be a canonical absolute directory")
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, err := unix.Open("/", flags, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		next, err := unix.Openat(fd, component, flags, 0)
		unix.Close(fd)
		if err != nil {
			return -1, fmt.Errorf("open native runtime root without symlinks: %w", err)
		}
		fd = next
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		unix.Close(fd)
		return -1, err
	}
	if info.Uid != uint32(os.Geteuid()) || info.Mode&0022 != 0 {
		unix.Close(fd)
		return -1, errors.New("native runtime root has untrusted ownership or permissions")
	}
	return fd, nil
}

func verifyNativeStateRootPath(runtimeRoot string, root int) error {
	currentRoot, err := openNativeStateRoot(runtimeRoot)
	if err != nil {
		return err
	}
	defer unix.Close(currentRoot)
	var openedRoot, currentRootInfo unix.Stat_t
	if err := errors.Join(unix.Fstat(root, &openedRoot), unix.Fstat(currentRoot, &currentRootInfo)); err != nil {
		return fmt.Errorf("recheck native runtime root: %w", err)
	}
	if openedRoot.Dev != currentRootInfo.Dev || openedRoot.Ino != currentRootInfo.Ino {
		return errors.New("native runtime root was replaced during verification")
	}
	return nil
}

func verifyNativeStateFile(root int, name string, fd int) error {
	var opened, current unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return err
	}
	if err := validateNativeStateFileInfo(name, &opened); err != nil {
		return err
	}
	if err := unix.Fstatat(root, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("recheck native runtime file %s: %w", name, err)
	}
	if current.Dev != opened.Dev || current.Ino != opened.Ino {
		return fmt.Errorf("native runtime file %s was replaced", name)
	}
	return nil
}

func validateNativeStateFileInfo(name string, info *unix.Stat_t) error {
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Nlink != 1 || info.Uid != uint32(os.Geteuid()) || info.Mode&0022 != 0 {
		return fmt.Errorf("native runtime file %s is not a trusted single-link regular file", name)
	}
	return nil
}

func validateNativeSavedState(payload []byte, root, id, bundle string) (int, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := validateNativeStateJSON(decoder, 0, ""); err != nil {
		return 0, "", fmt.Errorf("decode native runtime state: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return 0, "", errors.New("native runtime state contains trailing JSON")
	}
	var state struct {
		ID, BundleDir, Status string
		Sandbox               *struct {
			ID                string
			PID               int
			ControlSocketPath string
		}
		Saver *struct {
			RootDir string
			ID      *struct{ SandboxID, ContainerID string }
		}
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return 0, "", fmt.Errorf("decode native runtime identity: %w", err)
	}
	if state.ID != id || state.BundleDir != bundle || state.Sandbox == nil || state.Sandbox.ID != id || state.Sandbox.PID <= 0 || state.Saver == nil || state.Saver.RootDir != root || state.Saver.ID == nil || state.Saver.ID.SandboxID != id || state.Saver.ID.ContainerID != id {
		return 0, "", errors.New("native runtime state does not match the admitted allocation, bundle and runtime root")
	}
	switch state.Status {
	case "created", "running", "paused", "stopped":
		// Saved status is only format validation. It may be stale and never
		// substitutes for the callback's fenced kernel termination proof.
	default:
		return 0, "", errors.New("native runtime state has an unknown saved status")
	}
	return state.Sandbox.PID, state.Sandbox.ControlSocketPath, nil
}

func validateNativeStateJSON(decoder *json.Decoder, depth int, objectPath string) error {
	if depth > 64 {
		return errors.New("native runtime JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	keys := make(map[string]struct{})
	for decoder.More() {
		childPath := objectPath + "[]"
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid native runtime JSON object key")
			}
			// encoding/json matches identity fields case-insensitively. Reject
			// ambiguous duplicates rather than trusting the last spelling.
			canonical := name
			if objectPath == "" || objectPath == ".sandbox" || objectPath == ".saver" || objectPath == ".saver.id" {
				canonical = strings.ToLower(name)
			}
			if _, duplicate := keys[canonical]; duplicate {
				return fmt.Errorf("duplicate native runtime JSON field %q", name)
			}
			keys[canonical] = struct{}{}
			childPath = objectPath + "." + strings.ToLower(name)
		}
		if err := validateNativeStateJSON(decoder, depth+1, childPath); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
