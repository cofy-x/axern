package api

import (
	"errors"
	"fmt"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
)

var ErrNydusDisabled = errors.New("Nydus is disabled; image requires Nydus-enabled serving")

func (w *HttpWorker) requireNydusManager() error {
	if w.disableNydus {
		return ErrNydusDisabled
	}
	if w.mgr == nil {
		return fmt.Errorf("imagefsd manager is not initialized")
	}
	return nil
}

func (w *HttpWorker) validateMountType(record *mountstore.Record) error {
	switch MountType(record.MountType) {
	case MountTypeOCI:
		return nil
	case MountTypeNydus:
		if w.disableNydus {
			return ErrNydusDisabled
		}
		return nil
	default:
		return fmt.Errorf("unsupported persisted mount type %q", record.MountType)
	}
}
