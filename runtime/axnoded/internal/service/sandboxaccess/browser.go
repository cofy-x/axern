package sandboxaccess

import (
	"context"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func (a *Accessor) BrowserStatus(ctx context.Context, request *runtime.BrowserStatusRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserStatus(ctx)
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "status", err)
	}
	return runtimeBrowserStatus(status), nil
}

func (a *Accessor) BrowserOpen(ctx context.Context, request *runtime.BrowserOpenRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserOpen(ctx, runtimesandboxd.BrowserOpenRequest{URL: request.GetUrl()})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "open", err)
	}
	return runtimeBrowserStatus(status), nil
}

func (a *Accessor) BrowserClose(ctx context.Context, request *runtime.BrowserCloseRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserClose(ctx)
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "close", err)
	}
	return runtimeBrowserStatus(status), nil
}

func (a *Accessor) BrowserNavigate(ctx context.Context, request *runtime.BrowserNavigateRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserNavigate(ctx, runtimesandboxd.BrowserNavigateRequest{URL: request.GetUrl()})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "navigate", err)
	}
	return runtimeBrowserStatus(status), nil
}

func (a *Accessor) BrowserResize(ctx context.Context, request *runtime.BrowserResizeRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserResize(ctx, runtimesandboxd.BrowserResizeRequest{Width: int(request.GetWidth()), Height: int(request.GetHeight())})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "resize", err)
	}
	return runtimeBrowserStatus(status), nil
}

func (a *Accessor) BrowserClick(ctx context.Context, request *runtime.BrowserClickRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserClick(ctx, runtimesandboxd.BrowserClickRequest{X: int(request.GetX()), Y: int(request.GetY()), Button: request.GetButton()})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "click", err)
	}
	return runtimeBrowserStatus(status), nil
}

func (a *Accessor) BrowserType(ctx context.Context, request *runtime.BrowserTypeRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserType(ctx, runtimesandboxd.BrowserTypeRequest{Text: request.GetText(), DelayMS: int(request.GetDelayMs())})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "type", err)
	}
	return runtimeBrowserStatus(status), nil
}

func (a *Accessor) BrowserWait(ctx context.Context, request *runtime.BrowserWaitRequest) (*runtime.BrowserStatusResponse, error) {
	client, err := a.ClientForCapability(ctx, request.GetID(), CapabilityBrowser)
	if err != nil {
		return nil, err
	}
	status, err := client.BrowserWait(ctx, runtimesandboxd.BrowserWaitRequest{TimeoutMS: int(request.GetTimeoutMs())})
	if err != nil {
		return nil, a.OperationError(ctx, request.GetID(), CapabilityBrowser, "wait", err)
	}
	return runtimeBrowserStatus(status), nil
}

func runtimeBrowserStatus(status runtimesandboxd.BrowserStatusResponse) *runtime.BrowserStatusResponse {
	return &runtime.BrowserStatusResponse{
		Available: status.Available,
		Command:   status.Command,
		Running:   status.Running,
		Pid:       int32(status.Pid),
		Url:       status.URL,
		Reason:    status.Reason,
	}
}
