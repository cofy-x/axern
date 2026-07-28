package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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

	upstream, stopUpstream, err := startLocalUpstream()
	if err != nil {
		log.Fatal(err)
	}
	defer stopUpstream()

	sandbox, err := exampleutil.StartSandbox(ctx, client, config)
	if err != nil {
		log.Fatal(err)
	}
	defer sandbox.Close(context.Background())

	tunnel, err := sandbox.OpenTunnel(ctx, axern.TunnelOptions{
		Upstream: upstream,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("sandbox tunnel bound at http://%s\n", tunnel.BoundAddr())

	command := fmt.Sprintf("python -c \"import urllib.request; print(urllib.request.urlopen('http://%s/index.txt', timeout=10).read().decode().strip())\"", tunnel.BoundAddr())
	result, err := sandbox.Exec(ctx, axern.Shell(command), axern.ExecOptions{Check: true})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(result.StdoutString())
}

func startLocalUpstream() (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/index.txt", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "hello through tunnel\n")
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("local upstream server: %v", err)
		}
	}()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return listener.Addr().String(), stop, nil
}
