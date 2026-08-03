package allocationoutput

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	MaxOutputBytes = 64 << 20
	ChunkBytes     = 32 << 10
)

type ContainerLister interface {
	List(context.Context, *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error)
}

type Chunk struct {
	Stream    string
	Data      []byte
	Cursor    string
	Terminal  bool
	Truncated bool
}

type Reader struct{ containers ContainerLister }

func New(containers ContainerLister) *Reader { return &Reader{containers: containers} }

func (r *Reader) Read(ctx context.Context, allocationID, cursor string) ([]Chunk, bool, error) {
	response, err := r.containers.List(ctx, &runtimev1.ListContainersRequest{ID: allocationID})
	if err != nil {
		return nil, false, err
	}
	if len(response.GetContainers()) != 1 {
		return nil, false, grpcstatus.Error(codes.NotFound, "allocation output is not available")
	}
	container := response.GetContainers()[0]
	stdoutOffset, stderrOffset, truncationNotified, err := decodeCursor(cursor)
	if err != nil {
		return nil, false, grpcstatus.Error(codes.InvalidArgument, err.Error())
	}
	stdoutCurrentSize, err := outputSize(container.GetStdout())
	if err != nil {
		return nil, false, err
	}
	stderrCurrentSize, err := outputSize(container.GetStderr())
	if err != nil {
		return nil, false, err
	}
	if stdoutOffset > MaxOutputBytes || stderrOffset > MaxOutputBytes || stdoutOffset > stdoutCurrentSize || stderrOffset > stderrCurrentSize || stdoutOffset+stderrOffset > MaxOutputBytes || (truncationNotified && stdoutOffset+stderrOffset != MaxOutputBytes) {
		return nil, false, grpcstatus.Error(codes.InvalidArgument, "output cursor is invalid")
	}
	terminal := container.GetState() == runtimev1.ContainerState_CONTAINER_EXITED
	remaining := int64(MaxOutputBytes) - stdoutOffset - stderrOffset
	if remaining <= 0 {
		if terminal || !truncationNotified {
			return []Chunk{{Cursor: encodeCursor(stdoutOffset, stderrOffset, true), Terminal: terminal, Truncated: true}}, terminal, nil
		}
		return nil, false, nil
	}
	chunks := make([]Chunk, 0, 2)
	stdout, stdoutSize, err := readAt(container.GetStdout(), stdoutOffset, min64(ChunkBytes, remaining))
	if err != nil {
		return nil, false, err
	}
	if len(stdout) > 0 {
		stdoutOffset += int64(len(stdout))
		remaining -= int64(len(stdout))
		chunks = append(chunks, Chunk{Stream: "stdout", Data: stdout, Cursor: encodeCursor(stdoutOffset, stderrOffset, truncationNotified)})
	}
	stderr, stderrSize, err := readAt(container.GetStderr(), stderrOffset, min64(ChunkBytes, remaining))
	if err != nil {
		return nil, false, err
	}
	if len(stderr) > 0 {
		stderrOffset += int64(len(stderr))
		chunks = append(chunks, Chunk{Stream: "stderr", Data: stderr, Cursor: encodeCursor(stdoutOffset, stderrOffset, truncationNotified)})
	}
	truncated := stdoutSize+stderrSize > MaxOutputBytes
	complete := terminal && (truncated || (stdoutOffset >= stdoutSize && stderrOffset >= stderrSize))
	if truncated && !truncationNotified && stdoutOffset+stderrOffset >= MaxOutputBytes {
		truncationNotified = true
		if len(chunks) > 0 {
			chunks[len(chunks)-1].Truncated = true
			chunks[len(chunks)-1].Cursor = encodeCursor(stdoutOffset, stderrOffset, true)
		}
	}
	if len(chunks) == 0 && complete {
		chunks = append(chunks, Chunk{Cursor: encodeCursor(stdoutOffset, stderrOffset, truncationNotified), Terminal: true, Truncated: truncated})
	} else if len(chunks) > 0 && complete {
		chunks[len(chunks)-1].Terminal = true
		chunks[len(chunks)-1].Truncated = truncated
	}
	return chunks, complete, nil
}

func outputSize(path string) (int64, error) {
	if strings.TrimSpace(path) == "" {
		return 0, nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, grpcstatus.Error(codes.Internal, "inspect allocation output")
	}
	return info.Size(), nil
}

func readAt(path string, offset, limit int64) ([]byte, int64, error) {
	if strings.TrimSpace(path) == "" {
		return nil, 0, nil
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, grpcstatus.Error(codes.Internal, "open allocation output")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, grpcstatus.Error(codes.Internal, "inspect allocation output")
	}
	if offset >= info.Size() || limit <= 0 {
		return nil, info.Size(), nil
	}
	data := make([]byte, min64(limit, info.Size()-offset))
	count, err := file.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return nil, 0, grpcstatus.Error(codes.Internal, "read allocation output")
	}
	return data[:count], info.Size(), nil
}

func encodeCursor(stdout, stderr int64, truncationNotified bool) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("v1:%d:%d:%t", stdout, stderr, truncationNotified)))
}

func decodeCursor(value string) (int64, int64, bool, error) {
	if value == "" {
		return 0, 0, false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, 0, false, fmt.Errorf("output cursor is invalid")
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 4 || parts[0] != "v1" {
		return 0, 0, false, fmt.Errorf("output cursor is invalid")
	}
	stdout, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || stdout < 0 {
		return 0, 0, false, fmt.Errorf("output cursor is invalid")
	}
	stderr, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || stderr < 0 {
		return 0, 0, false, fmt.Errorf("output cursor is invalid")
	}
	notified, err := strconv.ParseBool(parts[3])
	if err != nil {
		return 0, 0, false, fmt.Errorf("output cursor is invalid")
	}
	return stdout, stderr, notified, nil
}

func min64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
