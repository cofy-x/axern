// axern-registry-healthcheck probes only the bundled Registry's loopback API.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

func probe(ctx context.Context, client *http.Client, address string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Docker-Distribution-Api-Version") != "registry/2.0" {
		return fmt.Errorf("Registry API is not ready: HTTP %d", response.StatusCode)
	}
	return nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if err := probe(ctx, client, "http://127.0.0.1:5000/v2/"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
