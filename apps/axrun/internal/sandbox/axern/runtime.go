package axern

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
	axernsdk "github.com/cofy-x/axern/sdk/go"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
	"google.golang.org/grpc"
)

type Runtime struct {
	Config Config
}

func NewRuntime(config Config) Runtime {
	return Runtime{Config: config}
}

func NewRuntimeFromEnv() Runtime {
	return NewRuntime(ConfigFromEnv())
}

func (r Runtime) Preflight() error {
	return r.Config.ValidateBase()
}

func (r Runtime) Create(ctx context.Context) (sandbox.Instance, error) {
	if err := r.Config.Validate(); err != nil {
		return nil, err
	}
	options := []axernsdk.ClientOption{}
	if r.Config.TLSCACert != "" || r.Config.TLSCert != "" || r.Config.TLSKey != "" || r.Config.TLSServerName != "" {
		options = append(options, axernsdk.WithTLS(r.Config.TLSCACert, r.Config.TLSCert, r.Config.TLSKey, r.Config.TLSServerName))
	}
	if r.Config.ProxyMode == clientconfig.ProxyModeDirect {
		options = append(options, axernsdk.WithDialOptions(grpc.WithNoProxy()))
	}
	client, err := axernsdk.NewClient(ctx, r.Config.Endpoint, options...)
	if err != nil {
		return nil, err
	}
	sb, err := axernsdk.NewSandbox(axernsdk.SandboxOptions{
		Client:        client,
		TemplateID:    r.Config.TemplateID,
		Image:         r.Config.Image,
		Namespace:     r.Config.NamespaceOrDefault(),
		RequestCPU:    axernsdk.ResourceQuantity(r.Config.RequestCPU),
		RequestMemory: axernsdk.ResourceQuantity(r.Config.RequestMemory),
		LimitCPU:      axernsdk.ResourceQuantity(r.Config.LimitCPU),
		LimitMemory:   axernsdk.ResourceQuantity(r.Config.LimitMemory),
		ImageMounts:   cloneImageMounts(r.Config.ImageMounts),
	})
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := sb.Start(ctx); err != nil {
		_ = sb.Close(ctx)
		_ = client.Close()
		return nil, err
	}
	return instance{client: client, sandbox: sb}, nil
}

func cloneImageMounts(mounts []axernsdk.ImageMount) []axernsdk.ImageMount {
	if len(mounts) == 0 {
		return nil
	}
	return append([]axernsdk.ImageMount(nil), mounts...)
}

type instance struct {
	client  *axernsdk.Client
	sandbox *axernsdk.Sandbox
}

func (i instance) Exec(ctx context.Context, command sandbox.ExecCommand, options sandbox.ExecOptions) (sandbox.ExecResult, error) {
	value, err := execCommandValue(command)
	if err != nil {
		return sandbox.ExecResult{}, err
	}
	result, err := i.sandbox.Exec(ctx, value, axernsdk.ExecOptions{
		Env:          options.Env,
		Cwd:          options.CWD,
		Timeout:      options.Timeout,
		User:         options.User,
		Check:        false,
		ManagedProxy: axernManagedProxyOptions(options.ManagedProxy),
	})
	execResult := sandbox.ExecResult{
		ExitCode:           int(result.ExitCode),
		Stdout:             result.StdoutString(),
		Stderr:             result.StderrString(),
		ManagedProxyReport: sandboxManagedProxyReport(result.ManagedProxyReport),
	}
	if err != nil && sandbox.IsFatalSandboxError(err) {
		return execResult, &sandbox.SandboxDeathError{
			Cause:  err,
			Reason: sandbox.ClassifyFatalReason(err),
		}
	}
	return execResult, err
}

func axernManagedProxyOptions(options *sandbox.ManagedProxyOptions) *axernsdk.ManagedProxyOptions {
	if options == nil {
		return nil
	}
	return &axernsdk.ManagedProxyOptions{
		Provider:            options.Provider,
		UpstreamBaseURL:     options.UpstreamBaseURL,
		UpstreamBearerToken: options.UpstreamBearerToken,
	}
}

func sandboxManagedProxyReport(report *axernsdk.ManagedProxyReport) *sandbox.ManagedProxyReport {
	if report == nil {
		return nil
	}
	return &sandbox.ManagedProxyReport{
		Provider:      report.Provider,
		RequestCount:  report.RequestCount,
		ResponseCount: report.ResponseCount,
		ErrorCount:    report.ErrorCount,
		ReportJSON:    append([]byte(nil), report.ReportJSON...),
	}
}

func execCommandValue(command sandbox.ExecCommand) (any, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	if command.Shell() != "" {
		return command.Shell(), nil
	}
	return command.Argv(), nil
}

func (i instance) UploadDir(ctx context.Context, localPath string, remotePath string, options sandbox.UploadDirOptions) error {
	if err := i.sandbox.UploadDir(ctx, localPath, remotePath, axernsdk.UploadDirOptions{
		NoCreateParents: options.NoCreateParents,
		NoOverwrite:     options.NoOverwrite,
	}); err != nil {
		return err
	}
	if !options.Writable {
		return nil
	}
	result, err := i.Exec(
		ctx,
		sandbox.ShellCommand("chmod -R a+rwX -- "+shellQuote(remotePath)),
		sandbox.ExecOptions{
			Timeout: 30 * time.Second,
			User:    "root",
		},
	)
	if err != nil {
		return fmt.Errorf("make uploaded directory writable: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("make uploaded directory writable exited with status %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (i instance) PathExists(ctx context.Context, path string) (bool, error) {
	return i.sandbox.Exists(ctx, path)
}

func (i instance) DownloadPath(ctx context.Context, remotePath string, localPath string, options sandbox.DownloadPathOptions) error {
	info, err := i.sandbox.Stat(ctx, remotePath)
	if err != nil {
		return err
	}
	switch info.Kind {
	case axernsdk.SandboxFileKindDirectory:
		return i.sandbox.DownloadDir(ctx, remotePath, localPath, axernsdk.DownloadDirOptions{
			NoOverwrite: options.NoOverwrite,
		})
	case axernsdk.SandboxFileKindFile:
		if options.NoOverwrite {
			if _, err := os.Stat(localPath); err == nil {
				return fmt.Errorf("local path %s already exists", localPath)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		data, err := i.sandbox.ReadFile(ctx, remotePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(info.Mode) & 0o777
		if mode == 0 {
			mode = 0o644
		}
		return os.WriteFile(localPath, data, mode)
	default:
		return fmt.Errorf("remote path %s has unsupported kind %q", remotePath, info.Kind)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (i instance) State() (sandbox.State, error) {
	state, err := i.sandbox.State()
	if err != nil {
		return sandbox.State{}, err
	}
	return sandbox.State{
		EnvironmentID: state.EnvironmentID,
		RunID:         state.RunID,
		AllocationID:  state.AllocationID,
		NodeID:        state.NodeID,
	}, nil
}

func (i instance) Close(ctx context.Context) error {
	closeErr := i.sandbox.Close(ctx)
	clientErr := i.client.Close()
	if closeErr != nil {
		return closeErr
	}
	return clientErr
}
