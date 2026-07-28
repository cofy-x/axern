package agent

import (
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

// WrapCommandWithShellPrelude returns a shell command that runs prelude in the
// same execution environment as command, then invokes command.
func WrapCommandWithShellPrelude(prelude string, command sandbox.ExecCommand) sandbox.ExecCommand {
	prelude = strings.TrimSpace(prelude)
	if prelude == "" {
		return command
	}
	prelude = shellPreludeBlock(prelude)
	if shell := strings.TrimSpace(command.Shell()); shell != "" {
		return sandbox.ShellCommand(prelude + "\n" + shell)
	}
	argv := command.Argv()
	if len(argv) == 0 {
		return sandbox.ShellCommand(prelude)
	}
	return sandbox.ShellCommand(prelude + "\nexec " + shellQuoteArgv(argv))
}

func shellPreludeBlock(prelude string) string {
	return "__axern_prelude_shell_options=$(set +o)\n" +
		prelude + "\n" +
		"__axern_prelude_status=$?\n" +
		"eval \"$__axern_prelude_shell_options\"\n" +
		"unset __axern_prelude_shell_options\n" +
		"if [ \"$__axern_prelude_status\" -ne 0 ]; then exit \"$__axern_prelude_status\"; fi\n" +
		"unset __axern_prelude_status"
}

func shellQuoteArgv(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
