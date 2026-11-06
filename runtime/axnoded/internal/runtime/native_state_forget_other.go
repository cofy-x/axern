//go:build !linux

package runtime

import (
	"context"
	"errors"
)

func forgetNativeRuntimeState(context.Context, string, string, string, func(int) error) error {
	return errors.New("native runtime state retirement requires Linux")
}

func nativeRuntimeStateAbsent(context.Context, string, string) (bool, error) {
	return false, errors.New("native runtime state absence verification requires Linux")
}
