package sandboxd

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) StatFile(ctx context.Context, path string) (FileStatResponse, error) {
	var response FileStatResponse
	err := c.getJSON(ctx, wire.PathFileStat+"?path="+url.QueryEscape(path), &response)
	return response, err
}

func (c *Client) ListDir(ctx context.Context, path string) (FileListResponse, error) {
	var response FileListResponse
	err := c.getJSON(ctx, wire.PathFileList+"?path="+url.QueryEscape(path), &response)
	return response, err
}

func (c *Client) ReadFile(ctx context.Context, path string) (FileReadResponse, error) {
	var response FileReadResponse
	err := c.getJSON(ctx, wire.PathFileRead+"?path="+url.QueryEscape(path), &response)
	return response, err
}

func (c *Client) Exists(ctx context.Context, path string) (FileExistsResponse, error) {
	var response FileExistsResponse
	err := c.getJSON(ctx, wire.PathFileExists+"?path="+url.QueryEscape(path), &response)
	return response, err
}

func (c *Client) WriteFile(ctx context.Context, request FileWriteRequest) error {
	return c.postJSON(ctx, wire.PathFileWrite, request, nil)
}

func (c *Client) Mkdir(ctx context.Context, request FileMkdirRequest) error {
	return c.postJSON(ctx, wire.PathFileMkdir, request, nil)
}

func (c *Client) Remove(ctx context.Context, request FileRemoveRequest) error {
	return c.postJSON(ctx, wire.PathFileRemove, request, nil)
}

func (c *Client) Copy(ctx context.Context, request FileCopyRequest) error {
	return c.postJSON(ctx, wire.PathFileCopy, request, nil)
}

func (c *Client) Move(ctx context.Context, request FileMoveRequest) error {
	return c.postJSON(ctx, wire.PathFileMove, request, nil)
}

func (c *Client) Chmod(ctx context.Context, request FileChmodRequest) error {
	return c.postJSON(ctx, wire.PathFileChmod, request, nil)
}

func (c *Client) Touch(ctx context.Context, request FileTouchRequest) error {
	return c.postJSON(ctx, wire.PathFileTouch, request, nil)
}

func (c *Client) UploadArchive(ctx context.Context, request FileArchiveUploadRequest, input io.Reader) error {
	path := wire.PathArchiveUpload + "?path=" + url.QueryEscape(request.Path) +
		"&format=" + url.QueryEscape(fmt.Sprintf("%d", request.Format)) +
		"&createParents=" + url.QueryEscape(fmt.Sprintf("%t", request.CreateParents)) +
		"&overwrite=" + url.QueryEscape(fmt.Sprintf("%t", request.Overwrite)) +
		"&symlinkPolicy=" + url.QueryEscape(fmt.Sprintf("%d", request.SymlinkPolicy))
	return c.postBody(ctx, path, "application/x-tar", input, nil)
}

func (c *Client) DownloadArchive(ctx context.Context, request FileArchiveDownloadRequest, output io.Writer) error {
	path := wire.PathArchiveDownload + "?path=" + url.QueryEscape(request.Path) +
		"&format=" + url.QueryEscape(fmt.Sprintf("%d", request.Format)) +
		"&symlinkPolicy=" + url.QueryEscape(fmt.Sprintf("%d", request.SymlinkPolicy))
	_, err := c.getBody(ctx, path, output)
	return err
}
