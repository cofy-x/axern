package axernsdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type SealedOutput struct {
	OutputID  string
	Path      string
	SizeBytes int64
	SHA256    string
	MediaType string
	Format    DeclaredOutputFormat
	Status    string
	Reason    string
	SealedAt  time.Time
	ExpiresAt time.Time
}

func (c *Client) GetSealedOutputManifest(ctx context.Context, runID string) ([]SealedOutput, error) {
	allocationID, err := c.runAllocationID(ctx, runID)
	if err != nil {
		return nil, err
	}
	response, err := c.nodes.GetSealedOutputManifest(ctx, &nodesandboxv1.GetSealedOutputManifestRequest{AllocationID: allocationID})
	if err != nil {
		return nil, mapRPCError(err, "get sealed output manifest", runID)
	}
	outputs := make([]SealedOutput, 0, len(response.GetOutputs()))
	for _, output := range response.GetOutputs() {
		item := SealedOutput{
			OutputID: output.GetOutputID(), Path: output.GetPath(), SizeBytes: output.GetSizeBytes(), SHA256: output.GetSha256(),
			MediaType: output.GetMediaType(), Status: sealedOutputStatusName(output.GetStatus()), Reason: output.GetReason(),
		}
		if output.GetSealedAt() != nil {
			item.SealedAt = output.GetSealedAt().AsTime()
		}
		if output.GetExpiresAt() != nil {
			item.ExpiresAt = output.GetExpiresAt().AsTime()
		}
		switch output.GetFormat() {
		case commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE:
			item.Format = DeclaredOutputFile
		case commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR:
			item.Format = DeclaredOutputTar
		}
		outputs = append(outputs, item)
	}
	return outputs, nil
}

// DownloadSealedOutput resumes at offset, writes the remaining bytes, and
// verifies the complete object digest when offset is zero.
func (c *Client) DownloadSealedOutput(ctx context.Context, runID, outputID string, offset int64, destination io.Writer) error {
	if outputID == "" {
		return requiredError("output_id")
	}
	if offset < 0 {
		return validationError("offset", "must be non-negative")
	}
	allocationID, err := c.runAllocationID(ctx, runID)
	if err != nil {
		return err
	}
	manifest, err := c.GetSealedOutputManifest(ctx, runID)
	if err != nil {
		return err
	}
	var selected *SealedOutput
	for index := range manifest {
		if manifest[index].OutputID == outputID {
			selected = &manifest[index]
			break
		}
	}
	if selected == nil || selected.Status != "available" {
		return fmt.Errorf("sealed output %s is not available", outputID)
	}
	stream, err := c.nodes.DownloadSealedOutput(ctx, &nodesandboxv1.DownloadSealedOutputRequest{AllocationID: allocationID, OutputID: outputID, Offset: offset})
	if err != nil {
		return mapRPCError(err, "download sealed output", runID)
	}
	digest := sha256.New()
	written := offset
	for {
		response, receiveErr := stream.Recv()
		if receiveErr != nil {
			return mapRPCError(receiveErr, "download sealed output", runID)
		}
		if response.GetNextOffset() != written+int64(len(response.GetData())) {
			return fmt.Errorf("sealed output returned a non-contiguous offset")
		}
		count, err := destination.Write(response.GetData())
		if err != nil {
			return err
		}
		if count != len(response.GetData()) {
			return io.ErrShortWrite
		}
		if offset == 0 {
			_, _ = digest.Write(response.GetData())
		}
		written = response.GetNextOffset()
		if response.GetEof() {
			break
		}
	}
	if written != selected.SizeBytes {
		return fmt.Errorf("sealed output size mismatch: received %d bytes, manifest has %d", written, selected.SizeBytes)
	}
	if offset == 0 && hex.EncodeToString(digest.Sum(nil)) != selected.SHA256 {
		return fmt.Errorf("sealed output digest mismatch")
	}
	return nil
}

func (c *Client) runAllocationID(ctx context.Context, runID string) (string, error) {
	if runID == "" {
		return "", requiredError("run_id")
	}
	response, err := c.runs.GetRun(ctx, &runv1.GetRunRequest{RunID: runID})
	if err != nil {
		return "", mapRPCError(err, "get run", runID)
	}
	if response.GetRun().GetAllocationID() == "" {
		return "", fmt.Errorf("run %s has no allocation", runID)
	}
	return response.GetRun().GetAllocationID(), nil
}

func sealedOutputStatusName(status nodesandboxv1.SealedOutputStatus) string {
	switch status {
	case nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_AVAILABLE:
		return "available"
	case nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_MISSING:
		return "missing"
	case nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_REJECTED:
		return "rejected"
	case nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_CAPTURE_FAILED:
		return "capture_failed"
	case nodesandboxv1.SealedOutputStatus_SEALED_OUTPUT_STATUS_NODE_UNAVAILABLE:
		return "node_unavailable"
	default:
		return "unspecified"
	}
}
