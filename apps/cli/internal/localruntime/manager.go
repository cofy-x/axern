package localruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/config"
	"github.com/cofy-x/axern/apps/cli/internal/localbundle"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
)

type Runner interface {
	Run(context.Context, io.Writer, io.Writer, string, ...string) error
	Output(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	value, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("%s: %s", name, message)
		}
		return nil, err
	}
	return value, nil
}

type Manager struct {
	Version    string
	ConfigPath string
	Stdout     io.Writer
	Stderr     io.Writer
	Runner     Runner
	Dir        string
	removeAll  func(string) error
}

func New(version, configPath string, stdout, stderr io.Writer) (*Manager, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	return &Manager{Version: normalizeVersion(version), ConfigPath: configPath, Stdout: stdout, Stderr: stderr, Runner: ExecRunner{}, Dir: dir}, nil
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	if value == "" {
		return "dev"
	}
	return value
}

func (m *Manager) Path() string { return m.Dir }

func (m *Manager) Up(ctx context.Context, options UpOptions) error {
	if options.Profile != "" && options.Profile != "default" && options.Profile != "observability" {
		return fmt.Errorf("local profile must be default or observability")
	}
	release, err := m.lock()
	if err != nil {
		return err
	}
	defer release()
	return m.up(ctx, options)
}

func (m *Manager) up(ctx context.Context, options UpOptions) error {
	existing, metadataErr := loadMetadata(m.metadataPath())
	if metadataErr == nil && existing.Version != m.Version {
		return fmt.Errorf("local stack version %s does not match CLI version %s; run `axern local upgrade`", existing.Version, m.Version)
	} else if metadataErr != nil && !errors.Is(metadataErr, os.ErrNotExist) {
		return metadataErr
	}
	if metadataErr == nil && options.Profile == "" {
		options.Profile = existing.Profile
	}
	if options.Profile == "default" {
		options.Profile = ""
	}
	if metadataErr == nil && options.Profile == existing.Profile {
		if status, statusErr := m.Status(ctx); statusErr == nil && status.State == "running" && m.localNodeReady(ctx, &http.Client{Timeout: 3 * time.Second}) {
			if err := m.writeContext(options.Use); err != nil {
				return err
			}
			m.printReady()
			return nil
		}
	}
	if report := m.doctor(ctx, false); !report.Healthy {
		return fmt.Errorf("local prerequisites are not ready; run `axern local doctor`")
	}
	if err := m.materialize(options.Profile); err != nil {
		return err
	}
	if metadataErr == nil && existing.Profile == "observability" && options.Profile == "" {
		if err := m.composeRun(ctx, existing.Profile, "rm", "-sf", "otel-collector", "otel-lgtm"); err != nil {
			return fmt.Errorf("disable observability profile: %w", err)
		}
	}
	fmt.Fprintln(m.Stderr, "Starting Axern local services...")
	if err := m.composeRun(ctx, options.Profile, "pull", "postgres", "minio", "controld", "tunneld", "node", "gatewayd"); err != nil {
		return err
	}
	if err := m.composeRun(ctx, options.Profile, "up", "-d", "postgres", "minio"); err != nil {
		return err
	}
	_ = m.composeRun(ctx, options.Profile, "rm", "-sf", "controld-migrate", "controld-access-bootstrap")
	if err := m.composeRun(ctx, options.Profile, "up", "--force-recreate", "--exit-code-from", "controld-access-bootstrap", "controld-access-bootstrap"); err != nil {
		return err
	}
	services := []string{"storaged", "controld", "controld-retention", "tunneld", "node", "gatewayd"}
	if options.Profile == "observability" {
		services = append(services, "otel-lgtm", "otel-collector")
	}
	args := append([]string{"up", "-d", "--remove-orphans"}, services...)
	if err := m.composeRun(ctx, options.Profile, args...); err != nil {
		return err
	}
	if err := m.waitReady(ctx, 3*time.Minute); err != nil {
		m.printStartupDiagnostics(options.Profile)
		return err
	}
	if err := m.writeContext(options.Use); err != nil {
		return err
	}
	metadata := Metadata{Version: m.Version, Profile: options.Profile, UpdatedAt: time.Now().UTC()}
	if metadataErr == nil {
		metadata.CreatedAt = existing.CreatedAt
	}
	if metadata.CreatedAt.IsZero() {
		metadata.CreatedAt = metadata.UpdatedAt
	}
	if err := saveMetadata(m.metadataPath(), metadata); err != nil {
		return err
	}
	m.printReady()
	return nil
}

func (m *Manager) printStartupDiagnostics(profile string) {
	fmt.Fprintln(m.Stderr, "Axern did not become ready; recent service status follows.")
	_ = m.composeRun(context.Background(), profile, "ps")
	fmt.Fprintln(m.Stderr, "Recent core service logs follow.")
	_ = m.composeRun(context.Background(), profile, "logs", "--no-color", "--tail", "80", "storaged", "controld", "tunneld", "node", "gatewayd")
}

func (m *Manager) printReady() {
	fmt.Fprintln(m.Stdout, "Axern local is ready.")
	fmt.Fprintf(m.Stdout, "Dashboard: http://127.0.0.1:%d\n", GatewayHTTPPort)
	fmt.Fprintln(m.Stdout, "Context:   local")
	fmt.Fprintln(m.Stdout, "Next:      axern run python:3.12-slim -- python -c 'print(\"hello from axern\")'")
}

func (m *Manager) Down(ctx context.Context) error {
	release, err := m.lock()
	if err != nil {
		return err
	}
	defer release()
	return m.down(ctx, false)
}

func (m *Manager) down(ctx context.Context, removeVolumes bool) error {
	if _, err := os.Stat(m.envPath()); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(m.Stdout, "Axern local is not initialized.")
		return nil
	}
	metadata, _ := loadMetadata(m.metadataPath())
	args := []string{"down", "--remove-orphans"}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	if err := m.composeRun(ctx, metadata.Profile, args...); err != nil {
		return err
	}
	if !removeVolumes {
		fmt.Fprintf(m.Stdout, "Axern local stopped. Data retained at %s\n", m.Dir)
	}
	return nil
}

func (m *Manager) Reset(ctx context.Context) error {
	clean, err := m.validatedResetPath()
	if err != nil {
		return err
	}
	release, err := m.lock()
	if err != nil {
		return err
	}
	defer release()
	helperImage := m.backupHelperImage()
	if err := m.down(ctx, true); err != nil {
		return err
	}
	if err := m.removeLocalData(ctx, clean, helperImage); err != nil {
		return err
	}
	if err := m.removeContext(); err != nil {
		return err
	}
	fmt.Fprintln(m.Stdout, "Axern local data was removed and cannot be recovered.")
	return nil
}

func (m *Manager) validatedResetPath() (string, error) {
	expected, err := DataDir()
	if err != nil {
		return "", fmt.Errorf("resolve expected local data path: %w", err)
	}
	clean, err := filepath.Abs(m.Dir)
	if err != nil {
		return "", fmt.Errorf("resolve local data path %q: %w", m.Dir, err)
	}
	expected, err = filepath.Abs(expected)
	if err != nil {
		return "", fmt.Errorf("resolve expected local data path %q: %w", expected, err)
	}
	if clean != expected || filepath.Base(clean) != ContextName {
		return "", fmt.Errorf("refusing to reset unexpected local path %q", m.Dir)
	}
	configPath := m.ConfigPath
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve Axern config path: %w", err)
	}
	if pathWithin(clean, configPath) {
		return "", fmt.Errorf("refusing to reset local data containing Axern config %q; move --config outside %q first", configPath, clean)
	}
	info, err := os.Lstat(clean)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing to reset symlinked local path %q", m.Dir)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("refusing to reset non-directory local path %q", m.Dir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect local data path %q: %w", m.Dir, err)
	}
	return clean, nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (m *Manager) removeLocalData(ctx context.Context, clean, helperImage string) error {
	removeAll := os.RemoveAll
	if m.removeAll != nil {
		removeAll = m.removeAll
	}
	hostErr := removeAll(clean)
	if hostErr == nil {
		return nil
	}
	if err := os.MkdirAll(clean, 0o700); err != nil {
		return fmt.Errorf("prepare local data cleanup after %v: %w", hostErr, err)
	}
	script := `set -eu
for path in /source/* /source/.[!.]* /source/..?*; do
  [ -e "$path" ] || [ -L "$path" ] || continue
  rm -rf -- "$path"
done`
	if err := m.Runner.Run(ctx, m.Stdout, m.Stderr, "docker", "run", "--rm", "--network", "none", "--read-only", "--user", "0:0", "--entrypoint", "/bin/sh", "-v", clean+":/source", helperImage, "-c", script); err != nil {
		return fmt.Errorf("remove root-owned local data after host cleanup failed (%v): %w", hostErr, err)
	}
	if err := removeAll(clean); err != nil {
		return fmt.Errorf("remove local data after root cleanup: %w", err)
	}
	return nil
}

func (m *Manager) removeContext() error {
	cfg, err := config.Load(m.ConfigPath)
	if err != nil {
		return err
	}
	delete(cfg.Contexts, ContextName)
	if cfg.CurrentContext == ContextName {
		cfg.CurrentContext = ""
	}
	return config.Save(m.ConfigPath, cfg)
}

func (m *Manager) Upgrade(ctx context.Context) error {
	release, lockErr := m.lock()
	if lockErr != nil {
		return lockErr
	}
	defer release()
	existing, err := loadMetadata(m.metadataPath())
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("local stack is not initialized; run `axern local up`")
	}
	if err != nil {
		return err
	}
	if existing.Version == m.Version {
		fmt.Fprintf(m.Stdout, "Axern local is already at version %s.\n", m.Version)
		return nil
	}
	if versionLess(m.Version, existing.Version) {
		return fmt.Errorf("downgrade from %s to %s is not supported", existing.Version, m.Version)
	}
	if !supportedUpgrade(existing.Version, m.Version) {
		return fmt.Errorf("no supported local migration path exists from %s to %s; run `axern local reset`", existing.Version, m.Version)
	}
	used, _ := directorySize(m.Dir)
	free, err := availableDisk(m.Dir)
	if err != nil {
		return fmt.Errorf("inspect free disk before upgrade: %w", err)
	}
	if free < used+(2<<30) {
		return fmt.Errorf("upgrade requires at least the current data size plus 2 GiB free; need %d bytes, have %d", used+(2<<30), free)
	}
	backup := filepath.Join(m.Dir, "backups", time.Now().UTC().Format("20060102T150405Z")+"-"+existing.Version)
	if err := m.down(ctx, false); err != nil {
		return err
	}
	if err := m.createBackup(ctx, backup); err != nil {
		_ = m.composeRun(context.Background(), existing.Profile, "up", "-d")
		return fmt.Errorf("create upgrade backup: %w", err)
	}
	if err := os.Remove(m.metadataPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := m.up(ctx, UpOptions{Profile: existing.Profile}); err != nil {
		upgradeErr := err
		_ = m.composeRun(context.Background(), existing.Profile, "down", "--remove-orphans")
		if restoreErr := m.restoreBackup(backup); restoreErr != nil {
			return fmt.Errorf("upgrade failed (%v) and automatic restore failed (%v); backup retained at %s", upgradeErr, restoreErr, backup)
		}
		if startErr := m.composeRun(context.Background(), existing.Profile, "up", "-d"); startErr != nil {
			return fmt.Errorf("upgrade failed (%v); old data and deployment were restored at %s but restart failed: %w", upgradeErr, backup, startErr)
		}
		return fmt.Errorf("upgrade failed and the previous stack was restored from %s: %w", backup, upgradeErr)
	}
	fmt.Fprintf(m.Stdout, "Upgraded Axern local from %s to %s. Backup: %s\n", existing.Version, m.Version, backup)
	return nil
}

func (m *Manager) restoreBackup(backup string) error {
	archive := filepath.Join(backup, "local-snapshot.tar")
	if _, err := os.Stat(archive); err != nil {
		return fmt.Errorf("upgrade snapshot is unavailable: %w", err)
	}
	image := m.backupHelperImage()
	script := `set -eu
for path in /source/* /source/.[!.]* /source/..?*; do
  [ -e "$path" ] || continue
  [ "$path" = /source/backups ] || rm -rf "$path"
done
tar -xf /backup/local-snapshot.tar -C /source`
	return m.Runner.Run(context.Background(), m.Stdout, m.Stderr, "docker", "run", "--rm", "--user", "0:0", "--entrypoint", "/bin/sh", "-v", m.Dir+":/source", "-v", backup+":/backup:ro", image, "-c", script)
}

func (m *Manager) createBackup(ctx context.Context, backup string) error {
	if err := os.MkdirAll(backup, 0o700); err != nil {
		return err
	}
	image := m.backupHelperImage()
	owner := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	script := `set -eu
tar --exclude='./backups' -cf /backup/local-snapshot.tar -C /source .
chown "$1" /backup/local-snapshot.tar
chmod 0600 /backup/local-snapshot.tar`
	return m.Runner.Run(ctx, m.Stdout, m.Stderr, "docker", "run", "--rm", "--user", "0:0", "--entrypoint", "/bin/sh", "-v", m.Dir+":/source:ro", "-v", backup+":/backup", image, "-c", script, "backup", owner)
}

func (m *Manager) backupHelperImage() string {
	data, err := os.ReadFile(m.envPath())
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok && key == "POSTGRES_IMAGE" {
				value = strings.Trim(strings.TrimSpace(value), `"`)
				if value != "" {
					return value
				}
			}
		}
	}
	return localbundle.ImageReferences(m.Version)["POSTGRES_IMAGE"]
}

func versionLess(left, right string) bool {
	type parsedVersion struct {
		core       [3]int
		prerelease []string
	}
	parse := func(value string) parsedVersion {
		value = strings.TrimPrefix(strings.SplitN(value, "+", 2)[0], "v")
		parts := strings.SplitN(value, "-", 2)
		var result parsedVersion
		for i, item := range strings.Split(parts[0], ".") {
			if i >= len(result.core) {
				break
			}
			result.core[i], _ = strconv.Atoi(item)
		}
		if len(parts) == 2 {
			result.prerelease = strings.Split(parts[1], ".")
		}
		return result
	}
	l, r := parse(left), parse(right)
	for i := range l.core {
		if l.core[i] != r.core[i] {
			return l.core[i] < r.core[i]
		}
	}
	if len(l.prerelease) == 0 || len(r.prerelease) == 0 {
		return len(l.prerelease) > 0 && len(r.prerelease) == 0
	}
	for i := 0; i < len(l.prerelease) && i < len(r.prerelease); i++ {
		if l.prerelease[i] == r.prerelease[i] {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(l.prerelease[i])
		rightNumber, rightErr := strconv.Atoi(r.prerelease[i])
		switch {
		case leftErr == nil && rightErr == nil:
			return leftNumber < rightNumber
		case leftErr == nil:
			return true
		case rightErr == nil:
			return false
		default:
			return l.prerelease[i] < r.prerelease[i]
		}
	}
	return len(l.prerelease) < len(r.prerelease)
}

func supportedUpgrade(from, to string) bool {
	parseCore := func(value string) [3]int {
		value = strings.TrimPrefix(strings.SplitN(strings.SplitN(value, "+", 2)[0], "-", 2)[0], "v")
		var core [3]int
		for i, item := range strings.Split(value, ".") {
			if i >= len(core) {
				break
			}
			core[i], _ = strconv.Atoi(item)
		}
		return core
	}
	current, target := parseCore(from), parseCore(to)
	if current == target {
		return true
	}
	if target[0] == 0 {
		return current[0] == 0 && (current[1] == target[1] || current[1]+1 == target[1])
	}
	return current[0] == target[0] && current[1] <= target[1]
}

func (m *Manager) Logs(ctx context.Context, options LogOptions) error {
	metadata, _ := loadMetadata(m.metadataPath())
	args := []string{"logs", "--tail", strconv.Itoa(options.Tail)}
	if options.Follow {
		args = append(args, "--follow")
	}
	if options.Since != "" {
		args = append(args, "--since", options.Since)
	}
	if options.Component != "" {
		args = append(args, options.Component)
	}
	return m.composeRun(ctx, metadata.Profile, args...)
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	status := Status{State: "not-initialized", CLIVersion: m.Version, DataPath: m.Dir, DashboardURL: fmt.Sprintf("http://127.0.0.1:%d", GatewayHTTPPort), GatewayTarget: fmt.Sprintf("127.0.0.1:%d", GatewayControlPort), Ports: map[string]int{"gateway_grpc": GatewayControlPort, "gateway_http": GatewayHTTPPort, "gateway_ssh": GatewaySSHPort, "control_http": 24101, "postgres": 25432, "minio_api": 29000, "minio_console": 29001}}
	metadata, err := loadMetadata(m.metadataPath())
	if err == nil {
		status.StackVersion, status.Profile = metadata.Version, metadata.Profile
		status.State = "stopped"
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}
	status.DiskBytes, _ = directorySize(m.Dir)
	if cfg, err := config.Load(m.ConfigPath); err == nil {
		status.CurrentContext = cfg.CurrentContext
		status.ContextCurrent = cfg.CurrentContext == ContextName
	}
	if _, err := os.Stat(m.envPath()); err != nil {
		return status, nil
	}
	data, err := m.composeOutput(ctx, metadata.Profile, "ps", "--format", "json")
	if err != nil {
		return status, nil
	}
	status.Components = parseComposePS(data)
	if len(status.Components) > 0 {
		status.State = "running"
		for _, component := range status.Components {
			if component.State != "running" || (component.Health != "" && component.Health != "healthy") {
				status.State = "degraded"
			}
		}
	}
	return status, nil
}

func parseComposePS(data []byte) []Component {
	type row struct {
		Service string `json:"Service"`
		Name    string `json:"Name"`
		State   string `json:"State"`
		Health  string `json:"Health"`
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		for _, line := range bytes.Split(data, []byte("\n")) {
			var value row
			if json.Unmarshal(line, &value) == nil && (value.Service != "" || value.Name != "") {
				rows = append(rows, value)
			}
		}
	}
	components := make([]Component, 0, len(rows))
	for _, value := range rows {
		name := value.Service
		if name == "" {
			name = value.Name
		}
		components = append(components, Component{Name: name, State: strings.ToLower(value.State), Health: strings.ToLower(value.Health)})
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	return components
}

func (m *Manager) Doctor(ctx context.Context) DoctorReport {
	return m.doctor(ctx, true)
}

func (m *Manager) doctor(ctx context.Context, inspectRuntime bool) DoctorReport {
	report := DoctorReport{Healthy: true}
	add := func(code string, ok bool, message, recommendation string) {
		report.Checks = append(report.Checks, Check{Code: code, OK: ok, Severity: "required", Message: message, Recommendation: recommendation})
		if !ok {
			report.Healthy = false
		}
	}
	advise := func(code string, ok bool, message, recommendation string) {
		report.Checks = append(report.Checks, Check{Code: code, OK: ok, Severity: "recommended", Message: message, Recommendation: recommendation})
	}
	if nameservers, err := localDNSNameservers(); err != nil {
		add("runtime_dns", false, "no usable host DNS resolver was found for local workloads", "set AXERN_LOCAL_DNS_NAMESERVERS to a comma-separated list of reachable resolver IPs")
	} else {
		add("runtime_dns", true, fmt.Sprintf("%d usable host DNS resolver(s) detected", len(nameservers)), "")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		add("host_platform", false, runtime.GOOS+" is not supported", "use macOS or Linux with Docker")
	} else {
		add("host_platform", true, runtime.GOOS+"/"+runtime.GOARCH+" is supported", "")
	}
	add("cpu_minimum", runtime.NumCPU() >= 2, fmt.Sprintf("%d CPU cores are available", runtime.NumCPU()), "allocate at least 2 CPU cores to this host")
	advise("cpu_recommended", runtime.NumCPU() >= 4, fmt.Sprintf("%d CPU cores are available; 4 are recommended", runtime.NumCPU()), "allocate 4 or more CPU cores for smoother workload startup")
	diskPath, pathErr := existingParent(m.Dir)
	if pathErr != nil {
		add("data_path", false, "local data directory cannot be inspected", "set AXERN_HOME to a writable directory")
	} else if info, err := os.Stat(diskPath); err != nil || info.Mode().Perm()&0o222 == 0 {
		add("data_path", false, "local data directory is not writable", "set AXERN_HOME to a writable directory")
	} else {
		add("data_path", true, "local data path is writable", "")
	}
	if pathErr != nil {
		add("disk", false, "free disk space could not be inspected", "verify the local data filesystem is mounted")
	} else if free, err := availableDisk(diskPath); err != nil {
		add("disk", false, "free disk space could not be inspected", "verify the local data filesystem is mounted and writable")
	} else {
		add("disk_minimum", free >= 10<<30, fmt.Sprintf("%s of disk space is available", humanBytes(free)), "free at least 10 GiB on the local data filesystem")
		advise("disk_recommended", free >= 20<<30, fmt.Sprintf("%s is available; 20 GiB is recommended", humanBytes(free)), "free 20 GiB or more for runtime images and workload output")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		add("docker_cli", false, "Docker CLI was not found", "install Docker Desktop or Docker Engine")
		return report
	}
	add("docker_cli", true, "Docker CLI is installed", "")
	dockerInfo, dockerErr := m.Runner.Output(ctx, "docker", "info", "--format", "{{.ServerVersion}} {{.Architecture}} {{.MemTotal}}")
	if dockerErr != nil {
		add("docker_daemon", false, "Docker daemon is not available", "start Docker Desktop or the Docker service")
	} else {
		add("docker_daemon", true, "Docker daemon is available", "")
		parts := strings.Fields(string(dockerInfo))
		if len(parts) >= 3 {
			architecture := parts[1]
			archOK := (runtime.GOARCH == "amd64" && (architecture == "x86_64" || architecture == "amd64")) || (runtime.GOARCH == "arm64" && (architecture == "aarch64" || architecture == "arm64"))
			add("docker_architecture", archOK, "Docker architecture is "+architecture, "configure Docker to use the host architecture")
			memory, _ := strconv.ParseInt(parts[2], 10, 64)
			add("docker_memory_minimum", memory >= 6<<30, fmt.Sprintf("Docker has %s of memory", humanBytes(memory)), "allocate at least 6 GiB to Docker")
			advise("docker_memory_recommended", memory >= 8<<30, fmt.Sprintf("Docker has %s of memory; 8 GiB is recommended", humanBytes(memory)), "allocate 8 GiB or more to Docker")
		}
	}
	if _, err := m.Runner.Output(ctx, "docker", "compose", "version", "--short"); err != nil {
		add("compose_v2", false, "Docker Compose v2 is not available", "install the Docker Compose v2 plugin")
	} else {
		add("compose_v2", true, "Docker Compose v2 is available", "")
	}
	metadata, metadataErr := loadMetadata(m.metadataPath())
	if metadataErr == nil && metadata.Version != m.Version {
		add("stack_version", false, fmt.Sprintf("local stack is %s and CLI is %s", metadata.Version, m.Version), "run `axern local upgrade`")
	} else {
		add("stack_version", true, "local stack version is compatible", "")
	}
	if metadataErr == nil {
		identityOK := validCertificateSet(filepath.Join(m.Dir, "certs")) && validSSHPrivateKey(filepath.Join(m.Dir, "ssh", "gateway_host_ed25519")) && validSSHPrivateKey(filepath.Join(m.Dir, "ssh", "gateway_client_ed25519"))
		identityMessage := "local certificate and SSH identity material is valid"
		if !identityOK {
			identityMessage = "local certificate or SSH identity material is missing or invalid"
		}
		add("local_identity", identityOK, identityMessage, "restore the identity directory from backup or run `axern local reset`")
		if inspectRuntime && dockerErr == nil {
			status, statusErr := m.Status(ctx)
			switch {
			case statusErr != nil:
				add("stack_runtime", false, "local component status could not be inspected", "run `axern local status` and inspect Docker Compose logs")
			case status.State == "stopped":
				advise("stack_runtime", false, "local services are stopped", "run `axern local up`")
			case status.State != "running":
				add("stack_runtime", false, "one or more local components are not healthy", "run `axern local status` and `axern local logs`")
			default:
				add("stack_runtime", true, "all local components are healthy", "")
				request, _ := http.NewRequestWithContext(ctx, http.MethodGet, status.DashboardURL+"/healthz", nil)
				response, gatewayErr := (&http.Client{Timeout: 3 * time.Second}).Do(request)
				gatewayOK := gatewayErr == nil && response.StatusCode == http.StatusOK
				if response != nil {
					response.Body.Close()
				}
				gatewayMessage := "local gateway health endpoint is reachable"
				if !gatewayOK {
					gatewayMessage = "local gateway health endpoint is not reachable"
				}
				add("gateway_connectivity", gatewayOK, gatewayMessage, "inspect `axern local logs gatewayd` and verify local firewall settings")
				nodeOK := m.localNodeReady(ctx, &http.Client{Timeout: 3 * time.Second})
				nodeMessage := "local node is registered with fresh heartbeat and inventory"
				if !nodeOK {
					nodeMessage = "local node registration, heartbeat, or inventory is not fresh"
				}
				add("node_registration", nodeOK, nodeMessage, "inspect `axern local logs node` and `axern local logs controld`")
			}
		}
	}
	if errors.Is(metadataErr, os.ErrNotExist) {
		for _, port := range []int{GatewayControlPort, GatewayHTTPPort, GatewaySSHPort, 24101, 25432, 29000, 29001} {
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				add("port_"+strconv.Itoa(port), false, fmt.Sprintf("port %d is already in use", port), fmt.Sprintf("stop the process using port %d", port))
			} else {
				listener.Close()
			}
		}
	}
	return report
}

func (m *Manager) materialize(profile string) error {
	for _, dir := range []string{m.Dir, filepath.Join(m.Dir, "data", "postgres"), filepath.Join(m.Dir, "data", "minio"), filepath.Join(m.Dir, "data", "axnoded"), filepath.Join(m.Dir, "data", "volumed"), filepath.Join(m.Dir, "run"), filepath.Join(m.Dir, "certs"), filepath.Join(m.Dir, "ssh")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := ensurePKI(filepath.Join(m.Dir, "certs")); err != nil {
		return err
	}
	if err := ensureSSH(filepath.Join(m.Dir, "ssh")); err != nil {
		return err
	}
	if err := writeAtomic(m.composePath(), localbundle.Compose, 0o644); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(m.Dir, "otel-collector.yaml"), localbundle.CollectorConfig, 0o644); err != nil {
		return err
	}
	return m.writeEnv(profile)
}

func (m *Manager) writeEnv(profile string) error {
	dnsNameservers, err := localDNSNameservers()
	if err != nil {
		return fmt.Errorf("configure local workload DNS: %w", err)
	}
	secretValues := map[string]string{}
	secretsPath := filepath.Join(m.Dir, "secrets.json")
	if data, err := os.ReadFile(secretsPath); err == nil {
		_ = json.Unmarshal(data, &secretValues)
	}
	if !validSecretsMasterKey(secretValues["master"]) {
		value, err := randomBase64(32)
		if err != nil {
			return err
		}
		secretValues["master"] = value
	}
	for _, key := range []string{"postgres", "minio_user", "minio_password", "dev_token", "node_token"} {
		if secretValues[key] == "" {
			value, err := randomHex(24)
			if err != nil {
				return err
			}
			secretValues[key] = value
		}
	}
	secretData, _ := json.MarshalIndent(secretValues, "", "  ")
	if err := writeAtomic(secretsPath, append(secretData, '\n'), 0o600); err != nil {
		return err
	}
	images := localbundle.ImageReferences(m.Version)
	noProxy := "localhost,127.0.0.1,::1,host.docker.internal,controld,storaged,gatewayd,tunneld,node,postgres,minio,.svc,.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
	httpProxy := containerProxy(os.Getenv("HTTP_PROXY"))
	httpsProxy := containerProxy(os.Getenv("HTTPS_PROXY"))
	otelEnabled, otelEndpoint := "false", ""
	if profile == "observability" {
		otelEnabled, otelEndpoint = "true", "http://otel-collector:4317"
	}
	values := map[string]string{
		"AXERN_LOCAL_DIR": m.Dir, "POSTGRES_IMAGE": images["POSTGRES_IMAGE"], "MINIO_IMAGE": images["MINIO_IMAGE"], "POSTGRES_PASSWORD": secretValues["postgres"], "MINIO_ROOT_USER": secretValues["minio_user"], "MINIO_ROOT_PASSWORD": secretValues["minio_password"],
		"CONTROLD_IMAGE": images["CONTROLD_IMAGE"], "TUNNELD_IMAGE": images["TUNNELD_IMAGE"], "GATEWAYD_IMAGE": images["GATEWAYD_IMAGE"], "NODE_ALL_IN_ONE_IMAGE": images["NODE_ALL_IN_ONE_IMAGE"],
		"PYTHON311_RUNTIME_IMAGE": images["PYTHON311_RUNTIME_IMAGE"], "SERVER_BASE_RUNTIME_IMAGE": images["SERVER_BASE_RUNTIME_IMAGE"], "CODING_BASE_RUNTIME_IMAGE": images["CODING_BASE_RUNTIME_IMAGE"], "DESKTOP_BASE_RUNTIME_IMAGE": images["DESKTOP_BASE_RUNTIME_IMAGE"], "CLAUDE_CODE_BUNDLE_IMAGE": images["CLAUDE_CODE_BUNDLE_IMAGE"], "CODEX_BUNDLE_IMAGE": images["CODEX_BUNDLE_IMAGE"],
		"OTEL_COLLECTOR_IMAGE": images["OTEL_COLLECTOR_IMAGE"], "OTEL_LGTM_IMAGE": images["OTEL_LGTM_IMAGE"], "AXERN_SECRETS_MASTER_KEY": secretValues["master"], "LOCAL_DEV_TOKEN": secretValues["dev_token"], "NODE_AUTH_TOKEN": secretValues["node_token"],
		"CONTAINER_HTTP_PROXY": httpProxy, "CONTAINER_HTTPS_PROXY": httpsProxy, "CONTAINER_NO_PROXY": noProxy, "REGISTRY_PROXY_URL": firstNonEmpty(httpsProxy, httpProxy), "CONTROLD_INSECURE_REGISTRIES": "", "OTEL_ENABLED": otelEnabled, "OTEL_EXPORTER_OTLP_ENDPOINT": otelEndpoint,
		"AXNODED_DNS_NAMESERVERS": strings.Join(dnsNameservers, ","),
		"LOCAL_UID":               strconv.Itoa(os.Getuid()), "LOCAL_GID": strconv.Itoa(os.Getgid()), "CONTROLD_HTTP_PORT": "24101", "GATEWAY_CONTROL_PORT": strconv.Itoa(GatewayControlPort), "GATEWAY_HTTP_PORT": strconv.Itoa(GatewayHTTPPort), "GATEWAY_SSH_PORT": strconv.Itoa(GatewaySSHPort), "POSTGRES_PORT": "25432", "MINIO_API_PORT": "29000", "MINIO_CONSOLE_PORT": "29001", "OTEL_GRPC_PORT": "4317", "OTEL_HTTP_PORT": "4318", "LGTM_UI_PORT": "13000",
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var content strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&content, "%s=%s\n", key, quoteDotEnv(values[key]))
	}
	return writeAtomic(m.envPath(), []byte(content.String()), 0o600)
}

func containerProxy(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	hostname := parsed.Hostname()
	if hostname != "localhost" && hostname != "127.0.0.1" && hostname != "::1" {
		return value
	}
	host := "host.docker.internal"
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	parsed.Host = host
	return parsed.String()
}

func quoteDotEnv(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r")
	return `"` + replacer.Replace(value) + `"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (m *Manager) writeContext(use bool) error {
	cfg, err := config.Load(m.ConfigPath)
	if err != nil {
		return err
	}
	cfg.Contexts[ContextName] = &clientconfig.Context{Endpoint: fmt.Sprintf("127.0.0.1:%d", GatewayControlPort), ServiceURL: fmt.Sprintf("http://127.0.0.1:%d", GatewayHTTPPort), SSHEndpoint: fmt.Sprintf("127.0.0.1:%d", GatewaySSHPort), SSHIdentityFile: filepath.Join(m.Dir, "ssh", "gateway_client_ed25519"), TLS: clientconfig.TLS{CACert: filepath.Join(m.Dir, "certs", "ca.crt"), Cert: filepath.Join(m.Dir, "certs", "client.crt"), Key: filepath.Join(m.Dir, "certs", "client.key")}, ProxyMode: clientconfig.ProxyModeDirect}
	if cfg.CurrentContext == "" || use {
		cfg.CurrentContext = ContextName
	}
	return config.Save(m.ConfigPath, cfg)
}

func (m *Manager) composeArgs(profile string, args ...string) []string {
	value := []string{"compose", "--project-name", ProjectName, "--env-file", m.envPath(), "-f", m.composePath()}
	if profile != "" {
		value = append(value, "--profile", profile)
	}
	return append(value, args...)
}

func (m *Manager) composeRun(ctx context.Context, profile string, args ...string) error {
	return m.Runner.Run(ctx, m.Stdout, m.Stderr, "docker", m.composeArgs(profile, args...)...)
}

func (m *Manager) composeOutput(ctx context.Context, profile string, args ...string) ([]byte, error) {
	return m.Runner.Output(ctx, "docker", m.composeArgs(profile, args...)...)
}

func (m *Manager) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", GatewayHTTPPort)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK && m.localNodeReady(ctx, client) {
				status, statusErr := m.Status(ctx)
				if statusErr == nil && status.State == "running" {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("Axern local did not become healthy within %s", timeout)
}

func (m *Manager) localNodeReady(ctx context.Context, client *http.Client) bool {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:24101/nodesz", nil)
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		Nodes []struct {
			NodeID string `json:"node_id"`
			Fresh  bool   `json:"fresh"`
		} `json:"nodes"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload) != nil {
		return false
	}
	for _, node := range payload.Nodes {
		if node.NodeID == "node-local" && node.Fresh {
			return true
		}
	}
	return false
}

func (m *Manager) lock() (func(), error) {
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(m.Dir, ".lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		fmt.Fprintln(file, os.Getpid())
		file.Close()
		return func() { _ = os.Remove(path) }, nil
	}
	data, readErr := os.ReadFile(path)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if readErr == nil && pid > 0 && syscall.Kill(pid, 0) != nil {
		_ = os.Remove(path)
		return m.lock()
	}
	return nil, fmt.Errorf("another axern local operation is running")
}

func (m *Manager) metadataPath() string { return filepath.Join(m.Dir, "metadata.json") }
func (m *Manager) composePath() string  { return filepath.Join(m.Dir, "compose.yaml") }
func (m *Manager) envPath() string      { return filepath.Join(m.Dir, "compose.env") }

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func availableDisk(path string) (int64, error) {
	var value syscall.Statfs_t
	if err := syscall.Statfs(path, &value); err != nil {
		return 0, err
	}
	return int64(value.Bavail) * int64(value.Bsize), nil
}

func existingParent(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		if _, err := os.Stat(current); err == nil {
			return current, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing parent for %s", path)
		}
		current = parent
	}
}

func humanBytes(value int64) string {
	const gib = int64(1 << 30)
	return fmt.Sprintf("%.1f GiB", float64(value)/float64(gib))
}
