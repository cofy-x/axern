package axernsdk

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestSandboxStartExecFileClose(t *testing.T) {
	fake := &fakeAxernServer{
		files: map[string][]byte{},
	}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := NewClient(
		ctx,
		"bufnet",
		WithDialOptions(
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	sandbox, err := NewSandbox(SandboxOptions{
		Client:                client,
		TemplateID:            "python311",
		ReadyTimeout:          time.Second,
		ExtensionCapabilities: []ExtensionCapability{{Name: "example.com/accelerator", Value: "v1"}},
		Volumes: []VolumeMount{{
			Name:     "workspace",
			Target:   "/workspace",
			Readonly: true,
			Options:  []string{"rbind"},
		}},
		ImageMounts: []ImageMount{{
			Image:  "example.com/axern/codex-tool:latest",
			Target: "/opt/axern/tools/codex",
		}},
	})
	if err != nil {
		t.Fatalf("new sandbox: %v", err)
	}
	if err := sandbox.Start(ctx); err != nil {
		t.Fatalf("start sandbox: %v", err)
	}

	state, err := sandbox.State()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.EnvironmentID != "env-1" || state.ServiceID != "svc-1" || state.AllocationID != "alloc-1" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.WorkspacePreparation.GetPayloadFormat() != "nydus" || state.WorkspacePreparation.GetPayloadDigest() != "sha256:payload" || !state.WorkspacePreparation.GetCacheHit() {
		t.Fatalf("unexpected workspace preparation: %+v", state.WorkspacePreparation)
	}
	if err := sandbox.MaterializeTaskAssets(ctx, "tasks/task-a/verifier/check.sh", "/workspace/check.sh", TaskAssetKindVerifier); err != nil {
		t.Fatalf("materialize task assets: %v", err)
	}
	state, err = sandbox.State()
	if err != nil {
		t.Fatalf("state after materialization: %v", err)
	}
	if state.VerifierMaterializeMs != 7 {
		t.Fatalf("verifier materialize ms = %d, want 7", state.VerifierMaterializeMs)
	}
	metadata, err := sandbox.Metadata()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if metadata.EnvironmentID != "env-1" || metadata.ServiceID != "svc-1" || metadata.AllocationID != "alloc-1" || metadata.Attempt != 2 || metadata.NodeID != "node-1" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	if got := fake.createServiceRequest.GetConfig().GetVolumeMounts(); len(got) != 1 || got[0].GetName() != "workspace" || got[0].GetTarget() != "/workspace" || !got[0].GetReadonly() || len(got[0].GetOptions()) != 1 || got[0].GetOptions()[0] != "rbind" {
		t.Fatalf("unexpected service volume mounts: %#v", got)
	}
	if got := fake.createServiceRequest.GetConfig().GetImageMounts(); len(got) != 1 || got[0].GetImage() != "example.com/axern/codex-tool:latest" || got[0].GetTarget() != "/opt/axern/tools/codex" || !got[0].GetReadonly() {
		t.Fatalf("unexpected image mounts: %#v", got)
	}
	if got := fake.createServiceRequest.GetConfig().GetExtensionCapabilityRequirements(); len(got) != 1 || got[0].GetCapability().GetName() != "example.com/accelerator" || got[0].GetCapability().GetValue() != "v1" {
		t.Fatalf("unexpected extension capability requirements: %#v", got)
	}

	result, err := sandbox.Exec(ctx, "echo hello", ExecOptions{Check: true})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.ExitCode != 0 || result.StdoutString() != "hello\n" {
		t.Fatalf("unexpected exec result: %+v", result)
	}
	if got := fake.execArgv; len(got) != 3 || got[0] != "/bin/sh" || got[1] != "-lc" || got[2] != "echo hello" {
		t.Fatalf("exec argv = %v", got)
	}

	if err := sandbox.WriteFile(ctx, "/tmp/message.txt", []byte("payload"), WriteFileOptions{CreateParents: true}); err != nil {
		t.Fatalf("write file: %v", err)
	}
	data, err := sandbox.ReadFile(ctx, "/tmp/message.txt")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("read file = %q", data)
	}
	exists, err := sandbox.Exists(ctx, "/tmp/message.txt")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true")
	}
	info, err := sandbox.Stat(ctx, "/tmp/message.txt")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Path != "/tmp/message.txt" || info.Kind != SandboxFileKindFile || info.Size != int64(len("payload")) || info.Mode != 0o644 || info.MtimeNS != 7 {
		t.Fatalf("unexpected file info: %+v", info)
	}
	entries, err := sandbox.ListDir(ctx, "/tmp")
	if err != nil {
		t.Fatalf("list dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "/tmp/message.txt" {
		t.Fatalf("unexpected list dir entries: %+v", entries)
	}
	if err := sandbox.Mkdir(ctx, "/tmp/nested", MkdirOptions{Parents: true}); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if fake.mkdirPath != "/tmp/nested" || !fake.mkdirParents {
		t.Fatalf("mkdir request path=%q parents=%v", fake.mkdirPath, fake.mkdirParents)
	}
	if err := sandbox.Copy(ctx, "/tmp/message.txt", "/tmp/copy.txt", CopyOptions{Overwrite: true}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if fake.copySrc != "/tmp/message.txt" || fake.copyDst != "/tmp/copy.txt" || !fake.copyOverwrite {
		t.Fatalf("copy request src=%q dst=%q overwrite=%v", fake.copySrc, fake.copyDst, fake.copyOverwrite)
	}
	if err := sandbox.Move(ctx, "/tmp/copy.txt", "/tmp/moved.txt", MoveOptions{Overwrite: true}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if fake.moveSrc != "/tmp/copy.txt" || fake.moveDst != "/tmp/moved.txt" || !fake.moveOverwrite {
		t.Fatalf("move request src=%q dst=%q overwrite=%v", fake.moveSrc, fake.moveDst, fake.moveOverwrite)
	}
	if err := sandbox.Chmod(ctx, "/tmp/moved.txt", 0o600, ChmodOptions{}); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if fake.chmodPath != "/tmp/moved.txt" || fake.chmodMode != 0o600 {
		t.Fatalf("chmod request path=%q mode=%#o", fake.chmodPath, fake.chmodMode)
	}
	if err := sandbox.Touch(ctx, "/tmp/touched.txt", TouchOptions{MtimeNS: 123}); err != nil {
		t.Fatalf("touch: %v", err)
	}
	if fake.touchPath != "/tmp/touched.txt" || !fake.touchCreate || fake.touchMtimeNS != 123 {
		t.Fatalf("touch request path=%q create=%v mtime_ns=%d", fake.touchPath, fake.touchCreate, fake.touchMtimeNS)
	}
	if err := sandbox.Remove(ctx, "/tmp/moved.txt", RemoveOptions{Force: true}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if fake.removePath != "/tmp/moved.txt" || !fake.removeForce {
		t.Fatalf("remove request path=%q force=%v", fake.removePath, fake.removeForce)
	}
	computerStatus, err := sandbox.ComputerUseStatus(ctx)
	if err != nil {
		t.Fatalf("computer-use status: %v", err)
	}
	if !computerStatus.Available || computerStatus.Display != ":99" || computerStatus.Backend != "x11" {
		t.Fatalf("computer-use status = %+v", computerStatus)
	}
	screenshot, err := sandbox.ComputerUseScreenshot(ctx, ComputerUseScreenshotOptions{
		Region:  &ComputerUseRegion{X: 1, Y: 2, Width: 3, Height: 4},
		Format:  "jpeg",
		Quality: 70,
	})
	if err != nil {
		t.Fatalf("computer-use screenshot: %v", err)
	}
	if string(screenshot.Data) != "png" || screenshot.ContentType != "image/png" {
		t.Fatalf("computer-use screenshot = %+v", screenshot)
	}
	display, err := sandbox.ComputerUseDisplay(ctx)
	if err != nil {
		t.Fatalf("computer-use display: %v", err)
	}
	if display.Width != 1280 || display.Height != 720 {
		t.Fatalf("computer-use display = %+v", display)
	}
	if err := sandbox.ComputerUseMouse(ctx, ComputerUseMouseOptions{Action: "move", X: 7, Y: 9}); err != nil {
		t.Fatalf("computer-use mouse: %v", err)
	}
	if err := sandbox.ComputerUseKeyboard(ctx, ComputerUseKeyboardOptions{Key: "Escape"}); err != nil {
		t.Fatalf("computer-use keyboard: %v", err)
	}
	if !fake.computerUseStatusSeen || !fake.computerUseScreenSeen || !fake.computerUseDisplaySeen || !fake.computerUseMouseSeen || !fake.computerUseKeySeen {
		t.Fatalf("computer-use calls seen status=%v screenshot=%v display=%v mouse=%v keyboard=%v", fake.computerUseStatusSeen, fake.computerUseScreenSeen, fake.computerUseDisplaySeen, fake.computerUseMouseSeen, fake.computerUseKeySeen)
	}
	capabilityStatus, err := sandbox.CapabilityStatus(ctx)
	if err != nil {
		t.Fatalf("capability status: %v", err)
	}
	if !capabilityStatus.Ready || capabilityStatus.ProviderSummary.Total != 2 || len(capabilityStatus.Providers) != 2 {
		t.Fatalf("capability status = %+v", capabilityStatus)
	}
	if !fake.capabilityStatusSeen {
		t.Fatal("capability status call was not observed")
	}
	process, err := sandbox.Process(ctx, []string{"cat"}, ProcessOptions{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := process.WriteString("process-ok\n"); err != nil {
		t.Fatalf("process write: %v", err)
	}
	if err := process.CloseStdin(); err != nil {
		t.Fatalf("process close stdin: %v", err)
	}
	processOutput, err := process.Output()
	if err != nil {
		t.Fatalf("process output: %v", err)
	}
	processResult, err := process.Wait()
	if err != nil {
		t.Fatalf("process wait: %v", err)
	}
	if processOutput.ExitCode != 0 || processResult.ExitCode != 0 || string(processOutput.Stdout) != "process-ok\n" {
		t.Fatalf("unexpected process output=%+v result=%+v", processOutput, processResult)
	}
	_ = process.Close()
	imageResult, err := sandbox.ExecImage(ctx, "ghcr.io/cofy-x/agent:latest", "tool run", ImageExecOptions{Check: true})
	if err != nil {
		t.Fatalf("exec image: %v", err)
	}
	if imageResult.StdoutString() != "image\n" {
		t.Fatalf("exec image stdout = %q", imageResult.StdoutString())
	}
	if len(fake.execImageSpecs) != 1 {
		t.Fatalf("exec image specs count = %d, want 1", len(fake.execImageSpecs))
	}
	if got := fake.execImageSpecs[0]; got.GetImage() != "ghcr.io/cofy-x/agent:latest" || len(got.GetMounts()) != 1 || got.GetMounts()[0].GetSandboxPath() != "/workspace" {
		t.Fatalf("unexpected default exec image spec = %#v", got)
	}
	_, err = sandbox.ExecImage(ctx, "ghcr.io/cofy-x/agent:latest", Args("tool", "isolated"), ImageExecOptions{Mounts: []ImageProcessMount{}})
	if err != nil {
		t.Fatalf("exec image isolated: %v", err)
	}
	if got := fake.execImageSpecs[1]; len(got.GetMounts()) != 0 {
		t.Fatalf("isolated exec image mounts = %#v, want empty", got.GetMounts())
	}
	imageProcess, err := sandbox.ProcessImage(ctx, "ghcr.io/cofy-x/agent:latest", []string{"cat"}, ImageProcessOptions{
		Mounts: []ImageProcessMount{WorkspaceMount("/workspace")},
	})
	if err != nil {
		t.Fatalf("process image: %v", err)
	}
	if err := imageProcess.WriteString("image-process-ok\n"); err != nil {
		t.Fatalf("image process write: %v", err)
	}
	if err := imageProcess.CloseStdin(); err != nil {
		t.Fatalf("image process close stdin: %v", err)
	}
	imageProcessOutput, err := imageProcess.Output()
	if err != nil {
		t.Fatalf("image process output: %v", err)
	}
	if imageProcessOutput.ExitCode != 0 || string(imageProcessOutput.Stdout) != "image-process-ok\n" {
		t.Fatalf("unexpected image process output=%+v", imageProcessOutput)
	}
	if got := fake.processImageSpec; got == nil || got.GetImage() != "ghcr.io/cofy-x/agent:latest" || len(got.GetMounts()) != 1 {
		t.Fatalf("unexpected process image spec = %#v", got)
	}
	root := t.TempDir()
	source := filepath.Join(root, "upload")
	target := filepath.Join(root, "download")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir local source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "data.txt"), []byte("archive-ok\n"), 0o644); err != nil {
		t.Fatalf("write local source: %v", err)
	}
	if err := sandbox.UploadDir(ctx, source, "/tmp/tree", UploadDirOptions{}); err != nil {
		t.Fatalf("upload dir: %v", err)
	}
	if fake.uploadPath != "/tmp/tree" || !fake.uploadCreateParents || !fake.uploadOverwrite || !fake.uploadSawNestedFile {
		t.Fatalf("unexpected upload archive state path=%q create=%v overwrite=%v nested=%v", fake.uploadPath, fake.uploadCreateParents, fake.uploadOverwrite, fake.uploadSawNestedFile)
	}
	if err := sandbox.DownloadDir(ctx, "/tmp/tree", target, DownloadDirOptions{}); err != nil {
		t.Fatalf("download dir: %v", err)
	}
	downloaded, err := os.ReadFile(filepath.Join(target, "nested", "data.txt"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(downloaded) != "archive-ok\n" {
		t.Fatalf("downloaded content = %q", downloaded)
	}

	if err := sandbox.Close(ctx); err != nil {
		t.Fatalf("close sandbox: %v", err)
	}
	if !fake.deletedService || !fake.deletedEnvironment {
		t.Fatalf("cleanup flags service=%v environment=%v", fake.deletedService, fake.deletedEnvironment)
	}
}

func TestSandboxRequiresExactlyOneSource(t *testing.T) {
	_, err := NewSandbox(SandboxOptions{Client: &Client{}, TemplateID: "python311", Image: "image"})
	if err != ErrInvalidSource {
		t.Fatalf("error = %v, want ErrInvalidSource", err)
	}
}

func TestSandboxCloseClosesRunningProcesses(t *testing.T) {
	fake := &fakeAxernServer{
		files:          map[string][]byte{},
		processStarted: make(chan struct{}),
		processClosed:  make(chan struct{}),
	}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := NewClient(
		ctx,
		"bufnet",
		WithDialOptions(
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	sandbox, err := NewSandbox(SandboxOptions{
		Client:       client,
		TemplateID:   "python311",
		ReadyTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new sandbox: %v", err)
	}
	if err := sandbox.Start(ctx); err != nil {
		t.Fatalf("start sandbox: %v", err)
	}
	process, err := sandbox.Process(ctx, []string{"cat"}, ProcessOptions{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	select {
	case <-fake.processStarted:
	case <-ctx.Done():
		t.Fatalf("process did not start: %v", ctx.Err())
	}
	if err := sandbox.Close(ctx); err != nil {
		t.Fatalf("close sandbox: %v", err)
	}
	select {
	case <-fake.processClosed:
	case <-ctx.Done():
		t.Fatalf("process was not closed by sandbox close: %v", ctx.Err())
	}
	if err := process.WriteString("after-close"); !errors.Is(err, ErrProcessClosed) {
		t.Fatalf("process write after sandbox close = %v, want ErrProcessClosed", err)
	}
}

func TestProcessCloseIsIdempotent(t *testing.T) {
	fake := &fakeAxernServer{
		files:          map[string][]byte{},
		processStarted: make(chan struct{}),
		processClosed:  make(chan struct{}),
	}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, sandbox := startTestSandbox(t, ctx, dialer)
	defer client.Close()
	defer sandbox.Close(ctx)

	process, err := sandbox.Process(ctx, []string{"cat"}, ProcessOptions{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	select {
	case <-fake.processStarted:
	case <-ctx.Done():
		t.Fatalf("process did not start: %v", ctx.Err())
	}
	if err := process.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	select {
	case <-fake.processClosed:
	case <-ctx.Done():
		t.Fatalf("process did not close: %v", ctx.Err())
	}
	if err := process.WriteString("after-close"); !errors.Is(err, ErrProcessClosed) {
		t.Fatalf("write after close = %v, want ErrProcessClosed", err)
	}
}

func TestProcessWaitReturnsWhenClosedConcurrently(t *testing.T) {
	fake := &fakeAxernServer{
		files:          map[string][]byte{},
		processStarted: make(chan struct{}),
		processClosed:  make(chan struct{}),
	}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, sandbox := startTestSandbox(t, ctx, dialer)
	defer client.Close()
	defer sandbox.Close(ctx)

	process, err := sandbox.Process(ctx, []string{"cat"}, ProcessOptions{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	select {
	case <-fake.processStarted:
	case <-ctx.Done():
		t.Fatalf("process did not start: %v", ctx.Err())
	}
	waitErr := make(chan error, 1)
	go func() {
		_, err := process.Wait()
		waitErr <- err
	}()
	if err := process.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case err := <-waitErr:
		if err == nil {
			t.Fatal("wait after concurrent close returned nil")
		}
	case <-ctx.Done():
		t.Fatalf("wait did not return after close: %v", ctx.Err())
	}
}

func TestProcessOutputCollectsStdoutStderr(t *testing.T) {
	fake := &fakeAxernServer{
		files:                map[string][]byte{},
		processStdoutChunks:  [][]byte{[]byte("hello "), []byte("world\n")},
		processStderrChunks:  [][]byte{[]byte("warn\n")},
		processExitCode:      7,
		processExitMessage:   "done",
		processSendScripted:  true,
		processCloseOnScript: true,
	}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, sandbox := startTestSandbox(t, ctx, dialer)
	defer client.Close()
	defer sandbox.Close(ctx)

	process, err := sandbox.Process(ctx, []string{"scripted"}, ProcessOptions{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	output, err := process.Output()
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if output.ExitCode != 7 || output.Message != "done" || string(output.Stdout) != "hello world\n" || string(output.Stderr) != "warn\n" {
		t.Fatalf("unexpected output: %+v stdout=%q stderr=%q", output.ProcessResult, output.Stdout, output.Stderr)
	}
	result, err := process.Wait()
	if err != nil {
		t.Fatalf("wait after output: %v", err)
	}
	if result.ExitCode != 7 || result.Message != "done" {
		t.Fatalf("wait result = %+v", result)
	}
}

func TestProcessOutputReportsMissingExit(t *testing.T) {
	fake := &fakeAxernServer{
		files:                map[string][]byte{},
		processStdoutChunks:  [][]byte{[]byte("partial\n")},
		processSendScripted:  true,
		processOmitExit:      true,
		processCloseOnScript: true,
	}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, sandbox := startTestSandbox(t, ctx, dialer)
	defer client.Close()
	defer sandbox.Close(ctx)

	process, err := sandbox.Process(ctx, []string{"missing-exit"}, ProcessOptions{})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	_, err = process.Output()
	if !errors.Is(err, ErrProcessExitMissing) {
		t.Fatalf("output error = %v, want ErrProcessExitMissing", err)
	}
}

func TestExtractSafeTarRejectsExistingSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "link/escape.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len("nope"))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte("nope")); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := extractSafeTar(root, &archive, true); err == nil {
		t.Fatal("extractSafeTar succeeded through symlink parent")
	}
	if _, err := os.Lstat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was created or stat failed: %v", err)
	}
}

func TestExtractSafeTarRejectsExistingSymlinkAncestorBeforeMkdir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "link/newdir/escape.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len("nope"))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte("nope")); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := extractSafeTar(root, &archive, true); err == nil {
		t.Fatal("extractSafeTar succeeded through symlink ancestor")
	}
	if _, err := os.Lstat(filepath.Join(outside, "newdir")); !os.IsNotExist(err) {
		t.Fatalf("outside directory was created or stat failed: %v", err)
	}
}

func startTestSandbox(t *testing.T, ctx context.Context, dialer func(context.Context, string) (net.Conn, error)) (*Client, *Sandbox) {
	t.Helper()
	client, err := NewClient(
		ctx,
		"bufnet",
		WithDialOptions(
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	sandbox, err := NewSandbox(SandboxOptions{
		Client:       client,
		TemplateID:   "python311",
		ReadyTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new sandbox: %v", err)
	}
	if err := sandbox.Start(ctx); err != nil {
		t.Fatalf("start sandbox: %v", err)
	}
	return client, sandbox
}

type fakeAxernServer struct {
	environmentv1.UnimplementedEnvironmentControlServer
	servicev1.UnimplementedServiceControlServer
	gatewayv1.UnimplementedGatewayControlServer
	tunnelcontrolv1.UnimplementedTunnelControlServer
	nodesandboxv1.UnimplementedNodeSandboxServer

	files                  map[string][]byte
	createServiceRequest   *servicev1.CreateServiceRequest
	execArgv               []string
	execImageSpecs         []*nodesandboxv1.ImageProcessSpec
	processImageSpec       *nodesandboxv1.ImageProcessSpec
	mkdirPath              string
	mkdirParents           bool
	removePath             string
	removeForce            bool
	copySrc                string
	copyDst                string
	copyOverwrite          bool
	moveSrc                string
	moveDst                string
	moveOverwrite          bool
	chmodPath              string
	chmodMode              uint32
	touchPath              string
	touchCreate            bool
	touchMtimeNS           int64
	uploadPath             string
	uploadCreateParents    bool
	uploadOverwrite        bool
	uploadSawNestedFile    bool
	deletedService         bool
	deletedEnvironment     bool
	tunnelAllocationID     string
	tunnelLocalTarget      string
	tunnelRemotePort       int32
	revokedTunnelSessionID string
	revokedTunnelReason    string
	processStarted         chan struct{}
	processStartedOnce     sync.Once
	processClosed          chan struct{}
	processClosedOnce      sync.Once
	processStdoutChunks    [][]byte
	processStderrChunks    [][]byte
	processExitCode        int32
	processExitMessage     string
	processSendScripted    bool
	processOmitExit        bool
	processCloseOnScript   bool
	watchMu                sync.Mutex
	watchCalls             int
	watchScripts           [][]int64
	watchErrors            []codes.Code
	capabilityStatusSeen   bool
	computerUseStatusSeen  bool
	computerUseScreenSeen  bool
	computerUseDisplaySeen bool
	computerUseMouseSeen   bool
	computerUseKeySeen     bool
}

func newBufconnServer(t *testing.T, fake *fakeAxernServer) (*grpc.Server, func(context.Context, string) (net.Conn, error)) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	environmentv1.RegisterEnvironmentControlServer(server, fake)
	servicev1.RegisterServiceControlServer(server, fake)
	gatewayv1.RegisterGatewayControlServer(server, fake)
	tunnelcontrolv1.RegisterTunnelControlServer(server, fake)
	nodesandboxv1.RegisterNodeSandboxServer(server, fake)
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("bufconn server stopped: %v", err)
		}
	}()
	return server, func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}
}

func (f *fakeAxernServer) CreateEnvironment(context.Context, *environmentv1.CreateEnvironmentRequest) (*environmentv1.CreateEnvironmentResponse, error) {
	return &environmentv1.CreateEnvironmentResponse{
		Environment: &environmentv1.Environment{ID: "env-1"},
	}, nil
}

func (f *fakeAxernServer) DeleteEnvironment(context.Context, *environmentv1.DeleteEnvironmentRequest) (*environmentv1.DeleteEnvironmentResponse, error) {
	f.deletedEnvironment = true
	return &environmentv1.DeleteEnvironmentResponse{Environment: &environmentv1.Environment{ID: "env-1"}}, nil
}

func (f *fakeAxernServer) CreateService(_ context.Context, request *servicev1.CreateServiceRequest) (*servicev1.CreateServiceResponse, error) {
	f.createServiceRequest = request
	return &servicev1.CreateServiceResponse{
		Service: &servicev1.Service{ID: "svc-1", Version: 1},
	}, nil
}

func (f *fakeAxernServer) WatchService(_ *servicev1.WatchServiceRequest, stream servicev1.ServiceControl_WatchServiceServer) error {
	f.watchMu.Lock()
	call := f.watchCalls
	f.watchCalls++
	versions := []int64{2}
	if call < len(f.watchScripts) {
		versions = append([]int64(nil), f.watchScripts[call]...)
	}
	errorCode := codes.OK
	if call < len(f.watchErrors) {
		errorCode = f.watchErrors[call]
	}
	f.watchMu.Unlock()
	for _, version := range versions {
		if err := stream.Send(&servicev1.WatchServiceResponse{Service: &servicev1.Service{
			ID:            "svc-1",
			Version:       version,
			Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
			ReadyReplicas: 1,
		}}); err != nil {
			return err
		}
	}
	if errorCode != codes.OK {
		return grpcstatus.Error(errorCode, "scripted watch failure")
	}
	return nil
}

func (f *fakeAxernServer) DeleteService(context.Context, *servicev1.DeleteServiceRequest) (*servicev1.DeleteServiceResponse, error) {
	f.deletedService = true
	return &servicev1.DeleteServiceResponse{Service: &servicev1.Service{ID: "svc-1"}}, nil
}

func (f *fakeAxernServer) ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest) (*servicev1.ListServiceReplicasResponse, error) {
	return &servicev1.ListServiceReplicasResponse{
		Replicas: []*servicev1.ServiceReplica{
			{
				ID:      "alloc-1",
				NodeID:  "node-1",
				Attempt: 2,
				Status:  commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
				Ready:   true,
				WorkspacePreparation: &commonv1.WorkspacePreparationFacts{
					PayloadFormat: "nydus",
					PayloadDigest: "sha256:payload",
					CacheHit:      true,
				},
			},
		},
	}, nil
}

func (f *fakeAxernServer) MaterializeTaskAssets(context.Context, *nodesandboxv1.MaterializeTaskAssetsRequest) (*nodesandboxv1.MaterializeTaskAssetsResponse, error) {
	return &nodesandboxv1.MaterializeTaskAssetsResponse{DurationMs: 7}, nil
}

func (f *fakeAxernServer) ResolveAllocationTerminal(context.Context, *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	return &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: "alloc-1",
		NodeID:       "node-1",
		NodeTarget:   "bufnet",
		Attempt:      2,
		Lease: &commonv1.ExecutionLease{
			PlaintextToken: "lease-token",
		},
	}, nil
}

func (f *fakeAxernServer) CreateTunnelSession(_ context.Context, request *tunnelcontrolv1.CreateTunnelSessionRequest) (*tunnelcontrolv1.CreateTunnelSessionResponse, error) {
	f.tunnelAllocationID = request.GetAllocationID()
	f.tunnelLocalTarget = request.GetLocalTarget()
	f.tunnelRemotePort = request.GetRemotePort()
	return &tunnelcontrolv1.CreateTunnelSessionResponse{
		Session:     fakeTunnelSession(),
		ClientToken: "client-token",
	}, nil
}

func (f *fakeAxernServer) GetTunnelSession(context.Context, *tunnelcontrolv1.GetTunnelSessionRequest) (*tunnelcontrolv1.GetTunnelSessionResponse, error) {
	return &tunnelcontrolv1.GetTunnelSessionResponse{Session: fakeTunnelSession()}, nil
}

func (f *fakeAxernServer) ListTunnelSessionEvents(context.Context, *tunnelcontrolv1.ListTunnelSessionEventsRequest) (*tunnelcontrolv1.ListTunnelSessionEventsResponse, error) {
	return &tunnelcontrolv1.ListTunnelSessionEventsResponse{
		Events: []*tunnelcontrolv1.TunnelSessionEvent{
			{
				SessionID: "tun-1",
				EventType: tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED,
			},
		},
	}, nil
}

func (f *fakeAxernServer) RevokeTunnelSession(_ context.Context, request *tunnelcontrolv1.RevokeTunnelSessionRequest) (*tunnelcontrolv1.RevokeTunnelSessionResponse, error) {
	f.revokedTunnelSessionID = request.GetSessionID()
	f.revokedTunnelReason = request.GetReason()
	return &tunnelcontrolv1.RevokeTunnelSessionResponse{Session: fakeTunnelSession()}, nil
}

func (f *fakeAxernServer) RenewTunnelSession(context.Context, *tunnelcontrolv1.RenewTunnelSessionRequest) (*tunnelcontrolv1.RenewTunnelSessionResponse, error) {
	return &tunnelcontrolv1.RenewTunnelSessionResponse{Session: fakeTunnelSession()}, nil
}

func fakeTunnelSession() *tunnelcontrolv1.TunnelSession {
	return &tunnelcontrolv1.TunnelSession{
		SessionID:        "tun-1",
		AllocationID:     "alloc-1",
		NodeID:           "node-1",
		RemotePort:       9000,
		LocalTarget:      "127.0.0.1:8080",
		EdgeTarget:       "gateway.example:25000",
		ClientEdgeTarget: "gateway.example:25000",
		BoundAddr:        "127.0.0.1:9000",
		Status:           tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
	}
}

func (f *fakeAxernServer) Exec(_ context.Context, request *nodesandboxv1.ExecRequest) (*nodesandboxv1.ExecResponse, error) {
	f.execArgv = append([]string(nil), request.GetSpec().GetArgv()...)
	return &nodesandboxv1.ExecResponse{
		ExitCode: 0,
		Stdout:   []byte("hello\n"),
	}, nil
}

func (f *fakeAxernServer) ExecImage(_ context.Context, request *nodesandboxv1.ExecImageRequest) (*nodesandboxv1.ExecImageResponse, error) {
	f.execImageSpecs = append(f.execImageSpecs, request.GetSpec())
	return &nodesandboxv1.ExecImageResponse{
		ExitCode: 0,
		Stdout:   []byte("image\n"),
	}, nil
}

func (f *fakeAxernServer) Process(stream nodesandboxv1.NodeSandbox_ProcessServer) error {
	if f.processClosed != nil {
		defer f.processClosedOnce.Do(func() { close(f.processClosed) })
	}
	request, err := stream.Recv()
	if err != nil {
		return err
	}
	if request.GetOpen() == nil {
		return nil
	}
	if f.processStarted != nil {
		f.processStartedOnce.Do(func() { close(f.processStarted) })
	}
	if err := stream.Send(&nodesandboxv1.ProcessResponse{
		Payload: &nodesandboxv1.ProcessResponse_Ready{Ready: &nodesandboxv1.ProcessReady{}},
	}); err != nil {
		return err
	}
	if f.processSendScripted {
		for _, chunk := range f.processStdoutChunks {
			if err := stream.Send(&nodesandboxv1.ProcessResponse{
				Payload: &nodesandboxv1.ProcessResponse_Stdout{Stdout: append([]byte(nil), chunk...)},
			}); err != nil {
				return err
			}
		}
		for _, chunk := range f.processStderrChunks {
			if err := stream.Send(&nodesandboxv1.ProcessResponse{
				Payload: &nodesandboxv1.ProcessResponse_Stderr{Stderr: append([]byte(nil), chunk...)},
			}); err != nil {
				return err
			}
		}
		if !f.processOmitExit {
			if err := stream.Send(&nodesandboxv1.ProcessResponse{
				Payload: &nodesandboxv1.ProcessResponse_Exit{Exit: &nodesandboxv1.ExecExit{ExitCode: f.processExitCode, Message: f.processExitMessage}},
			}); err != nil {
				return err
			}
		}
		if f.processCloseOnScript {
			return nil
		}
	}
	var stdin []byte
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch payload := request.GetPayload().(type) {
		case *nodesandboxv1.ProcessRequest_Stdin:
			stdin = append(stdin, payload.Stdin...)
		case *nodesandboxv1.ProcessRequest_CloseStdin:
			if err := stream.Send(&nodesandboxv1.ProcessResponse{
				Payload: &nodesandboxv1.ProcessResponse_Stdout{Stdout: append([]byte(nil), stdin...)},
			}); err != nil {
				return err
			}
			return stream.Send(&nodesandboxv1.ProcessResponse{
				Payload: &nodesandboxv1.ProcessResponse_Exit{Exit: &nodesandboxv1.ExecExit{ExitCode: 0}},
			})
		}
	}
}

func (f *fakeAxernServer) ProcessImage(stream nodesandboxv1.NodeSandbox_ProcessImageServer) error {
	request, err := stream.Recv()
	if err != nil {
		return err
	}
	if request.GetOpen() == nil {
		return nil
	}
	f.processImageSpec = request.GetOpen().GetSpec()
	if err := stream.Send(&nodesandboxv1.ProcessImageResponse{
		Payload: &nodesandboxv1.ProcessImageResponse_Ready{Ready: &nodesandboxv1.ProcessReady{}},
	}); err != nil {
		return err
	}
	var stdin []byte
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch payload := request.GetPayload().(type) {
		case *nodesandboxv1.ProcessImageRequest_Stdin:
			stdin = append(stdin, payload.Stdin...)
		case *nodesandboxv1.ProcessImageRequest_CloseStdin:
			if err := stream.Send(&nodesandboxv1.ProcessImageResponse{
				Payload: &nodesandboxv1.ProcessImageResponse_Stdout{Stdout: append([]byte(nil), stdin...)},
			}); err != nil {
				return err
			}
			return stream.Send(&nodesandboxv1.ProcessImageResponse{
				Payload: &nodesandboxv1.ProcessImageResponse_Exit{Exit: &nodesandboxv1.ExecExit{ExitCode: 0}},
			})
		}
	}
}

func (f *fakeAxernServer) ReadFile(_ context.Context, request *nodesandboxv1.ReadFileRequest) (*nodesandboxv1.ReadFileResponse, error) {
	return &nodesandboxv1.ReadFileResponse{Data: append([]byte(nil), f.files[request.GetPath()]...)}, nil
}

func (f *fakeAxernServer) WriteFile(_ context.Context, request *nodesandboxv1.WriteFileRequest) (*nodesandboxv1.WriteFileResponse, error) {
	f.files[request.GetPath()] = append([]byte(nil), request.GetData()...)
	return &nodesandboxv1.WriteFileResponse{}, nil
}

func (f *fakeAxernServer) StatFile(_ context.Context, request *nodesandboxv1.StatFileRequest) (*nodesandboxv1.StatFileResponse, error) {
	return &nodesandboxv1.StatFileResponse{Info: f.fileInfo(request.GetPath())}, nil
}

func (f *fakeAxernServer) ListDir(context.Context, *nodesandboxv1.ListDirRequest) (*nodesandboxv1.ListDirResponse, error) {
	return &nodesandboxv1.ListDirResponse{Entries: []*filev1.SandboxFileInfo{f.fileInfo("/tmp/message.txt")}}, nil
}

func (f *fakeAxernServer) Exists(_ context.Context, request *nodesandboxv1.ExistsRequest) (*nodesandboxv1.ExistsResponse, error) {
	_, ok := f.files[request.GetPath()]
	return &nodesandboxv1.ExistsResponse{Exists: ok}, nil
}

func (f *fakeAxernServer) Mkdir(_ context.Context, request *nodesandboxv1.MkdirRequest) (*nodesandboxv1.MkdirResponse, error) {
	f.mkdirPath = request.GetPath()
	f.mkdirParents = request.GetParents()
	return &nodesandboxv1.MkdirResponse{}, nil
}

func (f *fakeAxernServer) Remove(_ context.Context, request *nodesandboxv1.RemoveRequest) (*nodesandboxv1.RemoveResponse, error) {
	f.removePath = request.GetPath()
	f.removeForce = request.GetForce()
	delete(f.files, request.GetPath())
	return &nodesandboxv1.RemoveResponse{}, nil
}

func (f *fakeAxernServer) Copy(_ context.Context, request *nodesandboxv1.CopyRequest) (*nodesandboxv1.CopyResponse, error) {
	f.copySrc = request.GetSrcPath()
	f.copyDst = request.GetDstPath()
	f.copyOverwrite = request.GetOverwrite()
	f.files[request.GetDstPath()] = append([]byte(nil), f.files[request.GetSrcPath()]...)
	return &nodesandboxv1.CopyResponse{}, nil
}

func (f *fakeAxernServer) Move(_ context.Context, request *nodesandboxv1.MoveRequest) (*nodesandboxv1.MoveResponse, error) {
	f.moveSrc = request.GetSrcPath()
	f.moveDst = request.GetDstPath()
	f.moveOverwrite = request.GetOverwrite()
	f.files[request.GetDstPath()] = append([]byte(nil), f.files[request.GetSrcPath()]...)
	delete(f.files, request.GetSrcPath())
	return &nodesandboxv1.MoveResponse{}, nil
}

func (f *fakeAxernServer) Chmod(_ context.Context, request *nodesandboxv1.ChmodRequest) (*nodesandboxv1.ChmodResponse, error) {
	f.chmodPath = request.GetPath()
	f.chmodMode = request.GetMode()
	return &nodesandboxv1.ChmodResponse{}, nil
}

func (f *fakeAxernServer) Touch(_ context.Context, request *nodesandboxv1.TouchRequest) (*nodesandboxv1.TouchResponse, error) {
	f.touchPath = request.GetPath()
	f.touchCreate = request.GetCreate()
	f.touchMtimeNS = request.GetMtimeNs()
	if request.GetCreate() {
		f.files[request.GetPath()] = append([]byte(nil), f.files[request.GetPath()]...)
	}
	return &nodesandboxv1.TouchResponse{}, nil
}

func (f *fakeAxernServer) ComputerUseStatus(context.Context, *nodesandboxv1.ComputerUseStatusRequest) (*nodesandboxv1.ComputerUseStatusResponse, error) {
	f.computerUseStatusSeen = true
	return &nodesandboxv1.ComputerUseStatusResponse{
		Available: true,
		Display:   ":99",
		Backend:   "x11",
	}, nil
}

func (f *fakeAxernServer) CapabilityStatus(context.Context, *nodesandboxv1.CapabilityStatusRequest) (*nodesandboxv1.CapabilityStatusResponse, error) {
	f.capabilityStatusSeen = true
	return &nodesandboxv1.CapabilityStatusResponse{
		Ready:        true,
		Capabilities: []string{"health", "status", "process", "pty", "computer_use"},
		Providers: []*nodesandboxv1.CapabilityProviderStatus{
			{
				Name:         "process",
				State:        "available",
				Available:    true,
				Capabilities: []string{"process", "pty"},
			},
			{
				Name:         "computer_use",
				State:        "available",
				Available:    true,
				Capabilities: []string{"computer_use"},
				Backend:      "x11",
				Dependencies: []*nodesandboxv1.CapabilityDependencyStatus{{
					Name:      "xdotool",
					Available: true,
				}},
			},
		},
		ProviderSummary: &nodesandboxv1.CapabilityProviderSummary{
			Total:     2,
			Available: 2,
		},
	}, nil
}

func (f *fakeAxernServer) ComputerUseScreenshot(_ context.Context, request *nodesandboxv1.ComputerUseScreenshotRequest) (*nodesandboxv1.ComputerUseScreenshotResponse, error) {
	f.computerUseScreenSeen = true
	if request.GetRegion().GetWidth() != 3 || request.GetFormat() != "jpeg" || request.GetQuality() != 70 {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "bad screenshot request")
	}
	return &nodesandboxv1.ComputerUseScreenshotResponse{
		Data:        []byte("png"),
		ContentType: "image/png",
	}, nil
}

func (f *fakeAxernServer) ComputerUseDisplay(context.Context, *nodesandboxv1.ComputerUseDisplayRequest) (*nodesandboxv1.ComputerUseDisplayResponse, error) {
	f.computerUseDisplaySeen = true
	return &nodesandboxv1.ComputerUseDisplayResponse{
		Display: ":99",
		Backend: "x11",
		Width:   1280,
		Height:  720,
	}, nil
}

func (f *fakeAxernServer) ComputerUseMouse(_ context.Context, request *nodesandboxv1.ComputerUseMouseRequest) (*nodesandboxv1.ComputerUseMouseResponse, error) {
	f.computerUseMouseSeen = true
	if request.GetAction() != "move" || request.GetX() != 7 || request.GetY() != 9 {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "bad mouse request")
	}
	return &nodesandboxv1.ComputerUseMouseResponse{}, nil
}

func (f *fakeAxernServer) ComputerUseKeyboard(_ context.Context, request *nodesandboxv1.ComputerUseKeyboardRequest) (*nodesandboxv1.ComputerUseKeyboardResponse, error) {
	f.computerUseKeySeen = true
	if request.GetKey() != "Escape" {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "bad keyboard request")
	}
	return &nodesandboxv1.ComputerUseKeyboardResponse{}, nil
}

func (f *fakeAxernServer) fileInfo(path string) *filev1.SandboxFileInfo {
	return &filev1.SandboxFileInfo{
		Path:    path,
		Kind:    filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE,
		Size:    int64(len(f.files[path])),
		Mode:    0o644,
		MtimeNs: 7,
	}
}

func (f *fakeAxernServer) UploadArchive(stream nodesandboxv1.NodeSandbox_UploadArchiveServer) error {
	request, err := stream.Recv()
	if err != nil {
		return err
	}
	open := request.GetOpen()
	if open == nil {
		return nil
	}
	f.uploadPath = open.GetPath()
	f.uploadCreateParents = open.GetCreateParents()
	f.uploadOverwrite = open.GetOverwrite()
	var archive bytes.Buffer
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		archive.Write(request.GetChunk())
	}
	tr := tar.NewReader(bytes.NewReader(archive.Bytes()))
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name == "nested/data.txt" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			f.uploadSawNestedFile = string(data) == "archive-ok\n"
		}
	}
	return stream.SendAndClose(&nodesandboxv1.UploadArchiveResponse{})
}

func (f *fakeAxernServer) DownloadArchive(request *nodesandboxv1.DownloadArchiveRequest, stream nodesandboxv1.NodeSandbox_DownloadArchiveServer) error {
	if request.GetPath() != "/tmp/tree" {
		return nil
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "nested/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		return err
	}
	data := []byte("archive-ok\n")
	if err := tw.WriteHeader(&tar.Header{Name: "nested/data.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return stream.Send(&nodesandboxv1.DownloadArchiveResponse{Chunk: archive.Bytes()})
}
