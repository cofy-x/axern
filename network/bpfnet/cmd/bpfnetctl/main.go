package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/network/bpfnet/internal/inspect"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		writeUsage(stderr)
		return nil
	}

	switch args[0] {
	case "status":
		return runStatus(args[1:], stdout)
	case "maps":
		return runMaps(args[1:], stdout)
	case "dump":
		return runDump(args[1:], stdout)
	case "check":
		return runCheck(args[1:], stdout)
	case "help":
		if len(args) > 1 {
			return writeCommandUsage(stdout, args[1])
		}
		writeUsage(stdout)
		return nil
	case "-h", "--help":
		writeUsage(stdout)
		return nil
	default:
		writeUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runStatus(args []string, stdout io.Writer) error {
	if hasHelpFlag(args) {
		writeStatusUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { writeStatusUsage(stdout) }
	pinPath := fs.String("pin-path", bpfnet.DefaultPinPath, "bpfnet bpffs pin path")
	statePath := fs.String("state-path", "", "bpfnet state path")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg := bpfnet.Config{PinPath: *pinPath, StatePath: *statePath}.WithDefaults()
	status, err := bpfnet.NewController(cfg).Status()
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(stdout, status)
	}
	return writeStatus(stdout, status)
}

func runMaps(args []string, stdout io.Writer) error {
	if hasHelpFlag(args) {
		writeMapsUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("maps", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { writeMapsUsage(stdout) }
	pinPath := fs.String("pin-path", bpfnet.DefaultPinPath, "bpfnet bpffs pin path")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	objects := inspect.ListObjects(*pinPath)
	if *jsonOutput {
		return writeJSON(stdout, objects)
	}
	return writeObjects(stdout, objects)
}

func runDump(args []string, stdout io.Writer) error {
	if hasHelpFlag(args) {
		writeDumpUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { writeDumpUsage(stdout) }
	pinPath := fs.String("pin-path", bpfnet.DefaultPinPath, "bpfnet bpffs pin path")
	limit := fs.Int("limit", 100, "maximum entries to print")
	raw := fs.Bool("raw", false, "dump raw hex keys and values")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("missing map name")
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("unexpected dump arguments: %v", fs.Args()[1:])
	}
	mapName := inspect.NormalizeMapName(fs.Arg(0))

	dump, err := inspect.DumpMap(*pinPath, mapName, *limit, *raw)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(stdout, dump)
	}
	return writeDump(stdout, dump)
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: bpfnetctl <status|maps|dump|check> [options]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  status   show controller, dataplane, attachment, service, and counter status")
	fmt.Fprintln(w, "  maps     list pinned maps, programs, and localhost links")
	fmt.Fprintln(w, "  dump     dump a supported pinned map")
	fmt.Fprintln(w, "  check    validate the local bpfnet dataplane readiness")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run \"bpfnetctl help <command>\" for command options")
}

func writeCommandUsage(w io.Writer, command string) error {
	switch command {
	case "status":
		writeStatusUsage(w)
	case "maps":
		writeMapsUsage(w)
	case "dump":
		writeDumpUsage(w)
	case "check":
		writeCheckUsage(w)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	return nil
}

func writeStatusUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: bpfnetctl status [--pin-path PATH] [--state-path PATH] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "options:")
	fmt.Fprintf(w, "  --pin-path PATH    bpffs pin path (default %s)\n", bpfnet.DefaultPinPath)
	fmt.Fprintln(w, "  --state-path PATH  bpfnet state path (defaults from pin path)")
	fmt.Fprintln(w, "  --json             print the bpfnet.Status JSON")
}

func writeMapsUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: bpfnetctl maps [--pin-path PATH] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "options:")
	fmt.Fprintf(w, "  --pin-path PATH  bpffs pin path (default %s)\n", bpfnet.DefaultPinPath)
	fmt.Fprintln(w, "  --json           print JSON")
}

func writeDumpUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: bpfnetctl dump [--pin-path PATH] [--limit N] [--raw] [--json] <map-name>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "options:")
	fmt.Fprintf(w, "  --pin-path PATH  bpffs pin path (default %s)\n", bpfnet.DefaultPinPath)
	fmt.Fprintln(w, "  --limit N        maximum entries to print (default 100)")
	fmt.Fprintln(w, "  --raw            dump raw hex keys and values")
	fmt.Fprintln(w, "  --json           print JSON")
}

func writeCheckUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: bpfnetctl check [--pin-path PATH] [--state-path PATH] [--json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "options:")
	fmt.Fprintf(w, "  --pin-path PATH    bpffs pin path (default %s)\n", bpfnet.DefaultPinPath)
	fmt.Fprintln(w, "  --state-path PATH  bpfnet state path (defaults from pin path)")
	fmt.Fprintln(w, "  --json             print JSON")
}
