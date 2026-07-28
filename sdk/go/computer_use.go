package axernsdk

import (
	"context"

	"github.com/cofy-x/axern/sdk/go/internal/nodeclient"
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

func (s *Sandbox) ComputerUseStatus(ctx context.Context) (ComputerUseStatus, error) {
	node, err := s.nodeClient()
	if err != nil {
		return ComputerUseStatus{}, err
	}
	return node.ComputerUseStatus(ctx)
}

func (s *Sandbox) ComputerUseScreenshot(ctx context.Context, options ...ComputerUseScreenshotOptions) (ComputerUseScreenshot, error) {
	node, err := s.nodeClient()
	if err != nil {
		return ComputerUseScreenshot{}, err
	}
	return node.ComputerUseScreenshot(ctx, options...)
}

func (s *Sandbox) ComputerUseDisplay(ctx context.Context) (ComputerUseDisplay, error) {
	node, err := s.nodeClient()
	if err != nil {
		return ComputerUseDisplay{}, err
	}
	return node.ComputerUseDisplay(ctx)
}

func (s *Sandbox) ComputerUseMouse(ctx context.Context, options ComputerUseMouseOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.ComputerUseMouse(ctx, options)
}

func (s *Sandbox) ComputerUseKeyboard(ctx context.Context, options ComputerUseKeyboardOptions) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	return node.ComputerUseKeyboard(ctx, options)
}

func (n *NodeSandboxClient) ComputerUseStatus(ctx context.Context) (ComputerUseStatus, error) {
	if err := n.validate(); err != nil {
		return ComputerUseStatus{}, err
	}
	status, err := n.rpcClient().ComputerUseStatus(ctx)
	if err != nil {
		return ComputerUseStatus{}, mapRPCError(err, "sandbox computer-use status", n.allocationID)
	}
	return ComputerUseStatus{
		Available:    status.Available,
		Display:      status.Display,
		Backend:      status.Backend,
		Reason:       status.Reason,
		Dependencies: sdkComputerUseDependencies(status.Dependencies),
	}, nil
}

func (n *NodeSandboxClient) ComputerUseScreenshot(ctx context.Context, options ...ComputerUseScreenshotOptions) (ComputerUseScreenshot, error) {
	if err := n.validate(); err != nil {
		return ComputerUseScreenshot{}, err
	}
	screenshot, err := n.rpcClient().ComputerUseScreenshot(ctx, nodeScreenshotOptions(options))
	if err != nil {
		return ComputerUseScreenshot{}, mapRPCError(err, "sandbox computer-use screenshot", n.allocationID)
	}
	return ComputerUseScreenshot{
		Data:        screenshot.Data,
		ContentType: screenshot.ContentType,
	}, nil
}

func (n *NodeSandboxClient) ComputerUseDisplay(ctx context.Context) (ComputerUseDisplay, error) {
	if err := n.validate(); err != nil {
		return ComputerUseDisplay{}, err
	}
	display, err := n.rpcClient().ComputerUseDisplay(ctx)
	if err != nil {
		return ComputerUseDisplay{}, mapRPCError(err, "sandbox computer-use display", n.allocationID)
	}
	return ComputerUseDisplay{
		Display: display.Display,
		Backend: display.Backend,
		Width:   display.Width,
		Height:  display.Height,
	}, nil
}

func (n *NodeSandboxClient) ComputerUseMouse(ctx context.Context, options ComputerUseMouseOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if err := n.rpcClient().ComputerUseMouse(ctx, nodeMouseOptions(options)); err != nil {
		return mapRPCError(err, "sandbox computer-use mouse", n.allocationID)
	}
	return nil
}

func (n *NodeSandboxClient) ComputerUseKeyboard(ctx context.Context, options ComputerUseKeyboardOptions) error {
	if err := n.validate(); err != nil {
		return err
	}
	if err := n.rpcClient().ComputerUseKeyboard(ctx, nodeKeyboardOptions(options)); err != nil {
		return mapRPCError(err, "sandbox computer-use keyboard", n.allocationID)
	}
	return nil
}

func nodeScreenshotOptions(options []ComputerUseScreenshotOptions) nodeclient.ComputerUseScreenshotOptions {
	if len(options) == 0 {
		return nodeclient.ComputerUseScreenshotOptions{}
	}
	option := options[0]
	return nodeclient.ComputerUseScreenshotOptions{
		ShowCursor: option.ShowCursor,
		Region:     nodeRegion(option.Region),
		Format:     option.Format,
		Quality:    option.Quality,
		Scale:      option.Scale,
	}
}

func nodeMouseOptions(options ComputerUseMouseOptions) nodeclient.ComputerUseMouseOptions {
	return nodeclient.ComputerUseMouseOptions{
		Action:    options.Action,
		X:         options.X,
		Y:         options.Y,
		ToX:       options.ToX,
		ToY:       options.ToY,
		Button:    options.Button,
		Direction: options.Direction,
		Amount:    options.Amount,
	}
}

func nodeKeyboardOptions(options ComputerUseKeyboardOptions) nodeclient.ComputerUseKeyboardOptions {
	return nodeclient.ComputerUseKeyboardOptions{
		Text:    options.Text,
		Key:     options.Key,
		Keys:    append([]string(nil), options.Keys...),
		DelayMS: options.DelayMS,
	}
}

func nodeRegion(region *ComputerUseRegion) *nodeclient.ComputerUseRegion {
	if region == nil {
		return nil
	}
	return &nodeclient.ComputerUseRegion{
		X:      region.X,
		Y:      region.Y,
		Width:  region.Width,
		Height: region.Height,
	}
}

func sdkComputerUseDependencies(items []nodeclient.ComputerUseDependencyStatus) []ComputerUseDependencyStatus {
	out := make([]ComputerUseDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, ComputerUseDependencyStatus{
			Name:      item.Name,
			Available: item.Available,
			Reason:    item.Reason,
		})
	}
	return out
}
