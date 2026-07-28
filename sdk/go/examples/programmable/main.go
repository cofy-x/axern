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
	"github.com/cofy-x/axern/sdk/go/examples/internal/exampleutil"
)

func main() {
	config := exampleutil.Flags()
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := exampleutil.NewClient(ctx, config)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	sandbox, err := exampleutil.StartSandbox(ctx, client, config)
	if err != nil {
		log.Fatal(err)
	}
	defer sandbox.Close(context.Background())

	root, err := os.MkdirTemp("", "axern-go-programmable-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root)
	upload := filepath.Join(root, "upload")
	download := filepath.Join(root, "download")
	if err := os.MkdirAll(filepath.Join(upload, "input"), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upload, "input", "message.txt"), []byte("programmable sandbox\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	if err := sandbox.UploadDir(ctx, upload, "/tmp/axern-programmable", axern.UploadDirOptions{}); err != nil {
		log.Fatal(err)
	}

	process, err := sandbox.Process(ctx, axern.Args("python", "-u", "-c", strings.Join([]string{
		"from pathlib import Path",
		"p = Path('/tmp/axern-programmable/input/message.txt')",
		"text = p.read_text().upper()",
		"Path('/tmp/axern-programmable/output').mkdir(parents=True, exist_ok=True)",
		"Path('/tmp/axern-programmable/output/result.txt').write_text(text)",
		"print(text.strip())",
	}, "; ")), axern.ProcessOptions{Timeout: 30 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	output, err := process.Output()
	if closeErr := process.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		log.Fatal(err)
	}
	if output.ExitCode != 0 {
		log.Fatalf("process exited with %d: %s stderr=%q", output.ExitCode, output.Message, output.Stderr)
	}
	fmt.Println(strings.TrimSpace(string(output.Stdout)))

	if err := sandbox.DownloadDir(ctx, "/tmp/axern-programmable/output", download, axern.DownloadDirOptions{}); err != nil {
		log.Fatal(err)
	}
	result, err := os.ReadFile(filepath.Join(download, "result.txt"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(result))

	upstream, stop := startLocalUpstream("hello through tunnel")
	defer stop()
	tunnel, err := sandbox.OpenTunnel(ctx, axern.TunnelOptions{
		Upstream: upstream,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer tunnel.Close(context.Background())

	resultExec, err := sandbox.Exec(ctx, axern.Args("python", "-c", fmt.Sprintf("import urllib.request; print(urllib.request.urlopen('http://%s/index.txt', timeout=10).read().decode().strip())", tunnel.BoundAddr())), axern.ExecOptions{Check: true})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resultExec.StdoutString())
}

func startLocalUpstream(body string) (string, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
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
