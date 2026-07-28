package output

import (
	"fmt"
	"io"
	"time"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
)

func FunctionStatusLabel(status functionv1.FunctionStatus) string {
	return trimEnumPrefix(status.String(), "FUNCTION_STATUS_")
}

func FunctionDeploymentStatusLabel(status functionv1.FunctionDeploymentStatus) string {
	return trimEnumPrefix(status.String(), "FUNCTION_DEPLOYMENT_STATUS_")
}

func FunctionInvocationStatusLabel(status functionv1.FunctionInvocationStatus) string {
	return trimEnumPrefix(status.String(), "FUNCTION_INVOCATION_STATUS_")
}

func FunctionEventTypeLabel(eventType functionv1.FunctionEventType) string {
	return trimEnumPrefix(eventType.String(), "FUNCTION_EVENT_TYPE_")
}

func RenderFunction(w io.Writer, fn *functionv1.Function, deployment *functionv1.FunctionDeployment) {
	if fn == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", fn.GetID())
	fmt.Fprintf(w, "Name: %s\n", fn.GetName())
	fmt.Fprintf(w, "Namespace: %s\n", fn.GetNamespace())
	fmt.Fprintf(w, "Status: %s\n", FunctionStatusLabel(fn.GetStatus()))
	if fn.GetActiveRevisionID() != "" {
		fmt.Fprintf(w, "Active Revision: %s\n", fn.GetActiveRevisionID())
	}
	if deployment != nil {
		fmt.Fprintf(w, "Deployment: status=%s desired=%d ready=%d\n",
			FunctionDeploymentStatusLabel(deployment.GetStatus()),
			deployment.GetDesiredReplicas(),
			deployment.GetReadyReplicas())
	}
	if labels := fn.GetLabels(); len(labels) > 0 {
		fmt.Fprintf(w, "Labels: %s\n", formatLabels(labels))
	}
	if created := FormatProtoTimestamp(fn.GetCreatedAt()); created != "" {
		fmt.Fprintf(w, "Created At: %s\n", created)
	}
	if message := fn.GetMessage(); message != "" {
		fmt.Fprintf(w, "Message: %s\n", message)
	}
}

func RenderFunctionInvocation(w io.Writer, inv *functionv1.FunctionInvocation) {
	if inv == nil {
		return
	}
	fmt.Fprintf(w, "Invocation ID: %s\n", inv.GetID())
	fmt.Fprintf(w, "Function: %s (%s)\n", inv.GetFunctionName(), inv.GetFunctionID())
	fmt.Fprintf(w, "Namespace: %s\n", inv.GetNamespace())
	fmt.Fprintf(w, "Status: %s\n", FunctionInvocationStatusLabel(inv.GetStatus()))
	if inv.GetRevisionID() != "" {
		fmt.Fprintf(w, "Revision: %s\n", inv.GetRevisionID())
	}
	if inv.GetRequestID() != "" {
		fmt.Fprintf(w, "Request ID: %s\n", inv.GetRequestID())
	}
	if inv.GetResult() != nil && len(inv.GetResult().GetData()) > 0 {
		fmt.Fprintf(w, "Result: %s\n", string(inv.GetResult().GetData()))
	}
	if inv.GetError() != nil && inv.GetError().GetMessage() != "" {
		fmt.Fprintf(w, "Error: %s\n", inv.GetError().GetMessage())
	}
	if inv.GetDuration() != nil {
		fmt.Fprintf(w, "Duration: %s\n", inv.GetDuration().AsDuration().String())
	}
}

type FunctionListTableOptions struct {
	Wide       bool
	ShowLabels bool
}

func RenderFunctionTable(w io.Writer, functions []*functionv1.Function, opts FunctionListTableOptions) {
	headers := []string{"ID", "NAME", "NAMESPACE", "STATUS", "DEPLOYMENT"}
	if opts.Wide {
		headers = append(headers, "REVISION")
	}
	headers = append(headers, "AGE")
	if opts.ShowLabels {
		headers = append(headers, "LABELS")
	}
	rows := make([][]string, 0, len(functions))
	for _, fn := range functions {
		if fn == nil {
			continue
		}
		values := []string{
			fn.GetID(),
			fn.GetName(),
			firstNonEmpty(fn.GetNamespace(), "-"),
			FunctionStatusLabel(fn.GetStatus()),
			FunctionDeploymentStatusLabel(fn.GetDeploymentStatus()),
		}
		if opts.Wide {
			values = append(values, firstNonEmpty(fn.GetActiveRevisionID(), "-"))
		}
		values = append(values, formatFunctionAge(fn))
		if opts.ShowLabels {
			values = append(values, formatLabels(fn.GetLabels()))
		}
		rows = append(rows, values)
	}
	RenderTable(w, headers, rows)
}

func formatFunctionAge(fn *functionv1.Function) string {
	if fn == nil || fn.GetCreatedAt() == nil {
		return "-"
	}
	return FormatRelativeAge(fn.GetCreatedAt().AsTime(), time.Now().UTC())
}
