package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultRegistryURL  = "https://registry.npmjs.org"
	defaultXtermVersion = "6.0.0"
	defaultFitVersion   = "0.11.0"
	defaultVendorDir    = "gateway/gatewayd/internal/api/http/dashboard/vendor"
)

type asset struct {
	Package     string
	Version     string
	TarballName string
	SourcePath  string
	OutputName  string
}

func main() {
	var (
		vendorDir    string
		registryURL  string
		xtermVersion string
		fitVersion   string
	)
	flags := flag.NewFlagSet("dashassets", flag.ExitOnError)
	flags.StringVar(&vendorDir, "vendor-dir", defaultVendorDir, "dashboard vendor asset output directory")
	flags.StringVar(&registryURL, "registry-url", defaultRegistryURL, "npm registry base URL")
	flags.StringVar(&xtermVersion, "xterm-version", defaultXtermVersion, "@xterm/xterm version")
	flags.StringVar(&fitVersion, "fit-version", defaultFitVersion, "@xterm/addon-fit version")
	_ = flags.Parse(os.Args[1:])

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := syncAssets(ctx, http.DefaultClient, registryURL, vendorDir, xtermVersion, fitVersion); err != nil {
		fmt.Fprintf(os.Stderr, "dashassets: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "dashboard assets written to %s\n", vendorDir)
}

func syncAssets(ctx context.Context, client *http.Client, registryURL, vendorDir, xtermVersion, fitVersion string) error {
	if strings.TrimSpace(vendorDir) == "" {
		return fmt.Errorf("vendor-dir is required")
	}
	assets := []asset{
		{
			Package:     "@xterm/xterm",
			Version:     xtermVersion,
			TarballName: "xterm",
			SourcePath:  "package/lib/xterm.js",
			OutputName:  "xterm.js",
		},
		{
			Package:     "@xterm/xterm",
			Version:     xtermVersion,
			TarballName: "xterm",
			SourcePath:  "package/css/xterm.css",
			OutputName:  "xterm.css",
		},
		{
			Package:     "@xterm/addon-fit",
			Version:     fitVersion,
			TarballName: "addon-fit",
			SourcePath:  "package/lib/addon-fit.js",
			OutputName:  "addon-fit.js",
		},
	}
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		return err
	}
	for _, item := range assets {
		data, err := downloadAsset(ctx, client, registryURL, item)
		if err != nil {
			return err
		}
		if err := writeAsset(filepath.Join(vendorDir, item.OutputName), data); err != nil {
			return err
		}
	}
	return nil
}

func downloadAsset(ctx context.Context, client *http.Client, registryURL string, item asset) ([]byte, error) {
	url := tarballURL(registryURL, item)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return extractTarballFile(resp.Body, item.SourcePath)
}

func tarballURL(registryURL string, item asset) string {
	return strings.TrimRight(registryURL, "/") + "/" + item.Package + "/-/" + item.TarballName + "-" + item.Version + ".tgz"
}

func extractTarballFile(raw io.Reader, sourcePath string) ([]byte, error) {
	gz, err := gzip.NewReader(raw)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	cleanSource := path.Clean(sourcePath)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg || path.Clean(header.Name) != cleanSource {
			continue
		}
		return io.ReadAll(tr)
	}
	return nil, fmt.Errorf("%s not found in tarball", sourcePath)
}

func writeAsset(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
