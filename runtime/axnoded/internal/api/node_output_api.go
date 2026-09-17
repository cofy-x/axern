package api

import (
	"context"
	"time"

	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const sealedOutputDownloadChunkBytes int64 = 32 << 10

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

func (s *nodeSandboxServer) GetSealedOutputManifest(ctx context.Context, req *nodesandboxv1.GetSealedOutputManifestRequest) (*nodesandboxv1.GetSealedOutputManifestResponse, error) {
	target, err := s.validateAccessPurpose(ctx, req.GetAllocationID(), gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT)
	if err != nil {
		return nil, err
	}
	manifest, err := s.svc.SealedOutputManifest(ctx, target.targetID)
	if err != nil {
		return nil, err
	}
	response := &nodesandboxv1.GetSealedOutputManifestResponse{
		ExpiresAt: timestamppb.New(manifest.ExpiresAt),
		Outputs:   make([]*nodesandboxv1.SealedOutput, 0, len(manifest.Entries)),
	}
	for _, entry := range manifest.Entries {
		output := &nodesandboxv1.SealedOutput{
			OutputID:  entry.OutputID,
			Path:      entry.Path,
			SizeBytes: entry.SizeBytes,
			Sha256:    entry.SHA256,
			MediaType: entry.MediaType,
			Reason:    entry.Reason,
			SealedAt:  timestamppb.New(entry.SealedAt),
			ExpiresAt: timestamppb.New(manifest.ExpiresAt),
		}
		switch entry.Format {
		case "file":
			output.Format = commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE
		case "tar":
			output.Format = commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR
		}
		switch entry.Status {
		case "available":
			output.Status = nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_AVAILABLE
		case "missing":
			output.Status = nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_MISSING
		case "rejected":
			output.Status = nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_REJECTED
		case "capture_failed":
			output.Status = nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_CAPTURE_FAILED
		case "node_unavailable":
			output.Status = nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_NODE_UNAVAILABLE
		}
		response.Outputs = append(response.Outputs, output)
	}
	return response, nil
}

func (s *nodeSandboxServer) DownloadSealedOutput(req *nodesandboxv1.DownloadSealedOutputRequest, stream nodesandboxv1.NodeSandbox_DownloadSealedOutputServer) error {
	target, err := s.validateAccessPurpose(stream.Context(), req.GetAllocationID(), gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT)
	if err != nil {
		return err
	}
	if err := acknowledgeAllocationAccessGrant(stream); err != nil {
		return err
	}
	offset := req.GetOffset()
	for {
		data, next, eof, err := s.svc.ReadSealedOutput(stream.Context(), target.targetID, req.GetOutputID(), offset, sealedOutputDownloadChunkBytes)
		if err != nil {
			return err
		}
		if err := stream.Send(&nodesandboxv1.DownloadSealedOutputResponse{Data: data, NextOffset: next, Eof: eof}); err != nil {
			return err
		}
		if eof {
			return nil
		}
		offset = next
	}
}
