package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/imagemgr/api"
)

const defaultSocketPath = "/var/run/imagemgr.sock"

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [options] <command> [args]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  mount <image_url>    Mount an OCI image\n")
	fmt.Fprintf(os.Stderr, "  umount <image_url>   Unmount an OCI image\n")
	fmt.Fprintf(os.Stderr, "\nOptions:\n")
	fmt.Fprintf(os.Stderr, "  -sock string\n")
	fmt.Fprintf(os.Stderr, "        Unix socket path for imagemgr (default \"%s\")\n", defaultSocketPath)
	fmt.Fprintf(os.Stderr, "  -h    Show this help message\n")
}

func main() {
	var sockPath string
	flag.StringVar(&sockPath, "sock", defaultSocketPath, "Unix socket path for imagemgr")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}

	command := flag.Arg(0)

	client := api.NewHttpClient(sockPath)

	switch command {
	case "mount":
		if flag.NArg() < 2 {
			fmt.Fprintf(os.Stderr, "Error: mount command requires an image URL\n")
			fmt.Fprintf(os.Stderr, "Usage: %s mount <image_url>\n", os.Args[0])
			os.Exit(1)
		}
		imageURL := flag.Arg(1)
		if err := doMount(client, imageURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "umount":
		if flag.NArg() < 2 {
			fmt.Fprintf(os.Stderr, "Error: umount command requires an image URL\n")
			fmt.Fprintf(os.Stderr, "Usage: %s umount <image_url>\n", os.Args[0])
			os.Exit(1)
		}
		imageURL := flag.Arg(1)
		if err := doUmount(client, imageURL); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n", command)
		usage()
		os.Exit(1)
	}
}

func doMount(client *api.HttpClient, imageURL string) error {
	fmt.Printf("Mounting OCI image: %s\n", imageURL)

	req := &api.OCIMountRequest{
		ImageURL: imageURL,
		LeaseID:  commandLeaseID(imageURL),
		Owner:    "oci-client",
	}

	resp, err := client.MountOCI(req)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully mounted at: %s\n", resp.MountPath)
	return nil
}

func doUmount(client *api.HttpClient, imageURL string) error {
	fmt.Printf("Unmounting OCI image: %s\n", imageURL)

	req := &api.OCIUmountRequest{
		ImageURL: imageURL,
		LeaseID:  commandLeaseID(imageURL),
	}

	if err := client.UmountOCI(req); err != nil {
		return err
	}

	fmt.Printf("Successfully unmounted: %s\n", imageURL)
	return nil
}

func commandLeaseID(imageURL string) string {
	sum := sha256.Sum256([]byte(imageURL))
	return fmt.Sprintf("oci-client:%x", sum[:])
}
