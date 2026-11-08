// Package serverruntime delegates host operations to the installed native owner.
package serverruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

const helperRelativePath = "libexec/axern/native_install.py"

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var serviceNames = []string{"postgres", "bootstrap", "controld", "tunneld", "imagemgr", "egressd", "axnoded", "node-tunneld", "gatewayd"}

type Runner interface {
	Run(context.Context, io.Writer, io.Writer, string, ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, stdout, stderr io.Writer, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	// The root-owned helper invokes host utilities by name. Keep its existing
	// credential overrides while excluding a caller-selected executable path.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PATH=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "PATH=/usr/sbin:/usr/bin:/sbin:/bin")
	return cmd.Run()
}

type hostFiles interface {
	Lstat(string) (os.FileInfo, error)
	ReadFile(string) ([]byte, error)
}

type diskFiles struct{}

func (diskFiles) Lstat(path string) (os.FileInfo, error) { return os.Lstat(path) }
func (diskFiles) ReadFile(path string) ([]byte, error)   { return os.ReadFile(path) }

type Manager struct {
	runner         Runner
	files          hostFiles
	stdout, stderr io.Writer
	osName         string
}

func New(stdout, stderr io.Writer) Manager {
	return Manager{runner: ExecRunner{}, files: diskFiles{}, stdout: stdout, stderr: stderr, osName: runtime.GOOS}
}

func (m Manager) Operate(ctx context.Context, operation string) error {
	switch operation {
	case "start", "stop", "status", "renew-services":
	default:
		return fmt.Errorf("unsupported server operation %q", operation)
	}
	helper, err := m.installedHelper()
	if err != nil {
		return err
	}
	return m.runner.Run(ctx, m.stdout, m.stderr, "/usr/bin/python3", "-I", helper, operation)
}

type LogOptions struct {
	Service string
	Follow  bool
	Tail    int
}

func (m Manager) Logs(ctx context.Context, options LogOptions) error {
	if options.Tail < 0 || options.Tail > 10000 {
		return fmt.Errorf("log tail must be in [0, 10000]")
	}
	units := []string{"axern-native.target"}
	if options.Service != "" {
		found := false
		for _, name := range serviceNames {
			if options.Service == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown server service %q", options.Service)
		}
		units = []string{"axern-native-" + options.Service + ".service"}
	} else {
		for _, name := range serviceNames {
			units = append(units, "axern-native-"+name+".service")
		}
	}
	if _, err := m.installedHelper(); err != nil {
		return err
	}
	args := []string{"--no-pager", "--output", "short-iso", "--lines", strconv.Itoa(options.Tail)}
	for _, unit := range units {
		args = append(args, "--unit", unit)
	}
	if options.Follow {
		args = append(args, "--follow")
	}
	return m.runner.Run(ctx, m.stdout, m.stderr, "/usr/bin/journalctl", args...)
}

func (m Manager) installedHelper() (string, error) {
	if m.osName != "linux" {
		return "", fmt.Errorf("server operations require the native Linux host; use a context to connect from this platform")
	}
	identityBytes, err := m.ownedFile("/etc/axern/installation.json", 65536)
	if err != nil {
		return "", fmt.Errorf("read installed server identity: %w", err)
	}
	var identity struct {
		Schema         string `json:"schema"`
		ArchiveSHA256  string `json:"archive_sha256"`
		SourceRevision string `json:"source_revision"`
	}
	if err := json.Unmarshal(identityBytes, &identity); err != nil || identity.Schema != "axern.native-install.v1" || !digestPattern.MatchString(identity.ArchiveSHA256) {
		return "", fmt.Errorf("invalid installed server identity")
	}
	marker, err := m.ownedFile("/etc/axern/installed", 128)
	if err != nil || strings.TrimSpace(string(marker)) != identity.ArchiveSHA256 {
		return "", fmt.Errorf("server installation is incomplete or its installed identity differs")
	}
	release := filepath.Join("/opt/axern/releases", identity.ArchiveSHA256)
	manifestBytes, err := m.ownedFile(filepath.Join(release, "manifest.json"), 65536)
	if err != nil {
		return "", fmt.Errorf("read installed server manifest: %w", err)
	}
	var manifest struct {
		Schema         string `json:"schema"`
		OS             string `json:"os"`
		SourceRevision string `json:"source_revision"`
		Files          []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.Schema != "axern.native-runtime.v2" || manifest.OS != "linux" || identity.SourceRevision == "" || manifest.SourceRevision != identity.SourceRevision {
		return "", fmt.Errorf("installed server manifest does not match its identity")
	}
	helper := filepath.Join(release, helperRelativePath)
	data, err := m.ownedFile(helper, 1024*1024)
	if err != nil {
		return "", fmt.Errorf("read installed server helper: %w", err)
	}
	checksum := sha256.Sum256(data)
	matches := 0
	for _, entry := range manifest.Files {
		if entry.Path == helperRelativePath {
			if !digestPattern.MatchString(entry.SHA256) || entry.SHA256 != hex.EncodeToString(checksum[:]) || entry.Size != int64(len(data)) {
				return "", fmt.Errorf("installed server helper differs from its manifest")
			}
			matches++
		}
	}
	if matches != 1 {
		return "", fmt.Errorf("installed server manifest must identify exactly one lifecycle helper")
	}
	return helper, nil
}

func (m Manager) ownedFile(path string, limit int) ([]byte, error) {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := m.files.Lstat(parent)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || !rootOwned(info) {
			return nil, fmt.Errorf("managed server ancestor is not a root-owned non-writable directory: %s", parent)
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	info, err := m.files.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || !rootOwned(info) {
		return nil, fmt.Errorf("managed server file is not root-owned and non-writable: %s", path)
	}
	data, err := m.files.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > limit || bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("managed server file is invalid or too large: %s", path)
	}
	return data, nil
}
