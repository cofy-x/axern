package axernsdk

import (
	"context"
	"strings"
	"time"

	"github.com/cofy-x/axern/sdk/go/internal/nodeclient"
)

// ImageProcessMount shares a host-backed path from the target sandbox into an
// image-backed process.
type ImageProcessMount struct {
	SandboxPath string
	TargetPath  string
	Readonly    bool
	Options     []string
}

// WorkspaceMount shares path from the target sandbox at the same path inside
// the image-backed process.
func WorkspaceMount(path string) ImageProcessMount {
	return ImageProcessMount{SandboxPath: path, TargetPath: path}
}

// ImageExecOptions configures a collected command from a separate image.
type ImageExecOptions struct {
	Env          map[string]string
	Cwd          string
	Timeout      time.Duration
	User         string
	TTY          bool
	Check        bool
	Mounts       []ImageProcessMount
	ManagedProxy *ManagedProxyOptions
}

// ImageProcessOptions configures a streaming process from a separate image.
type ImageProcessOptions struct {
	Env          map[string]string
	Cwd          string
	Timeout      time.Duration
	User         string
	TTY          bool
	Mounts       []ImageProcessMount
	ManagedProxy *ManagedProxyOptions
}

// ExecImage runs a command from image against explicit host-backed paths from
// the sandbox and collects stdout/stderr.
func (s *Sandbox) ExecImage(ctx context.Context, image string, command any, options ImageExecOptions) (ExecResult, error) {
	node, err := s.nodeClient()
	if err != nil {
		return ExecResult{}, err
	}
	return node.ExecImage(ctx, image, command, options)
}

// ProcessImage starts a streaming process from image against explicit
// host-backed paths from the sandbox.
func (s *Sandbox) ProcessImage(ctx context.Context, image string, command any, options ImageProcessOptions) (*SandboxProcess, error) {
	node, err := s.nodeClient()
	if err != nil {
		return nil, err
	}
	process, err := node.ProcessImage(ctx, image, command, options)
	if err != nil {
		return nil, err
	}
	process.sandbox = s
	s.registerProcess(process)
	return process, nil
}

// ExecImage runs a command from image against explicit host-backed paths from
// the allocation and collects stdout/stderr.
func (n *NodeSandboxClient) ExecImage(ctx context.Context, image string, command any, options ImageExecOptions) (ExecResult, error) {
	if err := n.validate(); err != nil {
		return ExecResult{}, err
	}
	if err := validateImageExecOptions(image, options); err != nil {
		return ExecResult{}, err
	}
	argv, err := normalizeCommand(command)
	if err != nil {
		return ExecResult{}, err
	}
	result, err := n.rpcClient().ExecImage(ctx, image, argv, nodeclient.ImageOptions{
		Env:          options.Env,
		Cwd:          options.Cwd,
		Timeout:      options.Timeout,
		User:         options.User,
		TTY:          options.TTY,
		Mounts:       convertImageProcessMounts(options.Mounts),
		ManagedProxy: nodeManagedProxyOptions(options.ManagedProxy),
	})
	if err != nil {
		return ExecResult{}, mapRPCError(err, "sandbox exec image", n.allocationID)
	}
	sdkResult := ExecResult{
		ExitCode:           result.ExitCode,
		Stdout:             result.Stdout,
		Stderr:             result.Stderr,
		StdoutTruncated:    result.StdoutTruncated,
		StderrTruncated:    result.StderrTruncated,
		ManagedProxyReport: sdkManagedProxyReport(result.ManagedProxyReport),
	}
	if options.Check && sdkResult.ExitCode != 0 {
		return sdkResult, &ExecError{Argv: argv, Result: sdkResult}
	}
	return sdkResult, nil
}

// ProcessImage starts a streaming process from image against explicit
// host-backed paths from the allocation.
func (n *NodeSandboxClient) ProcessImage(ctx context.Context, image string, command any, options ImageProcessOptions) (*SandboxProcess, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	if err := validateImageProcessOptions(image, options); err != nil {
		return nil, err
	}
	argv, err := normalizeCommand(command)
	if err != nil {
		return nil, err
	}
	process, err := n.rpcClient().ProcessImage(ctx, image, argv, nodeclient.ImageOptions{
		Env:          options.Env,
		Cwd:          options.Cwd,
		Timeout:      options.Timeout,
		User:         options.User,
		TTY:          options.TTY,
		Mounts:       convertImageProcessMounts(options.Mounts),
		ManagedProxy: nodeManagedProxyOptions(options.ManagedProxy),
	})
	if err != nil {
		return nil, mapRPCError(err, "sandbox process image", n.allocationID)
	}
	return &SandboxProcess{allocationID: n.allocationID, process: process}, nil
}

func validateImageExecOptions(image string, options ImageExecOptions) error {
	if strings.TrimSpace(image) == "" {
		return requiredError("image")
	}
	if options.Timeout < 0 {
		return positiveDurationError("timeout")
	}
	return validateImageProcessMounts(options.Mounts)
}

func validateImageProcessOptions(image string, options ImageProcessOptions) error {
	if strings.TrimSpace(image) == "" {
		return requiredError("image")
	}
	if options.Timeout < 0 {
		return positiveDurationError("timeout")
	}
	return validateImageProcessMounts(options.Mounts)
}

func validateImageProcessMounts(mounts []ImageProcessMount) error {
	for _, mount := range mounts {
		if !strings.HasPrefix(mount.SandboxPath, "/") {
			return validationError("mounts.sandbox_path", "must be absolute")
		}
		if !strings.HasPrefix(mount.TargetPath, "/") {
			return validationError("mounts.target_path", "must be absolute")
		}
	}
	return nil
}

func convertImageProcessMounts(mounts []ImageProcessMount) []nodeclient.ImageProcessMount {
	if mounts == nil {
		return nil
	}
	out := make([]nodeclient.ImageProcessMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, nodeclient.ImageProcessMount{
			SandboxPath: mount.SandboxPath,
			TargetPath:  mount.TargetPath,
			Readonly:    mount.Readonly,
			Options:     append([]string(nil), mount.Options...),
		})
	}
	return out
}
