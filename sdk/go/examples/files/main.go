package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

	if err := sandbox.WriteFile(ctx, "/tmp/axern-go-file.txt", []byte("file api\n"), axern.WriteFileOptions{CreateParents: true}); err != nil {
		log.Fatal(err)
	}
	data, err := sandbox.ReadFile(ctx, "/tmp/axern-go-file.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(data))
	if _, err := sandbox.Stat(ctx, "/tmp/axern-go-missing.txt"); axern.IsNotFound(err) {
		fmt.Println("missing file reported as not found")
	} else if err != nil {
		log.Fatal(err)
	}

	root, err := os.MkdirTemp("", "axern-go-upload-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "message.txt"), []byte("archive api\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	if err := sandbox.UploadDir(ctx, root, "/tmp/axern-go-tree", axern.UploadDirOptions{}); err != nil {
		log.Fatal(err)
	}
	info, err := sandbox.Stat(ctx, "/tmp/axern-go-tree/nested/message.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s %d bytes\n", info.Path, info.Size)
}
