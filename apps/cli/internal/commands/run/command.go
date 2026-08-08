package run

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	apprun "github.com/cofy-x/axern/apps/cli/internal/application/run"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	"github.com/cofy-x/axern/apps/cli/internal/resourcespec"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func Command(runtime command.Runtime) *cobra.Command {
	options := &createOptions{namespace: "default", waitTimeout: apprun.DefaultCreateWaitTimeout}
	root := &cobra.Command{
		Use:   "run [flags] <image> [--] <command...>",
		Short: "Run a command in an isolated environment",
		Args: func(cmd *cobra.Command, args []string) error {
			if options.file != "" {
				if len(args) != 0 {
					return command.Usage(fmt.Errorf("--file does not accept image or command arguments"))
				}
				return nil
			}
			if options.environmentID != "" || options.templateID != "" {
				return nil
			}
			if len(args) == 0 {
				return command.Usage(fmt.Errorf("image is required unless --template, --environment, or --file is used"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.file == "" {
				switch {
				case options.environmentID != "" || options.templateID != "":
					options.argv = append([]string(nil), args...)
				default:
					options.imageRef = args[0]
					options.argv = append([]string(nil), args[1:]...)
				}
			}
			return execute(runtime, cmd, options)
		},
	}
	options.bind(root)
	root.AddCommand(getCommand(runtime), listCommand(runtime), cancelCommand(runtime), logsCommand(runtime))
	return root
}

type createOptions struct {
	file, namespace, environmentID, templateID, templateVersion, imageRef, credentialID, cwd, runtimeClass, requestCPU, requestMemory, requestEphemeralStorage, limitCPU, limitMemory, limitEphemeralStorage string
	argv, env, secretEnv, secretFile, imageMount, labels                                                                                                                                                     []string
	rootfsReadonly                                                                                                                                                                                           bool
	detach                                                                                                                                                                                                   bool
	waitTimeout                                                                                                                                                                                              time.Duration
}

func execute(runtime command.Runtime, cmd *cobra.Command, options *createOptions) error {
	params, err := options.params(cmd)
	if err != nil {
		return command.Usage(err)
	}
	s, err := runtime.Open(cmd.Context())
	if err != nil {
		return err
	}
	defer s.Close()
	control := apprun.NewWithEnvironment(s.Clients.Run, s.Clients.Environment)
	resp, err := control.Create(s.Context, params)
	if err != nil {
		return err
	}
	value := resp.GetRun()
	if options.detach {
		return renderRun(runtime, cmd, value)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Run: %s\n", value.GetID())
	executionCtx, forceExit := context.WithCancel(s.Context)
	defer forceExit()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	cancelDone := make(chan struct{})
	var interrupted atomic.Bool
	var forced atomic.Bool
	var cancelFailed atomic.Bool
	go func() {
		select {
		case <-signals:
			interrupted.Store(true)
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelling run; press Ctrl-C again to exit immediately...")
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if _, err := control.Cancel(cancelCtx, value.GetID()); err != nil {
				cancelFailed.Store(true)
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: cancellation request failed: %v\n", err)
			}
			cancel()
			select {
			case <-signals:
				forced.Store(true)
				forceExit()
			case <-cancelDone:
			}
		case <-cancelDone:
		}
	}()
	defer close(cancelDone)
	ready, waitErr := control.Wait(executionCtx, value.GetID(), apprun.WaitTargetRunning, options.waitTimeout, nil)
	if ready != nil {
		value = ready
	}
	if value.GetAllocationID() != "" {
		if _, err := apprun.ReadOutput(executionCtx, s.Clients.Node, value.GetAllocationID(), "", true, func(event apprun.OutputEvent) error {
			if event.Truncated {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: run output was truncated at 64 MiB")
			}
			switch event.Stream {
			case nodesandboxv1.OutputStream_OUTPUT_STREAM_STDOUT:
				_, err := cmd.OutOrStdout().Write(event.Data)
				return err
			case nodesandboxv1.OutputStream_OUTPUT_STREAM_STDERR:
				_, err := cmd.ErrOrStderr().Write(event.Data)
				return err
			}
			return nil
		}); err != nil && grpcstatus.Code(err) != codes.NotFound {
			if forced.Load() {
				return command.ExitError{Code: 130, Err: err}
			}
			return err
		}
	}
	if waitErr == nil || value.GetAllocationID() != "" {
		final, err := control.Wait(executionCtx, value.GetID(), apprun.WaitTargetTerminal, options.waitTimeout, nil)
		if final != nil {
			value = final
		}
		waitErr = err
	}
	if forced.Load() {
		return command.ExitError{Code: 130, Err: context.Canceled}
	}
	if interrupted.Load() {
		if cancelFailed.Load() && waitErr == nil {
			waitErr = fmt.Errorf("run cancellation could not be confirmed")
		}
		return command.ExitError{Code: 130, Err: waitErr}
	}
	if value.GetExitCodeKnown() && value.GetExitCode() != 0 {
		return command.ExitError{Code: int(value.GetExitCode())}
	}
	return waitErr
}

func (o *createOptions) bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.file, "file", "f", "", "axern/v1 Run spec")
	f.StringVar(&o.namespace, "namespace", "default", "namespace")
	f.StringArrayVar(&o.env, "env", nil, "environment KEY=VALUE; may be repeated")
	f.StringArrayVar(&o.secretEnv, "secret-env", nil, "secret environment mapping; may be repeated")
	f.StringArrayVar(&o.secretFile, "secret-file", nil, "secret file mapping; may be repeated")
	f.StringArrayVar(&o.imageMount, "image-mount", nil, "read-only image mount; may be repeated")
	f.StringVar(&o.cwd, "cwd", "", "working directory")
	f.StringVar(&o.runtimeClass, "runtime-class", "", "runtime class")
	f.StringArrayVar(&o.labels, "label", nil, "label key=value; may be repeated")
	f.StringVar(&o.environmentID, "environment", "", "existing environment id")
	f.StringVar(&o.templateID, "template", "", "runtime template id")
	f.StringVar(&o.templateVersion, "template-version", "", "runtime template version")
	f.StringVar(&o.credentialID, "registry-credential-id", "", "registry credential id")
	f.BoolVar(&o.rootfsReadonly, "rootfs-readonly", false, "mount rootfs read-only")
	f.StringVar(&o.requestCPU, "request-cpu", "", "CPU request")
	f.StringVar(&o.requestMemory, "request-memory", "", "memory request")
	f.StringVar(&o.requestEphemeralStorage, "request-ephemeral-storage", "", "node-local ephemeral storage request")
	f.StringVar(&o.limitCPU, "limit-cpu", "", "CPU limit")
	f.StringVar(&o.limitMemory, "limit-memory", "", "memory limit")
	f.StringVar(&o.limitEphemeralStorage, "limit-ephemeral-storage", "", "node-local ephemeral storage limit")
	f.BoolVar(&o.detach, "detach", false, "create the run without following output")
	f.DurationVar(&o.waitTimeout, "wait-timeout", apprun.DefaultCreateWaitTimeout, "wait timeout; 0 disables it")
}

func (o createOptions) params(cmd *cobra.Command) (apprun.CreateParams, error) {
	if o.file != "" {
		for _, name := range runDefinitionFlags {
			if cmd.Flags().Changed(name) {
				return apprun.CreateParams{}, fmt.Errorf("--file cannot be combined with --%s", name)
			}
		}
		value, err := resourcespec.Load(o.file, resourcespec.KindRun)
		if err != nil {
			return apprun.CreateParams{}, err
		}
		environmentID, environment := value.EnvironmentSpec()
		execution, err := value.ExecutionConfig()
		return apprun.CreateParams{Namespace: value.Metadata.Namespace, EnvironmentID: environmentID, Spec: environment, Config: execution, Labels: value.Metadata.Labels}, err
	}
	if o.templateVersion != "" && o.templateID == "" {
		return apprun.CreateParams{}, fmt.Errorf("--template-version requires --template")
	}
	if o.environmentID != "" && (o.credentialID != "" || cmd.Flags().Changed("rootfs-readonly")) {
		return apprun.CreateParams{}, fmt.Errorf("--registry-credential-id and --rootfs-readonly require an image")
	}
	if o.templateID != "" && (o.credentialID != "" || cmd.Flags().Changed("rootfs-readonly")) {
		return apprun.CreateParams{}, fmt.Errorf("--registry-credential-id and --rootfs-readonly cannot be combined with --template")
	}
	execution, err := executionConfig(o)
	if err != nil {
		return apprun.CreateParams{}, err
	}
	environment, err := environmentSpec(o)
	if err != nil {
		return apprun.CreateParams{}, err
	}
	return apprun.CreateParams{Namespace: o.namespace, EnvironmentID: o.environmentID, Spec: environment, Config: execution, Labels: parse.Labels(o.labels)}, nil
}

func executionConfig(o createOptions) (*commonv1.ExecutionConfig, error) {
	env, err := parse.EnvFlags(o.env)
	if err != nil {
		return nil, err
	}
	secretEnv, err := parse.SecretEnvVars(o.secretEnv)
	if err != nil {
		return nil, err
	}
	secretFiles, err := parse.SecretFiles(o.secretFile)
	if err != nil {
		return nil, err
	}
	imageMounts, err := parse.ImageMounts(o.imageMount)
	if err != nil {
		return nil, err
	}
	resources, err := command.Resources(o.requestCPU, o.requestMemory, o.requestEphemeralStorage, o.limitCPU, o.limitMemory, o.limitEphemeralStorage)
	if err != nil {
		return nil, err
	}
	return &commonv1.ExecutionConfig{Argv: o.argv, Env: env, SecretEnv: secretEnv, SecretFiles: secretFiles, ImageMounts: imageMounts, Cwd: o.cwd, RuntimeClass: o.runtimeClass, Resources: resources}, nil
}

func environmentSpec(o createOptions) (*environmentv1.EnvironmentSpec, error) {
	selected := 0
	if o.environmentID != "" {
		selected++
	}
	if o.templateID != "" {
		selected++
	}
	if o.imageRef != "" {
		selected++
	}
	if selected != 1 {
		return nil, fmt.Errorf("exactly one of environment, template, or image is required")
	}
	if o.environmentID != "" {
		return nil, nil
	}
	value := &environmentv1.EnvironmentSpec{Namespace: o.namespace}
	if o.templateID != "" {
		value.TemplateID = o.templateID
		value.TemplateVersion = o.templateVersion
	} else {
		value.Image = &environmentv1.EnvironmentImageSource{Ref: o.imageRef, RegistryCredentialID: o.credentialID, RootfsReadonly: o.rootfsReadonly}
	}
	return value, nil
}

var runDefinitionFlags = []string{"namespace", "env", "secret-env", "secret-file", "image-mount", "cwd", "runtime-class", "label", "environment", "template", "template-version", "registry-credential-id", "rootfs-readonly", "request-cpu", "request-memory", "request-ephemeral-storage", "limit-cpu", "limit-memory", "limit-ephemeral-storage"}

func logsCommand(runtime command.Runtime) *cobra.Command {
	var follow bool
	var cursor string
	cmd := &cobra.Command{Use: "logs <run-id>", Short: "Read run stdout and stderr", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		response, err := apprun.New(s.Clients.Run).Get(s.Context, args[0])
		if err != nil {
			return err
		}
		run := response.GetRun()
		if run.GetAllocationID() == "" {
			return fmt.Errorf("run %s has no allocation output yet", args[0])
		}
		_, err = apprun.ReadOutput(s.Context, s.Clients.Node, run.GetAllocationID(), cursor, follow, func(event apprun.OutputEvent) error {
			if runtime.Options.Output == "json" {
				stream := "unknown"
				if event.Stream == nodesandboxv1.OutputStream_OUTPUT_STREAM_STDOUT {
					stream = "stdout"
				} else if event.Stream == nodesandboxv1.OutputStream_OUTPUT_STREAM_STDERR {
					stream = "stderr"
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Stream     string `json:"stream"`
					DataBase64 string `json:"data_base64"`
					Cursor     string `json:"cursor"`
					Terminal   bool   `json:"terminal"`
					Truncated  bool   `json:"truncated"`
					ObservedAt int64  `json:"observed_at_unix_milli"`
				}{stream, base64.StdEncoding.EncodeToString(event.Data), event.NextCursor, event.Terminal, event.Truncated, event.ObservedAt})
			}
			writer := cmd.OutOrStdout()
			if event.Stream == nodesandboxv1.OutputStream_OUTPUT_STREAM_STDERR {
				writer = cmd.ErrOrStderr()
			}
			_, err := writer.Write(event.Data)
			return err
		})
		return err
	}}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow output until the run exits")
	cmd.Flags().StringVar(&cursor, "cursor", "", "resume from an opaque output cursor")
	return cmd
}

func getCommand(runtime command.Runtime) *cobra.Command {
	return runOne(runtime, "get", func(ctx context.Context, c apprun.Control, id string) (*runv1.Run, error) {
		r, err := c.Get(ctx, id)
		return r.GetRun(), err
	})
}
func cancelCommand(runtime command.Runtime) *cobra.Command {
	return runOne(runtime, "cancel", func(ctx context.Context, c apprun.Control, id string) (*runv1.Run, error) {
		r, err := c.Cancel(ctx, id)
		return r.GetRun(), err
	})
}
func runOne(runtime command.Runtime, name string, call func(context.Context, apprun.Control, string) (*runv1.Run, error)) *cobra.Command {
	return &cobra.Command{Use: name + " <run-id>", Short: name + " a run", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		value, err := call(s.Context, apprun.New(s.Clients.Run), args[0])
		if err != nil {
			return err
		}
		return renderRun(runtime, cmd, value)
	}}
}

func listCommand(runtime command.Runtime) *cobra.Command {
	var namespace, cursor string
	var statuses, labels []string
	var pageSize int32
	cmd := &cobra.Command{Use: "list", Short: "List runs", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		parsed, err := parse.RunStatuses(statuses)
		if err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := apprun.New(s.Clients.Run).List(s.Context, &runv1.ListRunsRequest{Filter: &runv1.RunListFilter{Namespace: namespace, Statuses: parsed, Labels: parse.Labels(labels), Cursor: cursor, PageSize: pageSize}})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintRunListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderRunTable(cmd.OutOrStdout(), resp.GetRuns())
		return nil
	}}
	f := cmd.Flags()
	f.StringVar(&namespace, "namespace", "", "namespace filter")
	f.StringArrayVar(&statuses, "status", nil, "status filter; may be repeated")
	f.StringArrayVar(&labels, "label", nil, "label filter; may be repeated")
	f.StringVar(&cursor, "cursor", "", "pagination cursor")
	f.Int32Var(&pageSize, "page-size", 0, "page size")
	return cmd
}

func renderRun(runtime command.Runtime, cmd *cobra.Command, value *runv1.Run) error {
	if runtime.Options.Output == "json" {
		return output.PrintRunResponseJSON(cmd.OutOrStdout(), value)
	}
	output.RenderRun(cmd.OutOrStdout(), value)
	return nil
}
