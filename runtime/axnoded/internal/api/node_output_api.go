package api

import (
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (s *nodeSandboxServer) ReadOutput(req *nodesandboxv1.ReadOutputRequest, stream nodesandboxv1.NodeSandbox_ReadOutputServer) error {
	target, err := s.validateDirectAuth(stream.Context(), req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return err
	}
	if err := acknowledgeExecutionLease(stream); err != nil {
		return err
	}
	reader := allocationoutput.New(s.svc)
	cursor := req.GetCursor()
	for {
		chunks, complete, err := reader.Read(stream.Context(), target.targetID, cursor)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			response := &nodesandboxv1.ReadOutputResponse{Data: chunk.Data, NextCursor: chunk.Cursor, Terminal: chunk.Terminal, Truncated: chunk.Truncated, ObservedAtUnixMilli: time.Now().UnixMilli()}
			switch chunk.Stream {
			case "stdout":
				response.Stream = nodesandboxv1.OutputStream_OUTPUT_STREAM_STDOUT
			case "stderr":
				response.Stream = nodesandboxv1.OutputStream_OUTPUT_STREAM_STDERR
			}
			if err := stream.Send(response); err != nil {
				return err
			}
			cursor = chunk.Cursor
		}
		if complete || !req.GetFollow() {
			return nil
		}
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
