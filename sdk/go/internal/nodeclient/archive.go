package nodeclient

import (
	"context"
	"errors"
	"io"

	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

const defaultArchiveChunkSize = 1024 * 1024

type ArchiveReaderFactory func() (io.Reader, func() error, error)
type ArchiveWriterFactory func() (io.Writer, func(error) error, error)

func (c *Client) UploadArchive(ctx context.Context, path string, readerFactory ArchiveReaderFactory, createParents, overwrite bool) error {
	reader, closeReader, err := readerFactory()
	if err != nil {
		return err
	}
	err = c.uploadArchiveOnce(ctx, path, reader, createParents, overwrite)
	closeErr := closeReader()
	if err != nil {
		return err
	}
	return closeErr
}

func (c *Client) uploadArchiveOnce(ctx context.Context, path string, reader io.Reader, createParents, overwrite bool) error {
	stream, err := c.nodes.UploadArchive(ctx)
	if err != nil {
		return err
	}
	if err := sendUploadArchive(stream, &nodesandboxv1.UploadArchiveRequest{
		Payload: &nodesandboxv1.UploadArchiveRequest_Open{
			Open: &nodesandboxv1.UploadArchiveOpen{
				AllocationID:  c.allocationID,
				Path:          path,
				Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
				CreateParents: createParents,
				Overwrite:     overwrite,
				SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
			},
		},
	}); err != nil {
		return err
	}
	buffer := make([]byte, defaultArchiveChunkSize)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if err := sendUploadArchive(stream, &nodesandboxv1.UploadArchiveRequest{
				Payload: &nodesandboxv1.UploadArchiveRequest_Chunk{Chunk: append([]byte(nil), buffer[:n]...)},
			}); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = stream.CloseSend()
			return readErr
		}
	}
	_, err = stream.CloseAndRecv()
	return err
}

func sendUploadArchive(stream nodesandboxv1.NodeSandbox_UploadArchiveClient, request *nodesandboxv1.UploadArchiveRequest) error {
	err := stream.Send(request)
	if errors.Is(err, io.EOF) {
		_, err = stream.CloseAndRecv()
	}
	return err
}

func (c *Client) DownloadArchive(ctx context.Context, path string, writerFactory ArchiveWriterFactory) error {
	writer, closeWriter, err := writerFactory()
	if err != nil {
		return err
	}
	counting := &countingWriter{writer: writer}
	err = c.downloadArchiveOnce(ctx, path, counting)
	closeErr := closeWriter(err)
	if err != nil {
		return err
	}
	return closeErr
}

func (c *Client) downloadArchiveOnce(ctx context.Context, path string, writer io.Writer) error {
	stream, err := c.nodes.DownloadArchive(ctx, &nodesandboxv1.DownloadArchiveRequest{
		AllocationID:  c.allocationID,
		Path:          path,
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	})
	if err != nil {
		return err
	}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(response.GetChunk()) == 0 {
			continue
		}
		if _, err := writer.Write(response.GetChunk()); err != nil {
			return err
		}
	}
}

type countingWriter struct {
	writer io.Writer
	wrote  bool
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.wrote = true
	}
	return w.writer.Write(p)
}
