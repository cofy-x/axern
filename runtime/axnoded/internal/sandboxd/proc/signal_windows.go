//go:build windows

package proc

import (
	"fmt"
	"os"
)

func SignalByNumber(value int) (os.Signal, error) {
	switch value {
	case 2:
		return os.Interrupt, nil
	case 9:
		return os.Kill, nil
	default:
		return nil, fmt.Errorf("unsupported signal %d", value)
	}
}

func SignalByName(value string) (os.Signal, bool) {
	switch value {
	case "TERM", "INT":
		return os.Interrupt, true
	case "KILL":
		return os.Kill, true
	default:
		return nil, false
	}
}
