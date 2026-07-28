package nodeclient

import (
	"bytes"
	"context"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type ComputerUseStatus struct {
	Available    bool
	Display      string
	Backend      string
	Reason       string
	Dependencies []ComputerUseDependencyStatus
}

type ComputerUseDependencyStatus struct {
	Name      string
	Available bool
	Reason    string
}

type ComputerUseScreenshot struct {
	Data        []byte
	ContentType string
}

type ComputerUseRegion struct {
	X      int32
	Y      int32
	Width  int32
	Height int32
}

type ComputerUseScreenshotOptions struct {
	ShowCursor bool
	Region     *ComputerUseRegion
	Format     string
	Quality    int32
	Scale      float64
}

type ComputerUseDisplay struct {
	Display string
	Backend string
	Width   int32
	Height  int32
}

type ComputerUseMouseOptions struct {
	Action    string
	X         int32
	Y         int32
	ToX       int32
	ToY       int32
	Button    string
	Direction string
	Amount    int32
}

type ComputerUseKeyboardOptions struct {
	Text    string
	Key     string
	Keys    []string
	DelayMS int32
}

func (c *Client) ComputerUseStatus(ctx context.Context) (ComputerUseStatus, error) {
	response, err := c.nodes.ComputerUseStatus(ctx, &nodesandboxv1.ComputerUseStatusRequest{
		AllocationID: c.allocationID,
	})
	if err != nil {
		return ComputerUseStatus{}, err
	}
	return ComputerUseStatus{
		Available:    response.GetAvailable(),
		Display:      response.GetDisplay(),
		Backend:      response.GetBackend(),
		Reason:       response.GetReason(),
		Dependencies: computerUseDependencies(response.GetDependencies()),
	}, nil
}

func (c *Client) ComputerUseScreenshot(ctx context.Context, options ComputerUseScreenshotOptions) (ComputerUseScreenshot, error) {
	response, err := c.nodes.ComputerUseScreenshot(ctx, &nodesandboxv1.ComputerUseScreenshotRequest{
		AllocationID: c.allocationID,
		ShowCursor:   options.ShowCursor,
		Region:       computerUseRegion(options.Region),
		Format:       options.Format,
		Quality:      options.Quality,
		Scale:        options.Scale,
	})
	if err != nil {
		return ComputerUseScreenshot{}, err
	}
	return ComputerUseScreenshot{
		Data:        bytes.Clone(response.GetData()),
		ContentType: response.GetContentType(),
	}, nil
}

func (c *Client) ComputerUseDisplay(ctx context.Context) (ComputerUseDisplay, error) {
	response, err := c.nodes.ComputerUseDisplay(ctx, &nodesandboxv1.ComputerUseDisplayRequest{
		AllocationID: c.allocationID,
	})
	if err != nil {
		return ComputerUseDisplay{}, err
	}
	return ComputerUseDisplay{
		Display: response.GetDisplay(),
		Backend: response.GetBackend(),
		Width:   response.GetWidth(),
		Height:  response.GetHeight(),
	}, nil
}

func (c *Client) ComputerUseMouse(ctx context.Context, options ComputerUseMouseOptions) error {
	_, err := c.nodes.ComputerUseMouse(ctx, &nodesandboxv1.ComputerUseMouseRequest{
		AllocationID: c.allocationID,
		Action:       options.Action,
		X:            options.X,
		Y:            options.Y,
		ToX:          options.ToX,
		ToY:          options.ToY,
		Button:       options.Button,
		Direction:    options.Direction,
		Amount:       options.Amount,
	})
	return err
}

func (c *Client) ComputerUseKeyboard(ctx context.Context, options ComputerUseKeyboardOptions) error {
	_, err := c.nodes.ComputerUseKeyboard(ctx, &nodesandboxv1.ComputerUseKeyboardRequest{
		AllocationID: c.allocationID,
		Text:         options.Text,
		Key:          options.Key,
		Keys:         append([]string(nil), options.Keys...),
		DelayMs:      options.DelayMS,
	})
	return err
}

func computerUseRegion(region *ComputerUseRegion) *nodesandboxv1.ComputerUseRegion {
	if region == nil {
		return nil
	}
	return &nodesandboxv1.ComputerUseRegion{
		X:      region.X,
		Y:      region.Y,
		Width:  region.Width,
		Height: region.Height,
	}
}

func computerUseDependencies(items []*nodesandboxv1.ComputerUseDependencyStatus) []ComputerUseDependencyStatus {
	out := make([]ComputerUseDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, ComputerUseDependencyStatus{
			Name:      item.GetName(),
			Available: item.GetAvailable(),
			Reason:    item.GetReason(),
		})
	}
	return out
}
