package sandboxd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) StartProcess(ctx context.Context, request ProcessStartRequest) (ProcessStatus, error) {
	var response ProcessStatus
	err := c.postJSON(ctx, wire.PathProcesses, request, &response)
	return response, err
}

func (c *Client) ListProcesses(ctx context.Context) (ProcessListResponse, error) {
	var response ProcessListResponse
	err := c.getJSON(ctx, wire.PathProcesses, &response)
	return response, err
}

func (c *Client) ProcessStatus(ctx context.Context, id string) (ProcessStatus, error) {
	var response ProcessStatus
	err := c.getJSON(ctx, wire.PathProcessesPrefix+url.PathEscape(id), &response)
	return response, err
}

func (c *Client) SignalProcess(ctx context.Context, id string, signal string) (ProcessStatus, error) {
	var response ProcessStatus
	err := c.postJSON(ctx, wire.PathProcessesPrefix+url.PathEscape(id)+"/signal", ProcessSignalRequest{Signal: signal}, &response)
	return response, err
}

func (c *Client) WriteProcessStdin(ctx context.Context, id string, data []byte) (ProcessStatus, error) {
	var response ProcessStatus
	err := c.postJSON(ctx, wire.PathProcessesPrefix+url.PathEscape(id)+"/stdin", ProcessStdinRequest{Data: data}, &response)
	return response, err
}

func (c *Client) CloseProcessStdin(ctx context.Context, id string) (ProcessStatus, error) {
	var response ProcessStatus
	err := c.postNoBody(ctx, wire.PathProcessesPrefix+url.PathEscape(id)+"/stdin-close", &response)
	return response, err
}

func (c *Client) ResizeProcess(ctx context.Context, id string, cols uint32, rows uint32) (ProcessStatus, error) {
	var response ProcessStatus
	err := c.postJSON(ctx, wire.PathProcessesPrefix+url.PathEscape(id)+"/resize", ProcessResizeRequest{Cols: cols, Rows: rows}, &response)
	return response, err
}

func (c *Client) StreamProcess(ctx context.Context, id string, emit func(ProcessStreamEvent) error) error {
	path := wire.PathProcessesPrefix + url.PathEscape(id) + "/stream"
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
	for {
		var event ProcessStreamEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode sandboxd process stream response: %w", err)
		}
		if err := emit(event); err != nil {
			return err
		}
	}
}

func (c *Client) WaitProcess(ctx context.Context, id string) (ProcessStatus, error) {
	var response ProcessStatus
	err := c.getJSON(ctx, wire.PathProcessesPrefix+url.PathEscape(id)+"/wait", &response)
	return response, err
}
