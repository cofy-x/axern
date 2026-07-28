package runtime

func runscSandboxdArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	out = append(out, "--host-uds=create")
	out = append(out, args...)
	return out
}
