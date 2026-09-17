package allocation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

const (
	maxSealedFileBytes    int64 = 64 << 20
	maxSealedArchiveBytes int64 = 256 << 20
	maxSealedTotalBytes   int64 = 256 << 20
)

var errDeclaredOutputTooLarge = errors.New("declared output exceeds the sealed-output limit")

// cleanupDeclaredOutputs resolves the control-plane cleanup contract against
// the node's crash-recovery copy. The Run specification remains authoritative;
// the durable node copy detects a mismatched identity rather than silently
// sealing a different set of paths. Carrying the contract on Delete also lets
// a retry publish explicit unavailable entries if local state was lost after a
// fail-stop, instead of publishing a misleading empty manifest.
func (h *Controller) cleanupDeclaredOutputs(allocationID string, requested []*commonv1.DeclaredOutput) ([]*commonv1.DeclaredOutput, bool, error) {
	local, present := h.durableDeclaredOutputs(allocationID)
	if present && !declaredOutputListsEqual(local, requested) {
		return nil, true, fmt.Errorf("allocation %q declared-output cleanup contract conflicts with durable execution specification: %w", allocationID, errord.ErrFailedPrecondition)
	}
	return cloneDeclaredOutputs(requested), present, nil
}

func (h *Controller) durableDeclaredOutputs(allocationID string) ([]*commonv1.DeclaredOutput, bool) {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[allocationID]
	if state == nil || state.record == nil || strings.TrimSpace(state.record.GetNodeID()) == "" {
		return nil, false
	}
	return cloneDeclaredOutputs(state.record.GetDeclaredOutputs()), true
}

func declaredOutputListsEqual(left, right []*commonv1.DeclaredOutput) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !proto.Equal(left[i], right[i]) {
			return false
		}
	}
	return true
}

func declaredOutputContractSHA256(outputs []*commonv1.DeclaredOutput) (string, error) {
	digest := sha256.New()
	for _, output := range outputs {
		if output == nil {
			return "", fmt.Errorf("declared-output contract contains a nil entry: %w", errord.ErrInvalidArgument)
		}
		payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(output)
		if err != nil {
			return "", fmt.Errorf("marshal declared-output contract: %w", err)
		}
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(payload)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write(payload)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func unavailableDeclaredOutputs(declarations []*commonv1.DeclaredOutput, now time.Time) []allocationoutput.Entry {
	entries := make([]allocationoutput.Entry, 0, len(declarations))
	for _, declaration := range declarations {
		if declaration == nil {
			continue
		}
		entries = append(entries, allocationoutput.Entry{
			OutputID:  "output-" + uuid.NewString(),
			Path:      declaration.GetPath(),
			MediaType: declaration.GetMediaType(),
			Format:    declaredOutputFormatName(declaration.GetFormat()),
			Status:    "node_unavailable",
			Reason:    "allocation runtime is unavailable before output sealing",
			SealedAt:  now,
		})
	}
	return entries
}

func (h *Controller) captureDeclaredOutputs(allocationID string, declarations []*commonv1.DeclaredOutput) allocationoutput.Capture {
	return func(ctx context.Context, objectsDir string) ([]allocationoutput.Entry, error) {
		if len(declarations) == 0 {
			return nil, nil
		}
		if h.runscHandler == nil || h.runscHandler.FileService() == nil {
			return nil, fmt.Errorf("capture declared outputs: runtime file service unavailable: %w", errord.ErrUnavailable)
		}
		fileService := h.runscHandler.FileService()
		options := contract.HandlerOptions{ContainerID: allocationID}
		entries := make([]allocationoutput.Entry, 0, len(declarations))
		var totalBytes int64
		for _, declaration := range declarations {
			entry, err := captureDeclaredOutput(ctx, fileService, options, declaration, objectsDir, maxSealedTotalBytes-totalBytes)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
			if entry.Status == "available" {
				totalBytes += entry.SizeBytes
			}
		}
		return entries, nil
	}
}

func captureDeclaredOutput(ctx context.Context, files contract.FileService, options contract.HandlerOptions, declaration *commonv1.DeclaredOutput, objectsDir string, remaining int64) (allocationoutput.Entry, error) {
	if declaration == nil {
		return allocationoutput.Entry{}, fmt.Errorf("declared output is nil: %w", errord.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	entry := allocationoutput.Entry{
		OutputID:  "output-" + uuid.NewString(),
		Path:      declaration.GetPath(),
		MediaType: declaration.GetMediaType(),
		Format:    declaredOutputFormatName(declaration.GetFormat()),
		SealedAt:  now,
	}
	stat, err := files.StatFile(ctx, &apipb.StatFileRequest{ID: options.ContainerID, Path: declaration.GetPath()}, options)
	if errors.Is(err, errord.ErrNotFound) {
		entry.Status = "missing"
		entry.Reason = "declared output path does not exist"
		return entry, nil
	}
	if err != nil {
		return entry, fmt.Errorf("stat declared output %q: %w", declaration.GetPath(), err)
	}
	if stat.GetInfo() == nil {
		return entry, fmt.Errorf("stat declared output %q returned no metadata: %w", declaration.GetPath(), errord.ErrUnavailable)
	}

	objectID := entry.OutputID
	object, err := allocationoutput.CreateObject(objectsDir, objectID)
	if err != nil {
		return entry, err
	}
	keep := false
	defer func() {
		_ = object.Close()
		if !keep {
			_ = os.Remove(object.Name())
		}
	}()
	digest := sha256.New()

	switch declaration.GetFormat() {
	case commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE:
		if stat.GetInfo().GetKind() != filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE {
			return rejectedOutput(entry, "declared file output is not a regular file"), nil
		}
		if stat.GetInfo().GetSize() > maxSealedFileBytes || stat.GetInfo().GetSize() > remaining {
			return rejectedOutput(entry, errDeclaredOutputTooLarge.Error()), nil
		}
		response, readErr := files.ReadFile(ctx, &apipb.ReadFileRequest{ID: options.ContainerID, Path: declaration.GetPath()}, options)
		if errors.Is(readErr, errord.ErrNotFound) {
			return failedOutput(entry, "declared output disappeared while sealing"), nil
		}
		if readErr != nil {
			return entry, fmt.Errorf("read declared output %q: %w", declaration.GetPath(), readErr)
		}
		if int64(len(response.GetData())) != stat.GetInfo().GetSize() {
			return entry, fmt.Errorf("declared output %q changed while sealing: %w", declaration.GetPath(), errord.ErrUnavailable)
		}
		if _, err := io.MultiWriter(object, digest).Write(response.GetData()); err != nil {
			return entry, err
		}
		entry.SizeBytes = int64(len(response.GetData()))
	case commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR:
		if stat.GetInfo().GetKind() != filev1.SandboxFileKind_SANDBOX_FILE_KIND_DIRECTORY {
			return rejectedOutput(entry, "declared tar output is not a directory"), nil
		}
		limit := minInt64(maxSealedArchiveBytes, remaining)
		writer := &boundedDigestWriter{output: object, digest: digest, remaining: limit}
		_, archiveErr := files.DownloadArchive(ctx, &apipb.DownloadArchiveRequest{
			ID:            options.ContainerID,
			Path:          declaration.GetPath(),
			Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
			SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
		}, writer, options)
		if writer.exceeded || errors.Is(archiveErr, errDeclaredOutputTooLarge) {
			return rejectedOutput(entry, errDeclaredOutputTooLarge.Error()), nil
		}
		if archiveErr != nil {
			if errors.Is(archiveErr, errord.ErrNotFound) {
				return failedOutput(entry, "declared output disappeared while sealing"), nil
			}
			if errors.Is(archiveErr, errord.ErrFailedPrecondition) || errors.Is(archiveErr, errord.ErrInvalidArgument) {
				return rejectedOutput(entry, "declared directory cannot be safely archived"), nil
			}
			return entry, fmt.Errorf("archive declared output %q: %w", declaration.GetPath(), archiveErr)
		}
		entry.SizeBytes = writer.written
	default:
		return entry, fmt.Errorf("unsupported declared output format: %w", errord.ErrInvalidArgument)
	}
	if err := object.Sync(); err != nil {
		return entry, err
	}
	if err := object.Close(); err != nil {
		return entry, err
	}
	keep = true
	entry.Object = objectID
	entry.SHA256 = hex.EncodeToString(digest.Sum(nil))
	entry.Status = "available"
	return entry, nil
}

func rejectedOutput(entry allocationoutput.Entry, reason string) allocationoutput.Entry {
	entry.Status = "rejected"
	entry.Reason = reason
	return entry
}

func failedOutput(entry allocationoutput.Entry, reason string) allocationoutput.Entry {
	entry.Status = "capture_failed"
	entry.Reason = reason
	return entry
}

func declaredOutputFormatName(format commonv1.DeclaredOutputFormat) string {
	switch format {
	case commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_FILE:
		return "file"
	case commonv1.DeclaredOutputFormat_DECLARED_OUTPUT_FORMAT_TAR:
		return "tar"
	default:
		return strings.ToLower(strings.TrimPrefix(format.String(), "DECLARED_OUTPUT_FORMAT_"))
	}
}

type boundedDigestWriter struct {
	output    io.Writer
	digest    hash.Hash
	remaining int64
	written   int64
	exceeded  bool
}

func (w *boundedDigestWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		w.exceeded = true
		return 0, errDeclaredOutputTooLarge
	}
	n, err := io.MultiWriter(w.output, w.digest).Write(data)
	w.remaining -= int64(n)
	w.written += int64(n)
	return n, err
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
