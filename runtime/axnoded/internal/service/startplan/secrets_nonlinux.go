//go:build !linux

package startplan

import (
	"errors"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func materializeSecretStorage(string, *runtime.StartRequest) ([]*runtime.Mount, func() error, error) {
	return nil, nil, errors.New("private node secret storage requires Linux")
}

func cleanupSecretStorage(string, string) error {
	return errors.New("private node secret storage requires Linux")
}
