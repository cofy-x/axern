package sandbox

import "testing"

func TestExecCommandValidateAcceptsShellOrArgv(t *testing.T) {
	for name, command := range map[string]ExecCommand{
		"shell": ShellCommand("printf ok"),
		"argv":  ArgvCommand([]string{"printf", "ok"}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := command.Validate(); err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
		})
	}
}

func TestExecCommandValidateRejectsAmbiguousOrEmptyCommand(t *testing.T) {
	for name, command := range map[string]ExecCommand{
		"empty":      {},
		"ambiguous":  {shell: "printf ok", argv: []string{"printf", "ok"}},
		"empty_argv": ArgvCommand([]string{""}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := command.Validate(); err == nil {
				t.Fatal("Validate error = nil")
			}
		})
	}
}

func TestArgvCommandCopiesInput(t *testing.T) {
	argv := []string{"printf", "ok"}
	command := ArgvCommand(argv)
	argv[0] = "mutated"
	if command.Argv()[0] != "printf" {
		t.Fatalf("argv = %#v", command.Argv())
	}
}
