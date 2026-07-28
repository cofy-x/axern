package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	axern "github.com/cofy-x/axern/sdk/go"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

func main() {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Fatal(recovered)
		}
	}()
	var endpoint string
	var tlsCACert string
	var tlsCert string
	var tlsKey string
	var tlsServerName string
	var runtimeClass string
	flag.StringVar(&endpoint, "endpoint", "", "gateway gRPC endpoint")
	flag.StringVar(&tlsCACert, "tls-ca-cert", "", "control plane TLS CA certificate")
	flag.StringVar(&tlsCert, "tls-cert", "", "control plane TLS client certificate")
	flag.StringVar(&tlsKey, "tls-key", "", "control plane TLS client key")
	flag.StringVar(&tlsServerName, "tls-server-name", "", "gateway TLS server name")
	flag.StringVar(&runtimeClass, "runtime-class", "runsc", "sandbox runtime class")
	flag.Parse()
	if endpoint == "" {
		failf("--endpoint is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	options := []axern.ClientOption{axern.WithTLS(tlsCACert, tlsCert, tlsKey, tlsServerName)}
	client, err := axern.NewClient(ctx, endpoint, options...)
	if err != nil {
		failf("new client: %v", err)
	}
	defer client.Close()

	sandbox, err := axern.NewSandbox(axern.SandboxOptions{
		Client:       client,
		TemplateID:   "python311",
		RuntimeClass: runtimeClass,
		Argv:         []string{"python", "-c", "import time; time.sleep(600)"},
		ReadyTimeout: 180 * time.Second,
	})
	if err != nil {
		failf("new sandbox: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer closeCancel()
		if err := sandbox.Close(closeCtx); err != nil {
			log.Printf("close sandbox: %v", err)
		}
	}()
	if err := sandbox.Start(ctx); err != nil {
		failf("start sandbox: %v", err)
	}
	upstream, stopUpstream := startLocalUpstream("go-sdk-tunnel-ok")
	defer stopUpstream()
	tunnel, err := sandbox.OpenTunnel(ctx, axern.TunnelOptions{
		Upstream: upstream,
	})
	if err != nil {
		failf("open tunnel: %v", err)
	}
	metadata, err := sandbox.Metadata()
	if err != nil {
		failf("metadata after tunnel: %v", err)
	}
	if metadata.TunnelSessionID != tunnel.SessionID() || metadata.BoundAddr != tunnel.BoundAddr() {
		failf("metadata tunnel mismatch metadata=%+v tunnel=%s/%s", metadata, tunnel.SessionID(), tunnel.BoundAddr())
	}
	tunnelResult, err := sandbox.Exec(ctx, []string{"python", "-c", fmt.Sprintf("import urllib.request; print(urllib.request.urlopen('http://%s/index.txt', timeout=10).read().decode().strip())", tunnel.BoundAddr())}, axern.ExecOptions{Check: true})
	if err != nil {
		failf("tunnel exec fetch: %v", err)
	}
	if strings.TrimSpace(tunnelResult.StdoutString()) != "go-sdk-tunnel-ok" {
		failf("unexpected tunnel response: %q", tunnelResult.StdoutString())
	}
	events, err := client.ListTunnelSessionEvents(ctx, tunnel.SessionID(), 30)
	if err != nil {
		failf("list tunnel events: %v", err)
	}
	if !hasTunnelEvent(events, "TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED") {
		failf("missing client connected tunnel event: %+v", events)
	}
	metadata, err = sandbox.Metadata()
	if err != nil {
		failf("metadata: %v", err)
	}
	if metadata.AllocationID == "" || metadata.ServiceID == "" {
		failf("metadata missing ids: %+v", metadata)
	}
	result, err := sandbox.Exec(ctx, []string{"python", "-c", "print('go-sdk-ok')"}, axern.ExecOptions{Check: true})
	if err != nil {
		failf("exec: %v", err)
	}
	if strings.TrimSpace(result.StdoutString()) != "go-sdk-ok" {
		failf("unexpected exec stdout: %q", result.StdoutString())
	}
	if runtimeClass == "runsc" {
		if err := sandbox.WriteFile(ctx, "/tmp/axern-go-sdk.txt", []byte("file-ok\n"), axern.WriteFileOptions{CreateParents: true}); err != nil {
			failf("write file: %v", err)
		}
		data, err := sandbox.ReadFile(ctx, "/tmp/axern-go-sdk.txt")
		if err != nil {
			failf("read file: %v", err)
		}
		if string(data) != "file-ok\n" {
			failf("read file = %q", data)
		}
		exists, err := sandbox.Exists(ctx, "/tmp/axern-go-sdk.txt")
		if err != nil {
			failf("exists: %v", err)
		}
		if !exists {
			failf("exists returned false for written file")
		}
		info, err := sandbox.Stat(ctx, "/tmp/axern-go-sdk.txt")
		if err != nil {
			failf("stat: %v", err)
		}
		if info.Size != int64(len("file-ok\n")) {
			failf("stat size = %d", info.Size)
		}
		if err := sandbox.Copy(ctx, "/tmp/axern-go-sdk.txt", "/tmp/axern-go-sdk-copy.txt", axern.CopyOptions{Overwrite: true}); err != nil {
			failf("copy: %v", err)
		}
		if err := sandbox.Move(ctx, "/tmp/axern-go-sdk-copy.txt", "/tmp/axern-go-sdk-moved.txt", axern.MoveOptions{Overwrite: true}); err != nil {
			failf("move: %v", err)
		}
		if err := sandbox.Chmod(ctx, "/tmp/axern-go-sdk-moved.txt", 0o600, axern.ChmodOptions{}); err != nil {
			failf("chmod: %v", err)
		}
		if err := sandbox.Touch(ctx, "/tmp/axern-go-sdk-moved.txt", axern.TouchOptions{}); err != nil {
			failf("touch: %v", err)
		}
		if err := sandbox.Mkdir(ctx, "/tmp/axern-go-sdk-dir", axern.MkdirOptions{Parents: true}); err != nil {
			failf("mkdir: %v", err)
		}
		if err := sandbox.Remove(ctx, "/tmp/axern-go-sdk-dir", axern.RemoveOptions{Recursive: true, Force: true}); err != nil {
			failf("remove: %v", err)
		}
	}

	process, err := sandbox.Process(ctx, []string{"python", "-u", "-c", "import sys; print(sys.stdin.read().upper())"}, axern.ProcessOptions{Timeout: 15 * time.Second})
	if err != nil {
		failf("process: %v", err)
	}
	defer process.Close()
	if err := process.WriteString("process-ok\n"); err != nil {
		failf("process write: %v", err)
	}
	if err := process.CloseStdin(); err != nil {
		failf("process close stdin: %v", err)
	}
	processOutput, err := process.Output()
	if err != nil {
		failf("process output: %v", err)
	}
	if processOutput.ExitCode != 0 {
		failf("process exit = %d: %s", processOutput.ExitCode, processOutput.Message)
	}
	if strings.TrimSpace(string(processOutput.Stdout)) != "PROCESS-OK" {
		failf("process stdout = %q", processOutput.Stdout)
	}
	if imageProcessImage := os.Getenv("AXERN_GO_SDK_E2E_IMAGE_PROCESS_IMAGE"); imageProcessImage != "" {
		runImageProcessE2E(ctx, client, runtimeClass, imageProcessImage)
	}

	if runtimeClass == "runsc" {
		root, err := os.MkdirTemp("", "axern-go-sdk-e2e-*")
		if err != nil {
			failf("tempdir: %v", err)
		}
		defer os.RemoveAll(root)
		upload := filepath.Join(root, "upload")
		download := filepath.Join(root, "download")
		if err := os.MkdirAll(filepath.Join(upload, "nested"), 0o755); err != nil {
			failf("mkdir upload: %v", err)
		}
		if err := os.WriteFile(filepath.Join(upload, "nested", "data.txt"), []byte("archive-ok\n"), 0o644); err != nil {
			failf("write upload: %v", err)
		}
		if err := sandbox.UploadDir(ctx, upload, "/tmp/axern-go-sdk-tree", axern.UploadDirOptions{}); err != nil {
			failf("upload dir: %v", err)
		}
		process, err := sandbox.Process(ctx, []string{"python", "-u", "-c", strings.Join([]string{
			"from pathlib import Path",
			"p = Path('/tmp/axern-go-sdk-tree/nested/data.txt')",
			"data = p.read_text()",
			"print(data.strip())",
			"p.write_text(data + 'process-archive-ok\\n')",
		}, "; ")}, axern.ProcessOptions{Timeout: 15 * time.Second})
		if err != nil {
			failf("archive process: %v", err)
		}
		archiveProcessOutput, err := process.Output()
		if closeErr := process.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			failf("archive process output: %v", err)
		}
		if archiveProcessOutput.ExitCode != 0 {
			failf("archive process exit = %d: %s stderr=%q", archiveProcessOutput.ExitCode, archiveProcessOutput.Message, archiveProcessOutput.Stderr)
		}
		if strings.TrimSpace(string(archiveProcessOutput.Stdout)) != "archive-ok" {
			failf("archive process stdout = %q", archiveProcessOutput.Stdout)
		}
		if err := sandbox.DownloadDir(ctx, "/tmp/axern-go-sdk-tree", download, axern.DownloadDirOptions{}); err != nil {
			failf("download dir: %v", err)
		}
		downloaded, err := os.ReadFile(filepath.Join(download, "nested", "data.txt"))
		if err != nil {
			failf("read downloaded archive file: %v", err)
		}
		if string(downloaded) != "archive-ok\nprocess-archive-ok\n" {
			failf("downloaded archive content = %q", downloaded)
		}
	}

	if _, err := io.WriteString(os.Stdout, fmt.Sprintf("go_sdk_sandbox_e2e_ok=true runtime_class=%s service_id=%s allocation_id=%s\n", runtimeClass, metadata.ServiceID, metadata.AllocationID)); err != nil {
		failf("%v", err)
	}
}

func runImageProcessE2E(ctx context.Context, client *axern.Client, runtimeClass, image string) {
	namespace := fmt.Sprintf("go-sdk-image-process-%d", time.Now().UnixNano())
	sandbox, err := axern.NewSandbox(axern.SandboxOptions{
		Client:       client,
		TemplateID:   "python311",
		Namespace:    namespace,
		RuntimeClass: runtimeClass,
		Argv:         []string{"python", "-c", "import time; time.sleep(600)"},
		ReadyTimeout: 180 * time.Second,
		Volumes: []axern.VolumeMount{{
			Name:    "workspace",
			Target:  "/workspace",
			Options: []string{"rbind"},
		}},
	})
	if err != nil {
		failf("new image process sandbox: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer closeCancel()
		if err := sandbox.Close(closeCtx); err != nil {
			log.Printf("close image process sandbox: %v", err)
		}
	}()
	if err := sandbox.Start(ctx); err != nil {
		failf("start image process sandbox: %v", err)
	}
	if _, err := sandbox.Exec(ctx, []string{"/bin/sh", "-lc", "printf task-input >/workspace/input.txt"}, axern.ExecOptions{Check: true}); err != nil {
		failf("seed image process workspace: %v", err)
	}
	result, err := sandbox.ExecImage(ctx, image, []string{"/bin/sh", "-lc", "cat /workspace/input.txt; printf image-result >/workspace/output.txt; printf image-mutated >/workspace/input.txt"}, axern.ImageExecOptions{
		Check:   true,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		failf("exec image process: %v", err)
	}
	if strings.TrimSpace(result.StdoutString()) != "task-input" {
		failf("exec image process stdout = %q", result.StdoutString())
	}
	readBack, err := sandbox.Exec(ctx, []string{"/bin/sh", "-lc", "cat /workspace/output.txt"}, axern.ExecOptions{Check: true})
	if err != nil {
		failf("read image process output file: %v", err)
	}
	if strings.TrimSpace(readBack.StdoutString()) != "image-result" {
		failf("image process output file = %q", readBack.StdoutString())
	}
	mutated, err := sandbox.Exec(ctx, []string{"/bin/sh", "-lc", "cat /workspace/input.txt"}, axern.ExecOptions{Check: true})
	if err != nil {
		failf("read image process mutated file: %v", err)
	}
	if strings.TrimSpace(mutated.StdoutString()) != "image-mutated" {
		failf("image process mutated file = %q", mutated.StdoutString())
	}

	process, err := sandbox.ProcessImage(ctx, image, []string{"/bin/sh", "-lc", "tr a-z A-Z"}, axern.ImageProcessOptions{Timeout: 30 * time.Second})
	if err != nil {
		failf("process image: %v", err)
	}
	defer process.Close()
	if err := process.WriteString("stream-ok\n"); err != nil {
		failf("process image write: %v", err)
	}
	if err := process.CloseStdin(); err != nil {
		failf("process image close stdin: %v", err)
	}
	output, err := process.Output()
	if err != nil {
		failf("process image output: %v", err)
	}
	if output.ExitCode != 0 {
		failf("process image exit = %d: %s stderr=%q", output.ExitCode, output.Message, output.Stderr)
	}
	if strings.TrimSpace(string(output.Stdout)) != "STREAM-OK" {
		failf("process image stdout = %q", output.Stdout)
	}
	if imageProcessLoopbackEnabled() {
		runImageProcessLoopbackE2E(ctx, sandbox, runtimeClass, image)
	}
	fmt.Printf("go_sdk_image_process_e2e_ok=true runtime_class=%s image=%s\n", runtimeClass, image)
	runImageProcessNegativeE2E(ctx, client, runtimeClass, image)
}

func imageProcessLoopbackEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("AXERN_GO_SDK_E2E_IMAGE_PROCESS_LOOPBACK")))
	return value == "1" || value == "true" || value == "yes"
}

func runImageProcessLoopbackE2E(ctx context.Context, sandbox *axern.Sandbox, runtimeClass, image string) {
	const loopbackAddr = "127.0.0.1:17653"
	server, err := sandbox.Process(ctx, []string{"python", "-u", "-c", strings.Join([]string{
		"import http.server",
		"class Handler(http.server.BaseHTTPRequestHandler):",
		"    def do_GET(self):",
		"        self.send_response(200)",
		"        self.end_headers()",
		"        self.wfile.write(b'image-process-loopback-ok\\n')",
		"    def log_message(self, *args):",
		"        pass",
		"http.server.ThreadingHTTPServer(('127.0.0.1', 17653), Handler).serve_forever()",
	}, "\n")}, axern.ProcessOptions{Timeout: 2 * time.Minute})
	if err != nil {
		failf("start image process loopback server: %v", err)
	}
	defer func() {
		_ = server.Terminate()
		_ = server.Close()
	}()
	waitForSandboxHTTP(ctx, sandbox, loopbackAddr)

	execResult, err := sandbox.ExecImage(ctx, image, []string{"/bin/sh", "-lc", "curl -fsS http://" + loopbackAddr + "/index.txt"}, axern.ImageExecOptions{
		Check:   true,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		failf("exec image loopback fetch: %v", err)
	}
	if strings.TrimSpace(execResult.StdoutString()) != "image-process-loopback-ok" {
		failf("exec image loopback stdout = %q", execResult.StdoutString())
	}

	process, err := sandbox.ProcessImage(ctx, image, []string{"/bin/sh", "-lc", "curl -fsS http://" + loopbackAddr + "/index.txt"}, axern.ImageProcessOptions{Timeout: 30 * time.Second})
	if err != nil {
		failf("process image loopback fetch: %v", err)
	}
	defer process.Close()
	if err := process.CloseStdin(); err != nil {
		failf("process image loopback close stdin: %v", err)
	}
	output, err := process.Output()
	if err != nil {
		failf("process image loopback output: %v", err)
	}
	if output.ExitCode != 0 {
		failf("process image loopback exit = %d: %s stderr=%q", output.ExitCode, output.Message, output.Stderr)
	}
	if strings.TrimSpace(string(output.Stdout)) != "image-process-loopback-ok" {
		failf("process image loopback stdout = %q", output.Stdout)
	}
	fmt.Printf("go_sdk_image_process_loopback_e2e_ok=true runtime_class=%s image=%s\n", runtimeClass, image)
}

func waitForSandboxHTTP(ctx context.Context, sandbox *axern.Sandbox, addr string) {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		result, err := sandbox.Exec(ctx, []string{"python", "-c", fmt.Sprintf("import urllib.request; print(urllib.request.urlopen('http://%s/index.txt', timeout=2).read().decode().strip())", addr)}, axern.ExecOptions{Check: true, Timeout: 5 * time.Second})
		if err == nil && strings.TrimSpace(result.StdoutString()) == "image-process-loopback-ok" {
			return
		}
		lastErr = err
		select {
		case <-ctx.Done():
			failf("wait for sandbox loopback http: %v", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	failf("wait for sandbox loopback http at %s: %v", addr, lastErr)
}

func runImageProcessNegativeE2E(ctx context.Context, client *axern.Client, runtimeClass, image string) {
	namespace := fmt.Sprintf("go-sdk-image-process-negative-%d", time.Now().UnixNano())
	sandbox, err := axern.NewSandbox(axern.SandboxOptions{
		Client:       client,
		TemplateID:   "python311",
		Namespace:    namespace,
		RuntimeClass: runtimeClass,
		Argv:         []string{"python", "-c", "import time; time.sleep(600)"},
		ReadyTimeout: 180 * time.Second,
	})
	if err != nil {
		failf("new negative image process sandbox: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer closeCancel()
		if err := sandbox.Close(closeCtx); err != nil {
			log.Printf("close negative image process sandbox: %v", err)
		}
	}()
	if err := sandbox.Start(ctx); err != nil {
		failf("start negative image process sandbox: %v", err)
	}
	_, err = sandbox.ExecImage(ctx, image, []string{"/bin/sh", "-lc", "true"}, axern.ImageExecOptions{Timeout: 30 * time.Second})
	if err == nil {
		failf("negative image process exec unexpectedly succeeded without host-backed /workspace")
	}
	if !imageProcessHostBackedMountError(err) {
		failf("negative image process error = %v, want host-backed mount failed precondition", err)
	}
	fmt.Printf("go_sdk_image_process_negative_e2e_ok=true runtime_class=%s image=%s\n", runtimeClass, image)
}

func imageProcessHostBackedMountError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	failedPrecondition := strings.Contains(msg, "failedprecondition") || strings.Contains(msg, "failed precondition")
	return failedPrecondition && strings.Contains(msg, "host-backed")
}

func startLocalUpstream(body string) (string, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		failf("listen local upstream: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/index.txt", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, body+"\n")
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("local upstream server: %v", err)
		}
	}()
	return listener.Addr().String(), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
}

func hasTunnelEvent(events []*tunnelcontrolv1.TunnelSessionEvent, want string) bool {
	for _, event := range events {
		if event.GetEventType().String() == want {
			return true
		}
	}
	return false
}

func failf(format string, args ...any) {
	panic(fmt.Errorf(format, args...))
}
