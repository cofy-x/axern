package runtime

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/processservice"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

type runtimeServices struct {
	file    contract.FileService
	process contract.ProcessService
}

func newRuntimeServices(containerRoot string, openExecSession processservice.OpenSessionFunc) runtimeServices {
	return runtimeServices{
		file:    runtimesandboxd.NewFileService(containerRoot),
		process: processservice.New(openExecSession),
	}
}
