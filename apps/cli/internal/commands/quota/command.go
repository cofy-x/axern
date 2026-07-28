package quota

import (
	"fmt"
	"strings"

	appquota "github.com/cofy-x/axern/apps/cli/internal/application/quota"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "quota", Short: "Manage namespace admission quotas"}
	root.AddCommand(getCommand(runtime), listCommand(runtime), setCommand(runtime), unsetCommand(runtime), eventsCommand(runtime))
	return root
}

func getCommand(runtime command.Runtime) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{Use: "get", Short: "Get quota, usage, and admission signals", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		result, err := appquota.New(s.Clients.Quota).Describe(s.Context, namespace, s.Clients.Service)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintNamespaceQuotaDescribeJSON(cmd.OutOrStdout(), result.Quota, result.AdmissionBlockedServices)
		}
		output.RenderNamespaceQuotaDescribe(cmd.OutOrStdout(), result.Quota, result.AdmissionBlockedServices)
		return nil
	}}
	cmd.Flags().StringVar(&namespace, "namespace", "default", "namespace")
	return cmd
}

func listCommand(runtime command.Runtime) *cobra.Command {
	var constrained, pressure bool
	var sort string
	var limit int
	cmd := &cobra.Command{Use: "list", Short: "List namespace quotas", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appquota.New(s.Clients.Quota).List(s.Context, appquota.ListOptions{ConstrainedOnly: constrained, PressureOnly: pressure, Sort: sort, Limit: limit})
		if err != nil {
			return command.Usage(err)
		}
		if runtime.Options.Output == "json" {
			return output.PrintNamespaceQuotaListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderNamespaceQuotaTable(cmd.OutOrStdout(), resp.GetQuotas())
		return nil
	}}
	f := cmd.Flags()
	f.BoolVar(&constrained, "constrained", false, "only constrained namespaces")
	f.BoolVar(&pressure, "pressure", false, "only namespaces under quota pressure")
	f.StringVar(&sort, "sort", "namespace", "namespace, pressure, or updated")
	f.IntVar(&limit, "limit", 0, "maximum rows")
	return cmd
}

func setCommand(runtime command.Runtime) *cobra.Command {
	var namespace, cpu, memory string
	cmd := &cobra.Command{Use: "set", Short: "Set namespace quota limits", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("cpu") && !cmd.Flags().Changed("memory") {
			return command.Usage(fmt.Errorf("cpu or memory is required"))
		}
		cpuValue, err := optionalCPU(cpu)
		if err != nil {
			return command.Usage(err)
		}
		memoryValue, err := optionalMemory(memory)
		if err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appquota.New(s.Clients.Quota).Set(s.Context, namespace, &quotav1.NamespaceQuotaLimits{CpuMilli: cpuValue, MemoryBytes: memoryValue})
		if err != nil {
			return err
		}
		return renderQuota(runtime, cmd, resp.GetQuota())
	}}
	f := cmd.Flags()
	f.StringVar(&namespace, "namespace", "default", "namespace")
	f.StringVar(&cpu, "cpu", "", "CPU limit or unlimited")
	f.StringVar(&memory, "memory", "", "memory limit or unlimited")
	return cmd
}

func unsetCommand(runtime command.Runtime) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{Use: "unset", Short: "Remove namespace quota limits", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appquota.New(s.Clients.Quota).Unset(s.Context, namespace)
		if err != nil {
			return err
		}
		return renderQuota(runtime, cmd, resp.GetQuota())
	}}
	cmd.Flags().StringVar(&namespace, "namespace", "default", "namespace")
	return cmd
}

func eventsCommand(runtime command.Runtime) *cobra.Command {
	var namespace string
	var limit int
	cmd := &cobra.Command{Use: "events", Short: "List quota admission events", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if limit < 0 {
			return command.Usage(fmt.Errorf("limit must be non-negative"))
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appquota.New(s.Clients.Quota).ListEvents(s.Context, namespace, limit)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintNamespaceQuotaEventsJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderNamespaceQuotaEventTable(cmd.OutOrStdout(), resp.GetEvents())
		return nil
	}}
	cmd.Flags().StringVar(&namespace, "namespace", "default", "namespace")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum events")
	return cmd
}

func optionalCPU(value string) (*wrapperspb.Int64Value, error) {
	if value = strings.TrimSpace(value); value == "" || strings.EqualFold(value, "unlimited") {
		return nil, nil
	}
	parsed, err := parse.CPU(value)
	if err != nil {
		return nil, fmt.Errorf("cpu: %w", err)
	}
	return wrapperspb.Int64(parsed), nil
}
func optionalMemory(value string) (*wrapperspb.Int64Value, error) {
	if value = strings.TrimSpace(value); value == "" || strings.EqualFold(value, "unlimited") {
		return nil, nil
	}
	parsed, err := parse.Memory(value)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	return wrapperspb.Int64(parsed), nil
}
func renderQuota(runtime command.Runtime, cmd *cobra.Command, value *quotav1.NamespaceQuota) error {
	if runtime.Options.Output == "json" {
		return output.PrintNamespaceQuotaJSON(cmd.OutOrStdout(), value)
	}
	output.RenderNamespaceQuota(cmd.OutOrStdout(), value)
	return nil
}
