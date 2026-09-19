package rootfssnapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

type Publisher interface {
	Publish(context.Context, Request) (*apipb.RootfsSnapshotSealingResult, error)
}

type Request struct {
	AllocationID           string
	BaseImageRef           string
	BaseRegistryCredential string
	UpperLayer             io.Reader
	MaxBytes               int64
}

type httpPublisher struct{ client *http.Client }

func responseErrorCause(statusCode int) error {
	switch statusCode {
	case http.StatusBadRequest:
		return errord.ErrInvalidArgument
	case http.StatusRequestEntityTooLarge:
		return errord.ErrResourceExhausted
	case http.StatusServiceUnavailable:
		return errord.ErrUnavailable
	default:
		return nil
	}
}

func NewPublisher(socketPath string) Publisher {
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "unix", socketPath)
	}}}
	return &httpPublisher{client: client}
}

func (p *httpPublisher) Publish(ctx context.Context, request Request) (*apipb.RootfsSnapshotSealingResult, error) {
	if request.UpperLayer == nil {
		return nil, fmt.Errorf("rootfs snapshot upper layer is required: %w", errord.ErrInvalidArgument)
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeErr := make(chan error, 1)
	go func() {
		defer close(writeErr)
		metadata, err := multipartWriter.CreateFormField("metadata")
		if err == nil {
			err = json.NewEncoder(metadata).Encode(map[string]string{
				"allocation_id":                 request.AllocationID,
				"base_image_ref":                request.BaseImageRef,
				"base_registry_credential_json": request.BaseRegistryCredential,
				"max_bytes":                     fmt.Sprintf("%d", request.MaxBytes),
			})
		}
		var layer io.Writer
		if err == nil {
			layer, err = multipartWriter.CreateFormFile("upper_layer", "upper.tar")
		}
		if err == nil {
			_, err = io.Copy(layer, request.UpperLayer)
		}
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
		writeErr <- err
	}()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/rootfs_snapshot", reader)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	response, err := p.client.Do(httpRequest)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-writeErr
		return nil, fmt.Errorf("publish rootfs snapshot: %w", err)
	}
	defer response.Body.Close()
	if err := <-writeErr; err != nil {
		return nil, fmt.Errorf("stream rootfs snapshot: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		cause := responseErrorCause(response.StatusCode)
		if cause != nil {
			return nil, fmt.Errorf("publish rootfs snapshot: %s: %w", bytes.TrimSpace(message), cause)
		}
		return nil, fmt.Errorf("publish rootfs snapshot: %s", bytes.TrimSpace(message))
	}
	var result struct {
		ImageRef        string `json:"image_ref"`
		Digest          string `json:"digest"`
		MediaType       string `json:"media_type"`
		SizeBytes       int64  `json:"size_bytes"`
		PlatformOS      string `json:"platform_os"`
		PlatformArch    string `json:"platform_arch"`
		PlatformVariant string `json:"platform_variant"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode rootfs snapshot response: %w", err)
	}
	if result.ImageRef == "" || result.Digest == "" || result.PlatformOS == "" || result.PlatformArch == "" {
		return nil, fmt.Errorf("rootfs snapshot response is incomplete")
	}
	return &apipb.RootfsSnapshotSealingResult{
		ImageRef:        result.ImageRef,
		ImageDescriptor: &environmentv1.OciImageDescriptor{Digest: result.Digest, MediaType: result.MediaType, SizeBytes: result.SizeBytes},
		PlatformOS:      result.PlatformOS, PlatformArch: result.PlatformArch, PlatformVariant: result.PlatformVariant,
	}, nil
}
