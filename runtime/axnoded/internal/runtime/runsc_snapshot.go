package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

func (r *RunscServiceHandler) SnapshotRootfsUpper(ctx context.Context, containerID string, output io.Writer) error {
	containerID = strings.TrimSpace(containerID)
	if containerID == "" || output == nil {
		return fmt.Errorf("container id and snapshot output are required")
	}
	archiveReader, archiveWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create runsc rootfs snapshot pipe: %w", err)
	}
	command := r.common.NewCommandContext(ctx, r.lifecycleArgs(rootfsSnapshotArgs(containerID)...)...)
	command.ExtraFiles = append(command.ExtraFiles, archiveWriter)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	streamed := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(output, archiveReader)
		_ = archiveReader.Close()
		streamed <- copyErr
	}()
	if err := command.Start(); err != nil {
		_ = archiveWriter.Close()
		_ = archiveReader.Close()
		<-streamed
		return fmt.Errorf("start runsc rootfs upper layer export: %w", err)
	}
	// The child owns fd 3 after Start. Closing the parent copy is required for
	// the reader to observe EOF when runsc completes.
	_ = archiveWriter.Close()
	waitErr := command.Wait()
	streamErr := <-streamed
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("export runsc rootfs upper layer: %w", ctxErr)
	}
	// A destination failure commonly makes runsc observe EPIPE and exit too.
	// Preserve the originating stream error instead of misreporting that
	// secondary process failure as a runtime export failure.
	if streamErr != nil {
		return fmt.Errorf("stream runsc rootfs upper layer: %w", streamErr)
	}
	if waitErr != nil {
		message := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
		if message == "" {
			return fmt.Errorf("export runsc rootfs upper layer: %w", waitErr)
		}
		return fmt.Errorf("export runsc rootfs upper layer: %s: %w", message, waitErr)
	}
	return nil
}

// runsc logs the rootfs-upper operation to stdout even when its global log
// destination is redirected. Stream the tar through inherited fd 3 instead;
// this avoids both stdout corruption and a node-local temporary archive.
func rootfsSnapshotArgs(containerID string) []string {
	return []string{"tar", "rootfs-upper", "--file=/proc/self/fd/3", containerID}
}
