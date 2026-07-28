package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
)

func assertSandboxdFileAPI(ctx context.Context, bundlePath string) error {
	client := runtimesandboxd.NewClient(runtimeoci.SandboxdBundleSocketPath(bundlePath))
	started, err := client.StartProcess(ctx, runtimesandboxd.ProcessStartRequest{
		Args:          []string{"/bin/sh", "-c", "printf file-ok >/tmp/axern-file-e2e; mkdir -p /tmp/axern-file-dir; printf nested >/tmp/axern-file-dir/nested.txt"},
		CaptureOutput: true,
	})
	if err != nil {
		return fmt.Errorf("prepare file api fixture: %w", err)
	}
	waited, err := client.WaitProcess(ctx, started.ID)
	if err != nil {
		return fmt.Errorf("wait file api fixture: %w", err)
	}
	if waited.ExitCode == nil || *waited.ExitCode != 0 {
		return fmt.Errorf("file api fixture status = %#v", waited)
	}

	if err := assertSandboxdFileReadOnly(ctx, client); err != nil {
		return err
	}
	if err := assertSandboxdFileMutations(ctx, client); err != nil {
		return err
	}
	return nil
}

func assertSandboxdFileReadOnly(ctx context.Context, client *runtimesandboxd.Client) error {
	stat, err := client.StatFile(ctx, "/tmp/axern-file-e2e")
	if err != nil {
		return fmt.Errorf("sandboxd file stat: %w", err)
	}
	if stat.Info.GetPath() != "/tmp/axern-file-e2e" || stat.Info.GetSize() != int64(len("file-ok")) {
		return fmt.Errorf("sandboxd file stat response = %#v", stat)
	}
	read, err := client.ReadFile(ctx, "/tmp/axern-file-e2e")
	if err != nil {
		return fmt.Errorf("sandboxd file read: %w", err)
	}
	if string(read.Data) != "file-ok" {
		return fmt.Errorf("sandboxd file read data = %q", string(read.Data))
	}
	list, err := client.ListDir(ctx, "/tmp/axern-file-dir")
	if err != nil {
		return fmt.Errorf("sandboxd file list: %w", err)
	}
	if len(list.Entries) != 1 || list.Entries[0].GetPath() != "/tmp/axern-file-dir/nested.txt" {
		return fmt.Errorf("sandboxd file list response = %#v", list)
	}
	exists, err := client.Exists(ctx, "/tmp/axern-file-missing")
	if err != nil {
		return fmt.Errorf("sandboxd file exists: %w", err)
	}
	if exists.Exists {
		return fmt.Errorf("sandboxd missing file exists response = %#v", exists)
	}
	return nil
}

func assertSandboxdFileMutations(ctx context.Context, client *runtimesandboxd.Client) error {
	if err := client.WriteFile(ctx, runtimesandboxd.FileWriteRequest{Path: "/tmp/axern-file-write.txt", Data: []byte("written")}); err != nil {
		return fmt.Errorf("sandboxd file write: %w", err)
	}
	if err := client.Mkdir(ctx, runtimesandboxd.FileMkdirRequest{Path: "/tmp/axern-file-mkdir", Parents: true}); err != nil {
		return fmt.Errorf("sandboxd file mkdir: %w", err)
	}
	if err := client.Copy(ctx, runtimesandboxd.FileCopyRequest{SrcPath: "/tmp/axern-file-write.txt", DstPath: "/tmp/axern-file-copy.txt", Overwrite: true}); err != nil {
		return fmt.Errorf("sandboxd file copy: %w", err)
	}
	if err := client.Move(ctx, runtimesandboxd.FileMoveRequest{SrcPath: "/tmp/axern-file-copy.txt", DstPath: "/tmp/axern-file-move.txt", Overwrite: true}); err != nil {
		return fmt.Errorf("sandboxd file move: %w", err)
	}
	if err := client.Chmod(ctx, runtimesandboxd.FileChmodRequest{Path: "/tmp/axern-file-move.txt", Mode: 0600}); err != nil {
		return fmt.Errorf("sandboxd file chmod: %w", err)
	}
	moved, err := client.StatFile(ctx, "/tmp/axern-file-move.txt")
	if err != nil {
		return fmt.Errorf("sandboxd file stat moved: %w", err)
	}
	if moved.Info.GetMode() != 0600 {
		return fmt.Errorf("sandboxd chmod mode = %#v", moved.Info)
	}
	if err := client.Touch(ctx, runtimesandboxd.FileTouchRequest{Path: "/tmp/axern-file-touch.txt", Create: true, MtimeNs: 1710000000123456789}); err != nil {
		return fmt.Errorf("sandboxd file touch: %w", err)
	}
	if err := client.UploadArchive(ctx, runtimesandboxd.FileArchiveUploadRequest{
		Path:          "/tmp/axern-file-archive",
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		CreateParents: true,
		Overwrite:     true,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, bytes.NewReader(testTar(map[string]string{"nested/archive.txt": "archived"}))); err != nil {
		return fmt.Errorf("sandboxd file archive upload: %w", err)
	}
	var archive bytes.Buffer
	if err := client.DownloadArchive(ctx, runtimesandboxd.FileArchiveDownloadRequest{
		Path:          "/tmp/axern-file-archive",
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, &archive); err != nil {
		return fmt.Errorf("sandboxd file archive download: %w", err)
	}
	if !containsString(tarNames(archive.Bytes()), "nested/archive.txt") {
		return fmt.Errorf("sandboxd archive names = %#v", tarNames(archive.Bytes()))
	}
	if err := client.Remove(ctx, runtimesandboxd.FileRemoveRequest{Path: "/tmp/axern-file-mkdir", Recursive: true}); err != nil {
		return fmt.Errorf("sandboxd file remove: %w", err)
	}
	return nil
}

func assertSandboxdBackedFileService(ctx context.Context, cfg config, bundlePath string) error {
	socketPath := runtimeoci.SandboxdBundleSocketPath(bundlePath)
	snapshot, err := runtimesandboxd.NewClient(socketPath).WaitReady(ctx, runtimesandboxd.DefaultReadyTimeout, runtimesandboxd.DefaultPollInterval)
	if err != nil {
		return fmt.Errorf("wait sandboxd ready for runtime file service: %w", err)
	}
	labels := runtimesandboxd.EnrichLabels(nil, socketPath, snapshot)

	containerRoot := filepath.Dir(bundlePath)
	runtimeRoot := filepath.Dir(containerRoot)
	handler, err := newVerifyRuntimeHandlerWithRoot(cfg, runtimeRoot)
	if err != nil {
		return err
	}
	service := handler.FileService()
	options := contract.HandlerOptions{
		ContainerID:     filepath.Base(bundlePath),
		ContainerLabels: labels,
	}
	if err := assertRuntimeFileReadOnly(ctx, service, options); err != nil {
		return err
	}
	if err := assertRuntimeFileMutations(ctx, service, options); err != nil {
		return err
	}
	return nil
}

func assertRuntimeFileReadOnly(ctx context.Context, service interface {
	StatFile(context.Context, *apipb.StatFileRequest, contract.HandlerOptions) (*apipb.StatFileResponse, error)
	ReadFile(context.Context, *apipb.ReadFileRequest, contract.HandlerOptions) (*apipb.ReadFileResponse, error)
	ListDir(context.Context, *apipb.ListDirRequest, contract.HandlerOptions) (*apipb.ListDirResponse, error)
	Exists(context.Context, *apipb.ExistsRequest, contract.HandlerOptions) (*apipb.ExistsResponse, error)
}, options contract.HandlerOptions) error {
	stat, err := service.StatFile(ctx, &apipb.StatFileRequest{Path: "/tmp/axern-file-e2e"}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed StatFile: %w", err)
	}
	if stat.GetInfo().GetSize() != int64(len("file-ok")) {
		return fmt.Errorf("sandboxd-backed StatFile response = %#v", stat)
	}
	read, err := service.ReadFile(ctx, &apipb.ReadFileRequest{Path: "/tmp/axern-file-e2e"}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed ReadFile: %w", err)
	}
	if string(read.GetData()) != "file-ok" {
		return fmt.Errorf("sandboxd-backed ReadFile data = %q", string(read.GetData()))
	}
	list, err := service.ListDir(ctx, &apipb.ListDirRequest{Path: "/tmp/axern-file-dir"}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed ListDir: %w", err)
	}
	if len(list.GetEntries()) != 1 || list.GetEntries()[0].GetPath() != "/tmp/axern-file-dir/nested.txt" {
		return fmt.Errorf("sandboxd-backed ListDir response = %#v", list)
	}
	exists, err := service.Exists(ctx, &apipb.ExistsRequest{Path: "/tmp/axern-file-missing"}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed Exists: %w", err)
	}
	if exists.GetExists() {
		return fmt.Errorf("sandboxd-backed Exists response = %#v", exists)
	}
	return nil
}

func assertRuntimeFileMutations(ctx context.Context, service interface {
	WriteFile(context.Context, *apipb.WriteFileRequest, contract.HandlerOptions) (*apipb.WriteFileResponse, error)
	ReadFile(context.Context, *apipb.ReadFileRequest, contract.HandlerOptions) (*apipb.ReadFileResponse, error)
	Mkdir(context.Context, *apipb.MkdirRequest, contract.HandlerOptions) (*apipb.MkdirResponse, error)
	Copy(context.Context, *apipb.CopyRequest, contract.HandlerOptions) (*apipb.CopyResponse, error)
	Move(context.Context, *apipb.MoveRequest, contract.HandlerOptions) (*apipb.MoveResponse, error)
	Chmod(context.Context, *apipb.ChmodRequest, contract.HandlerOptions) (*apipb.ChmodResponse, error)
	Touch(context.Context, *apipb.TouchRequest, contract.HandlerOptions) (*apipb.TouchResponse, error)
	UploadArchive(context.Context, *apipb.UploadArchiveRequest, io.Reader, contract.HandlerOptions) (*apipb.UploadArchiveResponse, error)
	DownloadArchive(context.Context, *apipb.DownloadArchiveRequest, io.Writer, contract.HandlerOptions) (*apipb.DownloadArchiveResponse, error)
	Remove(context.Context, *apipb.RemoveRequest, contract.HandlerOptions) (*apipb.RemoveResponse, error)
}, options contract.HandlerOptions) error {
	_, err := service.WriteFile(ctx, &apipb.WriteFileRequest{Path: "/tmp/axern-file-service-write.txt", Data: []byte("service"), CreateParents: true}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed WriteFile: %w", err)
	}
	written, err := service.ReadFile(ctx, &apipb.ReadFileRequest{Path: "/tmp/axern-file-service-write.txt"}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed ReadFile after write: %w", err)
	}
	if string(written.GetData()) != "service" {
		return fmt.Errorf("sandboxd-backed WriteFile data = %q", string(written.GetData()))
	}
	_, err = service.Mkdir(ctx, &apipb.MkdirRequest{Path: "/tmp/axern-file-service-dir", Parents: true}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed Mkdir: %w", err)
	}
	_, err = service.Copy(ctx, &apipb.CopyRequest{SrcPath: "/tmp/axern-file-service-write.txt", DstPath: "/tmp/axern-file-service-dir/copied.txt", Overwrite: true}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed Copy: %w", err)
	}
	_, err = service.Move(ctx, &apipb.MoveRequest{SrcPath: "/tmp/axern-file-service-dir/copied.txt", DstPath: "/tmp/axern-file-service-dir/moved.txt", Overwrite: true}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed Move: %w", err)
	}
	_, err = service.Chmod(ctx, &apipb.ChmodRequest{Path: "/tmp/axern-file-service-dir/moved.txt", Mode: 0600}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed Chmod: %w", err)
	}
	_, err = service.Touch(ctx, &apipb.TouchRequest{Path: "/tmp/axern-file-service-dir/touched.txt", Create: true}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed Touch: %w", err)
	}
	_, err = service.UploadArchive(ctx, &apipb.UploadArchiveRequest{
		Path:          "/tmp/axern-file-service-archive",
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		CreateParents: true,
		Overwrite:     true,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, bytes.NewReader(testTar(map[string]string{"service/archive.txt": "ok"})), options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed UploadArchive: %w", err)
	}
	var downloaded bytes.Buffer
	_, err = service.DownloadArchive(ctx, &apipb.DownloadArchiveRequest{
		Path:          "/tmp/axern-file-service-archive",
		Format:        filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR,
		SymlinkPolicy: filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
	}, &downloaded, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed DownloadArchive: %w", err)
	}
	if !containsString(tarNames(downloaded.Bytes()), "service/archive.txt") {
		return fmt.Errorf("sandboxd-backed archive names = %#v", tarNames(downloaded.Bytes()))
	}
	_, err = service.Remove(ctx, &apipb.RemoveRequest{Path: "/tmp/axern-file-service-dir", Recursive: true}, options)
	if err != nil {
		return fmt.Errorf("sandboxd-backed Remove: %w", err)
	}
	return nil
}

func testTar(files map[string]string) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0755})
	for name, data := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(data))})
		_, _ = tw.Write([]byte(data))
	}
	_ = tw.Close()
	return buf.Bytes()
}

func tarNames(data []byte) []string {
	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			return append(names, "tar-error:"+err.Error())
		}
		names = append(names, header.Name)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
