package axernsdk

import (
	"fmt"
)

type Command struct {
	Argv  []string
	Shell string
}

// Shell creates a command executed through /bin/sh -lc.
func Shell(command string) Command {
	return Command{Shell: command}
}

// Args creates a command executed directly without a shell wrapper.
func Args(argv ...string) Command {
	return Command{Argv: append([]string(nil), argv...)}
}

func normalizeCommand(command any) ([]string, error) {
	switch value := command.(type) {
	case string:
		if value == "" {
			return nil, requiredError("command")
		}
		return []string{"/bin/sh", "-lc", value}, nil
	case []string:
		return argvList(value)
	case Command:
		if value.Shell != "" {
			return []string{"/bin/sh", "-lc", value.Shell}, nil
		}
		return argvList(value.Argv)
	default:
		return nil, fmt.Errorf("unsupported command type %T", command)
	}
}

func argvList(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, requiredError("argv")
	}
	return append([]string(nil), argv...), nil
}
