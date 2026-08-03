package axernsdk

import (
	"context"
	"errors"
	"fmt"
	"io"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type RunOutputOptions struct {
	Cursor string
	Follow bool
}

type RunOutputEvent struct {
	Stream              string
	Data                []byte
	NextCursor          string
	Terminal            bool
	Truncated           bool
	ObservedAtUnixMilli int64
}

type RunOutput struct {
	stream nodesandboxv1.NodeSandbox_ReadOutputClient
}

func (c *Client) ReadRunOutput(ctx context.Context, runID string, options RunOutputOptions) (*RunOutput, error) {
	if runID == "" {
		return nil, requiredError("run_id")
	}
	response, err := c.runs.GetRun(ctx, &runv1.GetRunRequest{RunID: runID})
	if err != nil {
		return nil, mapRPCError(err, "get run output", runID)
	}
	allocationID := response.GetRun().GetAllocationID()
	if allocationID == "" {
		return nil, fmt.Errorf("run %s output is not available yet", runID)
	}
	stream, err := c.nodes.ReadOutput(ctx, &nodesandboxv1.ReadOutputRequest{AllocationID: allocationID, Cursor: options.Cursor, Follow: options.Follow})
	if err != nil {
		return nil, mapRPCError(err, "read run output", "")
	}
	return &RunOutput{stream: stream}, nil
}

type RunWatch struct {
	stream runv1.RunControl_WatchRunClient
}

func (c *Client) WatchRun(ctx context.Context, runID string, afterVersion int64) (*RunWatch, error) {
	if runID == "" {
		return nil, requiredError("run_id")
	}
	if afterVersion < 0 {
		return nil, fmt.Errorf("after_version must be non-negative")
	}
	stream, err := c.runs.WatchRun(ctx, &runv1.WatchRunRequest{RunID: runID, AfterVersion: afterVersion})
	if err != nil {
		return nil, mapRPCError(err, "watch run", runID)
	}
	return &RunWatch{stream: stream}, nil
}

func (s *RunWatch) Recv() (*runv1.Run, error) {
	if s == nil || s.stream == nil {
		return nil, io.EOF
	}
	response, err := s.stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, mapRPCError(err, "watch run", "")
	}
	return response.GetRun(), nil
}

func (s *RunOutput) Recv() (RunOutputEvent, error) {
	if s == nil || s.stream == nil {
		return RunOutputEvent{}, io.EOF
	}
	response, err := s.stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return RunOutputEvent{}, io.EOF
		}
		return RunOutputEvent{}, mapRPCError(err, "read run output", "")
	}
	stream := ""
	switch response.GetStream() {
	case nodesandboxv1.OutputStream_OUTPUT_STREAM_STDOUT:
		stream = "stdout"
	case nodesandboxv1.OutputStream_OUTPUT_STREAM_STDERR:
		stream = "stderr"
	}
	return RunOutputEvent{Stream: stream, Data: append([]byte(nil), response.GetData()...), NextCursor: response.GetNextCursor(), Terminal: response.GetTerminal(), Truncated: response.GetTruncated(), ObservedAtUnixMilli: response.GetObservedAtUnixMilli()}, nil
}
