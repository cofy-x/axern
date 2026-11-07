package admin

import (
	"errors"
	"fmt"
	"strings"

	appadmin "github.com/cofy-x/axern/apps/cli/internal/application/admin"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/controlv1"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func nodeCapabilityCheckCommand(runtime command.Runtime) *cobra.Command {
	var platforms []string
	var bootID, observedAfter string
	check := &cobra.Command{
		Use: "check <node-id>", Short: "Check fresh capability admission evidence for an expected host boot",
		Args: command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
				return command.Usage(fmt.Errorf("node ID is required"))
			}
			options, err := appadmin.NewNodeCapabilityCheck(platforms, bootID, observedAfter)
			if err != nil {
				return command.Usage(err)
			}
			session, err := runtime.Open(cmd.Context())
			if err != nil {
				return renderNodeCapabilityCheck(cmd, runtime, appadmin.NodeCapabilityCheckResult{}, err)
			}
			defer session.Close()
			result, err := appadmin.NewNode(session.Clients.AdminNode).CheckCapabilities(session.Context, args[0], options)
			return renderNodeCapabilityCheck(cmd, runtime, result, err)
		},
	}
	check.Flags().StringArrayVar(&platforms, "platform", nil, "required platform capability uppercase name without PLATFORM_CAPABILITY_; repeat for each requirement")
	check.Flags().StringVar(&bootID, "boot-id", "", "expected host boot UUID for all required host evidence")
	check.Flags().StringVar(&observedAfter, "observed-after", "", "require every requested capability and dependency to be observed strictly after this RFC3339 timestamp (nanoseconds accepted)")
	return check
}

func renderNodeCapabilityCheck(cmd *cobra.Command, runtime command.Runtime, result appadmin.NodeCapabilityCheckResult, checkErr error) error {
	exitCode := 0
	if checkErr != nil {
		result = appadmin.NodeCapabilityCheckResult{Reason: "node readiness API request failed"}
		exitCode = 1
		var configuration controlv1.ConfigurationError
		var usage command.UsageError
		switch {
		case errors.As(checkErr, &configuration), errors.As(checkErr, &usage):
			result.Reason = "client connection configuration or certificate is invalid"
			exitCode = 2
		case status.Code(checkErr) == codes.Unauthenticated:
			result.Reason = "gateway rejected the client identity"
			exitCode = 2
		case status.Code(checkErr) == codes.PermissionDenied:
			result.Reason = "client is not authorized to inspect node readiness"
			exitCode = 2
		case status.Code(checkErr) == codes.NotFound:
			result.Reason = "node capability snapshot has not been published"
		case status.Code(checkErr) == codes.DeadlineExceeded:
			result.Reason = "node readiness API request timed out"
		}
	} else if !result.Ready {
		exitCode = 1
	}
	if runtime.Options.Output == "json" {
		if err := output.PrintNodeCapabilityCheckJSON(cmd.OutOrStdout(), result.Ready, result.Reason); err != nil {
			return err
		}
	} else {
		output.RenderNodeCapabilityCheck(cmd.OutOrStdout(), result.Ready, result.Reason)
	}
	if exitCode != 0 {
		return command.ExitError{Code: exitCode}
	}
	return nil
}
