package contract

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

type BackgroundProcesses interface {
	StartBackground(context.Context, wire.BackgroundProcessSpec) (wire.ProcessStatus, error)
	GetBackground(context.Context, string) (wire.ProcessStatus, error)
	ListBackground(context.Context) ([]wire.ProcessStatus, error)
	CancelBackground(context.Context, string) (wire.ProcessStatus, error)
}
