package local

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	appdoctor "github.com/cofy-x/axern/apps/cli/internal/application/doctor"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	applocal "github.com/cofy-x/axern/apps/cli/internal/localruntime"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime, version string) *cobra.Command {
	root := &cobra.Command{Use: "local", Short: "Run Axern locally with Docker"}
	root.AddCommand(
		upCommand(runtime, version), statusCommand(runtime, version), logsCommand(runtime, version),
		doctorCommand(runtime, version), downCommand(runtime, version), resetCommand(runtime, version),
		upgradeCommand(runtime, version), pathCommand(runtime, version),
		imageCommand(runtime, version),
	)
	return root
}

func imageCommand(runtime command.Runtime, version string) *cobra.Command {
	root := &cobra.Command{Use: "image", Short: "Manage images in the local Axern node"}
	var pull bool
	load := &cobra.Command{Use: "load IMAGE", Short: "Stream a local Docker image into the local Axern node", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		result, err := service.ImageLoad(cmd.Context(), args[0], applocal.ImageLoadOptions{Pull: pull})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintJSON(cmd.OutOrStdout(), result)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Loaded %s as %s\n", result.SourceRef, result.ImmutableRef)
		fmt.Fprintf(cmd.OutOrStdout(), "  generation: %s\n  archive: %s\n  platform: %s\n  size: %d bytes\n  reused: %t\n", result.GenerationDigest, result.ArchiveDigest, result.Platform, result.SizeBytes, result.Reused)
		return nil
	}}
	load.Flags().BoolVar(&pull, "pull", false, "pull IMAGE before loading it")
	root.AddCommand(load)
	return root
}

func manager(runtime command.Runtime, version string, cmd *cobra.Command) (*applocal.Manager, error) {
	return applocal.New(version, runtime.Options.ConfigPath, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func upCommand(runtime command.Runtime, version string) *cobra.Command {
	options := applocal.UpOptions{ReadinessTimeout: applocal.DefaultReadinessTimeout}
	cmd := &cobra.Command{Use: "up", Short: "Start the local Axern stack", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if options.ReadinessTimeout <= 0 {
			return command.Usage(fmt.Errorf("--wait-timeout must be positive"))
		}
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		cancel := func() {}
		if runtime.Options != nil && runtime.Options.Timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, runtime.Options.Timeout)
		}
		defer cancel()
		return service.Up(ctx, options)
	}}
	cmd.Flags().StringVar(&options.Profile, "profile", "", "profile: default or observability; omitted preserves the current profile")
	cmd.Flags().BoolVar(&options.Use, "use", false, "select the local context even when another context is active")
	cmd.Flags().DurationVar(&options.ReadinessTimeout, "wait-timeout", options.ReadinessTimeout, "wait for fresh local capability certification; bounded by --timeout")
	return cmd
}

func statusCommand(runtime command.Runtime, version string) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show local component status", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		value, err := service.Status(cmd.Context())
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintJSON(cmd.OutOrStdout(), value)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "State:      %s\nCLI:        %s\n", value.State, value.CLIVersion)
		if value.StackVersion != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Stack:      %s\n", value.StackVersion)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Dashboard:  %s\nData:       %s\nDisk:       %s\n", value.DashboardURL, value.DataPath, formatBytes(value.DiskBytes))
		fmt.Fprintf(cmd.OutOrStdout(), "Gateway:    grpc=%d http=%d ssh=%d\n", value.Ports["gateway_grpc"], value.Ports["gateway_http"], value.Ports["gateway_ssh"])
		if value.CurrentContext != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Context:    %s\n", value.CurrentContext)
		}
		for _, component := range value.Components {
			health := component.Health
			if health != "" {
				health = " (" + health + ")"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-18s %s%s\n", component.Name, component.State, health)
		}
		return nil
	}}
}

func formatBytes(value int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	number := float64(value)
	unit := units[0]
	for _, candidate := range units {
		unit = candidate
		if number < 1024 || candidate == units[len(units)-1] {
			break
		}
		number /= 1024
	}
	return fmt.Sprintf("%.1f %s", number, unit)
}

func logsCommand(runtime command.Runtime, version string) *cobra.Command {
	options := applocal.LogOptions{Tail: 200}
	cmd := &cobra.Command{Use: "logs [component]", Short: "Show local service logs", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			options.Component = args[0]
		}
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		return service.Logs(cmd.Context(), options)
	}}
	cmd.Flags().BoolVarP(&options.Follow, "follow", "f", false, "follow new log output")
	cmd.Flags().IntVar(&options.Tail, "tail", 200, "number of lines to show per component")
	cmd.Flags().StringVar(&options.Since, "since", "", "show logs since a Docker duration or timestamp")
	return cmd
}

func doctorCommand(runtime command.Runtime, version string) *cobra.Command {
	options := applocal.DoctorOptions{QueryName: "axern.cofy-x.space.", CheckTimeout: 15 * time.Second}
	probeTimeout := 5 * time.Minute
	templateID := "python311"
	runtimeClass := "runsc"
	cmd := &cobra.Command{Use: "doctor", Short: "Diagnose local prerequisites and runtime DNS", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		queryName, err := applocal.NormalizeDNSQueryName(options.QueryName)
		if err != nil {
			return command.Usage(fmt.Errorf("--dns-query-name: %w", err))
		}
		options.QueryName = queryName
		if options.CheckTimeout <= 0 {
			return command.Usage(fmt.Errorf("--check-timeout must be positive"))
		}
		for _, name := range []string{"probe-timeout", "template-id", "runtime-class"} {
			if cmd.Flags().Changed(name) && !options.Probe {
				return command.Usage(fmt.Errorf("--%s requires --probe", name))
			}
		}
		if options.Probe {
			if probeTimeout <= 0 {
				return command.Usage(fmt.Errorf("--probe-timeout must be positive"))
			}
			if strings.TrimSpace(templateID) == "" || strings.TrimSpace(runtimeClass) == "" {
				return command.Usage(fmt.Errorf("--template-id and --runtime-class are required with --probe"))
			}
			if runtime.HasExplicitConnectionOverride() {
				return command.Usage(fmt.Errorf("explicit remote connection options cannot be used with local doctor --probe"))
			}
			options.SandboxProbe = func(ctx context.Context) applocal.Check {
				connection, resolveErr := runtime.ResolveNamedConnection(applocal.ContextName)
				if resolveErr != nil {
					return applocal.Check{Name: "runtime_dns_sandbox", Status: "fail", Code: "runtime_dns_sandbox_probe_failed", Message: "the product-owned local context is unavailable", Remediation: "run `axern local up` and retry"}
				}
				session, openErr := connection.Open(ctx)
				if openErr != nil {
					return applocal.Check{Name: "runtime_dns_sandbox", Status: "fail", Code: "runtime_dns_sandbox_probe_failed", Message: "the local control plane could not be reached", Remediation: "inspect `axern local status` and retry"}
				}
				defer session.Close()
				check := appdoctor.DNSProbe(ctx, &appdoctor.Session{
					Context: session.Context, Namespace: session.Clients.Namespace, Secret: session.Clients.Secret,
					Environment: session.Clients.Environment, Run: session.Clients.Run,
				}, appdoctor.DNSProbeOptions{QueryName: queryName, TemplateID: templateID, RuntimeClass: runtimeClass, Timeout: probeTimeout})
				return applocal.Check{Name: check.Name, Status: string(check.Status), Code: check.Code, DurationMS: check.DurationMS, Message: check.Message, Remediation: check.Remediation, Details: check.Details}
			}
		}
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		report := service.Doctor(cmd.Context(), options)
		if runtime.Options.Output == "json" {
			if err := output.PrintJSON(cmd.OutOrStdout(), report); err != nil {
				return err
			}
		} else {
			renderLocalDoctorTable(cmd, report)
		}
		switch report.Status {
		case "healthy":
			return nil
		case "degraded":
			return command.ExitError{Code: 1}
		default:
			return command.ExitError{Code: 3}
		}
	}}
	cmd.Flags().BoolVar(&options.Probe, "probe", false, "create temporary platform resources and run a DNS query in an OCI sandbox")
	cmd.Flags().StringVar(&options.QueryName, "dns-query-name", options.QueryName, "absolute DNS name to query")
	cmd.Flags().DurationVar(&options.CheckTimeout, "check-timeout", options.CheckTimeout, "timeout for each read-only DNS check")
	cmd.Flags().DurationVar(&probeTimeout, "probe-timeout", probeTimeout, "timeout for sandbox execution; requires --probe")
	cmd.Flags().StringVar(&templateID, "template-id", templateID, "runtime template used by --probe")
	cmd.Flags().StringVar(&runtimeClass, "runtime-class", runtimeClass, "runtime class used by --probe")
	return cmd
}

func renderLocalDoctorTable(cmd *cobra.Command, report applocal.DoctorReport) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Axern local doctor: %s\n", report.Status)
	fmt.Fprintf(w, "Mode: %s\n\n", strings.ReplaceAll(report.Mode, "_", "-"))
	rows := make([][]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		rows = append(rows, []string{
			check.Name,
			check.Status,
			check.Code,
			fmt.Sprintf("%dms", check.DurationMS),
			check.Message,
			check.Remediation,
		})
	}
	output.RenderTable(w, []string{"CHECK", "STATUS", "CODE", "LATENCY", "MESSAGE", "REMEDIATION"}, rows)
}

func downCommand(runtime command.Runtime, version string) *cobra.Command {
	return &cobra.Command{Use: "down", Short: "Stop local services and preserve data", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		return service.Down(cmd.Context())
	}}
}

func resetCommand(runtime command.Runtime, version string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "reset", Short: "Permanently delete the local stack and its data", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		if !force {
			if !terminal(os.Stdin) {
				return command.Usage(fmt.Errorf("--force is required when stdin is not a terminal"))
			}
			fmt.Fprint(cmd.ErrOrStderr(), "Type local to permanently delete all Axern local data: ")
			value, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimSpace(value) != applocal.ContextName {
				return fmt.Errorf("reset cancelled")
			}
		}
		return service.Reset(cmd.Context())
	}}
	cmd.Flags().BoolVar(&force, "force", false, "delete without interactive confirmation")
	return cmd
}

func terminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func upgradeCommand(runtime command.Runtime, version string) *cobra.Command {
	return &cobra.Command{Use: "upgrade", Short: "Safely upgrade the local stack", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		return service.Upgrade(cmd.Context())
	}}
}

func pathCommand(runtime command.Runtime, version string) *cobra.Command {
	return &cobra.Command{Use: "path", Short: "Print the local data path", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"path": service.Path()})
		}
		fmt.Fprintln(cmd.OutOrStdout(), service.Path())
		return nil
	}}
}
