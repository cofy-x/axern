//go:build linux

package proc

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxChildEnvironment = 4 << 20
const environmentSeals = unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE

// PrepareUnprivilegedCommand keeps the existing command's PID, session, pipes,
// environment and wait owner. The same trusted daemon image reduces its child
// credentials before replacing itself with the resolved caller program.
// Call cleanup after Start returns, including when starting the child fails.
func PrepareUnprivilegedCommand(cmd *exec.Cmd, uid, gid uint32) (func(), error) {
	if cmd == nil || cmd.Path == "" || len(cmd.Args) == 0 || uid == 0 || len(cmd.ExtraFiles) != 0 {
		return nil, fmt.Errorf("invalid unprivileged process command")
	}
	environment := cmd.Env
	if environment == nil {
		environment = os.Environ()
	}
	encoded, err := json.Marshal(environment)
	if err != nil || len(encoded) > maxChildEnvironment {
		return nil, fmt.Errorf("invalid unprivileged process environment")
	}
	fd, err := unix.MemfdCreate("axern-child-environment", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, fmt.Errorf("create private child environment: %w", err)
	}
	file := os.NewFile(uintptr(fd), "axern-child-environment")
	closeFile := func() { _ = file.Close() }
	if _, err := file.Write(encoded); err != nil {
		closeFile()
		return nil, fmt.Errorf("write private child environment: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		closeFile()
		return nil, fmt.Errorf("rewind private child environment: %w", err)
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, environmentSeals); err != nil {
		closeFile()
		return nil, fmt.Errorf("seal private child environment: %w", err)
	}
	argv := []string{"/proc/self/exe", InternalExecCommand,
		strconv.FormatUint(uint64(uid), 10), strconv.FormatUint(uint64(gid), 10), "--", cmd.Path}
	argv = append(argv, cmd.Args...)
	cmd.Path, cmd.Args = "/proc/self/exe", argv
	// No caller-controlled Go runtime or dynamic-loader settings may run
	// while the trusted child still has root setup capabilities.
	cmd.Env = []string{}
	cmd.ExtraFiles = []*os.File{file}
	return closeFile, nil
}

// RunInternalExec is called by explicit main dispatch, never package init.
// Error text never contains command arguments, environment or credential data.
func RunInternalExec(args []string, stderr io.Writer) int {
	if err := runInternalExec(args); err != nil {
		fmt.Fprintln(stderr, "axern-sandboxd: unprivileged process preparation failed:", err)
		return 126
	}
	return 0 // exec never returns on success.
}

func runInternalExec(args []string) error {
	if len(args) < 5 || args[2] != "--" || args[3] == "" || args[4] == "" {
		return fmt.Errorf("invalid internal process request")
	}
	uid, uidErr := strconv.ParseUint(args[0], 10, 32)
	gid, gidErr := strconv.ParseUint(args[1], 10, 32)
	if uidErr != nil || gidErr != nil || uid == 0 {
		return fmt.Errorf("invalid internal process identity")
	}
	// Capability operations and exec must apply to the same OS thread. Exec
	// atomically discards the other Go runtime threads in this private child.
	runtime.LockOSThread()
	// Bootstrap has an empty environment, so Getwd cannot trust caller PWD.
	// Capture the actual absolute directory before changing credentials.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("capture process working directory: %w", err)
	}
	before, err := os.Stat(".")
	if err != nil {
		return fmt.Errorf("inspect process working directory: %w", err)
	}
	if err := dropProcessPrivilege(uint32(uid), uint32(gid)); err != nil {
		return err
	}
	// Rewalk every ancestor as the final principal. Merely checking "."
	// would retain access obtained through a root-only parent directory.
	if err := unix.Chdir(cwd); err != nil {
		return fmt.Errorf("validate unprivileged working directory: %w", err)
	}
	after, err := os.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		return fmt.Errorf("working directory identity changed")
	}
	environment, err := readChildEnvironment()
	if err != nil {
		return err
	}
	if err := unix.Exec(args[3], args[4:], environment); err != nil {
		return fmt.Errorf("execute unprivileged process: %w", err)
	}
	return nil
}

// Descriptor 3 is the sole inherited extra file in this fixed internal protocol.
// Parse caller-controlled data only after every privilege set has been reduced.
func readChildEnvironment() ([]string, error) {
	file := os.NewFile(3, "axern-child-environment")
	if file == nil {
		return nil, fmt.Errorf("private child environment unavailable")
	}
	defer file.Close()
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&environmentSeals != environmentSeals {
		return nil, fmt.Errorf("private child environment is not sealed")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxChildEnvironment {
		return nil, fmt.Errorf("private child environment has invalid size/type")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxChildEnvironment+1))
	if err != nil || len(data) > maxChildEnvironment {
		return nil, fmt.Errorf("read private child environment")
	}
	var environment []string
	if json.Unmarshal(data, &environment) != nil {
		return nil, fmt.Errorf("invalid private child environment")
	}
	return environment, nil
}

func dropProcessPrivilege(uid, gid uint32) error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no-new-privileges: %w", err)
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("clear ambient capabilities: %w", err)
	}
	// Discover the kernel's actual capability ceiling; do not silently retain
	// capabilities introduced after the compiled CAP_LAST_CAP constant.
	last := -1
	for capability := 0; capability <= 64; capability++ {
		present, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, uintptr(capability), 0, 0, 0)
		if err == syscall.EINVAL {
			last = capability - 1
			break
		}
		if err != nil || capability == 64 {
			return fmt.Errorf("inspect capability bounding ceiling")
		}
		if present != 0 {
			if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0); err != nil {
				return fmt.Errorf("clear capability bounding set: %w", err)
			}
		}
	}
	if last < 0 {
		return fmt.Errorf("capability bounding controls unavailable")
	}
	if os.Geteuid() == 0 {
		if err := syscall.Setgroups(nil); err != nil {
			return fmt.Errorf("clear supplementary groups: %w", err)
		}
		if err := syscall.Setresgid(int(gid), int(gid), int(gid)); err != nil {
			return fmt.Errorf("set process group identity: %w", err)
		}
		if err := syscall.Setresuid(int(uid), int(uid), int(uid)); err != nil {
			return fmt.Errorf("set process user identity: %w", err)
		}
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capset(&header, &data[0]); err != nil {
		return fmt.Errorf("clear process capability sets: %w", err)
	}
	if err := verifyUnprivilegedProcess(uid, gid, last); err != nil {
		return err
	}

	return nil
}

func verifyUnprivilegedProcess(uid, gid uint32, last int) error {
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	if ruid != int(uid) || euid != int(uid) || suid != int(uid) || rgid != int(gid) || egid != int(gid) || sgid != int(gid) {
		return fmt.Errorf("process credential readback mismatch")
	}
	groups, err := syscall.Getgroups()
	if err != nil || len(groups) != 0 {
		return fmt.Errorf("supplementary group readback mismatch")
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capget(&header, &data[0]); err != nil {
		return fmt.Errorf("read process capability sets: %w", err)
	}
	if data[0] != (unix.CapUserData{}) || data[1] != (unix.CapUserData{}) {
		return fmt.Errorf("process capability readback mismatch")
	}
	for capability := 0; capability <= last; capability++ {
		bounding, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, uintptr(capability), 0, 0, 0)
		ambient, ambientErr := unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, uintptr(capability), 0, 0)
		if err != nil || ambientErr != nil || bounding != 0 || ambient != 0 {
			return fmt.Errorf("bounding or ambient capability readback mismatch")
		}
	}
	noNewPrivileges, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || noNewPrivileges != 1 {
		return fmt.Errorf("no-new-privileges readback mismatch")
	}
	return nil
}
