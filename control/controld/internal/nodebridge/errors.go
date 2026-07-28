package nodebridge

import (
	"fmt"
	"regexp"
)

var readonlyRootfsMountTargetPattern = regexp.MustCompile(`mount target "([^"]+)" does not exist in readonly rootfs`)

type createAllocationError struct {
	message string
	cause   error
}

func (e *createAllocationError) Error() string {
	return e.message
}

func (e *createAllocationError) Unwrap() error {
	return e.cause
}

func formatCreateAllocationError(err error) error {
	if err == nil {
		return nil
	}
	matches := readonlyRootfsMountTargetPattern.FindStringSubmatch(err.Error())
	if len(matches) != 2 {
		return err
	}
	target := matches[1]
	return &createAllocationError{
		message: fmt.Sprintf("mount target %q does not exist in the readonly image rootfs; use an existing image path or disable readonly rootfs", target),
		cause:   err,
	}
}
