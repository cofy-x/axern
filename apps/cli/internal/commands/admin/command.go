package admin

import (
	"fmt"

	appadmin "github.com/cofy-x/axern/apps/cli/internal/application/admin"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "admin", Short: "Operate audited administrative workflows"}
	root.AddCommand(principalCommand(runtime), credentialCommand(runtime), roleBindingCommand(runtime), nodeCommand(runtime), reliabilityCommand(runtime), consistencyCommand(runtime), auditCommand(runtime), allocationRetryCommand(runtime))
	return root
}

func nodeCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "node", Short: "Inspect and retire runtime nodes"}
	var lifecycle string
	list := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := appadmin.ValidateNodeLifecycle(lifecycle); err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewNode(s.Clients.AdminNode).List(s.Context, lifecycle)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintAdminNodeListJSON(cmd.OutOrStdout(), resp.GetNodes())
		}
		output.RenderAdminNodeTable(cmd.OutOrStdout(), resp.GetNodes())
		return nil
	}}
	list.Flags().StringVar(&lifecycle, "status", "", "active or retired")
	var reason string
	retire := &cobra.Command{Use: "retire <node-id>", Short: "Permanently retire an idle node identity", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := appadmin.ValidateOperatorReason(reason); err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewNode(s.Clients.AdminNode).Retire(s.Context, args[0], reason)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintAdminNodeJSON(cmd.OutOrStdout(), resp.GetNode())
		}
		output.RenderAdminNode(cmd.OutOrStdout(), resp.GetNode())
		return nil
	}}
	retire.Flags().StringVar(&reason, "operator-reason", "", "audit reason")
	root.AddCommand(list, retire, nodeCapabilityCommand(runtime))
	return root
}

func nodeCapabilityCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "capability", Short: "Inspect observed capability evidence and allocation enforcement"}
	snapshot := &cobra.Command{Use: "snapshot <node-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewNode(s.Clients.AdminNode).CapabilitySnapshot(s.Context, args[0])
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintProtoJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderCapabilitySnapshot(cmd.OutOrStdout(), resp.GetSnapshot())
		return nil
	}}

	var transitionNodeID string
	var transitionLimit int
	transitions := &cobra.Command{Use: "transitions", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if transitionLimit < 0 {
			return command.Usage(fmt.Errorf("limit must be >= 0"))
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewNode(s.Clients.AdminNode).CapabilityTransitions(s.Context, transitionNodeID, transitionLimit)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintProtoJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderCapabilityTransitions(cmd.OutOrStdout(), resp.GetTransitions())
		return nil
	}}
	transitions.Flags().StringVar(&transitionNodeID, "node-id", "", "node identity filter")
	transitions.Flags().IntVar(&transitionLimit, "limit", 0, "maximum transitions")

	var backlogNodeID string
	var backlogLimit int
	backlog := &cobra.Command{Use: "backlog", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if backlogLimit < 0 {
			return command.Usage(fmt.Errorf("limit must be >= 0"))
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewNode(s.Clients.AdminNode).CapabilityBacklog(s.Context, backlogNodeID, backlogLimit)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintProtoJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderCapabilityBacklog(cmd.OutOrStdout(), resp.GetItems())
		return nil
	}}
	backlog.Flags().StringVar(&backlogNodeID, "node-id", "", "node identity filter")
	backlog.Flags().IntVar(&backlogLimit, "limit", 0, "maximum reconcile items")

	allocation := &cobra.Command{Use: "allocation <allocation-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewNode(s.Clients.AdminNode).AllocationCapability(s.Context, args[0])
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintProtoJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderAllocationCapabilityDiagnostics(cmd.OutOrStdout(), resp)
		return nil
	}}
	root.AddCommand(snapshot, transitions, backlog, allocation)
	return root
}

func reliabilityCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "reliability", Short: "Inspect reliability health"}
	root.AddCommand(&cobra.Command{Use: "check", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewReliability(s.Clients.AdminReliability).Health(s.Context)
		if err != nil {
			return err
		}
		health := resp.GetHealth()
		if runtime.Options.Output == "json" {
			if err := output.PrintAdminReliabilityHealthJSON(cmd.OutOrStdout(), health); err != nil {
				return err
			}
		} else {
			output.RenderAdminReliabilityHealth(cmd.OutOrStdout(), health)
		}
		if health.GetStatus() != adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_OK {
			return fmt.Errorf("control-plane reliability is %s", health.GetStatus())
		}
		return nil
	}})
	return root
}

func consistencyCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "consistency", Short: "Inspect state consistency"}
	root.AddCommand(&cobra.Command{Use: "check", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewReliability(s.Clients.AdminReliability).CheckConsistency(s.Context)
		if err != nil {
			return err
		}
		value := resp.GetSnapshot()
		if runtime.Options.Output == "json" {
			if err := output.PrintConsistencySnapshotJSON(cmd.OutOrStdout(), value); err != nil {
				return err
			}
		} else {
			output.RenderConsistencySnapshot(cmd.OutOrStdout(), value)
		}
		if value.GetStatus() != adminv1.ConsistencyStatus_CONSISTENCY_STATUS_OK {
			return fmt.Errorf("control-plane consistency check failed")
		}
		return nil
	}})
	return root
}

func auditCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "audit", Short: "Inspect admin audit events"}
	var operation, targetType, targetID string
	var limit int
	list := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := appadmin.ValidateAuditOperation(operation); err != nil {
			return command.Usage(err)
		}
		if err := appadmin.ValidateAuditTargetType(targetType); err != nil {
			return command.Usage(err)
		}
		if err := appadmin.ValidateAuditTargetFilter(targetType, targetID); err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewAudit(s.Clients.AdminAudit).ListEvents(s.Context, appadmin.AuditListOptions{Operation: operation, TargetType: targetType, TargetID: targetID, Limit: limit})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintAdminAuditEventListJSON(cmd.OutOrStdout(), resp.GetEvents())
		}
		output.RenderAdminAuditEventTable(cmd.OutOrStdout(), resp.GetEvents())
		return nil
	}}
	f := list.Flags()
	f.StringVar(&operation, "operation", "", "operation filter")
	f.StringVar(&targetType, "target-type", "", "target type filter")
	f.StringVar(&targetID, "target-id", "", "target id filter")
	f.IntVar(&limit, "limit", 0, "maximum events")
	root.AddCommand(list)
	return root
}

func allocationRetryCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "allocation-retry", Short: "Inspect and repair lifecycle retries"}
	var reason string
	var due bool
	var limit int
	list := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if reason != "" {
			if err := appadmin.ValidateRetryReason(reason); err != nil {
				return command.Usage(err)
			}
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appadmin.NewAllocationLifecycle(s.Clients.Admin).ListRetries(s.Context, appadmin.LifecycleRetryListOptions{Reason: reason, DueOnly: due, Limit: limit})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintAllocationLifecycleRetryListJSON(cmd.OutOrStdout(), resp.GetRetries())
		}
		output.RenderAllocationLifecycleRetryTable(cmd.OutOrStdout(), resp.GetRetries())
		return nil
	}}
	f := list.Flags()
	f.StringVar(&reason, "reason", "", "create or delete")
	f.BoolVar(&due, "due", false, "only due retries")
	f.IntVar(&limit, "limit", 0, "maximum rows")
	root.AddCommand(list, retryWrite(runtime, "force"), retryWrite(runtime, "fail"), retryWrite(runtime, "clear"))
	return root
}

func retryWrite(runtime command.Runtime, operation string) *cobra.Command {
	var reason, operatorReason string
	cmd := &cobra.Command{Use: operation + " <allocation-id>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if operation != "fail" {
			if err := appadmin.ValidateRetryReason(reason); err != nil {
				return command.Usage(err)
			}
		}
		if err := appadmin.ValidateOperatorReason(operatorReason); err != nil {
			return command.Usage(err)
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		control := appadmin.NewAllocationLifecycle(s.Clients.Admin)
		var value *adminv1.AllocationLifecycleRetry
		switch operation {
		case "force":
			resp, err := control.ForceRetry(s.Context, args[0], reason, operatorReason)
			if err != nil {
				return err
			}
			value = resp.GetRetry()
		case "fail":
			resp, err := control.FailCreateRetry(s.Context, args[0], operatorReason)
			if err != nil {
				return err
			}
			value = resp.GetFailedRetry()
		case "clear":
			resp, err := control.ClearRetry(s.Context, args[0], reason, operatorReason)
			if err != nil {
				return err
			}
			value = resp.GetClearedRetry()
		}
		if runtime.Options.Output == "json" {
			return output.PrintAllocationLifecycleRetryJSON(cmd.OutOrStdout(), value)
		}
		output.RenderAllocationLifecycleRetry(cmd.OutOrStdout(), value)
		return nil
	}}
	if operation != "fail" {
		cmd.Flags().StringVar(&reason, "reason", "", "create or delete")
	}
	cmd.Flags().StringVar(&operatorReason, "operator-reason", "", "audit reason")
	return cmd
}
