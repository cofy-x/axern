package output

import (
	"io"

	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
)

type FunctionJSON struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Namespace        string            `json:"namespace"`
	ActiveRevisionID string            `json:"active_revision_id,omitempty"`
	Status           string            `json:"status"`
	DeploymentStatus string            `json:"deployment_status"`
	Labels           map[string]string `json:"labels,omitempty"`
	Version          int64             `json:"version"`
	CreatedAt        string            `json:"created_at,omitempty"`
	UpdatedAt        string            `json:"updated_at,omitempty"`
	Message          string            `json:"message,omitempty"`
}

type FunctionDeployResponseJSON struct {
	Function   *FunctionJSON         `json:"function"`
	Revision   *FunctionRevisionJSON `json:"revision,omitempty"`
	Deployment *FunctionDeployJSON   `json:"deployment,omitempty"`
}

type FunctionRevisionJSON struct {
	ID             string `json:"id"`
	FunctionID     string `json:"function_id"`
	RevisionNumber int64  `json:"revision_number"`
	SourceDigest   string `json:"source_digest,omitempty"`
	ManifestDigest string `json:"manifest_digest,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

type FunctionDeployJSON struct {
	FunctionID       string `json:"function_id"`
	ActiveRevisionID string `json:"active_revision_id"`
	Status           string `json:"status"`
	DesiredReplicas  int32  `json:"desired_replicas"`
	ReadyReplicas    int32  `json:"ready_replicas"`
}

type FunctionGetResponseJSON struct {
	Function   *FunctionJSON         `json:"function"`
	Revision   *FunctionRevisionJSON `json:"active_revision,omitempty"`
	Deployment *FunctionDeployJSON   `json:"deployment,omitempty"`
}

type FunctionListJSON struct {
	Functions  []*FunctionJSON `json:"functions"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type FunctionInvocationJSON struct {
	ID           string              `json:"id"`
	FunctionID   string              `json:"function_id"`
	FunctionName string              `json:"function_name"`
	Namespace    string              `json:"namespace"`
	RevisionID   string              `json:"revision_id,omitempty"`
	Status       string              `json:"status"`
	RequestID    string              `json:"request_id,omitempty"`
	Result       *FunctionResultJSON `json:"result,omitempty"`
	Error        *FunctionErrorJSON  `json:"error,omitempty"`
	Duration     string              `json:"duration,omitempty"`
	CreatedAt    string              `json:"created_at,omitempty"`
	CompletedAt  string              `json:"completed_at,omitempty"`
}

type FunctionResultJSON struct {
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
}

type FunctionErrorJSON struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Type       string `json:"type,omitempty"`
	StackTrace string `json:"stack_trace,omitempty"`
}

type FunctionEventJSON struct {
	ID           string            `json:"id"`
	FunctionID   string            `json:"function_id"`
	InvocationID string            `json:"invocation_id,omitempty"`
	RevisionID   string            `json:"revision_id,omitempty"`
	Type         string            `json:"type"`
	Message      string            `json:"message"`
	Details      map[string]string `json:"details,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
}

func NewFunctionJSON(fn *functionv1.Function) *FunctionJSON {
	if fn == nil {
		return nil
	}
	return &FunctionJSON{
		ID:               fn.GetID(),
		Name:             fn.GetName(),
		Namespace:        fn.GetNamespace(),
		ActiveRevisionID: fn.GetActiveRevisionID(),
		Status:           FunctionStatusLabel(fn.GetStatus()),
		DeploymentStatus: FunctionDeploymentStatusLabel(fn.GetDeploymentStatus()),
		Labels:           cloneStringMap(fn.GetLabels()),
		Version:          fn.GetVersion(),
		CreatedAt:        FormatProtoTimestamp(fn.GetCreatedAt()),
		UpdatedAt:        FormatProtoTimestamp(fn.GetUpdatedAt()),
		Message:          fn.GetMessage(),
	}
}

func newFunctionRevisionJSON(rev *functionv1.FunctionRevision) *FunctionRevisionJSON {
	if rev == nil {
		return nil
	}
	return &FunctionRevisionJSON{
		ID:             rev.GetID(),
		FunctionID:     rev.GetFunctionID(),
		RevisionNumber: rev.GetRevisionNumber(),
		SourceDigest:   rev.GetSourceDigest(),
		ManifestDigest: rev.GetManifestDigest(),
		CreatedAt:      FormatProtoTimestamp(rev.GetCreatedAt()),
	}
}

func newFunctionDeployJSON(dep *functionv1.FunctionDeployment) *FunctionDeployJSON {
	if dep == nil {
		return nil
	}
	return &FunctionDeployJSON{
		FunctionID:       dep.GetFunctionID(),
		ActiveRevisionID: dep.GetActiveRevisionID(),
		Status:           FunctionDeploymentStatusLabel(dep.GetStatus()),
		DesiredReplicas:  dep.GetDesiredReplicas(),
		ReadyReplicas:    dep.GetReadyReplicas(),
	}
}

func NewFunctionInvocationJSON(inv *functionv1.FunctionInvocation) *FunctionInvocationJSON {
	if inv == nil {
		return nil
	}
	out := &FunctionInvocationJSON{
		ID:           inv.GetID(),
		FunctionID:   inv.GetFunctionID(),
		FunctionName: inv.GetFunctionName(),
		Namespace:    inv.GetNamespace(),
		RevisionID:   inv.GetRevisionID(),
		Status:       FunctionInvocationStatusLabel(inv.GetStatus()),
		RequestID:    inv.GetRequestID(),
		CreatedAt:    FormatProtoTimestamp(inv.GetCreatedAt()),
		CompletedAt:  FormatProtoTimestamp(inv.GetCompletedAt()),
	}
	if inv.GetDuration() != nil {
		out.Duration = inv.GetDuration().AsDuration().String()
	}
	if r := inv.GetResult(); r != nil && len(r.GetData()) > 0 {
		out.Result = &FunctionResultJSON{
			ContentType: r.GetContentType(),
			Data:        string(r.GetData()),
		}
	}
	if e := inv.GetError(); e != nil && (e.GetCode() != "" || e.GetMessage() != "") {
		out.Error = &FunctionErrorJSON{
			Code:       e.GetCode(),
			Message:    e.GetMessage(),
			Type:       e.GetType(),
			StackTrace: e.GetStackTrace(),
		}
	}
	return out
}

func NewFunctionEventJSON(ev *functionv1.FunctionEvent) *FunctionEventJSON {
	if ev == nil {
		return nil
	}
	return &FunctionEventJSON{
		ID:           ev.GetID(),
		FunctionID:   ev.GetFunctionID(),
		InvocationID: ev.GetInvocationID(),
		RevisionID:   ev.GetRevisionID(),
		Type:         FunctionEventTypeLabel(ev.GetType()),
		Message:      ev.GetMessage(),
		Details:      cloneStringMap(ev.GetDetails()),
		CreatedAt:    FormatProtoTimestamp(ev.GetCreatedAt()),
	}
}

func PrintFunctionDeployResponseJSON(w io.Writer, resp *functionv1.DeployFunctionResponse) error {
	return PrintJSON(w, FunctionDeployResponseJSON{
		Function:   NewFunctionJSON(resp.GetFunction()),
		Revision:   newFunctionRevisionJSON(resp.GetRevision()),
		Deployment: newFunctionDeployJSON(resp.GetDeployment()),
	})
}

func PrintFunctionGetResponseJSON(w io.Writer, resp *functionv1.GetFunctionResponse) error {
	return PrintJSON(w, FunctionGetResponseJSON{
		Function:   NewFunctionJSON(resp.GetFunction()),
		Revision:   newFunctionRevisionJSON(resp.GetActiveRevision()),
		Deployment: newFunctionDeployJSON(resp.GetDeployment()),
	})
}

func PrintFunctionListJSON(w io.Writer, resp *functionv1.ListFunctionsResponse) error {
	out := FunctionListJSON{}
	if resp != nil {
		out.NextCursor = resp.GetNextCursor()
		out.Functions = make([]*FunctionJSON, 0, len(resp.GetFunctions()))
		for _, fn := range resp.GetFunctions() {
			out.Functions = append(out.Functions, NewFunctionJSON(fn))
		}
	}
	return PrintJSON(w, out)
}

func PrintFunctionInvokeResponseJSON(w io.Writer, inv *functionv1.FunctionInvocation) error {
	return PrintJSON(w, NewFunctionInvocationJSON(inv))
}

func PrintFunctionInvocationListJSON(w io.Writer, resp *functionv1.ListFunctionInvocationsResponse) error {
	out := struct {
		Invocations []*FunctionInvocationJSON `json:"invocations"`
		NextCursor  string                    `json:"next_cursor,omitempty"`
	}{}
	if resp != nil {
		out.NextCursor = resp.GetNextCursor()
		out.Invocations = make([]*FunctionInvocationJSON, 0, len(resp.GetInvocations()))
		for _, inv := range resp.GetInvocations() {
			out.Invocations = append(out.Invocations, NewFunctionInvocationJSON(inv))
		}
	}
	return PrintJSON(w, out)
}

func PrintFunctionEventListJSON(w io.Writer, resp *functionv1.ListFunctionEventsResponse) error {
	out := struct {
		Events []*FunctionEventJSON `json:"events"`
	}{}
	if resp != nil {
		out.Events = make([]*FunctionEventJSON, 0, len(resp.GetEvents()))
		for _, ev := range resp.GetEvents() {
			out.Events = append(out.Events, NewFunctionEventJSON(ev))
		}
	}
	return PrintJSON(w, out)
}
