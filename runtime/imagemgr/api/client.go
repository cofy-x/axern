package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
)

type HttpClient struct {
	clt *http.Client
}

func NewHttpClient(sockPath string) *HttpClient {
	if sockPath == "" {
		sockPath = DefaultHttpSockPath
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}
				return dialer.DialContext(ctx, "unix", sockPath)
			},
		},
	}
	return &HttpClient{clt: client}
}

func (c *HttpClient) MountOSS(req *OSSMountRequest) (*MountInfo, error) {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oss_mount", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to mount oss: %s, err: %v", req, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to mount oss: %s, err: %s", req, string(errMsg))
	}
	mi := &MountInfo{}
	if err = json.NewDecoder(resp.Body).Decode(mi); err != nil {
		return nil, fmt.Errorf("invalid reply body format: %v", err)
	}
	if mi.MountPath == "" {
		return nil, fmt.Errorf("mount_path not found in response")
	}
	return mi, nil
}

func (c *HttpClient) UmountOSS(req *OSSUmountRequest) error {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oss_umount", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to umount oss: %s, err: %v", req, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to umount oss: %s, err: %s", req, string(errMsg))
	}
	return nil
}

func (c *HttpClient) MountOCI(req *OCIMountRequest) (*OCIMountResponse, error) {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oci_mount", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to mount oci image: %s, err: %v", req, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to mount oci image: %s, err: %s", req, string(errMsg))
	}
	var result OCIMountResponse
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid reply body format: %v", err)
	}
	if result.MountPath == "" {
		return nil, fmt.Errorf("mount_path not found in response")
	}
	return &result, nil
}

func (c *HttpClient) UmountOCI(req *OCIUmountRequest) error {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/oci_umount", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to umount oci image: %s, err: %v", req, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to umount oci image: %s, err: %s", req, string(errMsg))
	}
	return nil
}

func (c *HttpClient) ReconcileMountLeases(req *ReconcileMountLeasesRequest) (*ReconcileMountLeasesResponse, error) {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/reconcile_mount_leases", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile mount leases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to reconcile mount leases: %s", string(errMsg))
	}
	var result ReconcileMountLeasesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid mount lease reconcile response: %w", err)
	}
	return &result, nil
}

func (c *HttpClient) ImportOCI(imageRef string, archive io.Reader) (*OCIImportResponse, error) {
	endpoint := "http://unix/oci_import?ref=" + url.QueryEscape(imageRef)
	resp, err := c.clt.Post(endpoint, "application/x-tar", archive)
	if err != nil {
		return nil, fmt.Errorf("failed to import oci image %s: %v", imageRef, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to import oci image %s: %s", imageRef, string(errMsg))
	}
	var result OCIImportResponse
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid reply body format: %v", err)
	}
	if result.CanonicalRef == "" || result.GenerationDigest == "" {
		return nil, fmt.Errorf("canonical_ref or generation_digest not found in response")
	}
	return &result, nil
}

func (c *HttpClient) CleanupDaemon(req *CleanupDaemonRequest) error {
	body, _ := json.Marshal(req)
	resp, err := c.clt.Post("http://unix/cleanup_daemon", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to cleanup daemon: %s, err: %v", req.DaemonID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to cleanup daemon: %s, err: %s", req.DaemonID, string(errMsg))
	}
	return nil
}

func (c *HttpClient) ListDaemons() ([]imagefsd.DaemonInfo, error) {
	resp, err := c.clt.Get("http://unix/list_daemons")
	if err != nil {
		return nil, fmt.Errorf("failed to list daemons, err: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list daemons, err: %s", string(errMsg))
	}
	var daemons []imagefsd.DaemonInfo
	if err = json.NewDecoder(resp.Body).Decode(&daemons); err != nil {
		return nil, fmt.Errorf("invalid reply body format: %v", err)
	}
	return daemons, nil
}

func (c *HttpClient) ListMountedOCIImages() ([]string, error) {
	resp, err := c.clt.Get("http://unix/list_oci_mounts")
	if err != nil {
		return nil, fmt.Errorf("failed to list oci mounts, err: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list oci mounts, err: %s", string(errMsg))
	}
	var result struct {
		ImageURLs []string `json:"image_urls"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid reply body format: %v", err)
	}
	return result.ImageURLs, nil
}

func (c *HttpClient) ListMountedOCIDetails() ([]MountedImageDetail, error) {
	resp, err := c.clt.Get("http://unix/list_oci_mount_details")
	if err != nil {
		return nil, fmt.Errorf("failed to list oci mount details, err: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list oci mount details, err: %s", string(errMsg))
	}
	var result struct {
		Mounts []MountedImageDetail `json:"mounts"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid reply body format: %v", err)
	}
	return result.Mounts, nil
}
