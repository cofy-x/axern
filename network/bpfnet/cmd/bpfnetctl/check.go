package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/network/bpfnet/internal/inspect"
)

type checkResult struct {
	OK     bool        `json:"ok"`
	Checks []checkItem `json:"checks"`
}

type checkItem struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func runCheck(args []string, stdout io.Writer) error {
	if hasHelpFlag(args) {
		writeCheckUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pinPath := fs.String("pin-path", bpfnet.DefaultPinPath, "bpfnet bpffs pin path")
	statePath := fs.String("state-path", "", "bpfnet state path")
	jsonOutput := fs.Bool("json", false, "print JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected check arguments: %v", fs.Args())
	}

	cfg := bpfnet.Config{PinPath: *pinPath, StatePath: *statePath}.WithDefaults()
	status, err := bpfnet.NewController(cfg).Status()
	if err != nil {
		return err
	}
	result := evaluateReadiness(status, inspect.ListObjects(cfg.PinPath))
	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			return err
		}
	} else {
		writeCheckResult(stdout, result)
	}
	if !result.OK {
		return errors.New("bpfnet check failed")
	}
	return nil
}

func evaluateReadiness(status bpfnet.Status, objects []inspect.ObjectInfo) checkResult {
	checks := []checkItem{
		checkBool("tc_ready", status.State.TCReady && !status.State.FullFallback, "tc dataplane is not ready or is in full fallback"),
		checkBool("tc_filters", status.Attachment.IngressTCAttached && status.Attachment.EgressTCAttached, "tc ingress/egress filters are not both attached"),
		checkBool("pinned_maps", status.Attachment.PinnedMapsReady, "required pinned maps are not all openable"),
		checkBool("pinned_programs", status.Attachment.PinnedProgramsReady, "required pinned program objects are not all openable"),
	}
	if status.State.LocalOutCompat {
		if status.State.LocalhostCompat {
			checks = append(checks, checkBool("localhost_compat", true, ""))
		} else {
			checks = append(checks,
				checkBool("localhost_path", status.State.LocalhostTCPDNAT && status.State.LocalhostPathReady, "localhost tcp path is not active"),
				checkBool("localhost_links", status.Attachment.LocalhostLinksAttached, "localhost cgroup links are not all openable"),
			)
		}
	}

	for _, obj := range objects {
		switch obj.Kind {
		case "map":
			checks = append(checks, checkObject("map:"+obj.Name, obj))
		case "program":
			checks = append(checks, checkObject("program:"+obj.Name, obj))
		case "link":
			if status.State.LocalhostCompat && isLocalhostLinkObject(obj.Name) {
				continue
			}
			checks = append(checks, checkObject("link:"+obj.Name, obj))
		}
	}

	result := checkResult{OK: true, Checks: checks}
	for _, check := range checks {
		if !check.OK {
			result.OK = false
			break
		}
	}
	return result
}

func isLocalhostLinkObject(name string) bool {
	return strings.HasPrefix(name, "localhost-")
}

func checkBool(name string, ok bool, message string) checkItem {
	if ok {
		return checkItem{Name: name, OK: true}
	}
	return checkItem{Name: name, Message: message}
}

func checkObject(name string, obj inspect.ObjectInfo) checkItem {
	if obj.Present && obj.Openable {
		return checkItem{Name: name, OK: true}
	}
	message := "not present or not openable"
	if obj.Error != "" {
		message = obj.Error
	}
	return checkItem{Name: name, Message: message}
}

func writeCheckResult(w io.Writer, result checkResult) {
	fmt.Fprintln(w, "bpfnet check")
	for _, check := range result.Checks {
		status := "ok"
		if !check.OK {
			status = "fail"
		}
		if check.Message == "" {
			fmt.Fprintf(w, "  %-4s %s\n", status, check.Name)
		} else {
			fmt.Fprintf(w, "  %-4s %s: %s\n", status, check.Name, check.Message)
		}
	}
	if result.OK {
		fmt.Fprintln(w, "result: ok")
	} else {
		fmt.Fprintln(w, "result: fail")
	}
}
