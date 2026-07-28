package output

import "io"

func RestoreTerminal(w io.Writer) {
	if w == nil {
		return
	}
	_, _ = io.WriteString(w, "\x1b[0m\x1b[?25h")
}
