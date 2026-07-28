package run

import (
	"context"
	"fmt"
	"time"

	apprun "github.com/cofy-x/axern/apps/cli/internal/application/run"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	"github.com/cofy-x/axern/apps/cli/internal/resourcespec"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "run", Short: "Manage one-shot runs"}
	root.AddCommand(createCommand(runtime), getCommand(runtime), listCommand(runtime), cancelCommand(runtime))
	return root
}

type createOptions struct {
	file, namespace, environmentID, templateID, templateVersion, imageRef, credentialID, cwd, runtimeClass, requestCPU, requestMemory, limitCPU, limitMemory, waitFor string
	argv, env, secretEnv, secretFile, imageMount, labels                                                                                                              []string
	rootfsReadonly, wait                                                                                                                                              bool
	waitTimeout                                                                                                                                                       time.Duration
}

func createCommand(runtime command.Runtime) *cobra.Command {
	options := &createOptions{namespace: "default", waitFor: string(apprun.WaitTargetTerminal), waitTimeout: apprun.DefaultCreateWaitTimeout}
	cmd := &cobra.Command{Use: "create", Short: "Create a run", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		if options.wait {
			target, err := apprun.ParseWaitTarget(options.waitFor, apprun.WaitTargetTerminal)
			if err != nil {
				return command.Usage(err)
			}
			final, waitErr := control.Wait(s.Context, value.GetID(), target, options.waitTimeout, nil)
			if final != nil {
				value = final
			}
			if err := renderRun(runtime, cmd, value); err != nil {
				return err
			}
			if value.GetExitCodeKnown() && value.GetExitCode() != 0 {
				return command.ExitError{Code: int(value.GetExitCode()), Err: waitErr}
			}
			return waitErr
		}
		return renderRun(runtime, cmd, value)
	}}
	options.bind(cmd)
	return cmd
}

func (o *createOptions) bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.file, "file", "f", "", "axern/v1 Run spec")
	f.StringVar(&o.namespace, "namespace", "default", "namespace")
	f.StringArrayVar(&o.argv, "argv", nil, "command argument; may be repeated")
	f.StringArrayVar(&o.env, "env", nil, "environment KEY=VALUE; may be repeated")
	f.StringArrayVar(&o.secretEnv, "secret-env", nil, "secret environment mapping; may be repeated")
	f.StringArrayVar(&o.secretFile, "secret-file", nil, "secret file mapping; may be repeated")
	f.StringArrayVar(&o.imageMount, "image-mount", nil, "read-only image mount; may be repeated")
	f.StringVar(&o.cwd, "cwd", "", "working directory")
	f.StringVar(&o.runtimeClass, "runtime-class", "", "runtime class")
	f.StringArrayVar(&o.labels, "label", nil, "label key=value; may be repeated")
	f.StringVar(&o.environmentID, "environment-id", "", "existing environment id")
	f.StringVar(&o.templateID, "template-id", "", "runtime template id")
	f.StringVar(&o.templateVersion, "template-version", "", "runtime template version")
	f.StringVar(&o.imageRef, "image-ref", "", "OCI image reference")
	f.StringVar(&o.credentialID, "registry-credential-id", "", "registry credential id")
	f.BoolVar(&o.rootfsReadonly, "rootfs-readonly", false, "mount rootfs read-only")
	f.StringVar(&o.requestCPU, "request-cpu", "", "CPU request")
	f.StringVar(&o.requestMemory, "request-memory", "", "memory request")
	f.StringVar(&o.limitCPU, "limit-cpu", "", "CPU limit")
	f.StringVar(&o.limitMemory, "limit-memory", "", "memory limit")
	f.BoolVar(&o.wait, "wait", false, "wait for selected state")
	f.StringVar(&o.waitFor, "wait-for", string(apprun.WaitTargetTerminal), "running or terminal")
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
	resources, err := command.Resources(o.requestCPU, o.requestMemory, o.limitCPU, o.limitMemory)
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
		return nil, fmt.Errorf("exactly one of environment-id, template-id, or image-ref is required")
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

var runDefinitionFlags = []string{"namespace", "argv", "env", "secret-env", "secret-file", "image-mount", "cwd", "runtime-class", "label", "environment-id", "template-id", "template-version", "image-ref", "registry-credential-id", "rootfs-readonly", "request-cpu", "request-memory", "limit-cpu", "limit-memory"}

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
