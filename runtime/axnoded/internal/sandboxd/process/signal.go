package process

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func ParseSignal(value string) (os.Signal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		sig, _ := proc.SignalByName("TERM")
		return sig, nil
	}
	if n, err := strconv.Atoi(value); err == nil {
		return proc.SignalByNumber(n)
	}
	value = strings.ToUpper(strings.TrimPrefix(value, "SIG"))
	sig, ok := proc.SignalByName(value)
	if !ok {
		return nil, fmt.Errorf("unsupported signal %q", value)
	}
	return sig, nil
}
