package local

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	)
	return root
}

func manager(runtime command.Runtime, version string, cmd *cobra.Command) (*applocal.Manager, error) {
	return applocal.New(version, runtime.Options.ConfigPath, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func upCommand(runtime command.Runtime, version string) *cobra.Command {
	var options applocal.UpOptions
	cmd := &cobra.Command{Use: "up", Short: "Start the local Axern stack", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		return service.Up(cmd.Context(), options)
	}}
	cmd.Flags().StringVar(&options.Profile, "profile", "", "profile: default or observability; omitted preserves the current profile")
	cmd.Flags().BoolVar(&options.Use, "use", false, "select the local context even when another context is active")
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
	return &cobra.Command{Use: "doctor", Short: "Diagnose local prerequisites and state", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := manager(runtime, version, cmd)
		if err != nil {
			return err
		}
		report := service.Doctor(cmd.Context())
		if runtime.Options.Output == "json" {
			if err := output.PrintJSON(cmd.OutOrStdout(), report); err != nil {
				return err
			}
		} else {
			for _, check := range report.Checks {
				state := "ok"
				if !check.OK {
					state = "failed"
					if check.Severity == "recommended" {
						state = "warning"
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-7s %-24s %s\n", state, check.Code, check.Message)
				if check.Recommendation != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "        Fix: %s\n", check.Recommendation)
				}
			}
		}
		if !report.Healthy {
			return fmt.Errorf("local environment has failed checks")
		}
		return nil
	}}
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
