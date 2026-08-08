package runtime

import (
	"errors"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func runtimeDeleteTargetAbsent(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errord.ErrNotFound) || errord.IsNotFound(errord.FromGRPC(err)) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "does not exist") || strings.Contains(message, "not found")
}
