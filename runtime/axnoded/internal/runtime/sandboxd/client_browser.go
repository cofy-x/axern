package sandboxd

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) BrowserStatus(ctx context.Context) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.getJSON(ctx, wire.PathBrowserStatus, &response)
	return response, err
}

func (c *Client) BrowserOpen(ctx context.Context, request BrowserOpenRequest) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.postJSON(ctx, wire.PathBrowserOpen, request, &response)
	return response, err
}

func (c *Client) BrowserClose(ctx context.Context) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.postNoBody(ctx, wire.PathBrowserClose, &response)
	return response, err
}

func (c *Client) BrowserNavigate(ctx context.Context, request BrowserNavigateRequest) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.postJSON(ctx, wire.PathBrowserNavigate, request, &response)
	return response, err
}

func (c *Client) BrowserResize(ctx context.Context, request BrowserResizeRequest) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.postJSON(ctx, wire.PathBrowserResize, request, &response)
	return response, err
}

func (c *Client) BrowserClick(ctx context.Context, request BrowserClickRequest) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.postJSON(ctx, wire.PathBrowserClick, request, &response)
	return response, err
}

func (c *Client) BrowserType(ctx context.Context, request BrowserTypeRequest) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.postJSON(ctx, wire.PathBrowserType, request, &response)
	return response, err
}

func (c *Client) BrowserWait(ctx context.Context, request BrowserWaitRequest) (BrowserStatusResponse, error) {
	var response BrowserStatusResponse
	err := c.postJSON(ctx, wire.PathBrowserWait, request, &response)
	return response, err
}
