package api

import (
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (s *nodeSandboxServer) ReadOutput(req *nodesandboxv1.ReadOutputRequest, stream nodesandboxv1.NodeSandbox_ReadOutputServer) error {
	target, err := s.validateAccessPurpose(stream.Context(), req.GetAllocationID(), gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT)
	if err != nil {
		return err
	}
	if err := acknowledgeAllocationAccessGrant(stream); err != nil {
		return err
	}
	read := s.svc.ReadAllocationOutput
	if s.localOnly {
		read = allocationoutput.New(s.svc).Read
	}
	cursor := req.GetCursor()
	for {
		chunks, complete, err := read(stream.Context(), target.targetID, cursor)
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
		if complete || (!req.GetFollow() && len(chunks) == 0) {
			return nil
		}
		if len(chunks) > 0 {
			continue
		}
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
