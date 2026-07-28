package function

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	appfunction "github.com/cofy-x/axern/apps/cli/internal/application/function"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/durationpb"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "function", Aliases: []string{"fn"}, Short: "Manage functions"}
	root.AddCommand(deployCommand(runtime), invokeCommand(runtime), getCommand(runtime), listCommand(runtime), deleteCommand(runtime), invocationCommand(runtime))
	return root
}

func getCommand(runtime command.Runtime) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{Use: "get <id-or-name>", Short: "Get a function", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		id, name := idOrName(args[0], namespace, cmd.Flags().Changed("namespace"))
		resp, err := appfunction.New(s.Clients.Function).Get(s.Context, id, namespace, name)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintFunctionGetResponseJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderFunction(cmd.OutOrStdout(), resp.GetFunction(), resp.GetDeployment())
		return nil
	}}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace for name lookup")
	return cmd
}

func deleteCommand(runtime command.Runtime) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{Use: "delete <id-or-name>", Short: "Delete a function", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		id, name := idOrName(args[0], namespace, cmd.Flags().Changed("namespace"))
		resp, err := appfunction.New(s.Clients.Function).Delete(s.Context, id, namespace, name)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintJSON(cmd.OutOrStdout(), output.NewFunctionJSON(resp.GetFunction()))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Function deleted: %s\n", resp.GetFunction().GetID())
		return nil
	}}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace for name lookup")
	return cmd
}

func listCommand(runtime command.Runtime) *cobra.Command {
	var namespace, cursor string
	var labels []string
	var pageSize int32
	var showLabels bool
	cmd := &cobra.Command{Use: "list", Short: "List functions", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appfunction.New(s.Clients.Function).List(s.Context, &functionv1.ListFunctionsRequest{Filter: &functionv1.FunctionListFilter{Namespace: namespace, Labels: parse.Labels(labels), Cursor: cursor, PageSize: pageSize}})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintFunctionListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderFunctionTable(cmd.OutOrStdout(), resp.GetFunctions(), output.FunctionListTableOptions{ShowLabels: showLabels})
		return nil
	}}
	f := cmd.Flags()
	f.StringVarP(&namespace, "namespace", "n", "", "namespace filter")
	f.StringArrayVarP(&labels, "selector", "l", nil, "label selector; may be repeated")
	f.StringVar(&cursor, "cursor", "", "pagination cursor")
	f.Int32Var(&pageSize, "page-size", 0, "page size")
	f.BoolVar(&showLabels, "show-labels", false, "show labels")
	return cmd
}

func invokeCommand(runtime command.Runtime) *cobra.Command {
	var namespace, data, payloadFile, requestID string
	var timeout int64
	var async bool
	cmd := &cobra.Command{Use: "invoke <name>", Short: "Invoke a function", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if data != "" && payloadFile != "" {
			return command.Usage(fmt.Errorf("data and payload-file cannot be combined"))
		}
		if timeout <= 0 {
			return command.Usage(fmt.Errorf("invocation-timeout must be greater than 0"))
		}
		payload := []byte(data)
		if payloadFile != "" {
			var err error
			payload, err = os.ReadFile(payloadFile)
			if err != nil {
				return command.Usage(err)
			}
		}
		if len(payload) != 0 && !json.Valid(payload) {
			return command.Usage(fmt.Errorf("function payload must be valid JSON"))
		}
		mode := functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_SYNC
		if async {
			mode = functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC
		}
		req := &functionv1.InvokeFunctionRequest{Namespace: namespace, Name: args[0], Mode: mode, Timeout: durationpb.New(time.Duration(timeout) * time.Second), RequestID: requestID}
		if len(payload) != 0 {
			req.Payload = &functionv1.FunctionPayload{ContentType: "application/json", Data: payload}
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appfunction.New(s.Clients.Function).Invoke(s.Context, req)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintFunctionInvokeResponseJSON(cmd.OutOrStdout(), resp.GetInvocation())
		}
		output.RenderFunctionInvocation(cmd.OutOrStdout(), resp.GetInvocation())
		return nil
	}}
	f := cmd.Flags()
	f.StringVarP(&namespace, "namespace", "n", "default", "namespace")
	f.StringVarP(&data, "data", "d", "", "JSON payload")
	f.StringVar(&payloadFile, "payload-file", "", "JSON payload file")
	f.Int64Var(&timeout, "invocation-timeout", 30, "invocation timeout in seconds")
	f.StringVar(&requestID, "request-id", "", "idempotency request id")
	f.BoolVar(&async, "async", false, "return before completion")
	return cmd
}

func invocationCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "invocation", Short: "Inspect function invocation history"}
	root.AddCommand(invocationGet(runtime), invocationList(runtime), invocationEvents(runtime))
	return root
}

func invocationGet(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "get <invocation-id>", Short: "Get an invocation", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appfunction.New(s.Clients.Function).GetInvocation(s.Context, args[0])
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintFunctionInvokeResponseJSON(cmd.OutOrStdout(), resp.GetInvocation())
		}
		output.RenderFunctionInvocation(cmd.OutOrStdout(), resp.GetInvocation())
		return nil
	}}
}

func invocationList(runtime command.Runtime) *cobra.Command {
	var namespace, cursor string
	var pageSize int32
	cmd := &cobra.Command{Use: "list <function-name>", Short: "List invocations", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appfunction.New(s.Clients.Function).ListInvocations(s.Context, &functionv1.ListFunctionInvocationsRequest{Filter: &functionv1.FunctionInvocationListFilter{Namespace: namespace, FunctionName: args[0], Cursor: cursor, PageSize: pageSize}})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintFunctionInvocationListJSON(cmd.OutOrStdout(), resp)
		}
		renderInvocations(cmd, resp.GetInvocations())
		return nil
	}}
	f := cmd.Flags()
	f.StringVarP(&namespace, "namespace", "n", "default", "namespace")
	f.StringVar(&cursor, "cursor", "", "pagination cursor")
	f.Int32Var(&pageSize, "page-size", 0, "page size")
	return cmd
}

func invocationEvents(runtime command.Runtime) *cobra.Command {
	var limit int32
	cmd := &cobra.Command{Use: "events <invocation-id>", Short: "List invocation events", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := s.Clients.Function.ListFunctionEvents(s.Context, &functionv1.ListFunctionEventsRequest{InvocationID: args[0], Limit: limit})
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintFunctionEventListJSON(cmd.OutOrStdout(), resp)
		}
		rows := make([][]string, 0, len(resp.GetEvents()))
		for _, event := range resp.GetEvents() {
			rows = append(rows, []string{event.GetID(), output.FunctionEventTypeLabel(event.GetType()), output.ShortMessage(event.GetMessage(), 80), output.FormatProtoTimestamp(event.GetCreatedAt())})
		}
		output.RenderTable(cmd.OutOrStdout(), []string{"ID", "TYPE", "MESSAGE", "CREATED"}, rows)
		return nil
	}}
	cmd.Flags().Int32Var(&limit, "limit", 50, "maximum events")
	return cmd
}

func renderInvocations(cmd *cobra.Command, values []*functionv1.FunctionInvocation) {
	rows := make([][]string, 0, len(values))
	for _, value := range values {
		duration := "-"
		if value.GetDuration() != nil {
			duration = value.GetDuration().AsDuration().String()
		}
		rows = append(rows, []string{value.GetID(), value.GetFunctionName(), output.FunctionInvocationStatusLabel(value.GetStatus()), duration, output.FormatProtoTimestamp(value.GetCreatedAt())})
	}
	output.RenderTable(cmd.OutOrStdout(), []string{"ID", "FUNCTION", "STATUS", "DURATION", "CREATED"}, rows)
}

func idOrName(value, namespace string, forceName bool) (string, string) {
	value = strings.TrimSpace(value)
	if !forceName && strings.HasPrefix(value, "fn-") {
		return value, ""
	}
	return "", value
}
