package sandboxd

import (
	"bytes"
	"context"
	"net/url"
	"strconv"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) ComputerUseStatus(ctx context.Context) (ComputerUseStatusResponse, error) {
	var response ComputerUseStatusResponse
	err := c.getJSON(ctx, wire.PathComputerUseStatus, &response)
	return response, err
}

func (c *Client) ComputerUseScreenshot(ctx context.Context, request ComputerUseScreenshotRequest) (ComputerUseScreenshotResponse, error) {
	var output bytes.Buffer
	contentType, err := c.getBody(ctx, computerUseScreenshotPath(request), &output)
	if err != nil {
		return ComputerUseScreenshotResponse{}, err
	}
	return ComputerUseScreenshotResponse{Data: output.Bytes(), ContentType: contentType}, nil
}

func (c *Client) ComputerUseDisplay(ctx context.Context) (ComputerUseDisplayResponse, error) {
	var response ComputerUseDisplayResponse
	err := c.getJSON(ctx, wire.PathComputerUseDisplay, &response)
	return response, err
}

func (c *Client) ComputerUseMouse(ctx context.Context, request ComputerUseMouseRequest) error {
	return c.postJSON(ctx, wire.PathComputerUseMouse, request, nil)
}

func (c *Client) ComputerUseKeyboard(ctx context.Context, request ComputerUseKeyboardRequest) error {
	return c.postJSON(ctx, wire.PathComputerUseKeyboard, request, nil)
}

func computerUseScreenshotPath(request ComputerUseScreenshotRequest) string {
	values := url.Values{}
	if request.ShowCursor {
		values.Set("showCursor", "true")
	}
	if request.Region != nil {
		values.Set("x", strconv.Itoa(request.Region.X))
		values.Set("y", strconv.Itoa(request.Region.Y))
		values.Set("width", strconv.Itoa(request.Region.Width))
		values.Set("height", strconv.Itoa(request.Region.Height))
	}
	if request.Format != "" {
		values.Set("format", request.Format)
	}
	if request.Quality > 0 {
		values.Set("quality", strconv.Itoa(request.Quality))
	}
	if request.Scale > 0 {
		values.Set("scale", strconv.FormatFloat(request.Scale, 'f', -1, 64))
	}
	if len(values) == 0 {
		return wire.PathComputerUseScreenshot
	}
	return wire.PathComputerUseScreenshot + "?" + values.Encode()
}
