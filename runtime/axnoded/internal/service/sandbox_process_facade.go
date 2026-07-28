package service

import (
	"context"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/process"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (h *sandboxService) configureProcessController() {
	h.processController = process.NewController(process.Options{
		ExecTarget: h.sandboxTargetResolver().ExecDirect,
	})
}

func (h *sandboxService) Exec(ctx context.Context, request *runtime.ExecRequest) (*runtime.ExecResponse, error) {
	resp, err := h.sandboxProcessController().Exec(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ExecStream(stream ExecStreamServer) error {
	return errord.ToGRPC(h.sandboxProcessController().ExecStream(stream))
}

func (h *sandboxService) Process(stream ProcessStreamServer) error {
	return errord.ToGRPC(h.sandboxProcessController().Process(stream))
}

func (h *sandboxService) sandboxProcessController() *process.Controller {
	if h.processController != nil {
		return h.processController
	}
	h.configureProcessController()
	return h.processController
}
