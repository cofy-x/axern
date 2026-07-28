package taskset

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type kovaPublisher struct {
	endpoint, token string
	preheat         bool
}

type kovaJob struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}
type kovaResult struct {
	Format         string `json:"format"`
	Status         string `json:"status"`
	Repository     string `json:"repository"`
	ManifestDigest string `json:"manifest_digest"`
	MediaType      string `json:"media_type"`
	Error          string `json:"error"`
	Size           int64  `json:"size"`
}
type kovaResultResponse struct {
	Results []kovaResult `json:"results"`
}

func (p kovaPublisher) Publish(ctx context.Context, resolved Resolved, target string) (payloadPublishResult, error) {
	if strings.TrimSpace(p.endpoint) == "" {
		return payloadPublishResult{}, fmt.Errorf("Kova endpoint is required")
	}
	archivePath, cleanup, err := createKovaBuildArchive(filepath.Dir(resolved.DescriptorPath), target)
	if err != nil {
		return payloadPublishResult{}, err
	}
	defer cleanup()
	job, err := p.submit(ctx, archivePath, resolved.Descriptor.SourceDigest, target)
	if err != nil {
		return payloadPublishResult{}, err
	}
	for {
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = p.cancel(cancelCtx, job.ID)
			cancel()
			return payloadPublishResult{BuildID: job.ID}, fmt.Errorf("Kova build %s: %w", job.ID, ctx.Err())
		case <-time.After(time.Second):
		}
		job, err = p.get(ctx, job.ID)
		if err != nil {
			return payloadPublishResult{BuildID: job.ID}, err
		}
		switch job.Status {
		case "succeeded":
			results, err := p.results(ctx, job.ID, target)
			if err != nil {
				return payloadPublishResult{BuildID: job.ID}, err
			}
			if p.preheat {
				if err := p.preheatBuild(ctx, job.ID); err != nil {
					return payloadPublishResult{BuildID: job.ID}, err
				}
			}
			return payloadPublishResult{Payloads: results, BuildID: job.ID}, nil
		case "failed", "cancelled":
			return payloadPublishResult{BuildID: job.ID}, fmt.Errorf("Kova build %s %s: %s", job.ID, job.Status, job.Error)
		}
	}
}

func kovaBuildArchive(bundleRoot, target string) ([]byte, error) {
	var out bytes.Buffer
	if err := writeKovaBuildArchive(&out, bundleRoot, target); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func createKovaBuildArchive(bundleRoot, target string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "axrun-kova-context-*.zip")
	if err != nil {
		return "", nil, err
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := writeKovaBuildArchive(tmp, bundleRoot, target); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}

func writeKovaBuildArchive(out io.Writer, bundleRoot, target string) error {
	metadata, err := json.Marshal(map[string]string{"target": target})
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	for _, file := range []struct {
		name string
		data []byte
	}{
		{name: "taskset/Dockerfile", data: []byte("FROM scratch\nADD payload.tar /\n")},
		{name: "taskset/metadata.json", data: metadata},
	} {
		if err := writeKovaZipEntry(zw, file.name, bytes.NewReader(file.data)); err != nil {
			return err
		}
	}
	payload, err := os.Open(filepath.Join(bundleRoot, "payload.tar"))
	if err != nil {
		return err
	}
	defer payload.Close()
	if err := writeKovaZipEntry(zw, "taskset/payload.tar", payload); err != nil {
		return err
	}
	return zw.Close()
}

func writeKovaZipEntry(zw *zip.Writer, name string, content io.Reader) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	header.SetModTime(time.Unix(0, 0))
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, content)
	return err
}

func (p kovaPublisher) submit(ctx context.Context, archivePath, sourceDigest, target string) (kovaJob, error) {
	bodyReader, bodyWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(bodyWriter)
	contentType := multipartWriter.FormDataContentType()
	archive, err := os.Open(archivePath)
	if err != nil {
		return kovaJob{}, err
	}
	keyHash := sha256.Sum256([]byte(sourceDigest + "\x00" + target + "\x00oci,nydus"))
	go func() {
		defer archive.Close()
		file, writeErr := multipartWriter.CreateFormFile("file", "source.zip")
		if writeErr == nil {
			_, writeErr = io.Copy(file, archive)
		}
		for _, field := range []struct{ key, value string }{
			{key: "source_digest", value: sourceDigest},
			{key: "idempotency_key", value: "axrun-taskset-" + hex.EncodeToString(keyHash[:])},
			{key: "target", value: target},
			{key: "formats", value: "oci,nydus"},
		} {
			if writeErr == nil {
				writeErr = multipartWriter.WriteField(field.key, field.value)
			}
		}
		if closeErr := multipartWriter.Close(); writeErr == nil {
			writeErr = closeErr
		}
		_ = bodyWriter.CloseWithError(writeErr)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(p.endpoint, "/")+"/v1/builds", bodyReader)
	if err != nil {
		_ = bodyReader.Close()
		return kovaJob{}, err
	}
	req.Header.Set("Content-Type", contentType)
	return p.doJob(req)
}

func (p kovaPublisher) get(ctx context.Context, id string) (kovaJob, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.url("builds/"+url.PathEscape(id)), nil)
	return p.doJob(req)
}
func (p kovaPublisher) cancel(ctx context.Context, id string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.url("builds/"+url.PathEscape(id)+"/cancel"), nil)
	_, err := p.doJob(req)
	return err
}
func (p kovaPublisher) preheatBuild(ctx context.Context, id string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.url("builds/"+url.PathEscape(id)+"/preheat"), nil)
	_, err := p.do(req)
	return err
}
func (p kovaPublisher) url(path string) string {
	return strings.TrimSuffix(p.endpoint, "/") + "/v1/" + path
}

func (p kovaPublisher) results(ctx context.Context, id, target string) ([]PayloadDescriptor, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.url("builds/"+url.PathEscape(id)+"/results"), nil)
	data, err := p.do(req)
	if err != nil {
		return nil, err
	}
	var response kovaResultResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	var payloads []PayloadDescriptor
	seen := map[string]bool{}
	for _, result := range response.Results {
		if result.Format != "oci" && result.Format != "nydus" {
			return nil, fmt.Errorf("Kova returned unsupported payload format %q", result.Format)
		}
		if seen[result.Format] {
			return nil, fmt.Errorf("Kova returned duplicate %s payload result", result.Format)
		}
		seen[result.Format] = true
		expectedRepository := target
		if result.Format == "nydus" {
			expectedRepository += "_nydus_v3"
		}
		if result.Repository != expectedRepository {
			return nil, fmt.Errorf("Kova %s payload repository is %q, want %q", result.Format, result.Repository, expectedRepository)
		}
		if result.Status != "succeeded" || result.ManifestDigest == "" {
			return nil, fmt.Errorf("Kova %s payload failed: %s", result.Format, result.Error)
		}
		if result.MediaType == "" || result.Size <= 0 {
			return nil, fmt.Errorf("Kova %s payload result is missing registry descriptor metadata", result.Format)
		}
		immutableRef, err := immutableDigestReference(result.Repository, result.ManifestDigest)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, PayloadDescriptor{
			Format:    result.Format,
			Reference: immutableRef,
			Digest:    result.ManifestDigest,
			MediaType: result.MediaType,
			SizeBytes: result.Size,
		})
	}
	if len(payloads) != 2 || !seen["oci"] || !seen["nydus"] {
		return nil, fmt.Errorf("Kova must return exactly one OCI and one Nydus payload")
	}
	return payloads, nil
}

func (p kovaPublisher) doJob(req *http.Request) (kovaJob, error) {
	data, err := p.do(req)
	if err != nil {
		return kovaJob{}, err
	}
	var job kovaJob
	if err := json.Unmarshal(data, &job); err != nil {
		return kovaJob{}, err
	}
	if job.ID == "" {
		return kovaJob{}, fmt.Errorf("Kova response is missing job id")
	}
	return job, nil
}
func (p kovaPublisher) do(req *http.Request) ([]byte, error) {
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Kova returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}
