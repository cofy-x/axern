package proc

// InternalExecCommand is dispatched only by the axern-sandboxd entrypoint.
// It is not a process API option or a configurable launcher.
const InternalExecCommand = "internal-unprivileged-exec"
