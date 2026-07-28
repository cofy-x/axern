package storage

import (
	"fmt"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

func validateRuntimeCompatibility(runtimeClass string, compat *storagev1.VolumeRuntimeCompatibility) error {
	runtimeClass = strings.ToLower(strings.TrimSpace(runtimeClass))
	if runtimeClass == "" {
		return fmt.Errorf("runtime class is required")
	}
	if compat == nil {
		return fmt.Errorf("volume runtime compatibility is required")
	}
	switch runtimeClass {
	case "runc":
		if !compat.GetSupportsRunc() {
			return fmt.Errorf("volume does not support runtime class %q", runtimeClass)
		}
	case "runsc":
		if !compat.GetSupportsRunsc() {
			return fmt.Errorf("volume does not support runtime class %q", runtimeClass)
		}
	default:
		return fmt.Errorf("runtime class %q is not supported by volumed", runtimeClass)
	}
	return nil
}
