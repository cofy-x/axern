package sandboxaccess

import (
	"context"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func (a *Accessor) ComputerUseStatus(ctx context.Context, request *runtime.ComputerUseStatusRequest) (*runtime.ComputerUseStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityComputerUse)
	if err != nil {
		return nil, err
	}
	status, err := client.ComputerUseStatus(ctx)
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityComputerUse, "status", err)
	}
	return &runtime.ComputerUseStatusResponse{
		Available:    status.Available,
		Display:      status.Display,
		Backend:      status.Backend,
		Reason:       status.Reason,
		Dependencies: runtimeComputerUseDependencies(status.Dependencies),
	}, nil
}

func (a *Accessor) ComputerUseScreenshot(ctx context.Context, request *runtime.ComputerUseScreenshotRequest) (*runtime.ComputerUseScreenshotResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityComputerUse)
	if err != nil {
		return nil, err
	}
	screenshot, err := client.ComputerUseScreenshot(ctx, runtimesandboxd.ComputerUseScreenshotRequest{
		ShowCursor: request.GetShowCursor(),
		Region:     runtimeComputerUseRegion(request.GetRegion()),
		Format:     request.GetFormat(),
		Quality:    int(request.GetQuality()),
		Scale:      request.GetScale(),
	})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityComputerUse, "screenshot", err)
	}
	return &runtime.ComputerUseScreenshotResponse{Data: screenshot.Data, ContentType: screenshot.ContentType}, nil
}

func (a *Accessor) ComputerUseDisplay(ctx context.Context, request *runtime.ComputerUseDisplayRequest) (*runtime.ComputerUseDisplayResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityComputerUse)
	if err != nil {
		return nil, err
	}
	display, err := client.ComputerUseDisplay(ctx)
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityComputerUse, "display", err)
	}
	return &runtime.ComputerUseDisplayResponse{
		Display: display.Display,
		Backend: display.Backend,
		Width:   int32(display.Width),
		Height:  int32(display.Height),
	}, nil
}

func (a *Accessor) ComputerUseMouse(ctx context.Context, request *runtime.ComputerUseMouseRequest) (*runtime.ComputerUseMouseResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityComputerUse)
	if err != nil {
		return nil, err
	}
	err = client.ComputerUseMouse(ctx, runtimesandboxd.ComputerUseMouseRequest{
		Action:    request.GetAction(),
		X:         int(request.GetX()),
		Y:         int(request.GetY()),
		ToX:       int(request.GetToX()),
		ToY:       int(request.GetToY()),
		Button:    request.GetButton(),
		Direction: request.GetDirection(),
		Amount:    int(request.GetAmount()),
	})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityComputerUse, "mouse", err)
	}
	return &runtime.ComputerUseMouseResponse{}, nil
}

func (a *Accessor) ComputerUseKeyboard(ctx context.Context, request *runtime.ComputerUseKeyboardRequest) (*runtime.ComputerUseKeyboardResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityComputerUse)
	if err != nil {
		return nil, err
	}
	err = client.ComputerUseKeyboard(ctx, runtimesandboxd.ComputerUseKeyboardRequest{
		Text:    request.GetText(),
		Key:     request.GetKey(),
		Keys:    request.GetKeys(),
		DelayMS: int(request.GetDelayMs()),
	})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityComputerUse, "keyboard", err)
	}
	return &runtime.ComputerUseKeyboardResponse{}, nil
}

func runtimeComputerUseRegion(region *runtime.ComputerUseRegion) *runtimesandboxd.ComputerUseRegion {
	if region == nil {
		return nil
	}
	return &runtimesandboxd.ComputerUseRegion{
		X:      int(region.GetX()),
		Y:      int(region.GetY()),
		Width:  int(region.GetWidth()),
		Height: int(region.GetHeight()),
	}
}

func runtimeComputerUseDependencies(items []runtimesandboxd.ComputerUseDependencyStatus) []*runtime.ComputerUseDependencyStatus {
	out := make([]*runtime.ComputerUseDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, &runtime.ComputerUseDependencyStatus{
			Name:      item.Name,
			Available: item.Available,
			Reason:    item.Reason,
		})
	}
	return out
}
