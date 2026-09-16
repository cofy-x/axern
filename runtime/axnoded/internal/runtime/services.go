package runtime

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

type runtimeServices struct {
	file contract.FileService
}

func newRuntimeServices(containerRoot string) runtimeServices {
	return runtimeServices{
		file: runtimesandboxd.NewFileService(containerRoot),
	}
}
