package service

import (
	"fmt"
	"strings"
	"time"

	appservice "github.com/cofy-x/axern/apps/cli/internal/application/service"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	"github.com/cofy-x/axern/apps/cli/internal/resourcespec"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "service", Aliases: []string{"svc"}, Short: "Manage long-running services"}
	root.AddCommand(createCommand(runtime), getCommand(runtime), listCommand(runtime), updateCommand(runtime), deleteCommand(runtime), replicasCommand(runtime), eventsCommand(runtime), tunnelCommand(runtime))
	return root
}

type createOptions struct {
	file, namespace, environmentID, templateID, templateVersion, imageRef, credentialID, runtimeClass string
	requestCPU, requestMemory, requestEphemeralStorage, limitCPU, limitMemory, limitEphemeralStorage  string
	argv, env, secretEnv, secretFile, volumes, imageMount, labels                                     []string
	replicas                                                                                          int32
	rootfsReadonly, wait                                                                              bool
	waitTimeout                                                                                       time.Duration
	readiness, liveness                                                                               probeOptions
	autoscaleMin, autoscaleMax                                                                        int32
}

type probeOptions struct {
	httpPort, tcpPort                  int32
	path, scheme                       string
	initialDelay, period, timeout      time.Duration
	successThreshold, failureThreshold int32
}

func createCommand(runtime command.Runtime) *cobra.Command {
	o := &createOptions{namespace: "default", replicas: 1, waitTimeout: appservice.DefaultCreateWaitTimeout}
	cmd := &cobra.Command{Use: "create", Short: "Create a service", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		params, err := o.params(cmd)
		if err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		control := appservice.NewWithEnvironment(s.Clients.Service, s.Clients.Environment)
		resp, err := control.Create(s.Context, params)
		if err != nil {
			return err
		}
		value := resp.GetService()
		if o.wait {
			snapshot, waitErr := control.WaitReady(s.Context, value.GetID(), o.waitTimeout, nil)
			if snapshot.Service != nil {
				value = snapshot.Service
			}
			if err := renderService(runtime, cmd, value, snapshot.Events); err != nil {
				return err
			}
			return waitErr
		}
		return renderService(runtime, cmd, value, nil)
	}}
	o.bind(cmd)
	return cmd
}

func (o *createOptions) bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.file, "file", "f", "", "axern/v1 Service spec")
	f.StringVar(&o.namespace, "namespace", "default", "namespace")
	f.Int32Var(&o.replicas, "replicas", 1, "desired replicas")
	o.bindExecution(f)
	f.StringArrayVar(&o.labels, "label", nil, "label key=value; may be repeated")
	f.StringVar(&o.environmentID, "environment-id", "", "existing environment id")
	f.StringVar(&o.templateID, "template-id", "", "runtime template id")
	f.StringVar(&o.templateVersion, "template-version", "", "runtime template version")
	f.StringVar(&o.imageRef, "image-ref", "", "OCI image reference")
	f.StringVar(&o.credentialID, "registry-credential-id", "", "registry credential id")
	f.BoolVar(&o.rootfsReadonly, "rootfs-readonly", false, "mount rootfs read-only")
	bindProbe(f, "readiness", &o.readiness)
	bindProbe(f, "liveness", &o.liveness)
	f.Int32Var(&o.autoscaleMin, "autoscale-min-replicas", 0, "autoscaling minimum")
	f.Int32Var(&o.autoscaleMax, "autoscale-max-replicas", 0, "autoscaling maximum")
	f.BoolVar(&o.wait, "wait", false, "wait for readiness")
	f.DurationVar(&o.waitTimeout, "wait-timeout", appservice.DefaultCreateWaitTimeout, "readiness timeout; 0 disables it")
}

func (o *createOptions) bindExecution(f *pflag.FlagSet) {
	f.StringArrayVar(&o.argv, "argv", nil, "command argument; may be repeated")
	f.StringArrayVar(&o.env, "env", nil, "environment KEY=VALUE; may be repeated")
	f.StringArrayVar(&o.secretEnv, "secret-env", nil, "secret environment mapping; may be repeated")
	f.StringArrayVar(&o.secretFile, "secret-file", nil, "secret file mapping; may be repeated")
	f.StringArrayVar(&o.volumes, "volume", nil, "volume mount; may be repeated")
	f.StringArrayVar(&o.imageMount, "image-mount", nil, "read-only image mount; may be repeated")
	f.StringVar(&o.runtimeClass, "runtime-class", "", "runtime class")
	f.StringVar(&o.requestCPU, "request-cpu", "", "CPU request")
	f.StringVar(&o.requestMemory, "request-memory", "", "memory request")
	f.StringVar(&o.requestEphemeralStorage, "request-ephemeral-storage", "", "node-local ephemeral storage request")
	f.StringVar(&o.limitCPU, "limit-cpu", "", "CPU limit")
	f.StringVar(&o.limitMemory, "limit-memory", "", "memory limit")
	f.StringVar(&o.limitEphemeralStorage, "limit-ephemeral-storage", "", "node-local ephemeral storage limit")
}

func bindProbe(f interface {
	Int32Var(*int32, string, int32, string)
	StringVar(*string, string, string, string)
	DurationVar(*time.Duration, string, time.Duration, string)
}, prefix string, value *probeOptions) {
	f.Int32Var(&value.httpPort, prefix+"-http-port", 0, prefix+" HTTP port")
	f.StringVar(&value.path, prefix+"-http-path", "", prefix+" HTTP path")
	f.StringVar(&value.scheme, prefix+"-http-scheme", "", prefix+" HTTP scheme")
	f.Int32Var(&value.tcpPort, prefix+"-tcp-port", 0, prefix+" TCP port")
	f.DurationVar(&value.initialDelay, prefix+"-initial-delay", 0, prefix+" initial delay")
	f.DurationVar(&value.period, prefix+"-period", 0, prefix+" period")
	f.DurationVar(&value.timeout, prefix+"-timeout", 0, prefix+" timeout")
	f.Int32Var(&value.successThreshold, prefix+"-success-threshold", 0, prefix+" success threshold")
	f.Int32Var(&value.failureThreshold, prefix+"-failure-threshold", 0, prefix+" failure threshold")
}

func (o createOptions) params(cmd *cobra.Command) (appservice.CreateParams, error) {
	if o.file != "" {
		for _, name := range serviceDefinitionFlags {
			if cmd.Flags().Changed(name) {
				return appservice.CreateParams{}, fmt.Errorf("--file cannot be combined with --%s", name)
			}
		}
		value, err := resourcespec.Load(o.file, resourcespec.KindService)
		if err != nil {
			return appservice.CreateParams{}, err
		}
		environmentID, environment := value.EnvironmentSpec()
		execution, err := value.ExecutionConfig()
		if err != nil {
			return appservice.CreateParams{}, err
		}
		readiness, liveness, autoscaling, err := value.ServiceConfig()
		return appservice.CreateParams{Namespace: value.Metadata.Namespace, EnvironmentID: environmentID, Spec: environment, Replicas: value.ServiceReplicas(), Config: execution, Labels: value.Metadata.Labels, ReadinessProbe: readiness, LivenessProbe: liveness, AutoscalingPolicy: autoscaling}, err
	}
	environmentID, environment, err := o.environment()
	if err != nil {
		return appservice.CreateParams{}, err
	}
	execution, err := o.execution()
	if err != nil {
		return appservice.CreateParams{}, err
	}
	readiness, err := o.readiness.build()
	if err != nil {
		return appservice.CreateParams{}, fmt.Errorf("readiness: %w", err)
	}
	liveness, err := o.liveness.build()
	if err != nil {
		return appservice.CreateParams{}, fmt.Errorf("liveness: %w", err)
	}
	var autoscaling *servicev1.ServiceAutoscalingPolicy
	if o.autoscaleMin != 0 || o.autoscaleMax != 0 {
		if o.autoscaleMin < 0 || o.autoscaleMax < o.autoscaleMin {
			return appservice.CreateParams{}, fmt.Errorf("invalid autoscaling range")
		}
		autoscaling = &servicev1.ServiceAutoscalingPolicy{MinReplicas: o.autoscaleMin, MaxReplicas: o.autoscaleMax}
	}
	return appservice.CreateParams{Namespace: o.namespace, EnvironmentID: environmentID, Spec: environment, Replicas: o.replicas, Config: execution, Labels: parse.Labels(o.labels), ReadinessProbe: readiness, LivenessProbe: liveness, AutoscalingPolicy: autoscaling}, nil
}

func (o createOptions) environment() (string, *environmentv1.EnvironmentSpec, error) {
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
		return "", nil, fmt.Errorf("exactly one of environment-id, template-id, or image-ref is required")
	}
	if o.environmentID != "" {
		return o.environmentID, nil, nil
	}
	value := &environmentv1.EnvironmentSpec{Namespace: o.namespace}
	if o.templateID != "" {
		value.TemplateID, value.TemplateVersion = o.templateID, o.templateVersion
	} else {
		value.Image = &environmentv1.EnvironmentImageSource{Ref: o.imageRef, RegistryCredentialID: o.credentialID, RootfsReadonly: o.rootfsReadonly}
	}
	return "", value, nil
}
func (o createOptions) execution() (*commonv1.ExecutionConfig, error) {
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
	volumes, err := parse.ServiceVolumeMounts(o.volumes)
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
	return &commonv1.ExecutionConfig{Argv: o.argv, Env: env, SecretEnv: secretEnv, SecretFiles: secretFiles, VolumeMounts: volumes, ImageMounts: imageMounts, RuntimeClass: o.runtimeClass, Resources: resources}, nil
}

func (p probeOptions) build() (*servicev1.ServiceProbe, error) {
	if p.httpPort == 0 && p.tcpPort == 0 {
		if p.path != "" || p.scheme != "" || p.initialDelay != 0 || p.period != 0 || p.timeout != 0 || p.successThreshold != 0 || p.failureThreshold != 0 {
			return nil, fmt.Errorf("probe action is required")
		}
		return nil, nil
	}
	if p.httpPort != 0 && p.tcpPort != 0 {
		return nil, fmt.Errorf("HTTP and TCP probes cannot be combined")
	}
	if p.httpPort < 0 || p.httpPort > 65535 || p.tcpPort < 0 || p.tcpPort > 65535 {
		return nil, fmt.Errorf("probe port must be in 1..65535")
	}
	if p.initialDelay < 0 || p.period < 0 || p.timeout < 0 {
		return nil, fmt.Errorf("probe durations must not be negative")
	}
	if p.successThreshold < 0 || p.failureThreshold < 0 {
		return nil, fmt.Errorf("probe thresholds must not be negative")
	}
	value := &servicev1.ServiceProbe{SuccessThreshold: p.successThreshold, FailureThreshold: p.failureThreshold}
	if p.initialDelay != 0 {
		value.InitialDelay = durationpb.New(p.initialDelay)
	}
	if p.period != 0 {
		value.Period = durationpb.New(p.period)
	}
	if p.timeout != 0 {
		value.Timeout = durationpb.New(p.timeout)
	}
	if p.httpPort != 0 {
		scheme := servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTP
		if strings.EqualFold(p.scheme, "https") {
			scheme = servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTPS
		} else if p.scheme != "" && !strings.EqualFold(p.scheme, "http") {
			return nil, fmt.Errorf("HTTP scheme must be http or https")
		}
		value.Action = &servicev1.ServiceProbe_Http{Http: &servicev1.HttpProbe{Port: p.httpPort, Path: p.path, Scheme: scheme}}
	} else {
		value.Action = &servicev1.ServiceProbe_Tcp{Tcp: &servicev1.TcpProbe{Port: p.tcpPort}}
	}
	return value, nil
}

var serviceDefinitionFlags = []string{"namespace", "replicas", "argv", "env", "secret-env", "secret-file", "volume", "image-mount", "runtime-class", "label", "environment-id", "template-id", "template-version", "image-ref", "registry-credential-id", "rootfs-readonly", "request-cpu", "request-memory", "request-ephemeral-storage", "limit-cpu", "limit-memory", "limit-ephemeral-storage", "readiness-http-port", "readiness-http-path", "readiness-http-scheme", "readiness-tcp-port", "readiness-initial-delay", "readiness-period", "readiness-timeout", "readiness-success-threshold", "readiness-failure-threshold", "liveness-http-port", "liveness-http-path", "liveness-http-scheme", "liveness-tcp-port", "liveness-initial-delay", "liveness-period", "liveness-timeout", "liveness-success-threshold", "liveness-failure-threshold", "autoscale-min-replicas", "autoscale-max-replicas"}

func getCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "get <service-id>", Short: "Get service, rollout, and latest event", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		result, err := appservice.New(s.Clients.Service).Get(s.Context, args[0])
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintServiceDescribeJSON(cmd.OutOrStdout(), result.Service, result.LatestEvent)
		}
		output.RenderServiceDescribe(cmd.OutOrStdout(), result.Service, result.LatestEvent)
		return nil
	}}
}

func listCommand(runtime command.Runtime) *cobra.Command {
	var namespace, cursor string
	var statuses, labels []string
	var pageSize int32
	cmd := &cobra.Command{Use: "list", Short: "List services", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		parsed, err := parse.ServiceStatuses(statuses)
		if err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appservice.New(s.Clients.Service).List(s.Context, &servicev1.ListServicesRequest{Filter: &servicev1.ServiceListFilter{Namespace: namespace, Statuses: parsed, Labels: parse.Labels(labels), Cursor: cursor, PageSize: pageSize}})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintServiceListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderServiceTable(cmd.OutOrStdout(), resp.GetServices(), output.ServiceListTableOptions{})
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

func updateCommand(runtime command.Runtime) *cobra.Command {
	o := &createOptions{}
	var replicas int32
	var environmentID string
	var expectedVersion int64
	var maxSurge, maxUnavailable int32
	cmd := &cobra.Command{Use: "update <service-id>", Short: "Update service rollout fields", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		paths := []string{}
		req := &servicev1.UpdateServiceRequest{ServiceID: args[0], ExpectedVersion: expectedVersion}
		executionChanged := anyFlagChanged(cmd, serviceExecutionFlags...)
		if cmd.Flags().Changed("replicas") {
			req.Replicas = &replicas
			paths = append(paths, "replicas")
		}
		if cmd.Flags().Changed("environment-id") {
			req.EnvironmentID = &environmentID
			paths = append(paths, "environment_id")
		}
		if cmd.Flags().Changed("max-surge") || cmd.Flags().Changed("max-unavailable") {
			req.RolloutPolicy = &servicev1.ServiceRolloutPolicy{MaxSurge: maxSurge, MaxUnavailable: maxUnavailable}
			paths = append(paths, "rollout_policy")
		}
		if len(paths) == 0 && !executionChanged {
			return command.Usage(fmt.Errorf("at least one mutable field is required"))
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		control := appservice.New(s.Clients.Service)
		if executionChanged {
			current, err := control.GetService(s.Context, args[0])
			if err != nil {
				return err
			}
			if current.GetService() == nil {
				return fmt.Errorf("service %q not found", args[0])
			}
			if req.GetExpectedVersion() == 0 {
				req.ExpectedVersion = current.GetService().GetVersion()
			}
			req.Config, err = o.executionUpdate(cmd, current.GetService().GetConfig())
			if err != nil {
				return command.Usage(err)
			}
			paths = append(paths, "config")
		}
		req.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}
		resp, err := control.Update(s.Context, req)
		if err != nil {
			return err
		}
		return renderService(runtime, cmd, resp.GetService(), nil)
	}}
	f := cmd.Flags()
	f.Int32Var(&replicas, "replicas", 0, "desired replicas")
	f.StringVar(&environmentID, "environment-id", "", "environment id")
	o.bindExecution(f)
	f.Int64Var(&expectedVersion, "expected-version", 0, "optimistic concurrency version")
	f.Int32Var(&maxSurge, "max-surge", 0, "rollout surge")
	f.Int32Var(&maxUnavailable, "max-unavailable", 0, "rollout unavailable budget")
	return cmd
}

var serviceExecutionFlags = []string{"argv", "env", "secret-env", "secret-file", "volume", "image-mount", "runtime-class", "request-cpu", "request-memory", "request-ephemeral-storage", "limit-cpu", "limit-memory", "limit-ephemeral-storage"}

func (o createOptions) executionUpdate(cmd *cobra.Command, current *commonv1.ExecutionConfig) (*commonv1.ExecutionConfig, error) {
	next := &commonv1.ExecutionConfig{}
	if current != nil {
		next = proto.Clone(current).(*commonv1.ExecutionConfig)
	}
	if cmd.Flags().Changed("argv") {
		next.Argv = append([]string(nil), o.argv...)
	}
	if cmd.Flags().Changed("env") {
		value, err := parse.EnvFlags(o.env)
		if err != nil {
			return nil, err
		}
		next.Env = value
	}
	if cmd.Flags().Changed("secret-env") {
		value, err := parse.SecretEnvVars(o.secretEnv)
		if err != nil {
			return nil, err
		}
		next.SecretEnv = value
	}
	if cmd.Flags().Changed("secret-file") {
		value, err := parse.SecretFiles(o.secretFile)
		if err != nil {
			return nil, err
		}
		next.SecretFiles = value
	}
	if cmd.Flags().Changed("volume") {
		value, err := parse.ServiceVolumeMounts(o.volumes)
		if err != nil {
			return nil, err
		}
		next.VolumeMounts = value
	}
	if cmd.Flags().Changed("image-mount") {
		value, err := parse.ImageMounts(o.imageMount)
		if err != nil {
			return nil, err
		}
		next.ImageMounts = value
	}
	if cmd.Flags().Changed("runtime-class") {
		next.RuntimeClass = o.runtimeClass
	}
	if err := o.mergeUpdatedResources(cmd, next); err != nil {
		return nil, err
	}
	return next, nil
}

func (o createOptions) mergeUpdatedResources(cmd *cobra.Command, config *commonv1.ExecutionConfig) error {
	resourceFlags := []struct {
		name             string
		value            string
		memory           bool
		ephemeralStorage bool
		limit            bool
	}{
		{name: "request-cpu", value: o.requestCPU},
		{name: "request-memory", value: o.requestMemory, memory: true},
		{name: "request-ephemeral-storage", value: o.requestEphemeralStorage, memory: true, ephemeralStorage: true},
		{name: "limit-cpu", value: o.limitCPU, limit: true},
		{name: "limit-memory", value: o.limitMemory, memory: true, limit: true},
		{name: "limit-ephemeral-storage", value: o.limitEphemeralStorage, memory: true, ephemeralStorage: true, limit: true},
	}
	for _, field := range resourceFlags {
		if !cmd.Flags().Changed(field.name) {
			continue
		}
		var (
			value int64
			err   error
		)
		if field.memory {
			value, err = parse.Memory(field.value)
		} else {
			value, err = parse.CPU(field.value)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", field.name, err)
		}
		if config.Resources == nil {
			config.Resources = &commonv1.ResourceSpec{}
		}
		quantity := config.Resources.Requests
		if field.limit {
			quantity = config.Resources.Limits
		}
		if quantity == nil {
			quantity = &commonv1.ResourceQuantity{}
			if field.limit {
				config.Resources.Limits = quantity
			} else {
				config.Resources.Requests = quantity
			}
		}
		if field.ephemeralStorage {
			quantity.EphemeralStorageBytes = value
		} else if field.memory {
			quantity.MemoryBytes = value
		} else {
			quantity.CpuMilli = value
		}
	}
	if config.Resources == nil {
		return nil
	}
	if config.GetResources().GetRequests().GetCpuMilli() == 0 && config.GetResources().GetRequests().GetMemoryBytes() == 0 && config.GetResources().GetRequests().GetEphemeralStorageBytes() == 0 {
		config.Resources.Requests = nil
	}
	if config.GetResources().GetLimits().GetCpuMilli() == 0 && config.GetResources().GetLimits().GetMemoryBytes() == 0 && config.GetResources().GetLimits().GetEphemeralStorageBytes() == 0 {
		config.Resources.Limits = nil
	}
	if config.GetResources().GetRequests() == nil && config.GetResources().GetLimits() == nil {
		config.Resources = nil
	}
	return nil
}

func anyFlagChanged(cmd *cobra.Command, names ...string) bool {
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func deleteCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "delete <service-id>", Short: "Delete a service", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		result, err := appservice.New(s.Clients.Service).DeleteService(s.Context, args[0])
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintServiceResponseJSON(cmd.OutOrStdout(), result.Service)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Service deleted: %s\n", result.ServiceID)
		return nil
	}}
}

func replicasCommand(runtime command.Runtime) *cobra.Command {
	var view string
	var statuses []string
	cmd := &cobra.Command{Use: "replicas <service-id>", Short: "List observed replicas", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		parsedView, err := parse.ServiceReplicaView(view)
		if err != nil {
			return command.Usage(err)
		}
		parsedStatuses, err := parse.AllocationStatuses(statuses)
		if err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appservice.New(s.Clients.Service).ListReplicas(s.Context, args[0], &servicev1.ServiceReplicaListFilter{View: parsedView, Statuses: parsedStatuses})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintServiceReplicaListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderServiceReplicaTable(cmd.OutOrStdout(), resp.GetReplicas())
		return nil
	}}
	cmd.Flags().StringVar(&view, "view", "all", "all, current, ended, unhealthy, outdated, or updated")
	cmd.Flags().StringArrayVar(&statuses, "status", nil, "allocation status filter; may be repeated")
	return cmd
}

func eventsCommand(runtime command.Runtime) *cobra.Command {
	var limit int32
	cmd := &cobra.Command{Use: "events <service-id>", Short: "List rollout and health events", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appservice.New(s.Clients.Service).ListEvents(s.Context, args[0], limit)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintServiceEventListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderServiceEventTable(cmd.OutOrStdout(), resp.GetEvents())
		return nil
	}}
	cmd.Flags().Int32Var(&limit, "limit", 50, "maximum events")
	return cmd
}

func renderService(runtime command.Runtime, cmd *cobra.Command, value *servicev1.Service, events []*servicev1.ServiceEvent) error {
	var latest *servicev1.ServiceEvent
	if len(events) != 0 {
		latest = events[0]
	}
	if runtime.Options.Output == "json" {
		return output.PrintServiceDescribeJSON(cmd.OutOrStdout(), value, latest)
	}
	output.RenderServiceDescribe(cmd.OutOrStdout(), value, latest)
	return nil
}
