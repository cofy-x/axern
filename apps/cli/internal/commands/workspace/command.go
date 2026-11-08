package workspace

import (
	"context"
	"fmt"
	"strings"
	"time"

	appworkspace "github.com/cofy-x/axern/apps/cli/internal/application/workspace"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "workspace", Short: "Manage retained single-writer working data"}
	root.AddCommand(createCommand(runtime), oneCommand(runtime, "get"), listCommand(runtime), oneCommand(runtime, "delete"))
	return root
}

func createCommand(runtime command.Runtime) *cobra.Command {
	params := appworkspace.CreateParams{Namespace: "default"}
	var capacity string
	var labels []string
	var wait bool
	var waitTimeout time.Duration
	cmd := &cobra.Command{Use: "create", Short: "Provision a Workspace on one Linux node", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var err error
		params.CapacityBytes, err = parse.Memory(capacity)
		if err != nil || params.CapacityBytes <= 0 || params.CapacityBytes%512 != 0 {
			return command.Usage(fmt.Errorf("--capacity must specify positive, 512-byte-aligned storage bytes"))
		}
		if strings.TrimSpace(params.Namespace) == "" || strings.TrimSpace(params.NodeID) == "" || params.InodeLimit <= 0 {
			return command.Usage(fmt.Errorf("--namespace, --node and positive --inodes are required"))
		}
		if waitTimeout <= 0 {
			return command.Usage(fmt.Errorf("--wait-timeout must be positive"))
		}
		params.Labels = parse.Labels(labels)
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		control := appworkspace.New(s.Clients.Workspace)
		resp, err := control.Create(s.Context, params)
		if err != nil {
			return err
		}
		value := resp.GetWorkspace()
		if wait {
			if value.GetID() == "" {
				return fmt.Errorf("created Workspace response has no identity")
			}
			ctx, cancel := context.WithTimeout(s.Context, waitTimeout)
			defer cancel()
			value, err = control.WaitReady(ctx, value.GetID())
			if err != nil {
				return err
			}
		}
		return render(runtime, cmd, value)
	}}
	cmd.Flags().StringVar(&params.Namespace, "namespace", params.Namespace, "namespace")
	cmd.Flags().StringVar(&params.NodeID, "node", "", "bound Linux node ID")
	cmd.Flags().StringVar(&capacity, "capacity", "", "persistent hard storage limit, for example 1Gi")
	cmd.Flags().Int64Var(&params.InodeLimit, "inodes", 0, "persistent hard inode limit")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label key=value; may be repeated")
	cmd.Flags().BoolVar(&wait, "wait", true, "wait for the managed data to become ready")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 2*time.Minute, "Workspace readiness timeout; bounded by --timeout")
	return cmd
}

func oneCommand(runtime command.Runtime, operation string) *cobra.Command {
	return &cobra.Command{Use: operation + " <workspace-id>", Short: operation + " a Workspace", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		control := appworkspace.New(s.Clients.Workspace)
		var value *workspacev1.Workspace
		if operation == "get" {
			resp, err := control.Get(s.Context, args[0])
			if err != nil {
				return err
			}
			value = resp.GetWorkspace()
		} else {
			resp, err := control.Delete(s.Context, args[0])
			if err != nil {
				return err
			}
			value = resp.GetWorkspace()
		}
		return render(runtime, cmd, value)
	}}
}

func listCommand(runtime command.Runtime) *cobra.Command {
	var namespace, cursor string
	var pageSize int32
	cmd := &cobra.Command{Use: "list", Short: "List Workspaces and retained capacity", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if pageSize < 0 {
			return command.Usage(fmt.Errorf("--page-size must be nonnegative"))
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appworkspace.New(s.Clients.Workspace).List(s.Context, namespace, cursor, pageSize)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintWorkspaceListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderWorkspaceTable(cmd.OutOrStdout(), resp.GetWorkspaces())
		return nil
	}}
	cmd.Flags().StringVar(&namespace, "namespace", "default", "namespace")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	cmd.Flags().Int32Var(&pageSize, "page-size", 100, "maximum number of Workspaces")
	return cmd
}

func render(runtime command.Runtime, cmd *cobra.Command, value *workspacev1.Workspace) error {
	if runtime.Options.Output == "json" {
		return output.PrintWorkspaceResponseJSON(cmd.OutOrStdout(), value)
	}
	output.RenderWorkspace(cmd.OutOrStdout(), value)
	return nil
}
