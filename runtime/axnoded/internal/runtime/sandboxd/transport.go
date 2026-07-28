package sandboxd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sandboxdStatusError(path, resp)
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode sandboxd %s response: %w", path, err)
	}
	return nil
}

func (c *Client) getBody(ctx context.Context, path string, output io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", sandboxdStatusError(path, resp)
	}
	_, err = io.Copy(output, resp.Body)
	return resp.Header.Get("Content-Type"), err
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.postBody(ctx, path, wire.ContentTypeJSON, bytes.NewReader(body), target)
}

func (c *Client) postNoBody(ctx context.Context, path string, target any) error {
	return c.postBody(ctx, path, "", nil, target)
}

func (c *Client) postBody(ctx context.Context, path string, contentType string, body io.Reader, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sandboxdStatusError(path, resp)
	}
	if target == nil {
		return nil
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode sandboxd %s response: %w", path, err)
	}
	return nil
}
