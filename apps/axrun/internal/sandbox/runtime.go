package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Runtime interface {
	Preflight() error
	Create(context.Context) (Instance, error)
}

type Instance interface {
	Exec(context.Context, ExecCommand, ExecOptions) (ExecResult, error)
	UploadDir(context.Context, string, string, UploadDirOptions) error
	DownloadPath(context.Context, string, string, DownloadPathOptions) error
	State() (State, error)
	Close(context.Context) error
}

type TaskAssetMaterializer interface {
	MaterializeTaskAssets(context.Context, string, string, TaskAssetKind) error
}

type PathExister interface {
	PathExists(context.Context, string) (bool, error)
}

type TaskAssetKind string

const (
	TaskAssetKindVerifier TaskAssetKind = "verifier"
	TaskAssetKindOracle   TaskAssetKind = "oracle"
)

type ExecCommand struct {
	shell string
	argv  []string
}

func ShellCommand(command string) ExecCommand {
	return ExecCommand{shell: strings.TrimSpace(command)}
}

func ArgvCommand(argv []string) ExecCommand {
	return ExecCommand{argv: append([]string(nil), argv...)}
}

func (c ExecCommand) Shell() string {
	return c.shell
}

func (c ExecCommand) Argv() []string {
	return append([]string(nil), c.argv...)
}

func (c ExecCommand) Validate() error {
	hasShell := strings.TrimSpace(c.shell) != ""
	hasArgv := len(c.argv) > 0
	if hasShell == hasArgv {
		return fmt.Errorf("exec command requires exactly one of shell or argv")
	}
	if hasArgv && strings.TrimSpace(c.argv[0]) == "" {
		return fmt.Errorf("exec argv command is required")
	}
	return nil
}

type State struct {
	EnvironmentID         string
	ServiceID             string
	AllocationID          string
	NodeID                string
	RuntimeClass          string
	PayloadFormat         string
	PayloadDigest         string
	CacheHit              bool
	ImageResolveMs        int64
	ImagePullMs           int64
	CowPrepareMs          int64
	VerifierMaterializeMs int64
}

type ExecOptions struct {
	CWD          string
	Timeout      time.Duration
	User         string
	Env          map[string]string
	ManagedProxy *ManagedProxyOptions
}

type ExecResult struct {
	ExitCode           int
	Stdout             string
	Stderr             string
	ManagedProxyReport *ManagedProxyReport
}

type ManagedProxyOptions struct {
	Provider            string
	UpstreamBaseURL     string
	UpstreamBearerToken string
}

type ManagedProxyReport struct {
	Provider      string
	RequestCount  int32
	ResponseCount int32
	ErrorCount    int32
	ReportJSON    []byte
}

type UploadDirOptions struct {
	NoCreateParents bool
	NoOverwrite     bool
	Writable        bool // make uploaded files mutable by non-root agent users.
}

type DownloadPathOptions struct {
	NoOverwrite bool
}
