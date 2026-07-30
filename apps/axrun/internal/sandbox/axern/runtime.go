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
	"google.golang.org/grpc/metadata"
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
	if r.Config.RolloutExecutionLease != "" {
		options = append(options, axernsdk.WithDialOptions(
			grpc.WithChainUnaryInterceptor(rolloutExecutionUnary(r.Config.RolloutExecutionLease)),
			grpc.WithChainStreamInterceptor(rolloutExecutionStream(r.Config.RolloutExecutionLease)),
		))
	}
	client, err := axernsdk.NewClient(ctx, r.Config.Endpoint, options...)
	if err != nil {
		return nil, err
	}
	sb, err := axernsdk.NewSandbox(axernsdk.SandboxOptions{
		Client:         client,
		TemplateID:     r.Config.TemplateID,
		Image:          r.Config.Image,
		Namespace:      r.Config.NamespaceOrDefault(),
		RuntimeClass:   r.Config.RuntimeClass,
		RequestCPU:     axernsdk.ResourceQuantity(r.Config.RequestCPU),
		RequestMemory:  axernsdk.ResourceQuantity(r.Config.RequestMemory),
		LimitCPU:       axernsdk.ResourceQuantity(r.Config.LimitCPU),
		LimitMemory:    axernsdk.ResourceQuantity(r.Config.LimitMemory),
		Volumes:        workspaceVolumes(r.Config),
		ImageMounts:    cloneImageMounts(r.Config.ImageMounts),
		WorkspaceImage: r.Config.WorkspaceImage,
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
	return instance{client: client, sandbox: sb, runtimeClass: r.Config.RuntimeClass}, nil
}

const rolloutExecutionLeaseMetadata = "x-axern-rollout-work-lease"

func rolloutExecutionUnary(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(metadata.AppendToOutgoingContext(ctx, rolloutExecutionLeaseMetadata, token), method, req, reply, cc, opts...)
	}
}

func rolloutExecutionStream(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(metadata.AppendToOutgoingContext(ctx, rolloutExecutionLeaseMetadata, token), desc, cc, method, opts...)
	}
}

func workspaceVolumes(config Config) []axernsdk.VolumeMount {
	if !config.WorkspaceVolume {
		return nil
	}
	return []axernsdk.VolumeMount{{
		Name:    "workspace",
		Target:  "/workspace",
		Options: []string{"rbind"},
	}}
}

func cloneImageMounts(mounts []axernsdk.ImageMount) []axernsdk.ImageMount {
	if len(mounts) == 0 {
		return nil
	}
	return append([]axernsdk.ImageMount(nil), mounts...)
}

type instance struct {
	client       *axernsdk.Client
	sandbox      *axernsdk.Sandbox
	runtimeClass string
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

func (i instance) MaterializeTaskAssets(ctx context.Context, sourcePath, target string, kind sandbox.TaskAssetKind) error {
	sdkKind := axernsdk.TaskAssetKindVerifier
	if kind == sandbox.TaskAssetKindOracle {
		sdkKind = axernsdk.TaskAssetKindOracle
	}
	return i.sandbox.MaterializeTaskAssets(ctx, sourcePath, target, sdkKind)
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
	out := sandbox.State{
		EnvironmentID:         state.EnvironmentID,
		ServiceID:             state.ServiceID,
		AllocationID:          state.AllocationID,
		NodeID:                state.NodeID,
		RuntimeClass:          i.runtimeClass,
		VerifierMaterializeMs: state.VerifierMaterializeMs,
	}
	if preparation := state.WorkspacePreparation; preparation != nil {
		out.PayloadFormat = preparation.GetPayloadFormat()
		out.PayloadDigest = preparation.GetPayloadDigest()
		out.CacheHit = preparation.GetCacheHit()
		out.ImageResolveMs = preparation.GetImageResolveMs()
		out.ImagePullMs = preparation.GetImagePullMs()
		out.CowPrepareMs = preparation.GetCowPrepareMs()
	}
	return out, nil
}

func (i instance) Close(ctx context.Context) error {
	closeErr := i.sandbox.Close(ctx)
	clientErr := i.client.Close()
	if closeErr != nil {
		return closeErr
	}
	return clientErr
}
